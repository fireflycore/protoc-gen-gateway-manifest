package generator

import (
	"fmt"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
)

// httpRulesForMethod 从 method options 中提取 google.api.http 规则。
func httpRulesForMethod(serviceFullName string, method *protogen.Method) ([]HTTPRule, error) {
	// method.Desc.Options() 保存 protoc 解析出的 MethodOptions。
	options := method.Desc.Options()
	// 没有 options 或没有 google.api.http 时，表示该 method 不开放 HTTP 访问。
	if options == nil || !proto.HasExtension(options, annotations.E_Http) {
		return []HTTPRule{}, nil
	}
	// 读取 google.api.http 扩展原始值。
	extension := proto.GetExtension(options, annotations.E_Http)
	// 扩展值必须是 *annotations.HttpRule，否则说明 descriptor 或依赖版本异常。
	root, ok := extension.(*annotations.HttpRule)
	// 类型不符合或值为空时返回明确错误，避免生成半正确 manifest。
	if !ok || root == nil {
		return nil, fmt.Errorf("google.api.http extension has unexpected type %T", extension)
	}

	// rules 收集主规则和 additional_bindings 展开后的所有 HTTPRule。
	var rules []HTTPRule
	// methodName 用于生成稳定 route ID 和错误上下文。
	methodName := string(method.Desc.Name())
	// appendHTTPRule 会展开 root 与 additional_bindings，并执行基础校验。
	if err := appendHTTPRule(serviceFullName, methodName, root, &rules); err != nil {
		return nil, err
	}
	// 返回该 method 的所有 HTTP 映射。
	return rules, nil
}

// appendHTTPRule 展开主 HTTP rule 和 additional_bindings。
func appendHTTPRule(serviceFullName, methodName string, root *annotations.HttpRule, rules *[]HTTPRule) error {
	// google.api.http 的 additional_bindings 与主规则语义相同，都需要生成路由。
	allRules := append([]*annotations.HttpRule{root}, root.AdditionalBindings...)
	// index 是 binding 序号，用于构造稳定 ID；0 表示主规则。
	for index, rule := range allRules {
		// convertHTTPRule 将 protobuf HttpRule 转成 manifest HTTPRule。
		httpRule, err := convertHTTPRule(rule)
		// 转换失败时带上 service/method/http index，方便定位具体 annotation。
		if err != nil {
			return fmt.Errorf("%s/%s http%d: %w", serviceFullName, methodName, index, err)
		}
		// ID 采用 service.method.httpN，确保 additional binding 有稳定标识。
		httpRule.ID = fmt.Sprintf("%s.%s.http%d", serviceFullName, methodName, index)
		// validateHTTPRule 负责检查 method token 和 path template 的基础合法性。
		if err := validateHTTPRule(httpRule); err != nil {
			return fmt.Errorf("%s/%s http%d: %w", serviceFullName, methodName, index, err)
		}
		// 校验通过后追加到输出切片。
		*rules = append(*rules, httpRule)
	}
	// 所有 binding 处理完成。
	return nil
}

// convertHTTPRule 将 protobuf HttpRule 转换成 manifest HTTPRule。
func convertHTTPRule(rule *annotations.HttpRule) (HTTPRule, error) {
	// nil rule 表示 descriptor 异常，不能进入 manifest。
	if rule == nil {
		return HTTPRule{}, fmt.Errorf("rule is nil")
	}
	// httpPattern 提取 GET/POST/PUT/DELETE/PATCH/custom 中的 method 和 path。
	method, path, err := httpPattern(rule)
	// pattern 缺失或 custom 不合法时返回错误。
	if err != nil {
		return HTTPRule{}, err
	}
	// 返回 manifest 规则；ID 由 appendHTTPRule 根据 binding 序号补充。
	return HTTPRule{
		// Method 是标准化后的 HTTP 方法。
		Method: method,
		// Path 是清理空白后的 HTTP path template。
		Path: path,
		// Body 是 google.api.http body 配置，空值表示无请求体映射。
		Body: strings.TrimSpace(rule.Body),
		// ResponseBody 是 google.api.http response_body 配置。
		ResponseBody: strings.TrimSpace(rule.ResponseBody),
		// Transcoding 固定为 true，因为来源是 google.api.http。
		Transcoding: true,
	}, nil
}

// httpPattern 提取 HttpRule 中具体的 HTTP 方法和 path。
func httpPattern(rule *annotations.HttpRule) (string, string, error) {
	// HttpRule.Pattern 是 oneof，同一条规则只能设置一种 HTTP pattern。
	switch pattern := rule.Pattern.(type) {
	case *annotations.HttpRule_Get:
		// GET pattern 使用 get 字段作为 path。
		return "GET", strings.TrimSpace(pattern.Get), nil
	case *annotations.HttpRule_Post:
		// POST pattern 使用 post 字段作为 path。
		return "POST", strings.TrimSpace(pattern.Post), nil
	case *annotations.HttpRule_Put:
		// PUT pattern 使用 put 字段作为 path。
		return "PUT", strings.TrimSpace(pattern.Put), nil
	case *annotations.HttpRule_Delete:
		// DELETE pattern 使用 delete 字段作为 path。
		return "DELETE", strings.TrimSpace(pattern.Delete), nil
	case *annotations.HttpRule_Patch:
		// PATCH pattern 使用 patch 字段作为 path。
		return "PATCH", strings.TrimSpace(pattern.Patch), nil
	case *annotations.HttpRule_Custom:
		// custom pattern 允许业务定义非标准 HTTP method。
		if pattern.Custom == nil {
			return "", "", fmt.Errorf("custom pattern is nil")
		}
		// custom.kind 统一转大写，path 只裁剪空白。
		return strings.ToUpper(strings.TrimSpace(pattern.Custom.Kind)), strings.TrimSpace(pattern.Custom.Path), nil
	default:
		// 缺少 pattern 表示 annotation 不完整。
		return "", "", fmt.Errorf("missing http pattern")
	}
}
