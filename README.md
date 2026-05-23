# protoc-gen-gateway-manifest

`protoc-gen-gateway-manifest` 从 proto descriptor 中读取 gRPC service/method 与 `google.api.http` annotation，生成 Firefly 网关体系使用的 `gateway.manifest.json`。

它只生成机器可读契约，不生成 Envoy xDS，也不生成业务 HTTP handler。

## Buf 示例

```yaml
plugins:
  - local: protoc-gen-gateway-manifest
    out: dep/protobuf/manifest
    # 单个 gateway.manifest.json 需要合并所有待生成 proto，不能使用 Buf 默认的 directory 策略。
    strategy: all
    opt:
      - out_file=gateway.manifest.json
      - include_package_prefix=acme.auth.
      - require_include=true
      - include_unannotated_methods=true
      - fail_on_duplicate_route=true
```

没有 `google.api.http` 的 gRPC method 只会作为 gRPC 能力进入 `services[].methods`，不会进入 `routes[]`，也不会被自动合成 HTTP path。

注意：本插件生成的是单个聚合文件，Buf 配置必须设置 `strategy: all`。如果使用默认 `directory` 策略，Buf 会按目录多次调用插件，每次都会生成同名 `gateway.manifest.json`，从而出现 `duplicate generated file name` 并丢弃后续产物。

## 参数

- `out_file`：输出文件名，默认 `gateway.manifest.json`。
- `module` / `module_ref`：写入 `source` 元数据。
- `descriptor_ref` / `descriptor_sha256`：写入 descriptor 引用。
- `include_package` / `include_package_prefix` / `include_service`：只生成当前业务服务拥有的 proto 范围。
- `exclude_package` / `exclude_package_prefix` / `exclude_service`：排除依赖服务。
- `require_include=true`：强制配置 include，生产模板建议开启。
- `include_unannotated_methods=true`：保留未标注 HTTP 的 gRPC method，但不生成 HTTP route。
- `fail_on_duplicate_route=true`：重复 HTTP method + path 时失败。

## 开发

```bash
go test ./...
go build ./cmd/protoc-gen-gateway-manifest
```
