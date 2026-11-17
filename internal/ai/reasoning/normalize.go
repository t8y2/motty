package reasoning

import (
	"regexp"
	"strings"
)

var (
	// parentheticalRegex 匹配小括号内的注释或修饰，如 "o1-preview (free)"
	parentheticalRegex = regexp.MustCompile(`\s*\([^)]*\)`)

	// quantSuffixRegex 匹配量化或精度后缀，如 "-int4", "-int8", "-fp16", "-fp8", "-q4_k_m", "-q8_0"
	quantSuffixRegex = regexp.MustCompile(`(?i)[-_](?:(?:q\d+_[a-z0-9_]+)|(?:int[48])|(?:fp(?:8|16|32)))\b`)

	// tagSuffixRegex 匹配镜像或网关 tag，如 ":free", ":nitro", ":latest", ":online", 或 ":q4_k_m" 等
	tagSuffixRegex = regexp.MustCompile(`:(?:free|nitro|latest|online|[0-9a-z_]+)\b`)

	// vendorPrefixDotRegex 匹配类似 "zai.glm-5.3" 里的组织前缀（注意点号后必须以字母开头，避免误伤 3.5、5.3 等版本号）
	vendorPrefixDotRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+\.([a-zA-Z][a-zA-Z0-9_.-]*)`)
)

// CanonicalBaseName 对模型名称执行深度清洗与规范化：
// 1. 剥离多层目录路径（保留最后一个斜杠之后的部分）
// 2. 剥离小括号修饰语
// 3. 剥离量化标记及 tag 后缀
// 4. 去除可能的前置点号组织前缀
// 5. 转为全小写便于后续正则与语义匹配
func CanonicalBaseName(rawName string) string {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return ""
	}

	// 快速路径：若不含路径、括号、tag、量化及点号前缀，直接转小写返回
	if !strings.ContainsAny(name, "/(:.") && !strings.Contains(name, "-int") && !strings.Contains(name, "-fp") && !strings.Contains(name, "-q") && !strings.Contains(name, "_q") {
		return strings.ToLower(name)
	}

	// 1. 剥离路径前缀，例如 "accounts/fireworks/models/deepseek-v4-flash" -> "deepseek-v4-flash"
	if idx := strings.LastIndex(name, "/"); idx != -1 {
		name = name[idx+1:]
	}

	// 2. 剥离小括号修饰，例如 "o1-preview (free)" -> "o1-preview"
	name = parentheticalRegex.ReplaceAllString(name, "")

	// 3. 剥离 tag 后缀，例如 ":free", ":latest", ":14b-q4_k_m"
	name = tagSuffixRegex.ReplaceAllString(name, "")

	// 4. 剥离量化后缀，例如 "-int4", "-fp16", "-q4_k_m"
	name = quantSuffixRegex.ReplaceAllString(name, "")

	// 5. 剥离如 "zai.glm-5.3" 的点号前缀
	if m := vendorPrefixDotRegex.FindStringSubmatch(name); len(m) == 2 {
		name = m[1]
	}

	name = strings.TrimSpace(name)
	return strings.ToLower(name)
}
