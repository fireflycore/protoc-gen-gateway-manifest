package generator

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Options 保存 protoc/buf 传给插件的所有参数。
type Options struct {
	// OutFile 是生成的 manifest 文件名。
	OutFile string
	// Module 是 proto 来源模块名，通常对应 buf.build/lhdht/grpc。
	Module string
	// ModuleRef 是 proto 来源版本，通常对应分支、tag 或 commit。
	ModuleRef string
	// DescriptorRef 是运行时可加载的 descriptor set 引用。
	DescriptorRef string
	// DescriptorSHA256 是 descriptor set 的可选完整性校验值。
	DescriptorSHA256 string
	// GeneratedAt 是 manifest 生成时间，测试可传固定值保证 golden 稳定。
	GeneratedAt string
	// IncludePackages 是允许生成的精确 proto package 列表。
	IncludePackages []string
	// IncludePackagePrefixes 是允许生成的 proto package 前缀列表。
	IncludePackagePrefixes []string
	// IncludeServices 是允许生成的完整 service 名称列表。
	IncludeServices []string
	// ExcludePackages 是需要排除的精确 proto package 列表。
	ExcludePackages []string
	// ExcludePackagePrefixes 是需要排除的 proto package 前缀列表。
	ExcludePackagePrefixes []string
	// ExcludeServices 是需要排除的完整 service 名称列表。
	ExcludeServices []string
	// RequireInclude 控制是否强制提供 include 条件，生产环境建议开启。
	RequireInclude bool
	// IncludeUnannotatedMethods 控制是否把没有 google.api.http 的 gRPC-only 方法写入 services.methods。
	IncludeUnannotatedMethods bool
	// FailOnDuplicateRoute 控制 HTTP method+path 重复时是否直接失败。
	FailOnDuplicateRoute bool
	// EmitYAML 是预留开关；当前版本只支持 JSON，因此解析到 true 会报错。
	EmitYAML bool
}

// ParseOptions 解析标准 protoc plugin parameter。
func ParseOptions(parameter string) (Options, error) {
	// 从默认值开始叠加用户传入参数。
	options := defaultOptions()
	// protoc 可能传入空白字符串，这里先统一裁剪。
	parameter = strings.TrimSpace(parameter)
	// 没有任何参数时直接返回默认配置。
	if parameter == "" {
		return options, nil
	}

	// lastListKey 记录最近一个列表型参数，用于兼容 include_package=a,b 这种写法。
	var lastListKey string
	// protoc plugin parameter 顶层以逗号分隔。
	for _, token := range strings.Split(parameter, ",") {
		// 每个 token 都先去除首尾空白，避免配置格式细节影响解析。
		token = strings.TrimSpace(token)
		// 跳过连续逗号或末尾逗号产生的空 token。
		if token == "" {
			continue
		}
		// 标准参数形态是 key=value。
		if key, value, ok := strings.Cut(token, "="); ok {
			// key 去空白后用于匹配支持的插件参数名。
			key = strings.TrimSpace(key)
			// value 去空白后再按参数类型解析。
			value = strings.TrimSpace(value)
			// 将单个参数应用到 Options。
			if err := options.apply(key, value); err != nil {
				return Options{}, err
			}
			// 列表参数后面的裸 token 会被视为当前列表的延续值。
			if isListOption(key) {
				lastListKey = key
			} else {
				// 非列表参数不能接收后续裸 token。
				lastListKey = ""
			}
			// 当前 key=value 已处理完成，进入下一个 token。
			continue
		}
		// Buf/protoc 的参数天然以逗号分隔；这里把 include_package=a,b 中的 b 视为上一列表参数的延续。
		if lastListKey == "" {
			return Options{}, fmt.Errorf("invalid option token %q", token)
		}
		// 将裸 token 应用到上一项列表参数。
		if err := options.apply(lastListKey, token); err != nil {
			return Options{}, err
		}
	}

	// normalize 统一默认值、去重、排序和空数组语义。
	options.normalize()
	// YAML 输出还没有实现，显式报错比静默忽略更安全。
	if options.EmitYAML {
		return Options{}, fmt.Errorf("emit_yaml is not implemented yet")
	}
	// 生产模式下必须显式 include，防止 auth 服务依赖的 user/config/secure 等服务误进入 manifest。
	if options.RequireInclude && !options.hasInclude() {
		return Options{}, fmt.Errorf("require_include=true requires include_package, include_package_prefix or include_service")
	}
	// 返回解析并规范化后的选项。
	return options, nil
}

// defaultOptions 返回插件默认配置。
func defaultOptions() Options {
	// 默认输出 JSON manifest，且默认拒绝重复 HTTP 路由。
	return Options{
		// OutFile 是 buf/protoc 未显式指定时的生成文件名。
		OutFile: "gateway.manifest.json",
		// FailOnDuplicateRoute 默认 true，避免上线后路由冲突才暴露。
		FailOnDuplicateRoute: true,
	}
}

// apply 将单个 key=value 参数写入 Options。
func (o *Options) apply(key, value string) error {
	// 根据参数名分派到对应字段。
	switch key {
	case "out_file":
		// out_file 指定生成文件路径。
		o.OutFile = value
	case "module":
		// module 记录 proto 来源模块。
		o.Module = value
	case "module_ref":
		// module_ref 记录 proto 来源版本。
		o.ModuleRef = value
	case "descriptor_ref":
		// descriptor_ref 记录运行时 descriptor set 引用。
		o.DescriptorRef = value
	case "descriptor_sha256":
		// descriptor_sha256 记录 descriptor set 校验值。
		o.DescriptorSHA256 = value
	case "generated_at":
		// generated_at 允许调用方固定生成时间，主要用于测试和可重复构建。
		o.GeneratedAt = value
	case "include_package":
		// include_package 支持单个值，也支持 ; 或 | 分隔多个值。
		o.IncludePackages = append(o.IncludePackages, splitOptionValues(value)...)
	case "include_package_prefix":
		// include_package_prefix 用于一次包含某个业务域下的多个 package。
		o.IncludePackagePrefixes = append(o.IncludePackagePrefixes, splitOptionValues(value)...)
	case "include_service":
		// include_service 精确包含完整 service 名称。
		o.IncludeServices = append(o.IncludeServices, splitOptionValues(value)...)
	case "exclude_package":
		// exclude_package 精确排除某个 proto package。
		o.ExcludePackages = append(o.ExcludePackages, splitOptionValues(value)...)
	case "exclude_package_prefix":
		// exclude_package_prefix 排除一组依赖或内部 package。
		o.ExcludePackagePrefixes = append(o.ExcludePackagePrefixes, splitOptionValues(value)...)
	case "exclude_service":
		// exclude_service 精确排除某个完整 service 名称。
		o.ExcludeServices = append(o.ExcludeServices, splitOptionValues(value)...)
	case "require_include":
		// require_include 是布尔参数，需要使用 strconv 的标准解析语义。
		parsed, err := parseBoolOption(key, value)
		// 布尔解析失败时返回带参数名的错误。
		if err != nil {
			return err
		}
		// 保存解析后的布尔值。
		o.RequireInclude = parsed
	case "include_unannotated_methods":
		// include_unannotated_methods 控制是否记录 gRPC-only method。
		parsed, err := parseBoolOption(key, value)
		// 布尔解析失败时返回错误。
		if err != nil {
			return err
		}
		// 保存解析后的布尔值。
		o.IncludeUnannotatedMethods = parsed
	case "fail_on_duplicate_route":
		// fail_on_duplicate_route 控制重复 HTTP 路由是否阻断生成。
		parsed, err := parseBoolOption(key, value)
		// 布尔解析失败时返回错误。
		if err != nil {
			return err
		}
		// 保存解析后的布尔值。
		o.FailOnDuplicateRoute = parsed
	case "emit_yaml":
		// emit_yaml 是预留布尔开关，ParseOptions 会在 true 时统一报未实现。
		parsed, err := parseBoolOption(key, value)
		// 布尔解析失败时返回错误。
		if err != nil {
			return err
		}
		// 保存解析后的布尔值。
		o.EmitYAML = parsed
	case "paths":
		// paths 是 protoc-gen-go 常见参数；本插件不生成 Go 文件，保留 no-op 兼容可减少接入噪音。
		return nil
	default:
		// 未知参数直接失败，防止配置拼写错误被静默忽略。
		return fmt.Errorf("unknown option %q", key)
	}
	// 当前参数应用成功。
	return nil
}

// normalize 规范化 Options 字段，保证输出稳定。
func (o *Options) normalize() {
	// out_file 传空时回退默认文件名。
	if strings.TrimSpace(o.OutFile) == "" {
		o.OutFile = "gateway.manifest.json"
	}
	// module 去除首尾空白。
	o.Module = strings.TrimSpace(o.Module)
	// module_ref 去除首尾空白。
	o.ModuleRef = strings.TrimSpace(o.ModuleRef)
	// descriptor_ref 去除首尾空白。
	o.DescriptorRef = strings.TrimSpace(o.DescriptorRef)
	// descriptor_sha256 去除首尾空白。
	o.DescriptorSHA256 = strings.TrimSpace(o.DescriptorSHA256)
	// generated_at 去除首尾空白。
	o.GeneratedAt = strings.TrimSpace(o.GeneratedAt)
	// 如果调用方没传 generated_at，就使用当前 UTC 时间。
	if o.GeneratedAt == "" {
		// 默认写入 UTC 时间，保证离线 manifest 也能被审计；测试可通过 generated_at 固定输出。
		o.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}
	// include package 列表去空白、去重并排序。
	o.IncludePackages = uniqueSorted(o.IncludePackages)
	// include package prefix 列表去空白、去重并排序。
	o.IncludePackagePrefixes = uniqueSorted(o.IncludePackagePrefixes)
	// include service 列表去空白、去重并排序。
	o.IncludeServices = uniqueSorted(o.IncludeServices)
	// exclude package 列表去空白、去重并排序。
	o.ExcludePackages = uniqueSorted(o.ExcludePackages)
	// exclude package prefix 列表去空白、去重并排序。
	o.ExcludePackagePrefixes = uniqueSorted(o.ExcludePackagePrefixes)
	// exclude service 列表去空白、去重并排序。
	o.ExcludeServices = uniqueSorted(o.ExcludeServices)
}

// hasInclude 判断调用方是否提供了任意 include 条件。
func (o Options) hasInclude() bool {
	// 三类 include 任意一个非空，都表示调用方显式声明了生成范围。
	return len(o.IncludePackages) > 0 || len(o.IncludePackagePrefixes) > 0 || len(o.IncludeServices) > 0
}

// filterInfo 将 Options 中的过滤规则转换为 manifest 可序列化结构。
func (o Options) filterInfo() FilterInfo {
	// 克隆切片可以避免 manifest 持有 Options 内部切片引用。
	return FilterInfo{
		// IncludePackages 记录精确 package include 规则。
		IncludePackages: cloneStrings(o.IncludePackages),
		// IncludePackagePrefixes 记录 package 前缀 include 规则。
		IncludePackagePrefixes: cloneStrings(o.IncludePackagePrefixes),
		// IncludeServices 记录完整 service include 规则。
		IncludeServices: cloneStrings(o.IncludeServices),
		// ExcludePackages 记录精确 package exclude 规则。
		ExcludePackages: cloneStrings(o.ExcludePackages),
		// ExcludePackagePrefixes 记录 package 前缀 exclude 规则。
		ExcludePackagePrefixes: cloneStrings(o.ExcludePackagePrefixes),
		// ExcludeServices 记录完整 service exclude 规则。
		ExcludeServices: cloneStrings(o.ExcludeServices),
		// RequireInclude 记录本次是否启用了显式 include 强约束。
		RequireInclude: o.RequireInclude,
	}
}

// isListOption 判断参数是否可以接收多个值。
func isListOption(key string) bool {
	// 只有 include/exclude 相关参数属于列表参数。
	switch key {
	case "include_package", "include_package_prefix", "include_service", "exclude_package", "exclude_package_prefix", "exclude_service":
		// 返回 true 表示后续裸 token 可以继续追加到该列表。
		return true
	default:
		// 其它参数都是标量参数。
		return false
	}
}

// splitOptionValues 拆分单个列表参数值。
func splitOptionValues(value string) []string {
	// values 收集清理后的非空值。
	var values []string
	// 这里支持 ; 和 |，逗号由 ParseOptions 顶层分隔逻辑兼容。
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == '|'
	}) {
		// 每个列表值都裁剪空白。
		part = strings.TrimSpace(part)
		// 空值不进入最终列表。
		if part != "" {
			values = append(values, part)
		}
	}
	// 返回拆分后的值列表。
	return values
}

// parseBoolOption 解析布尔参数并补充参数名上下文。
func parseBoolOption(key, value string) (bool, error) {
	// strconv.ParseBool 支持 true/false/1/0/t/f 等 Go 标准布尔写法。
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	// 解析失败时包装 key 和原始 value，便于用户定位错误配置。
	if err != nil {
		return false, fmt.Errorf("parse %s=%q: %w", key, value, err)
	}
	// 返回解析后的布尔值。
	return parsed, nil
}

// uniqueSorted 对字符串列表做去空白、去重和排序。
func uniqueSorted(values []string) []string {
	// 空输入统一返回空数组，避免 JSON 中出现 null。
	if len(values) == 0 {
		return []string{}
	}
	// seen 用作去重集合。
	seen := make(map[string]struct{}, len(values))
	// 遍历原始值并清理空白。
	for _, value := range values {
		// 裁剪每个值的首尾空白。
		value = strings.TrimSpace(value)
		// 空字符串不参与过滤匹配。
		if value == "" {
			continue
		}
		// map value 使用空结构体，表示只关心 key 是否存在。
		seen[value] = struct{}{}
	}
	// result 预分配为去重后的大小。
	result := make([]string, 0, len(seen))
	// map 遍历顺序随机，因此先收集再排序。
	for value := range seen {
		result = append(result, value)
	}
	// 字符串升序排序保证 manifest.filter 输出稳定。
	sort.Strings(result)
	// 返回规范化后的列表。
	return result
}

// cloneStrings 克隆字符串切片并稳定空数组语义。
func cloneStrings(values []string) []string {
	// JSON manifest 中空集合统一输出 []，避免不同路径出现 null / [] 抖动。
	if len(values) == 0 {
		return []string{}
	}
	// 返回浅拷贝，避免外部修改 Options 切片影响 manifest.filter。
	return append([]string{}, values...)
}
