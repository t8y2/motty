package configformat

import (
	"fmt"
	"sort"
	"strings"
)

// Kind 表示受支持的配置格式类型。
type Kind string

const (
	KindNone Kind = ""
	KindJSON Kind = "json"
	KindYAML Kind = "yaml"
	KindTOML Kind = "toml"
)

// FormatError 表示配置语法错误及精确定位（1-based 行列号）。
type FormatError struct {
	Line    int
	Column  int
	Message string
}

func (e *FormatError) Error() string {
	if e == nil {
		return ""
	}
	if e.Line > 0 && e.Column > 0 {
		return fmt.Sprintf("line %d, col %d: %s", e.Line, e.Column, e.Message)
	}
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Message)
	}
	return e.Message
}

// FoldRange 表示一个可折叠的逻辑行区间（0-based，与 Viva CodeEditorFoldRange 对齐）。
// 折叠时保留 Header 行可见，隐藏 [Header+1, LastLine] 闭区间。
type FoldRange struct {
	Header   int
	LastLine int
}

// DetectKind 根据语言标识识别配置格式。
func DetectKind(language string) Kind {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "json":
		return KindJSON
	case "yaml", "yml":
		return KindYAML
	case "toml":
		return KindTOML
	default:
		return KindNone
	}
}

// Format 按指定缩进宽度美化配置文本（indentWidth <= 0 时默认 2 空格），严格保留空行、注释与键顺序。
func Format(kind Kind, src string, indentWidth int) (string, error) {
	if indentWidth <= 0 {
		indentWidth = 2
	}
	switch kind {
	case KindJSON:
		return formatJSON(src, indentWidth)
	case KindYAML:
		return formatYAML(src, indentWidth)
	case KindTOML:
		return formatTOML(src, indentWidth)
	default:
		return src, nil
	}
}

// FoldRanges 解析配置文本中的所有可折叠层级区间，结果按 Header 升序排列且去重。
func FoldRanges(kind Kind, src string) []FoldRange {
	var ranges []FoldRange
	switch kind {
	case KindJSON:
		ranges = foldJSON(src)
	case KindYAML:
		ranges = foldYAML(src)
	case KindTOML:
		ranges = foldTOML(src)
	default:
		return nil
	}
	return normalizeFoldRanges(ranges)
}

func normalizeFoldRanges(ranges []FoldRange) []FoldRange {
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Header != ranges[j].Header {
			return ranges[i].Header < ranges[j].Header
		}
		return ranges[i].LastLine > ranges[j].LastLine
	})
	out := ranges[:0]
	for _, r := range ranges {
		if r.Header < 0 || r.LastLine <= r.Header {
			continue
		}
		if len(out) > 0 && out[len(out)-1].Header == r.Header {
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// lineColFromOffset 将 0-based 字节偏移转换为 1-based 行号与列号。
func lineColFromOffset(src string, offset int) (line, col int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(src) {
		offset = len(src)
	}
	line = 1
	lastNL := -1
	for i := 0; i < offset; i++ {
		if src[i] == '\n' {
			line++
			lastNL = i
		}
	}
	col = offset - lastNL
	if col < 1 {
		col = 1
	}
	return line, col
}

// normalizeLineEndings 将文本统一为 LF 并保留末尾状态。
func normalizeLineEndings(src string) string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	return strings.ReplaceAll(src, "\r", "\n")
}
