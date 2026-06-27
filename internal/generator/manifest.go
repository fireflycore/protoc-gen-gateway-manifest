package generator

// ManifestSchema 是当前 route manifest 的稳定 schema 标识。
const ManifestSchema = "firefly.gateway.manifest.v1"

// Manifest 表示插件输出的最小 gateway route manifest。
type Manifest struct {
	// Schema 用于让消费方识别当前 manifest 类型。
	Schema string `json:"schema"`
	// Services 记录 gRPC service 及其完整 method 名称。
	Services []Service `json:"services"`
	// Routes 记录可直接消费的 HTTP 路由。
	Routes []Route `json:"routes"`
}

// Service 表示一个 gRPC service 的最小视图。
type Service struct {
	// Name 是完整 service 名称，例如 acme.auth.v1.AuthService。
	Name string `json:"name"`
	// Methods 是该 service 下的完整 method 名称列表。
	Methods []string `json:"methods"`
}

// Route 表示一条 HTTP 到 gRPC 的路由映射。
type Route struct {
	// HTTPMethod 是网关匹配时使用的 HTTP 方法。
	HTTPMethod string `json:"http_method"`
	// Path 是网关匹配时使用的 path template。
	Path string `json:"path"`
	// FullMethod 是目标 gRPC full method。
	FullMethod string `json:"full_method"`
}

// HTTPRule 是从 google.api.http 提取出的内部中间结构，不直接输出到 manifest。
type HTTPRule struct {
	// Method 是标准化后的 HTTP 方法。
	Method string
	// Path 是 HTTP path template。
	Path string
}
