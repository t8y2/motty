package configformat

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func formatJSON(src string, indentWidth int) (string, error) {
	normalized := normalizeLineEndings(src)
	if strings.TrimSpace(normalized) == "" {
		return normalized, nil
	}

	// 先校验是否包含多余的尾随内容，避免 json.Indent 静默截断或跳过异常。
	dec := json.NewDecoder(strings.NewReader(normalized))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return "", jsonErrorToFormatError(normalized, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			offset := int(dec.InputOffset())
			line, col := lineColFromOffset(normalized, offset)
			return "", &FormatError{Line: line, Column: col, Message: "unexpected trailing JSON value"}
		}
		return "", jsonErrorToFormatError(normalized, err)
	}

	var buf bytes.Buffer
	buf.Grow(len(normalized) + len(normalized)/4)
	indent := strings.Repeat(" ", indentWidth)
	if err := json.Indent(&buf, []byte(normalized), "", indent); err != nil {
		return "", jsonErrorToFormatError(normalized, err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, nil
}

func jsonErrorToFormatError(src string, err error) error {
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		// json.SyntaxError.Offset 是 1-based 已读字节数。
		offset := int(syn.Offset) - 1
		if offset < 0 {
			offset = 0
		}
		line, col := lineColFromOffset(src, offset)
		return &FormatError{Line: line, Column: col, Message: syn.Error()}
	}
	return &FormatError{Line: 1, Column: 1, Message: err.Error()}
}

func foldJSON(src string) []FoldRange {
	src = normalizeLineEndings(src)
	type bracketFrame struct {
		opener byte
		line   int
	}
	var stack []bracketFrame
	var ranges []FoldRange
	line := 0
	inString := false
	escaped := false

	for i := 0; i < len(src); i++ {
		ch := src[i]
		if ch == '\n' {
			line++
			escaped = false
			continue
		}
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, bracketFrame{opener: ch, line: line})
		case '}', ']':
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			if (ch == '}' && top.opener == '{') || (ch == ']' && top.opener == '[') {
				stack = stack[:len(stack)-1]
				if line > top.line {
					// 保留闭合符号行可见，使折叠结构保持清晰。
					last := line - 1
					if last > top.line {
						ranges = append(ranges, FoldRange{Header: top.line, LastLine: last})
					}
				}
			}
		}
	}
	return ranges
}
