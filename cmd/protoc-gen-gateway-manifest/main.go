package main

import (
	"github.com/fireflycore/protoc-gen-gateway-manifest/internal/generator"
	"google.golang.org/protobuf/compiler/protogen"
)

// version 标识当前生成器版本，最终会写入 manifest.generator.version 方便排查产物来源。
const version = "v0.1.0"

// main 是 protoc-gen-gateway-manifest 的进程入口。
func main() {
	// 插件入口保持极薄，所有生成规则都收敛在 internal/generator，便于单元测试覆盖。
	options := protogen.Options{}
	// Run 会从 stdin 读取 protoc/buf 传入的请求，并把回调返回的生成结果写回 stdout。
	options.Run(func(plugin *protogen.Plugin) error {
		// 将 protoc 解析出的文件描述符交给 generator，由它生成稳定的 gateway manifest。
		return generator.Generate(plugin, version)
	})
}
