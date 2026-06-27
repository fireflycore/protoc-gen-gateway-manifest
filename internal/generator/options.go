package generator

import (
	"fmt"
	"sort"
	"strings"
)

// DefaultOutFile 是插件固定输出的聚合 manifest 文件名。
const DefaultOutFile = "gateway.manifest.json"

// Options 保存 protoc/buf 传给插件的所有参数。
type Options struct {
	// IncludePackages 是允许生成的精确 proto package 列表。
	IncludePackages []string
	// IncludePackagePrefixes 是允许生成的 proto package 前缀列表。
	IncludePackagePrefixes []string
	// IncludeServices 是允许生成的完整 service 名称列表。
	IncludeServices []string
}

// ParseOptions 解析标准 protoc plugin parameter。
func ParseOptions(parameter string) (Options, error) {
	// 从默认值开始叠加用户传入参数。
	options := defaultOptions()
	// protoc 可能传入空白字符串，这里先统一裁剪。
	parameter = strings.TrimSpace(parameter)
	// 没有任何参数时直接返回默认配置。
	if parameter == "" {
		return options, nil
	}

	// lastListKey 记录最近一个列表型参数，用于支持 include_package=a,b 这种写法。
	var lastListKey string
	// protoc plugin parameter 顶层以逗号分隔。
	for _, token := range strings.Split(parameter, ",") {
		// 每个 token 都先去除首尾空白，避免配置格式细节影响解析。
		token = strings.TrimSpace(token)
		// 跳过连续逗号或末尾逗号产生的空 token。
		if token == "" {
			continue
		}
		// 标准参数形态是 key=value。
		if key, value, ok := strings.Cut(token, "="); ok {
			// key 去空白后用于匹配支持的插件参数名。
			key = strings.TrimSpace(key)
			// value 去空白后再按参数类型解析。
			value = strings.TrimSpace(value)
			// 将单个参数应用到 Options。
			if err := options.apply(key, value); err != nil {
				return Options{}, err
			}
			// 列表参数后面的裸 token 会被视为当前列表的延续值。
			if isListOption(key) {
				lastListKey = key
			} else {
				// 非列表参数不能接收后续裸 token。
				lastListKey = ""
			}
			// 当前 key=value 已处理完成，进入下一个 token。
			continue
		}
		// Buf/protoc 的参数天然以逗号分隔；这里把 include_package=a,b 中的 b 视为上一列表参数的延续。
		if lastListKey == "" {
			return Options{}, fmt.Errorf("invalid option token %q", token)
		}
		// 将裸 token 应用到上一项列表参数。
		if err := options.apply(lastListKey, token); err != nil {
			return Options{}, err
		}
	}

	// normalize 统一默认值、去重、排序和空数组语义。
	options.normalize()
	// 返回解析并规范化后的选项。
	return options, nil
}

// defaultOptions 返回插件默认配置。
func defaultOptions() Options {
	// 当前没有需要设置的动态默认值，返回零值即可。
	return Options{}
}

// apply 将单个 key=value 参数写入 Options。
func (o *Options) apply(key, value string) error {
	// 根据参数名分派到对应字段。
	switch key {
	case "include_package":
		// include_package 支持单个值，也支持 ; 或 | 分隔多个值。
		o.IncludePackages = append(o.IncludePackages, splitOptionValues(value)...)
	case "include_package_prefix":
		// include_package_prefix 用于一次包含某个业务域下的多个 package。
		o.IncludePackagePrefixes = append(o.IncludePackagePrefixes, splitOptionValues(value)...)
	case "include_service":
		// include_service 精确包含完整 service 名称。
		o.IncludeServices = append(o.IncludeServices, splitOptionValues(value)...)
	default:
		// 未知参数直接失败，防止配置拼写错误被静默忽略。
		return fmt.Errorf("unknown option %q", key)
	}
	// 当前参数应用成功。
	return nil
}

// normalize 规范化 Options 字段，保证输出稳定。
func (o *Options) normalize() {
	// include package 列表去空白、去重并排序。
	o.IncludePackages = uniqueSorted(o.IncludePackages)
	// include package prefix 列表去空白、去重并排序。
	o.IncludePackagePrefixes = uniqueSorted(o.IncludePackagePrefixes)
	// include service 列表去空白、去重并排序。
	o.IncludeServices = uniqueSorted(o.IncludeServices)
}

// hasInclude 判断调用方是否提供了任意 include 条件。
func (o Options) hasInclude() bool {
	// 三类 include 任意一个非空，都表示调用方显式声明了生成范围。
	return len(o.IncludePackages) > 0 || len(o.IncludePackagePrefixes) > 0 || len(o.IncludeServices) > 0
}

// isListOption 判断参数是否可以接收多个值。
func isListOption(key string) bool {
	// 只有 include 相关参数属于列表参数。
	switch key {
	case "include_package", "include_package_prefix", "include_service":
		// 返回 true 表示后续裸 token 可以继续追加到该列表。
		return true
	default:
		// 其它参数都是标量参数。
		return false
	}
}

// splitOptionValues 拆分单个列表参数值。
func splitOptionValues(value string) []string {
	// values 收集清理后的非空值。
	var values []string
	// 这里支持 ; 和 |，逗号由 ParseOptions 顶层分隔逻辑解析。
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == '|'
	}) {
		// 每个列表值都裁剪空白。
		part = strings.TrimSpace(part)
		// 空值不进入最终列表。
		if part != "" {
			values = append(values, part)
		}
	}
	// 返回拆分后的值列表。
	return values
}

// uniqueSorted 对字符串列表做去空白、去重和排序。
func uniqueSorted(values []string) []string {
	// 空输入统一返回空数组，避免 JSON 中出现 null。
	if len(values) == 0 {
		return []string{}
	}
	// seen 用作去重集合。
	seen := make(map[string]struct{}, len(values))
	// 遍历原始值并清理空白。
	for _, value := range values {
		// 裁剪每个值的首尾空白。
		value = strings.TrimSpace(value)
		// 空字符串不参与过滤匹配。
		if value == "" {
			continue
		}
		// map value 使用空结构体，表示只关心 key 是否存在。
		seen[value] = struct{}{}
	}
	// result 预分配为去重后的大小。
	result := make([]string, 0, len(seen))
	// map 遍历顺序随机，因此先收集再排序。
	for value := range seen {
		result = append(result, value)
	}
	// 字符串升序排序保证 manifest.filter 输出稳定。
	sort.Strings(result)
	// 返回规范化后的列表。
	return result
}
