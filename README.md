# protoc-gen-gateway-manifest

`protoc-gen-gateway-manifest` 从 proto descriptor 中读取 gRPC service/method 与 `google.api.http` annotation，生成 Firefly 网关体系使用的 `gateway.manifest.json`。

它只生成机器可读契约，不生成 Envoy xDS，也不生成业务 HTTP handler。

当前插件版本：`v0.0.5`。

代码结构：

```text
cmd/protoc-gen-gateway-manifest    protoc 插件二进制入口
internal/generator                 manifest 生成、过滤、HTTP rule 解析和校验
```

## 安装

普通使用者推荐直接安装已发布版本，不需要从源码构建：

```bash
go install github.com/fireflycore/protoc-gen-gateway-manifest/cmd/protoc-gen-gateway-manifest@v0.0.5
```

安装后确保 `$GOBIN` 或 `$GOPATH/bin` 在 `PATH` 中，Buf/protoc 才能找到 `protoc-gen-gateway-manifest`。

## Buf 示例

```yaml
plugins:
  - local: protoc-gen-gateway-manifest
    out: dep/protobuf/gen
    # 单个 gateway.manifest.json 需要合并所有待生成 proto，不能使用 Buf 默认的 directory 策略。
    strategy: all
    opt:
      - include_package_prefix=acme.auth.
```

所有 gRPC method 都会进入 `services[].methods`。没有 `google.api.http` 的 method 不会进入 `routes[]`，也不会被自动合成 HTTP path。

注意：本插件生成的是单个聚合文件，Buf 配置必须设置 `strategy: all`。如果使用默认 `directory` 策略，Buf 会按目录多次调用插件，每次都会生成同名 `gateway.manifest.json`，从而出现 `duplicate generated file name` 并丢弃后续产物。

## 参数

- `include_package` / `include_package_prefix` / `include_service`：只生成当前业务服务拥有的 proto 范围。

输出文件名固定为 `gateway.manifest.json`，默认落在 `dep/protobuf/gen/`。重复 HTTP method + path 时保留第一条 route，后续重复项跳过。未配置 include 时，插件只处理本次 `file_to_generate` 中的 service；业务服务有依赖 proto 时，建议显式配置 include 范围。

api-gateway/Envoy 做 gRPC-JSON 转码时需要 descriptor set 才能知道 JSON 字段、path 参数和 protobuf message 之间如何映射。新主线由 proto 仓库按 namespace 发布 descriptor current；业务服务 manifest 只表达 service/method/HTTP route 事实。

输出 schema 只保留运行时消费所需字段：`schema`、`services[]`、`routes[]`。`routes[]` 仅包含 `http_method`、`path`、`full_method`。

## 开发

只有在本地修改插件代码时，才需要从源码测试或构建：

```bash
go test ./...
go build ./cmd/protoc-gen-gateway-manifest
```
