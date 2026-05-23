package generator

import (
	"os"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// TestBuildGeneratesAnnotatedRoutesAndKeepsGRPCOnlyMethods 覆盖完整 manifest 生成主路径。
func TestBuildGeneratesAnnotatedRoutesAndKeepsGRPCOnlyMethods(t *testing.T) {
	// 构造只包含 app type package 的测试插件，并开启 gRPC-only method 记录。
	plugin := testPlugin(t, "include_package=acme.app.type.v1,require_include=true,include_unannotated_methods=true,generated_at=2026-05-23T00:00:00Z", appTypeFile())

	// Build 应当从 descriptor 和 google.api.http annotation 生成 manifest。
	manifest, err := Build(plugin, mustOptions(t, plugin), "v0.1.0")
	// 主路径不应该返回错误。
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// 只应生成一个 app type service。
	if got, want := len(manifest.Services), 1; got != want {
		t.Fatalf("services len = %d, want %d", got, want)
	}
	// 开启 include_unannotated_methods 后，InternalOnly 也应进入 services.methods。
	if got, want := len(manifest.Services[0].Methods), 3; got != want {
		t.Fatalf("methods len = %d, want %d", got, want)
	}
	// 两个带 annotation 的方法中，CreateType 有 additional binding，因此总计三条 route。
	if got, want := len(manifest.Routes), 3; got != want {
		t.Fatalf("routes len = %d, want %d", got, want)
	}
	// gRPC-only method 即使进入 methods，也不能生成 HTTP route。
	assertNoRouteForMethod(t, manifest.Routes, "/acme.app.type.v1.AppTypeService/InternalOnly")

	// MarshalManifest 应输出稳定缩进 JSON。
	content, err := MarshalManifest(manifest)
	// JSON 序列化不应该失败。
	if err != nil {
		t.Fatalf("MarshalManifest() error = %v", err)
	}
	// 读取 golden 文件作为稳定输出基准。
	golden, err := os.ReadFile("testdata/app.manifest.golden.json")
	// golden 文件缺失时测试应明确失败。
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	// 对比完整 JSON，确保字段、排序和空数组语义都没有回归。
	if string(content) != string(golden) {
		t.Fatalf("manifest mismatch\n--- got ---\n%s\n--- want ---\n%s", content, golden)
	}
}

// TestBuildSkipsUnannotatedMethodsUnlessRequested 确认未标注方法默认不进入 manifest。
func TestBuildSkipsUnannotatedMethodsUnlessRequested(t *testing.T) {
	// 不开启 include_unannotated_methods，InternalOnly 应被完全跳过。
	plugin := testPlugin(t, "include_package=acme.app.type.v1,require_include=true", appTypeFile())

	// 构建 manifest。
	manifest, err := Build(plugin, mustOptions(t, plugin), "v0.1.0")
	// 构建不应该失败。
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	// 遍历 service methods，确认 InternalOnly 没被写入。
	for _, method := range manifest.Services[0].Methods {
		// 未标注 google.api.http 的方法默认不允许 HTTP 访问。
		if method.Name == "InternalOnly" {
			t.Fatalf("unannotated method should be skipped without include_unannotated_methods")
		}
	}
	// 再确认扁平 routes 中也没有 InternalOnly。
	assertNoRouteForMethod(t, manifest.Routes, "/acme.app.type.v1.AppTypeService/InternalOnly")
}

// TestBuildFiltersDependencyPackages 覆盖 auth 服务依赖 proto 不应进入 manifest 的场景。
func TestBuildFiltersDependencyPackages(t *testing.T) {
	// auth 服务依赖 user/config/secure，但 include_package_prefix 只允许 acme.auth.。
	plugin := testPlugin(t, "include_package_prefix=acme.auth.,require_include=true,include_unannotated_methods=true", authFile(), userFile(), configFile(), secureFile())

	// 构建 auth manifest。
	manifest, err := Build(plugin, mustOptions(t, plugin), "v0.1.0")
	// 过滤逻辑不应该导致构建失败。
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	// 只有 auth service 应进入 manifest。
	if got, want := len(manifest.Services), 1; got != want {
		t.Fatalf("services len = %d, want %d", got, want)
	}
	// 唯一 service 必须是 AuthService。
	if got, want := manifest.Services[0].Name, "acme.auth.v1.AuthService"; got != want {
		t.Fatalf("service = %q, want %q", got, want)
	}
	// 检查 routes 中没有依赖 package 泄露。
	for _, route := range manifest.Routes {
		// user/config/secure 都是 auth 输入中的依赖服务，不属于 auth manifest 暴露范围。
		if strings.HasPrefix(route.GRPCService, "acme.user.") || strings.HasPrefix(route.GRPCService, "acme.config.") || strings.HasPrefix(route.GRPCService, "acme.secure.") {
			t.Fatalf("dependency route leaked into auth manifest: %+v", route)
		}
	}
}

// TestBuildExcludeOverridesInclude 确认 exclude 规则优先级高于 include。
func TestBuildExcludeOverridesInclude(t *testing.T) {
	// include acme. 全域，再用 exclude_package_prefix 排除 secure。
	plugin := testPlugin(t, "include_package_prefix=acme.,exclude_package_prefix=acme.secure.,require_include=true", authFile(), secureFile())

	// 构建 manifest。
	manifest, err := Build(plugin, mustOptions(t, plugin), "v0.1.0")
	// 构建不应该失败。
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	// 遍历所有 service，确认 secure 被 exclude 覆盖掉。
	for _, service := range manifest.Services {
		// 任何 acme.secure. service 出现都说明 exclude 优先级失效。
		if strings.HasPrefix(service.Name, "acme.secure.") {
			t.Fatalf("excluded service leaked into manifest: %s", service.Name)
		}
	}
}

// TestBuildRejectsDuplicateRoutes 确认重复 HTTP method+path 默认阻断生成。
func TestBuildRejectsDuplicateRoutes(t *testing.T) {
	// duplicateRouteFile 中两个 service 都声明 GET /v1/dup。
	plugin := testPlugin(t, "include_package=acme.dup.v1,require_include=true,fail_on_duplicate_route=true", duplicateRouteFile())

	// 构建应失败，因为重复入口路由会导致运行时匹配不确定。
	_, err := Build(plugin, mustOptions(t, plugin), "v0.1.0")
	// 错误消息应指出重复的 HTTP route。
	if err == nil || !strings.Contains(err.Error(), "duplicate http route GET /v1/dup") {
		t.Fatalf("Build() error = %v, want duplicate route error", err)
	}
}

// TestParseOptionsRequiresIncludeWhenConfigured 确认 require_include 的保护行为。
func TestParseOptionsRequiresIncludeWhenConfigured(t *testing.T) {
	// 只开启 require_include 但不传任何 include 条件。
	_, err := ParseOptions("require_include=true")
	// 应返回明确错误，提醒调用方指定业务服务范围。
	if err == nil || !strings.Contains(err.Error(), "requires include_package") {
		t.Fatalf("ParseOptions() error = %v, want require include error", err)
	}
}

// TestGenerateWritesConfiguredOutputFile 覆盖 protoc 插件入口写文件行为。
func TestGenerateWritesConfiguredOutputFile(t *testing.T) {
	// out_file 指定自定义输出文件名。
	plugin := testPlugin(t, "out_file=custom.manifest.json,include_package=acme.app.type.v1,require_include=true", appTypeFile())

	// Generate 应解析参数、构建 manifest 并写入 protogen response。
	if err := Generate(plugin, "v0.1.0"); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	// Response 是 protoc 插件最终会返回给 protoc/buf 的生成结果。
	response := plugin.Response()
	// 该测试只应生成一个 manifest 文件。
	if got, want := len(response.File), 1; got != want {
		t.Fatalf("generated files len = %d, want %d", got, want)
	}
	// 输出文件名应等于 out_file 参数。
	if got, want := response.File[0].GetName(), "custom.manifest.json"; got != want {
		t.Fatalf("generated file = %q, want %q", got, want)
	}
}

// mustOptions 解析插件参数，失败时直接终止测试。
func mustOptions(t *testing.T, plugin *protogen.Plugin) Options {
	// 标记为测试 helper，使失败行号指向调用处。
	t.Helper()
	// 从 CodeGeneratorRequest 中读取原始 parameter。
	options, err := ParseOptions(plugin.Request.GetParameter())
	// 参数解析失败说明测试 fixture 配置错误。
	if err != nil {
		t.Fatalf("ParseOptions() error = %v", err)
	}
	// 返回解析后的 Options。
	return options
}

// assertNoRouteForMethod 断言指定 gRPC full method 没有对应 HTTP route。
func assertNoRouteForMethod(t *testing.T, routes []Route, fullMethod string) {
	// 标记为测试 helper，使失败行号指向具体断言调用处。
	t.Helper()
	// 遍历所有扁平 route。
	for _, route := range routes {
		// 如果 full method 相同，说明 gRPC-only 方法被错误暴露成 HTTP。
		if route.FullMethod == fullMethod {
			t.Fatalf("unexpected HTTP route for gRPC-only method: %+v", route)
		}
	}
}

// testPlugin 根据手写 descriptor 构造 protogen.Plugin。
func testPlugin(t *testing.T, parameter string, files ...*descriptorpb.FileDescriptorProto) *protogen.Plugin {
	// 标记为测试 helper。
	t.Helper()
	// 构造 protoc 插件请求，FileToGenerate 后续按传入 files 填充。
	request := &pluginpb.CodeGeneratorRequest{
		// Parameter 模拟 buf/protoc 传入的插件参数。
		Parameter: proto.String(parameter),
		// FileToGenerate 预分配容量，减少 append 扩容。
		FileToGenerate: make([]string, 0, len(files)),
		// ProtoFile 包含本次测试的所有文件描述符。
		ProtoFile: files,
	}
	// 将每个传入 proto 文件都标记为待生成文件。
	for _, file := range files {
		request.FileToGenerate = append(request.FileToGenerate, file.GetName())
	}
	// 使用 protogen 官方入口把 CodeGeneratorRequest 转为 Plugin。
	plugin, err := protogen.Options{}.New(request)
	// descriptor fixture 如果不合法，测试应立即失败。
	if err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	// 返回可传给 Build/Generate 的插件对象。
	return plugin
}

// appTypeFile 构造带主 HTTP rule、additional binding 和 gRPC-only method 的 app 测试文件。
func appTypeFile() *descriptorpb.FileDescriptorProto {
	// 返回 acme.app.type.v1 的 FileDescriptorProto。
	return protoFile("acme/app/type/v1/type.proto", "acme.app.type.v1",
		// AppTypeService 覆盖普通 HTTP 映射、additional binding 和未标注方法。
		service("AppTypeService",
			// CreateType 通过 POST 暴露，同时额外提供一个 GET preview binding。
			method("CreateType", httpPost("/v1/app/type", "*", httpGet("/v1/app/type/create-preview"))),
			// GetTypeInfo 通过 GET 暴露。
			method("GetTypeInfo", httpGet("/v1/app/type")),
			// InternalOnly 没有 google.api.http，因此只能作为 gRPC-only 方法。
			method("InternalOnly", nil),
		),
	)
}

// authFile 构造 auth 业务服务测试文件。
func authFile() *descriptorpb.FileDescriptorProto {
	// 返回 acme.auth.v1 的 FileDescriptorProto。
	return protoFile("acme/auth/v1/auth.proto", "acme.auth.v1",
		// AuthService 模拟 auth 服务自身。
		service("AuthService",
			// Login 通过 HTTP POST 暴露。
			method("Login", httpPost("/v1/auth/login", "*")),
			// ValidateToken 没有 HTTP annotation，默认不开放 HTTP 访问。
			method("ValidateToken", nil),
		),
	)
}

// userFile 构造 auth 依赖的 user 服务测试文件。
func userFile() *descriptorpb.FileDescriptorProto {
	// 返回 acme.user.v1 的 FileDescriptorProto。
	return protoFile("acme/user/v1/user.proto", "acme.user.v1",
		// UserService 用于验证依赖服务不会泄露到 auth manifest。
		service("UserService", method("GetUser", httpGet("/v1/user"))),
	)
}

// configFile 构造 auth 依赖的 config 服务测试文件。
func configFile() *descriptorpb.FileDescriptorProto {
	// 返回 acme.config.v1 的 FileDescriptorProto。
	return protoFile("acme/config/v1/config.proto", "acme.config.v1",
		// ConfigService 用于验证依赖服务过滤。
		service("ConfigService", method("GetConfig", httpGet("/v1/config"))),
	)
}

// secureFile 构造 auth 依赖的 secure 服务测试文件。
func secureFile() *descriptorpb.FileDescriptorProto {
	// 返回 acme.secure.code.verify.email.v1 的 FileDescriptorProto。
	return protoFile("acme/secure/code/verify/email/v1/email.proto", "acme.secure.code.verify.email.v1",
		// EmailVerifyService 用于验证 exclude/include 不会误收依赖 secure 服务。
		service("EmailVerifyService", method("SendCode", httpPost("/v1/secure/code/verify/email", "*"))),
	)
}

// duplicateRouteFile 构造重复 HTTP 路由测试文件。
func duplicateRouteFile() *descriptorpb.FileDescriptorProto {
	// 返回 acme.dup.v1 的 FileDescriptorProto。
	return protoFile("acme/dup/v1/dup.proto", "acme.dup.v1",
		// FirstService 声明 GET /v1/dup。
		service("FirstService", method("Get", httpGet("/v1/dup"))),
		// SecondService 声明相同 GET /v1/dup，用于触发重复路由检测。
		service("SecondService", method("Get", httpGet("/v1/dup"))),
	)
}

// protoFile 构造最小可用 FileDescriptorProto。
func protoFile(name, pkg string, services ...*descriptorpb.ServiceDescriptorProto) *descriptorpb.FileDescriptorProto {
	// 测试 fixture 让每个 package 自带 Request/Response，避免不同文件之间产生无关依赖。
	for _, service := range services {
		// 给 service 内每个 method 绑定当前 package 下的 Request/Response。
		for _, method := range service.Method {
			// InputType 必须使用完整 proto type 名称。
			method.InputType = proto.String("." + pkg + ".Request")
			// OutputType 必须使用完整 proto type 名称。
			method.OutputType = proto.String("." + pkg + ".Response")
		}
	}
	// 返回包含消息和服务定义的文件描述符。
	return &descriptorpb.FileDescriptorProto{
		// Syntax 固定为 proto3。
		Syntax: proto.String("proto3"),
		// Name 是 proto 文件路径。
		Name: proto.String(name),
		// Package 是 proto package。
		Package: proto.String(pkg),
		// Options 至少提供 go_package，满足 protogen 构造要求。
		Options: &descriptorpb.FileOptions{
			// GoPackage 使用测试专用路径，按 package 拼接避免冲突。
			GoPackage: proto.String("github.com/fireflycore/protoc-gen-gateway-manifest/internal/generator/testdata/" + strings.ReplaceAll(pkg, ".", "")),
		},
		// MessageType 提供 method 输入输出依赖的 Request/Response。
		MessageType: []*descriptorpb.DescriptorProto{
			// Request 是所有测试 method 的输入消息。
			{Name: proto.String("Request")},
			// Response 是所有测试 method 的输出消息。
			{Name: proto.String("Response")},
		},
		// Service 写入调用方传入的测试服务。
		Service: services,
	}
}

// service 构造 ServiceDescriptorProto。
func service(name string, methods ...*descriptorpb.MethodDescriptorProto) *descriptorpb.ServiceDescriptorProto {
	// 返回包含名称和 method 列表的 service descriptor。
	return &descriptorpb.ServiceDescriptorProto{
		// Name 是 service 短名称。
		Name: proto.String(name),
		// Method 是该 service 下的 method 列表。
		Method: methods,
	}
}

// method 构造 MethodDescriptorProto，并按需设置 google.api.http。
func method(name string, httpRule *annotations.HttpRule) *descriptorpb.MethodDescriptorProto {
	// MethodOptions 用于承载 google.api.http 扩展。
	options := &descriptorpb.MethodOptions{}
	// 有 httpRule 时才设置扩展；nil 表示 gRPC-only method。
	if httpRule != nil {
		// 测试直接设置扩展，避免依赖外部 protoc/buf 或真实 proto 文件。
		proto.SetExtension(options, annotations.E_Http, httpRule)
	}
	// 返回 method descriptor。
	return &descriptorpb.MethodDescriptorProto{
		// Name 是 method 短名称。
		Name: proto.String(name),
		// InputType 先给默认值，protoFile 会覆盖为当前 package 下的 Request。
		InputType: proto.String(".acme.app.type.v1.Request"),
		// OutputType 先给默认值，protoFile 会覆盖为当前 package 下的 Response。
		OutputType: proto.String(".acme.app.type.v1.Response"),
		// Options 携带可能存在的 google.api.http 扩展。
		Options: options,
	}
}

// httpGet 构造 GET google.api.http 规则。
func httpGet(path string, additional ...*annotations.HttpRule) *annotations.HttpRule {
	// 返回 annotations.HttpRule，additional_bindings 原样透传。
	return &annotations.HttpRule{
		// Pattern 设置为 get oneof。
		Pattern: &annotations.HttpRule_Get{Get: path},
		// AdditionalBindings 用于测试一对多 HTTP 映射。
		AdditionalBindings: additional,
	}
}

// httpPost 构造 POST google.api.http 规则。
func httpPost(path, body string, additional ...*annotations.HttpRule) *annotations.HttpRule {
	// 返回 annotations.HttpRule，body 和 additional_bindings 原样透传。
	return &annotations.HttpRule{
		// Pattern 设置为 post oneof。
		Pattern: &annotations.HttpRule_Post{Post: path},
		// Body 设置请求体映射规则。
		Body: body,
		// AdditionalBindings 用于测试一对多 HTTP 映射。
		AdditionalBindings: additional,
	}
}
