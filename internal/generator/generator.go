package generator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// Generate 是 protoc 插件的主入口，负责解析参数、构建 manifest 并写出生成文件。
func Generate(plugin *protogen.Plugin, version string) error {
	// 先解析 protoc/buf 通过 --gateway-manifest_opt 传入的参数。
	options, err := ParseOptions(plugin.Request.GetParameter())
	// 参数错误必须立即返回，避免生成一个范围不符合预期的 manifest。
	if err != nil {
		return err
	}
	// Build 负责把 descriptor 里的 service/method/http annotation 转换为 Manifest。
	manifest, err := Build(plugin, options, version)
	// descriptor 或规则校验失败时直接中止本次代码生成。
	if err != nil {
		return err
	}
	// MarshalManifest 统一 JSON 缩进和结尾换行，避免不同调用路径输出不一致。
	content, err := MarshalManifest(manifest)
	// JSON 序列化失败属于结构定义或字段值异常，需要向 protoc 返回错误。
	if err != nil {
		return err
	}
	// NewGeneratedFile 声明本插件要写出的目标 manifest 文件。
	generated := plugin.NewGeneratedFile(options.OutFile, "")
	// protogen.P 会自动追加换行，所以这里先去掉 MarshalManifest 的末尾换行。
	generated.P(strings.TrimSuffix(string(content), "\n"))
	// 返回 nil 表示插件生成成功，protogen 会把文件内容写入 CodeGeneratorResponse。
	return nil
}

// Build 把 protogen.Plugin 中的 descriptor 事实转换成稳定的 manifest 结构。
func Build(plugin *protogen.Plugin, options Options, version string) (Manifest, error) {
	// 只处理 protoc/buf 明确要求本插件生成的文件，避免把依赖 proto 全量扫入结果。
	files := generatedFiles(plugin)
	// 初始化 manifest 顶层结构，所有切片字段显式使用空数组语义而不是 nil。
	manifest := Manifest{
		// Schema 是控制面识别当前文件类型的固定标识。
		Schema: ManifestSchema,
		// Version 是 manifest JSON 结构版本。
		Version: ManifestVersion,
		// GeneratedAt 来自参数或默认 UTC 时间。
		GeneratedAt: options.GeneratedAt,
		// Generator 写入插件名称和版本，便于排查不同插件版本产物差异。
		Generator: GeneratorInfo{
			// Name 固定为当前插件二进制名称。
			Name: "protoc-gen-gateway-manifest",
			// Version 由 main 注入，后续发布时只需要修改入口版本号。
			Version: version,
		},
		// Source 记录本次生成关联的 proto 来源信息。
		Source: SourceInfo{
			// Module 通常对应 buf.build 上的模块名。
			Module: options.Module,
			// ModuleRef 通常对应分支、tag 或 commit。
			ModuleRef: options.ModuleRef,
			// Files 先设为空数组，遍历服务后再填入实际命中的 proto 文件。
			Files: []string{},
		},
		// Filter 把 include/exclude 参数写回产物，方便上线后审计过滤行为。
		Filter: options.filterInfo(),
		// Descriptor 记录 api-gateway/Envoy 加载 transcoder descriptor 所需的引用。
		Descriptor: DescriptorRef{
			// Ref 是 descriptor set 的发布或存储引用。
			Ref: options.DescriptorRef,
			// SHA256 是 descriptor set 的可选校验值。
			SHA256: options.DescriptorSHA256,
		},
		// Services 保留 service/method 视图，供 sidecar-agent 注册时使用。
		Services: []Service{},
		// Routes 保留扁平 HTTP 路由视图，供 api-gateway 快速装载。
		Routes: []Route{},
	}

	// sourceFiles 用 set 收集真正进入 manifest 的 proto 文件，避免重复。
	sourceFiles := make(map[string]struct{})
	// routeKeys 用 method+path 检测重复 HTTP 路由，防止运行时出现不可预测匹配。
	routeKeys := make(map[string]string)
	// 按稳定顺序遍历待生成文件，保证每次生成结果可重复。
	for _, file := range files {
		// protoPackage 是 include/exclude 按 package 过滤时的主输入。
		protoPackage := string(file.Desc.Package())
		// service 也按稳定顺序遍历，避免 descriptor 原始顺序导致 diff 抖动。
		for _, service := range sortedServices(file.Services) {
			// serviceFullName 使用 package.service 形式，和 gRPC full method 保持一致。
			serviceFullName := fullServiceName(protoPackage, service)
			// 先应用 include/exclude，防止依赖服务被误写入业务服务 manifest。
			if !options.allowsService(protoPackage, serviceFullName) {
				continue
			}
			// 将单个 service 转成 manifest service 条目和它包含的 HTTP routes。
			entry, routes, err := buildService(file, service, serviceFullName, options)
			// 任一 method 的 http annotation 不合法时，整个生成都应该失败。
			if err != nil {
				return Manifest{}, err
			}
			// 如果 service 没有任何需要暴露或记录的方法，就不写入 manifest。
			if len(entry.Methods) == 0 {
				continue
			}
			// 追加扁平路由，同时按配置检测重复 method+path。
			if err := appendRoutes(&manifest.Routes, routeKeys, routes, options.FailOnDuplicateRoute); err != nil {
				return Manifest{}, err
			}
			// service 条目通过过滤且存在 method 后才写入顶层 services。
			manifest.Services = append(manifest.Services, entry)
			// 只记录实际命中的 proto 文件，帮助消费方判断 descriptor 与 manifest 是否匹配。
			sourceFiles[string(file.Desc.Path())] = struct{}{}
		}
	}

	// 将 proto 文件 set 转成稳定排序数组。
	manifest.Source.Files = sortedMapKeys(sourceFiles)
	// 再次排序 services，确保不同文件遍历顺序也不会影响最终输出。
	sortServices(manifest.Services)
	// 每个 service 内部 methods 由 buildService 排序，这里只排序顶层 routes。
	sortRoutes(manifest.Routes)
	// 返回完整 manifest，调用方负责序列化或断言。
	return manifest, nil
}

// MarshalManifest 使用固定缩进输出 JSON，保证 CI diff 和 golden test 稳定。
func MarshalManifest(manifest Manifest) ([]byte, error) {
	// MarshalIndent 使用两个空格缩进，便于人工 review 生成结果。
	content, err := json.MarshalIndent(manifest, "", "  ")
	// 序列化失败时包装上下文，方便定位是 manifest 阶段的问题。
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	// 追加末尾换行，让生成文件符合常见文本文件约定。
	return append(content, '\n'), nil
}

// generatedFiles 返回 protoc/buf 明确要求当前插件生成的文件。
func generatedFiles(plugin *protogen.Plugin) []*protogen.File {
	// 预分配容量为所有输入文件数量，减少追加过程中的切片扩容。
	files := make([]*protogen.File, 0, len(plugin.Files))
	// plugin.Files 同时包含待生成文件和依赖文件，必须根据 Generate 标记过滤。
	for _, file := range plugin.Files {
		// file.Generate 为 true 表示该文件在 CodeGeneratorRequest.FileToGenerate 中。
		if file.Generate {
			files = append(files, file)
		}
	}
	// 按 proto path 排序，保证生成结果不依赖 protoc 传入文件顺序。
	sort.Slice(files, func(i, j int) bool {
		return files[i].Desc.Path() < files[j].Desc.Path()
	})
	// 返回稳定排序后的待生成文件列表。
	return files
}

// buildService 构建单个 service 的聚合视图和扁平 route 视图。
func buildService(file *protogen.File, service *protogen.Service, serviceFullName string, options Options) (Service, []Route, error) {
	// entry 是最终写入 manifest.services 的 service 条目。
	entry := Service{
		// Name 使用完整 service 名称，避免不同 package 下 service 同名冲突。
		Name: serviceFullName,
		// ProtoPackage 保留 package 便于消费方按业务域聚合。
		ProtoPackage: string(file.Desc.Package()),
		// ProtoFile 保留定义文件路径，便于排查 descriptor 与 manifest 对齐问题。
		ProtoFile: string(file.Desc.Path()),
		// Methods 显式初始化为空数组，避免 JSON 输出 null。
		Methods: []Method{},
	}
	// routes 收集当前 service 内所有带 google.api.http 的 method 映射。
	var routes []Route
	// 按 method 名称排序后遍历，保证同一 service 输出稳定。
	for _, method := range sortedMethods(service.Methods) {
		// 从 method options 读取 google.api.http 及 additional_bindings。
		httpRules, err := httpRulesForMethod(serviceFullName, method)
		// annotation 类型异常或 path/method 不合法时直接失败。
		if err != nil {
			return Service{}, nil, err
		}
		// 默认只输出带 HTTP annotation 的方法；未标注方法表示不允许 HTTP 访问。
		if len(httpRules) == 0 && !options.IncludeUnannotatedMethods {
			continue
		}
		// 构建 method 条目；未标注 HTTP 的 method 会带空 http_rules。
		methodEntry := buildMethod(serviceFullName, method, httpRules)
		// 追加到 service 聚合视图。
		entry.Methods = append(entry.Methods, methodEntry)
		// 每条 HTTP binding 都会展开为 api-gateway 可直接消费的一条 route。
		for _, httpRule := range httpRules {
			routes = append(routes, buildRoute(entry, methodEntry, httpRule))
		}
	}
	// method 输出按名称排序，避免 additional 逻辑或 descriptor 顺序造成抖动。
	sortMethods(entry.Methods)
	// 返回 service 条目和对应扁平路由。
	return entry, routes, nil
}

// buildMethod 将 protogen.Method 转成 manifest Method。
func buildMethod(serviceFullName string, method *protogen.Method, httpRules []HTTPRule) Method {
	// methodName 是短名称，例如 Login。
	methodName := string(method.Desc.Name())
	// 返回完整 method 元数据，供 sidecar-agent 或 api-gateway 做反查和注册。
	return Method{
		// Name 保留短名称，便于和 gRPC service 内 method 对齐。
		Name: methodName,
		// FullMethod 使用 gRPC 标准 /package.Service/Method 形式。
		FullMethod: "/" + serviceFullName + "/" + methodName,
		// InputType 写入请求消息完整类型名。
		InputType: string(method.Desc.Input().FullName()),
		// OutputType 写入响应消息完整类型名。
		OutputType: string(method.Desc.Output().FullName()),
		// HTTPRules 克隆一份，避免调用方后续修改切片影响 manifest。
		HTTPRules: cloneHTTPRules(httpRules),
	}
}

// buildRoute 将单条 HTTPRule 展开成 api-gateway 可直接装载的 Route。
func buildRoute(service Service, method Method, httpRule HTTPRule) Route {
	// Route 是 HTTP 维度的扁平视图，运行时无需再遍历 service/method 树。
	return Route{
		// ID 使用 HTTPRule 中已经生成的稳定 binding ID。
		ID: httpRule.ID,
		// Protocol 当前固定为 grpc，表示目标是 gRPC 业务服务。
		Protocol: "grpc",
		// HTTPMethod 是网关匹配请求 method 的主键之一。
		HTTPMethod: httpRule.Method,
		// Path 是网关匹配请求 path template 的主键之一。
		Path: httpRule.Path,
		// Body 原样传递 google.api.http body 规则。
		Body: httpRule.Body,
		// ResponseBody 原样传递 google.api.http response_body 规则。
		ResponseBody: httpRule.ResponseBody,
		// GRPCService 是最终转发的目标 service。
		GRPCService: service.Name,
		// GRPCMethod 是最终转发的目标 method。
		GRPCMethod: method.Name,
		// FullMethod 是标准 gRPC 调用路径，便于运行时直接路由或打日志。
		FullMethod: method.FullMethod,
		// Transcoding 表示这条路由需要 gRPC-JSON transcoder。
		Transcoding: httpRule.Transcoding,
		// ProtoFile 记录来源文件，方便定位 annotation 定义位置。
		ProtoFile: service.ProtoFile,
	}
}

// appendRoutes 追加路由并按 HTTP method + path 检测重复项。
func appendRoutes(target *[]Route, routeKeys map[string]string, routes []Route, failOnDuplicate bool) error {
	// 逐条处理 route，便于在发现重复时报告具体冲突 ID。
	for _, route := range routes {
		// HTTP method + path 是网关入口匹配的天然唯一键。
		key := route.HTTPMethod + " " + route.Path
		// 如果已经存在相同 key 且启用严格模式，就阻止生成不确定路由表。
		if previousID, exists := routeKeys[key]; exists && failOnDuplicate {
			return fmt.Errorf("duplicate http route %s used by %q and %q", key, previousID, route.ID)
		}
		// 记录当前 key 的来源 ID；非严格模式下后写入 ID 也可用于审计最后一次来源。
		routeKeys[key] = route.ID
		// 将 route 追加到目标切片。
		*target = append(*target, route)
	}
	// 所有路由追加完成且未发现阻断性冲突。
	return nil
}

// fullServiceName 生成完整 service 名称。
func fullServiceName(protoPackage string, service *protogen.Service) string {
	// 没有 package 的 proto 直接使用 service 名称。
	if protoPackage == "" {
		return string(service.Desc.Name())
	}
	// 常规 proto 使用 package.service 形式。
	return protoPackage + "." + string(service.Desc.Name())
}

// sortedServices 返回按完整 service 名称排序的副本。
func sortedServices(services []*protogen.Service) []*protogen.Service {
	// 复制切片，避免修改 protogen 提供的原始 service 顺序。
	result := append([]*protogen.Service(nil), services...)
	// 用 descriptor full name 排序，可以稳定处理同名 service 位于不同 package 的情况。
	sort.Slice(result, func(i, j int) bool {
		return string(result[i].Desc.FullName()) < string(result[j].Desc.FullName())
	})
	// 返回排序后的副本。
	return result
}

// sortedMethods 返回按 method 名称排序的副本。
func sortedMethods(methods []*protogen.Method) []*protogen.Method {
	// 复制切片，避免改变 protogen 中原始 method 顺序。
	result := append([]*protogen.Method(nil), methods...)
	// method 名称在同一个 service 内唯一，适合作为稳定排序键。
	sort.Slice(result, func(i, j int) bool {
		return string(result[i].Desc.Name()) < string(result[j].Desc.Name())
	})
	// 返回排序后的副本。
	return result
}

// sortServices 原地按 service 名称排序 manifest service 列表。
func sortServices(services []Service) {
	// service.Name 是完整名称，因此跨 package 排序也稳定。
	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})
}

// sortMethods 原地按 method 名称排序 manifest method 列表。
func sortMethods(methods []Method) {
	// 同一 service 内 method.Name 唯一，排序结果稳定且易读。
	sort.Slice(methods, func(i, j int) bool {
		return methods[i].Name < methods[j].Name
	})
}

// sortRoutes 原地按来源文件、service、method、binding ID 排序扁平路由。
func sortRoutes(routes []Route) {
	// 组合排序 key 可以让 route 输出同时兼顾来源定位和稳定 diff。
	sort.Slice(routes, func(i, j int) bool {
		// 使用 NUL 分隔字段，避免普通字符分隔导致 key 拼接歧义。
		left := strings.Join([]string{routes[i].ProtoFile, routes[i].GRPCService, routes[i].GRPCMethod, routes[i].ID}, "\x00")
		// right 与 left 使用相同字段顺序，确保比较语义一致。
		right := strings.Join([]string{routes[j].ProtoFile, routes[j].GRPCService, routes[j].GRPCMethod, routes[j].ID}, "\x00")
		// 字典序比较组合 key。
		return left < right
	})
}

// sortedMapKeys 将 set 风格 map 转换为稳定排序数组。
func sortedMapKeys(values map[string]struct{}) []string {
	// 预分配 key 数量，减少 append 扩容。
	keys := make([]string, 0, len(values))
	// Go map 遍历顺序随机，所以必须先取出 key 再排序。
	for key := range values {
		keys = append(keys, key)
	}
	// 字符串升序排序保证 JSON 输出稳定。
	sort.Strings(keys)
	// 返回稳定排序后的 key 列表。
	return keys
}

// cloneHTTPRules 克隆 HTTP 规则切片并稳定空数组语义。
func cloneHTTPRules(values []HTTPRule) []HTTPRule {
	// 未标注 google.api.http 的 gRPC-only method 需要稳定输出 http_rules: []。
	if len(values) == 0 {
		return []HTTPRule{}
	}
	// 返回浅拷贝，避免外部继续 append 时影响 Method.HTTPRules。
	return append([]HTTPRule{}, values...)
}
