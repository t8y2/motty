package configformat

import (
	"errors"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func formatTOML(src string, indentWidth int) (string, error) {
	normalized := normalizeLineEndings(src)
	if strings.TrimSpace(normalized) == "" {
		return normalized, nil
	}
	if err := validateTOMLSyntax(normalized); err != nil {
		return "", err
	}
	formatted := formatValidTOML(normalized, indentWidth)
	// 二次安全校验：确保格式化绝不会破坏原本合法的 TOML 语义。
	if err := validateTOMLSyntax(formatted); err != nil {
		return normalized, nil
	}
	return formatted, nil
}

// validateTOMLSyntax 使用标准 TOML 解析器（go-toml/v2）校验语法、字面量及重复键等语义错误。
func validateTOMLSyntax(src string) error {
	var doc any
	err := toml.Unmarshal([]byte(src), &doc)
	if err == nil {
		return nil
	}
	var derr *toml.DecodeError
	if errors.As(err, &derr) {
		line, col := derr.Position()
		if line < 1 {
			line = 1
		}
		if col < 1 {
			col = 1
		}
		return &FormatError{Line: line, Column: col, Message: derr.Error()}
	}
	return &FormatError{Line: 1, Column: 1, Message: err.Error()}
}

func formatValidTOML(src string, indentWidth int) string {
	lines := strings.Split(src, "\n")
	// 去除末尾多余空行，最后统一补一个换行符
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	unit := strings.Repeat(" ", indentWidth)
	var out []string
	var (
		inMLBasic  bool
		inMLLit    bool
		arrayDepth int
		prevBlank  bool
	)

	for _, raw := range lines {
		if inMLBasic {
			out = append(out, raw)
			if pos := findClosingTripleQuote(raw, '"'); pos >= 0 {
				inMLBasic = false
				delta, opensMLBasic, opensMLLit := scanTOMLDelimDelta(raw[pos+3:])
				arrayDepth = max(0, arrayDepth+delta)
				inMLBasic = opensMLBasic
				inMLLit = opensMLLit
			}
			prevBlank = false
			continue
		}
		if inMLLit {
			out = append(out, raw)
			if pos := findClosingTripleQuote(raw, '\''); pos >= 0 {
				inMLLit = false
				delta, opensMLBasic, opensMLLit := scanTOMLDelimDelta(raw[pos+3:])
				arrayDepth = max(0, arrayDepth+delta)
				inMLBasic = opensMLBasic
				inMLLit = opensMLLit
			}
			prevBlank = false
			continue
		}

		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			if len(out) > 0 && !prevBlank {
				out = append(out, "")
				prevBlank = true
			}
			continue
		}

		if arrayDepth > 0 {
			// 多行数组内部：若本行以闭合 ']' 开头，则提前减少缩进层级。
			indentDepth := arrayDepth
			if strings.HasPrefix(trimmed, "]") {
				indentDepth = max(0, arrayDepth-1)
			}
			formattedLine := strings.Repeat(unit, indentDepth) + formatTOMLInlineSpacing(trimmed)
			out = append(out, formattedLine)
			prevBlank = false
			delta, opensMLBasic, opensMLLit := scanTOMLDelimDelta(trimmed)
			arrayDepth = max(0, arrayDepth+delta)
			inMLBasic = opensMLBasic
			inMLLit = opensMLLit
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			out = append(out, trimmed)
			prevBlank = false
			continue
		}

		if strings.HasPrefix(trimmed, "[") {
			// 表头之间保留单空行（若上一行不是注释且不是空行且不是文件首行）。
			if len(out) > 0 && !prevBlank && !strings.HasPrefix(strings.TrimSpace(out[len(out)-1]), "#") {
				out = append(out, "")
			}
			out = append(out, formatTOMLTableHeaderLine(trimmed))
			prevBlank = false
			continue
		}

		eqPos := findUnquotedByte(trimmed, '=')
		if eqPos > 0 {
			key := formatTOMLDottedKey(strings.TrimSpace(trimmed[:eqPos]))
			val := formatTOMLInlineSpacing(strings.TrimSpace(trimmed[eqPos+1:]))
			out = append(out, key+" = "+val)
			prevBlank = false
			delta, opensMLBasic, opensMLLit := scanTOMLDelimDelta(val)
			arrayDepth = max(0, arrayDepth+delta)
			inMLBasic = opensMLBasic
			inMLLit = opensMLLit
			continue
		}

		out = append(out, trimmed)
		prevBlank = false
	}

	return strings.Join(out, "\n") + "\n"
}

func formatTOMLTableHeaderLine(trimmed string) string {
	isArray := strings.HasPrefix(trimmed, "[[")
	openTok, closeTok := "[", "]"
	if isArray {
		openTok, closeTok = "[[", "]]"
	}
	rest := trimmed[len(openTok):]
	end := findUnquotedSubstring(rest, closeTok)
	if end < 0 {
		return trimmed
	}
	headerKey := formatTOMLDottedKey(strings.TrimSpace(rest[:end]))
	after := strings.TrimSpace(rest[end+len(closeTok):])
	if after != "" {
		return openTok + headerKey + closeTok + " " + after
	}
	return openTok + headerKey + closeTok
}

func formatTOMLDottedKey(key string) string {
	parts := splitTOMLDottedKey(key)
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ".")
}

func splitTOMLDottedKey(key string) []string {
	var parts []string
	start := 0
	inDQ, inSQ, esc := false, false, false
	for i := 0; i < len(key); i++ {
		c := key[i]
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
				inSQ = false
			}
			continue
		}
		switch c {
		case '"':
			inDQ = true
		case '\'':
			inSQ = true
		case '.':
			parts = append(parts, key[start:i])
			start = i + 1
		}
	}
	parts = append(parts, key[start:])
	return parts
}

func formatTOMLInlineSpacing(s string) string {
	// 规范内联逗号/等号外部空格，保留字符串、多行字符串与注释原文。
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == '#' {
			// 保证行内注释前至少有一个空格
			if b.Len() > 0 && s[i-1] != ' ' && s[i-1] != '\t' {
				b.WriteByte(' ')
			}
			b.WriteString(s[i:])
			break
		}
		if strings.HasPrefix(s[i:], `"""`) {
			closePos := findClosingTripleQuote(s[i+3:], '"')
			if closePos >= 0 {
				b.WriteString(s[i : i+3+closePos+3])
				i += 3 + closePos + 3
				continue
			}
			b.WriteString(s[i:])
			break
		}
		if strings.HasPrefix(s[i:], `'''`) {
			closePos := findClosingTripleQuote(s[i+3:], '\'')
			if closePos >= 0 {
				b.WriteString(s[i : i+3+closePos+3])
				i += 3 + closePos + 3
				continue
			}
			b.WriteString(s[i:])
			break
		}
		if ch == '"' {
			end, ok := scanBasicStringEnd(s, i+1)
			if ok {
				b.WriteString(s[i:end])
				i = end
				continue
			}
			b.WriteString(s[i:])
			break
		}
		if ch == '\'' {
			end, ok := scanLiteralStringEnd(s, i+1)
			if ok {
				b.WriteString(s[i:end])
				i = end
				continue
			}
			b.WriteString(s[i:])
			break
		}
		if ch == ',' {
			b.WriteByte(',')
			i++
			for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			if i < len(s) && s[i] != ']' && s[i] != '}' && s[i] != '#' {
				b.WriteByte(' ')
			}
			continue
		}
		b.WriteByte(ch)
		i++
	}
	return b.String()
}

func scanTOMLDelimDelta(s string) (arrayDelta int, opensMLBasic, opensMLLit bool) {
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == '#' {
			break
		}
		if strings.HasPrefix(s[i:], `"""`) {
			closePos := findClosingTripleQuote(s[i+3:], '"')
			if closePos >= 0 {
				i += 3 + closePos + 3
				continue
			}
			return arrayDelta, true, false
		}
		if strings.HasPrefix(s[i:], `'''`) {
			closePos := findClosingTripleQuote(s[i+3:], '\'')
			if closePos >= 0 {
				i += 3 + closePos + 3
				continue
			}
			return arrayDelta, false, true
		}
		if ch == '"' {
			if end, ok := scanBasicStringEnd(s, i+1); ok {
				i = end
				continue
			}
			break
		}
		if ch == '\'' {
			if end, ok := scanLiteralStringEnd(s, i+1); ok {
				i = end
				continue
			}
			break
		}
		switch ch {
		case '[':
			arrayDelta++
		case ']':
			arrayDelta--
		}
		i++
	}
	return arrayDelta, false, false
}

func foldTOML(src string) []FoldRange {
	src = normalizeLineEndings(src)
	lines := strings.Split(src, "\n")
	if len(lines) < 2 {
		return nil
	}

	type sectionFrame struct {
		path []string
		line int
	}

	var (
		ranges       []FoldRange
		sectionStack []sectionFrame
		arrayStack   []int
		inMLBasic    bool
		mlBasicStart int
		inMLLit      bool
		mlLitStart   int
		lastNonEmpty = -1
	)

	closeSectionsFor := func(nextPath []string, closeAt int) {
		for len(sectionStack) > 0 {
			top := sectionStack[len(sectionStack)-1]
			if nextPath != nil && isStrictTOMLPrefix(top.path, nextPath) {
				break
			}
			sectionStack = sectionStack[:len(sectionStack)-1]
			if closeAt > top.line {
				ranges = append(ranges, FoldRange{Header: top.line, LastLine: closeAt})
			}
		}
	}

	for lineIdx, raw := range lines {
		if inMLBasic {
			lastNonEmpty = lineIdx
			if findClosingTripleQuote(raw, '"') >= 0 {
				inMLBasic = false
				if lineIdx > mlBasicStart {
					ranges = append(ranges, FoldRange{Header: mlBasicStart, LastLine: lineIdx})
				}
			}
			continue
		}
		if inMLLit {
			lastNonEmpty = lineIdx
			if findClosingTripleQuote(raw, '\'') >= 0 {
				inMLLit = false
				if lineIdx > mlLitStart {
					ranges = append(ranges, FoldRange{Header: mlLitStart, LastLine: lineIdx})
				}
			}
			continue
		}

		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		// 若在顶层遇到新的表头，先收口不以新表头为子孙的父表头区间。
		if len(arrayStack) == 0 && strings.HasPrefix(trimmed, "[") {
			if path, ok := parseTOMLTablePath(trimmed); ok {
				closeSectionsFor(path, lastNonEmpty)
				sectionStack = append(sectionStack, sectionFrame{path: path, line: lineIdx})
				lastNonEmpty = lineIdx
				continue
			}
		}

		lastNonEmpty = lineIdx

		// 扫描行内的多行数组与多行字符串起止
		for i := 0; i < len(raw); i++ {
			ch := raw[i]
			if ch == '#' {
				break
			}
			if strings.HasPrefix(raw[i:], `"""`) {
				closePos := findClosingTripleQuote(raw[i+3:], '"')
				if closePos >= 0 {
					i += 3 + closePos + 2
					continue
				}
				inMLBasic = true
				mlBasicStart = lineIdx
				break
			}
			if strings.HasPrefix(raw[i:], `'''`) {
				closePos := findClosingTripleQuote(raw[i+3:], '\'')
				if closePos >= 0 {
					i += 3 + closePos + 2
					continue
				}
				inMLLit = true
				mlLitStart = lineIdx
				break
			}
			if ch == '"' {
				if end, ok := scanBasicStringEnd(raw, i+1); ok {
					i = end - 1
					continue
				}
				break
			}
			if ch == '\'' {
				if end, ok := scanLiteralStringEnd(raw, i+1); ok {
					i = end - 1
					continue
				}
				break
			}
			switch ch {
			case '[':
				arrayStack = append(arrayStack, lineIdx)
			case ']':
				if len(arrayStack) > 0 {
					startLine := arrayStack[len(arrayStack)-1]
					arrayStack = arrayStack[:len(arrayStack)-1]
					if lineIdx > startLine+1 {
						ranges = append(ranges, FoldRange{Header: startLine, LastLine: lineIdx - 1})
					}
				}
			}
		}
	}

	closeSectionsFor(nil, lastNonEmpty)
	return ranges
}

func parseTOMLTablePath(trimmed string) ([]string, bool) {
	isArray := strings.HasPrefix(trimmed, "[[")
	openLen, closeTok := 1, "]"
	if isArray {
		openLen, closeTok = 2, "]]"
	}
	rest := trimmed[openLen:]
	end := findUnquotedSubstring(rest, closeTok)
	if end < 0 {
		return nil, false
	}
	rawPath := strings.TrimSpace(rest[:end])
	if rawPath == "" {
		return nil, false
	}
	parts := splitTOMLDottedKey(rawPath)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && ((p[0] == '"' && p[len(p)-1] == '"') || (p[0] == '\'' && p[len(p)-1] == '\'')) {
			p = p[1 : len(p)-1]
		}
		out = append(out, p)
	}
	return out, len(out) > 0
}

func isStrictTOMLPrefix(parent, child []string) bool {
	if len(parent) >= len(child) {
		return false
	}
	for i := range parent {
		if parent[i] != child[i] {
			return false
		}
	}
	return true
}

func findClosingTripleQuote(s string, quote byte) int {
	tok := string([]byte{quote, quote, quote})
	for i := 0; i <= len(s)-3; i++ {
		if quote == '"' && s[i] == '\\' {
			i++
			continue
		}
		if strings.HasPrefix(s[i:], tok) {
			// TOML 1.0 允许在闭合三引号前紧贴最多 2 个同类引号（共 4~5 个连续引号），
			// 因此闭合三引号位于连续引号段的最后 3 个字符。
			end := i + 3
			for end < len(s) && s[end] == quote && end-i < 5 {
				end++
			}
			return end - 3
		}
	}
	return -1
}

func scanBasicStringEnd(s string, start int) (int, bool) {
	esc := false
	for i := start; i < len(s); i++ {
		if esc {
			esc = false
			continue
		}
		if s[i] == '\\' {
			esc = true
			continue
		}
		if s[i] == '"' {
			return i + 1, true
		}
	}
	return len(s), false
}

func scanLiteralStringEnd(s string, start int) (int, bool) {
	for i := start; i < len(s); i++ {
		if s[i] == '\'' {
			return i + 1, true
		}
	}
	return len(s), false
}

func findUnquotedByte(s string, target byte) int {
	inDQ, inSQ, esc := false, false, false
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
				inSQ = false
			}
			continue
		}
		if c == '#' {
			return -1
		}
		if c == '"' {
			inDQ = true
			continue
		}
		if c == '\'' {
			inSQ = true
			continue
		}
		if c == target {
			return i
		}
	}
	return -1
}

func findUnquotedSubstring(s, sub string) int {
	inDQ, inSQ, esc := false, false, false
	for i := 0; i <= len(s)-len(sub); i++ {
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
				inSQ = false
			}
			continue
		}
		if c == '"' {
			inDQ = true
			continue
		}
		if c == '\'' {
			inSQ = true
			continue
		}
		if strings.HasPrefix(s[i:], sub) {
			return i
		}
	}
	return -1
}

func stripTOMLComment(s string) string {
	if idx := findUnquotedByte(s, '#'); idx >= 0 {
		return s[:idx]
	}
	return s
}
