package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
)

func (d *fileDocument) highlight(language, line string, dst []components.CodeEditorRun) []components.CodeEditorRun {
	d.ui.marks = highlightCode(language, line, d.ui.marks[:0])
	colors := d.codeColors()
	for _, run := range d.ui.marks {
		if int(run.Class) < len(colors) {
			dst = append(dst, components.CodeEditorRun{Start: run.Start, End: run.End, Color: colors[run.Class]})
		}
	}
	return dst
}

func highlightXML(line string, dst []components.MarkdownCodeRun) []components.MarkdownCodeRun {
	inside := false
	for i := 0; i < len(line); {
		if strings.HasPrefix(line[i:], "<!--") {
			end := len(line)
			if n := strings.Index(line[i+4:], "-->"); n >= 0 {
				end = i + 4 + n + 3
			}
			dst = append(dst, components.MarkdownCodeRun{Start: i, End: end, Class: components.CodeComment})
			i = end
			continue
		}
		if line[i] == '<' {
			start := i
			i++
			for i < len(line) && (line[i] == '/' || line[i] == '?' || line[i] == '!') {
				i++
			}
			for i < len(line) && line[i] != ' ' && line[i] != '\t' && line[i] != '>' {
				i++
			}
			dst = append(dst, components.MarkdownCodeRun{Start: start, End: i, Class: components.CodeKeyword})
			inside = true
			continue
		}
		if inside && (line[i] == '\'' || line[i] == '"') {
			end := stringEnd(line, i)
			dst = append(dst, components.MarkdownCodeRun{Start: i, End: end, Class: components.CodeString})
			i = end
			continue
		}
		if line[i] == '>' {
			inside = false
			i++
			continue
		}
		if inside && (isCodeLetter(line[i]) || line[i] == '_') {
			start := i
			for i < len(line) && isCodeWord(line[i], "-:") {
				i++
			}
			if strings.HasPrefix(strings.TrimLeft(line[i:], " \t"), "=") {
				dst = append(dst, components.MarkdownCodeRun{Start: start, End: i, Class: components.CodeKey})
			}
			continue
		}
		i++
	}
	return dst
}
