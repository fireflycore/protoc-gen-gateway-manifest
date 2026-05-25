package main

import (
	"github.com/fireflycore/protoc-gen-gateway-manifest/internal/generator"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"
)

// main 是 protoc-gen-gateway-manifest 的进程入口。
func main() {
	// 插件入口保持极薄，所有生成规则都收敛在 internal/generator，便于单元测试覆盖。
	options := protogen.Options{}
	// Run 会从 stdin 读取 protoc/buf 传入的请求，并把回调返回的生成结果写回 stdout。
	options.Run(func(plugin *protogen.Plugin) error {
		// manifest 只读取 descriptor，不生成语言绑定代码；声明 proto3 optional 支持可避免 Buf 对 optional 字段告警。
		plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
		// 将 protoc 解析出的文件描述符交给 generator，由它生成稳定的 gateway manifest。
		return generator.Generate(plugin)
	})
}
