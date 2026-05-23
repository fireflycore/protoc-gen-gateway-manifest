package generator

import (
	"strings"
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
