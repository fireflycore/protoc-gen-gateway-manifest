package generator

// ManifestSchema 是当前 route manifest 的稳定 schema 标识。
const ManifestSchema = "firefly.gateway.manifest.v1"

// ManifestVersion 是当前 JSON 结构版本。
const ManifestVersion = "v1"

// Manifest 表示插件输出的完整 gateway route manifest。
type Manifest struct {
	// Schema 用于让 api-gateway/sidecar-agent 快速识别 manifest 类型。
	Schema string `json:"schema"`
	// Version 表示当前 JSON 字段结构版本，后续结构演进时用于兼容判断。
	Version string `json:"version"`
	// GeneratedAt 记录生成时间，生产环境可用于审计配置何时刷新。
	GeneratedAt string `json:"generated_at"`
	// Generator 记录生成器名称和版本，方便定位不同版本插件产物差异。
	Generator GeneratorInfo `json:"generator"`
	// Source 记录 manifest 来源模块、引用和实际参与生成的 proto 文件。
	Source SourceInfo `json:"source"`
	// Filter 回写本次 include/exclude 条件，便于确认依赖服务是否被正确过滤。
	Filter FilterInfo `json:"filter"`
	// Descriptor 记录 Envoy transcoder 运行时需要加载的 descriptor 引用。
	Descriptor DescriptorRef `json:"descriptor"`
	// Services 保留按 gRPC service 聚合的视图，适合 sidecar-agent 做服务级注册。
	Services []Service `json:"services"`
	// Routes 是扁平化 HTTP 路由列表，适合 api-gateway 直接构建 HTTP 转 gRPC 路由表。
	Routes []Route `json:"routes"`
}

// GeneratorInfo 记录生成器信息，便于后续排查 manifest 来源。
type GeneratorInfo struct {
	// Name 是生成器二进制名称。
	Name string `json:"name"`
	// Version 是生成器发布版本。
	Version string `json:"version"`
}

// SourceInfo 记录本次生成关联的 Buf/proto 来源。
type SourceInfo struct {
	// Module 通常填写 buf module 名称，例如 buf.build/lhdht/grpc。
	Module string `json:"module"`
	// ModuleRef 通常填写分支、tag 或 commit，用于追踪 proto 版本。
	ModuleRef string `json:"module_ref"`
	// Files 是实际进入 manifest 的 proto 文件列表，依赖文件被过滤后不会出现在这里。
	Files []string `json:"files"`
}

// FilterInfo 记录本次生成使用的过滤条件，便于审计依赖 proto 为什么没有进入 manifest。
type FilterInfo struct {
	// IncludePackages 精确包含指定 proto package。
	IncludePackages []string `json:"include_packages"`
	// IncludePackagePrefixes 按 package 前缀包含一组业务服务。
	IncludePackagePrefixes []string `json:"include_package_prefixes"`
	// IncludeServices 精确包含指定完整 service 名称。
	IncludeServices []string `json:"include_services"`
	// ExcludePackages 精确排除指定 proto package，优先级高于 include。
	ExcludePackages []string `json:"exclude_packages"`
	// ExcludePackagePrefixes 按 package 前缀排除依赖或内部服务，优先级高于 include。
	ExcludePackagePrefixes []string `json:"exclude_package_prefixes"`
	// ExcludeServices 精确排除指定完整 service 名称，优先级高于 include。
	ExcludeServices []string `json:"exclude_services"`
	// RequireInclude 为 true 时强制调用方显式声明 include，避免依赖 proto 被误生成。
	RequireInclude bool `json:"require_include"`
}

// DescriptorRef 记录 api-gateway 后续加载 Envoy transcoder descriptor 所需的引用。
type DescriptorRef struct {
	// Ref 是 descriptor set 的存储引用或发布引用。
	Ref string `json:"ref"`
	// SHA256 是 descriptor set 的可选完整性校验值。
	SHA256 string `json:"sha256"`
}

// Service 表示一个 gRPC service 的 manifest 视图。
type Service struct {
	// Name 是完整 service 名称，例如 acme.auth.v1.AuthService。
	Name string `json:"name"`
	// ProtoPackage 是 service 所属 proto package。
	ProtoPackage string `json:"proto_package"`
	// ProtoFile 是定义该 service 的 proto 文件路径。
	ProtoFile string `json:"proto_file"`
	// Methods 是该 service 下被纳入 manifest 的 method 列表。
	Methods []Method `json:"methods"`
}

// Method 表示一个 gRPC method 及其可选 HTTP 映射。
type Method struct {
	// Name 是 method 短名称，例如 Login。
	Name string `json:"name"`
	// FullMethod 是 gRPC 标准全路径，例如 /acme.auth.v1.AuthService/Login。
	FullMethod string `json:"full_method"`
	// InputType 是请求消息完整类型名。
	InputType string `json:"input_type"`
	// OutputType 是响应消息完整类型名。
	OutputType string `json:"output_type"`
	// HTTPRules 是 google.api.http 规则展开后的列表；gRPC-only 方法会稳定输出空数组。
	HTTPRules []HTTPRule `json:"http_rules"`
}

// HTTPRule 表示从 google.api.http 提取出的单条 HTTP 映射。
type HTTPRule struct {
	// ID 是单条 HTTP 映射的稳定标识，包含 service、method 和 binding 序号。
	ID string `json:"id"`
	// Method 是 HTTP 方法，例如 GET、POST 或 custom kind。
	Method string `json:"method"`
	// Path 是 HTTP path template，例如 /v1/auth/login。
	Path string `json:"path"`
	// Body 是 google.api.http body 字段，空字符串表示不映射请求体。
	Body string `json:"body"`
	// ResponseBody 是 google.api.http response_body 字段，空字符串表示使用完整响应。
	ResponseBody string `json:"response_body"`
	// Transcoding 标记该规则需要 HTTP/JSON 到 gRPC 的转码。
	Transcoding bool `json:"transcoding"`
}

// Route 是 sidecar-agent 和 api-gateway 最容易直接消费的扁平 HTTP 路由。
type Route struct {
	// ID 与 HTTPRule.ID 保持一致，便于从扁平路由反查来源 method。
	ID string `json:"id"`
	// Protocol 当前固定为 grpc，后续如支持原生 HTTP 服务可扩展。
	Protocol string `json:"protocol"`
	// HTTPMethod 是网关匹配请求时使用的 HTTP 方法。
	HTTPMethod string `json:"http_method"`
	// Path 是网关匹配请求时使用的 path template。
	Path string `json:"path"`
	// Body 是转码时请求体映射规则。
	Body string `json:"body"`
	// ResponseBody 是转码时响应体映射规则。
	ResponseBody string `json:"response_body"`
	// GRPCService 是目标 gRPC service 完整名称。
	GRPCService string `json:"grpc_service"`
	// GRPCMethod 是目标 gRPC method 短名称。
	GRPCMethod string `json:"grpc_method"`
	// FullMethod 是目标 gRPC method 完整路径。
	FullMethod string `json:"full_method"`
	// Transcoding 表示该路由是否需要 Envoy gRPC-JSON transcoder 参与。
	Transcoding bool `json:"transcoding"`
	// ProtoFile 记录该路由来源 proto 文件，便于诊断生成和运行时 descriptor 对齐问题。
	ProtoFile string `json:"proto_file"`
}
