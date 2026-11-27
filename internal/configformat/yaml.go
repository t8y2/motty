package configformat

import (
	"errors"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var yamlLineErrPattern = regexp.MustCompile(`line (\d+):`)

func formatYAML(src string, indentWidth int) (string, error) {
	normalized := normalizeLineEndings(src)
	if strings.TrimSpace(normalized) == "" {
		return normalized, nil
	}

	// 先使用 yaml.v3 校验全部文档语法，保留空行与注释的工作交由下方的缩进规范化器完成。
	if err := validateYAMLSyntax(normalized); err != nil {
		return "", err
	}

	formatted := normalizeYAMLIndentation(normalized, indentWidth)
	// 二次安全校验：缩进规范化只能改变空白，绝不能改变解码后的值
	// （例如带显式缩进指示符的块标量 "|2"）；不一致就原样返回。
	if !sameYAMLValues(normalized, formatted) {
		return normalized, nil
	}
	return formatted, nil
}

// sameYAMLValues reports whether both sources are valid YAML that decode to the
// same documents.
func sameYAMLValues(a, b string) bool {
	va, err := decodeYAMLDocuments(a)
	if err != nil {
		return false
	}
	vb, err := decodeYAMLDocuments(b)
	return err == nil && reflect.DeepEqual(va, vb)
}

func decodeYAMLDocuments(src string) ([]any, error) {
	dec := yaml.NewDecoder(strings.NewReader(src))
	var docs []any
	for {
		var doc any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
}

func validateYAMLSyntax(src string) error {
	dec := yaml.NewDecoder(strings.NewReader(src))
	for {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return yamlErrorToFormatError(err)
		}
	}
	return nil
}

type yamlIndentLevel struct {
	raw  int
	norm int
}

// normalizeYAMLIndentation 只规范每行的缩进宽度，完整保留所有空行、注释、键顺序与标量样式。
func normalizeYAMLIndentation(src string, indentWidth int) string {
	hadTrailingNewline := strings.HasSuffix(src, "\n")
	lines := strings.Split(src, "\n")
	if hadTrailingNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	stack := []yamlIndentLevel{{raw: 0, norm: 0}}

	resolveIndent := func(rawIndent int) int {
		for len(stack) > 1 && rawIndent < stack[len(stack)-1].raw {
			stack = stack[:len(stack)-1]
		}
		top := stack[len(stack)-1]
		if rawIndent > top.raw {
			norm := top.norm + indentWidth
			stack = append(stack, yamlIndentLevel{raw: rawIndent, norm: norm})
			return norm
		}
		return top.norm
	}

	out := make([]string, len(lines))
	var (
		inBlock        bool
		blockHeaderRaw int
		blockBodyRaw   int
		blockTarget    int
	)

	for i, line := range lines {
		rawIndent := countLeadingSpaces(line)
		trimmedLeft := strings.TrimLeft(line, " \t")
		trimmed := strings.TrimRight(trimmedLeft, " \t")

		if inBlock {
			if trimmed == "" {
				out[i] = ""
				continue
			}
			if rawIndent > blockHeaderRaw {
				rel := max(0, rawIndent-blockBodyRaw)
				// 块标量内部保留原始内容（包括刻意保留的行尾空格）。
				out[i] = strings.Repeat(" ", blockTarget+rel) + trimmedLeft
				continue
			}
			inBlock = false
		}

		if trimmed == "" {
			out[i] = ""
			continue
		}
		if trimmed == "---" || trimmed == "..." {
			stack = stack[:1]
			out[i] = trimmed
			continue
		}

		var targetIndent int
		if strings.HasPrefix(trimmed, "#") {
			// 注释行按当前已知缩进栈映射，不改变缩进栈结构。
			targetIndent = mapCommentIndent(stack, rawIndent, indentWidth)
		} else {
			targetIndent = resolveIndent(rawIndent)
			// 序列项带同行内容（如 "- path: /upload"）时，同一项的后续键必须与 "-" 后的内容列对齐。
			if seqPrefix := yamlSeqInlinePrefixLen(trimmed); seqPrefix > 0 {
				stack = append(stack, yamlIndentLevel{
					raw:  rawIndent + seqPrefix,
					norm: targetIndent + seqPrefix,
				})
			}
			if isYAMLBlockScalarHeader(trimmed) {
				inBlock = true
				blockHeaderRaw = rawIndent
				blockBodyRaw = minYAMLBlockBodyIndent(lines, i+1, rawIndent)
				blockTarget = targetIndent + indentWidth
			}
		}
		out[i] = strings.Repeat(" ", targetIndent) + trimmed
	}

	result := strings.Join(out, "\n")
	if hadTrailingNewline || result != "" {
		result += "\n"
	}
	return result
}

// minYAMLBlockBodyIndent 预扫描块标量所有非空内容行的最小原始缩进，确保相对缩进不失真。
func minYAMLBlockBodyIndent(lines []string, start, headerRaw int) int {
	minIndent := -1
	for j := start; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		ind := countLeadingSpaces(lines[j])
		if ind <= headerRaw {
			break
		}
		if minIndent < 0 || ind < minIndent {
			minIndent = ind
		}
	}
	if minIndent < 0 {
		return headerRaw + 1
	}
	return minIndent
}

// yamlSeqInlinePrefixLen 返回 "- <content>" 序列项前缀长度（含 '-' 与其后的空白），
// 若不是带同行内容的序列项则返回 0。
func yamlSeqInlinePrefixLen(trimmed string) int {
	if len(trimmed) < 2 || trimmed[0] != '-' || (trimmed[1] != ' ' && trimmed[1] != '\t') {
		return 0
	}
	i := 1
	for i < len(trimmed) && (trimmed[i] == ' ' || trimmed[i] == '\t') {
		i++
	}
	if i >= len(trimmed) || trimmed[i] == '#' {
		return 0
	}
	return i
}

func mapCommentIndent(stack []yamlIndentLevel, rawIndent, indentWidth int) int {
	if rawIndent == 0 {
		return 0
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if rawIndent == stack[i].raw {
			return stack[i].norm
		}
		if rawIndent > stack[i].raw {
			return stack[i].norm + indentWidth
		}
	}
	return 0
}

func countLeadingSpaces(line string) int {
	n := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			n++
		case '\t':
			n += 2
		default:
			return n
		}
	}
	return n
}

func yamlErrorToFormatError(err error) error {
	msg := err.Error()
	if m := yamlLineErrPattern.FindStringSubmatch(msg); len(m) == 2 {
		if line, convErr := strconv.Atoi(m[1]); convErr == nil && line > 0 {
			return &FormatError{Line: line, Column: 1, Message: msg}
		}
	}
	return &FormatError{Line: 1, Column: 1, Message: msg}
}

func foldYAML(src string) []FoldRange {
	src = normalizeLineEndings(src)
	lines := strings.Split(src, "\n")
	if len(lines) < 2 {
		return nil
	}

	type lineInfo struct {
		indent  int
		blank   bool
		comment bool
		block   bool
	}

	infos := make([]lineInfo, len(lines))
	for i, line := range lines {
		indent := countYAMLIndent(line)
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			infos[i] = lineInfo{indent: indent, blank: true}
		case strings.HasPrefix(trimmed, "#"):
			infos[i] = lineInfo{indent: indent, comment: true}
		default:
			infos[i] = lineInfo{
				indent: indent,
				block:  isYAMLBlockScalarHeader(trimmed),
			}
		}
	}

	var ranges []FoldRange
	for i := 0; i < len(lines); i++ {
		if infos[i].blank || infos[i].comment {
			continue
		}
		baseIndent := infos[i].indent
		lastContent := -1
		for j := i + 1; j < len(lines); j++ {
			if infos[j].blank || infos[j].comment {
				continue
			}
			// 列表项允许与父 key 同缩进（如 key:\n- item）。
			trimmed := strings.TrimSpace(lines[j])
			if infos[j].indent > baseIndent || (!infos[i].block && infos[j].indent == baseIndent && strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(strings.TrimSpace(lines[i]), "-")) {
				lastContent = j
				continue
			}
			break
		}
		if lastContent > i {
			ranges = append(ranges, FoldRange{Header: i, LastLine: lastContent})
		}
	}
	return ranges
}

func countYAMLIndent(line string) int {
	return countLeadingSpaces(line)
}

func isYAMLBlockScalarHeader(trimmed string) bool {
	// 去掉未加引号的行内注释
	s := stripYAMLInlineComment(trimmed)
	if s == "" {
		return false
	}
	// 支持纯序列项块标量，例如 "- |"、"- >-"
	if seqPrefix := yamlSeqInlinePrefixLen(s); seqPrefix > 0 {
		if isYAMLBlockIndicator(strings.TrimSpace(s[seqPrefix:])) {
			return true
		}
	}
	// 查找未加引号的键值冒号（':' 后接空白或行尾）
	colon := findYAMLKeyColon(s)
	if colon < 0 {
		return false
	}
	return isYAMLBlockIndicator(strings.TrimSpace(s[colon+1:]))
}

func isYAMLBlockIndicator(s string) bool {
	if s == "" || (s[0] != '|' && s[0] != '>') {
		return false
	}
	// YAML 块标量指示符后只允许可选的 chomping (+ / -) 与缩进数字 (1-9)
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c != '+' && c != '-' && (c < '1' || c > '9') {
			return false
		}
	}
	return true
}

func stripYAMLInlineComment(s string) string {
	inSQ, inDQ, esc := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inDQ {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inDQ = false
			}
			continue
		}
		if inSQ {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inSQ = false
			}
			continue
		}
		switch c {
		case '"':
			inDQ = true
		case '\'':
			inSQ = true
		case '#':
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return strings.TrimSpace(s[:i])
			}
		}
	}
	return strings.TrimSpace(s)
}

func findYAMLKeyColon(s string) int {
	inSQ, inDQ, esc := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inDQ {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inDQ = false
			}
			continue
		}
		if inSQ {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inSQ = false
			}
			continue
		}
		switch c {
		case '"':
			inDQ = true
		case '\'':
			inSQ = true
		case ':':
			if i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t' {
				return i
			}
		}
	}
	return -1
}
