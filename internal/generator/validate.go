package generator

import (
	"fmt"
	"strings"
)

// validateHTTPRule 校验从 google.api.http 得到的 HTTP 规则是否适合进入网关控制面。
func validateHTTPRule(rule HTTPRule) error {
	// HTTP method 必须符合 HTTP token 语义，避免生成 Envoy 或网关无法识别的 method。
	if !isHTTPToken(rule.Method) {
		return fmt.Errorf("http method %q is invalid", rule.Method)
	}
	// path template 必须从 / 开始，网关路由表不接受相对路径。
	if !strings.HasPrefix(rule.Path, "/") {
		return fmt.Errorf("http path %q must start with /", rule.Path)
	}
	// path 中不允许包含空白、query 或 fragment；这些不属于路由 path template。
	if strings.ContainsAny(rule.Path, " \t\r\n?#") {
		return fmt.Errorf("http path %q must not contain whitespace, query or fragment", rule.Path)
	}
	// 规则通过基础校验。
	return nil
}

// isHTTPToken 判断字符串是否符合 HTTP token 字符集合。
func isHTTPToken(value string) bool {
	// 空字符串不能作为 HTTP method。
	if value == "" {
		return false
	}
	// 逐 rune 检查，先拒绝非 ASCII，再按 tchar 规则判断。
	for _, r := range value {
		// HTTP method token 在这里限制为 ASCII tchar，避免 Unicode 字符进入路由 key。
		if r > 127 || !isTChar(byte(r)) {
			return false
		}
	}
	// 所有字符都合法。
	return true
}

// isTChar 判断单个 ASCII 字节是否属于 RFC 7230 tchar 集合。
func isTChar(ch byte) bool {
	// 数字允许出现在 token 中。
	switch {
	case ch >= '0' && ch <= '9':
		return true
	case ch >= 'A' && ch <= 'Z':
		return true
	case ch >= 'a' && ch <= 'z':
		return true
	}
	// 额外允许的符号集合来自 HTTP token/tchar 定义。
	switch ch {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}
