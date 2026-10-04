package generator

import (
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// allowsService 判断某个 service 是否属于当前业务服务自己的生成范围。
func (o Options) allowsService(protoPackage, serviceFullName string) bool {
	// 没有 include 条件时默认只按 file_to_generate 生成，不再额外过滤。
	if !o.hasInclude() {
		return true
	}
	// 精确 package、package 前缀、完整 service 三种 include 任意命中即可。
	return contains(o.IncludePackages, protoPackage) ||
		hasAnyPrefix(protoPackage, o.IncludePackagePrefixes) ||
		contains(o.IncludeServices, serviceFullName)
}

// allowsPackage 判断某个 protoPackage 是否属于当前业务服务 include_package/include_package_prefix 范围。
func (o Options) allowsPackage(protoPackage string) bool {
	if !o.hasInclude() {
		return true
	}
	return contains(o.IncludePackages, protoPackage) || hasAnyPrefix(protoPackage, o.IncludePackagePrefixes)
}

// matchesInvocation 判断当前插件调用是否包含属于业务服务的 proto 文件。
// 在 Buf v2 多 inputs 场景下，依赖模块（如 validate、xds、第三方公共 proto）也会分别触发插件调用；
// 只有当前调用包含 opt 明确指定的业务服务 package 或 service 时，才属于业务服务调用。
func (o Options) matchesInvocation(files []*protogen.File) bool {
	if !o.hasInclude() {
		return true
	}
	for _, file := range files {
		protoPackage := string(file.Desc.Package())
		if o.allowsPackage(protoPackage) {
			return true
		}
		for _, service := range file.Services {
			serviceFullName := fullServiceName(protoPackage, service)
			if contains(o.IncludeServices, serviceFullName) {
				return true
			}
		}
	}
	return false
}

// contains 判断字符串列表中是否存在目标值。
func contains(values []string, target string) bool {
	// 线性扫描足够应对插件参数规模，并保持逻辑直观。
	for _, value := range values {
		// 精确匹配才算命中。
		if value == target {
			return true
		}
	}
	// 扫描结束仍未命中。
	return false
}

// hasAnyPrefix 判断 value 是否命中任意前缀。
func hasAnyPrefix(value string, prefixes []string) bool {
	// 遍历配置中的所有前缀。
	for _, prefix := range prefixes {
		// 使用 strings.HasPrefix 支持 acme.auth. 这类业务域过滤。
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	// 没有任何前缀命中。
	return false
}
