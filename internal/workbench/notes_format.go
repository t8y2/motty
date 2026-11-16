package workbench

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/workbench/icons"
)

var noteFormatLabels = []string{"H1", "B", "I", "`", "{ }", "•", "1.", "☑", "❞", "↗", "Table", "---"}

func noteFormatIcon(i int) gfx.Icon {
	switch i {
	case 2:
		return icons.Italic
	case 10:
		return icons.Table
	case 11:
		return icons.Minus
	}
	return gfx.Icon{}
}
func (s *State) noteFormatName(i int) string {
	en := []string{"Heading", "Bold", "Italic", "Inline code", "Code block", "Bullet list", "Numbered list", "Task list", "Quote", "Link", "Table", "Horizontal rule"}
	zh := []string{"标题", "加粗", "斜体", "行内代码", "代码块", "无序列表", "有序列表", "任务项", "引用", "链接", "表格", "分隔线"}
	if i < 0 || i >= len(en) {
		return ""
	}
	return s.noteText(en[i], zh[i])
}
func (s *State) formatNote(i int) {
	if i < 0 || i >= len(noteFormatLabels) || s.noteContent().composing() {
		return
	}
	if !s.activateNoteView(s.noteContent()) {
		return
	}
	e := &s.noteContent().body
	r := e.Selection()
	runes := []rune(e.String())
	start, end := min(r.Start, r.End), max(r.Start, r.End)
	selected := string(runes[start:end])
	prefix, suffix := "", ""
	switch i {
	case 0:
		prefix = "# "
	case 1:
		prefix = "**"
		suffix = "**"
	case 2:
		prefix = "*"
		suffix = "*"
	case 3:
		prefix = "`"
		suffix = "`"
	case 4:
		prefix = "```\n"
		suffix = "\n```"
	case 5:
		prefix = "- "
	case 6:
		prefix = "1. "
	case 7:
		prefix = "- [ ] "
	case 8:
		prefix = "> "
	case 9:
		prefix = "["
		suffix = "](https://)"
	case 10:
		prefix = "| " + s.noteText("Column 1", "列1") + " | " + s.noteText("Column 2", "列2") + " | " + s.noteText("Column 3", "列3") + " |\n| --- | --- | --- |\n| "
		if start > 0 && runes[start-1] != '\n' {
			prefix = "\n\n" + prefix
		}
		selected = strings.ReplaceAll(strings.ReplaceAll(selected, "|", `\|`), "\n", "<br>")
		if selected == "" {
			selected = s.noteText("Content", "内容")
		}
		suffix = " |  |  |\n|  |  |  |\n\n"
	case 11:
		suffix = "\n\n---\n\n"
		if start == 0 && selected == "" {
			suffix = "---\n\n"
		}
	}
	if i == 0 || i >= 4 && i <= 8 {
		for start > 0 && runes[start-1] != '\n' {
			start--
		}
		for end < len(runes) && runes[end] != '\n' {
			end++
		}
		selected = string(runes[start:end])
		if i != 4 {
			selected = prefix + strings.ReplaceAll(selected, "\n", "\n"+prefix)
			prefix = ""
			suffix = ""
		}
	}
	e.BreakUndoGroup()
	e.SetSelection(text.Range{Start: start, End: end})
	e.Insert(prefix + selected + suffix)
	e.BreakUndoGroup()
	e.SetSelection(text.Range{Start: start + len([]rune(prefix)), End: start + len([]rune(prefix+selected))})
	if i == 11 {
		cursor := start + len([]rune(prefix+selected+suffix))
		e.SetSelection(text.Range{Start: cursor, End: cursor})
	}
	s.noteContent().view.focus = hitNoteBody
	s.save()
}
