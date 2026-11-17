package workbench

import (
	"testing"
	"unicode/utf8"
)

func TestNoteMarkdownHighlight(t *testing.T) {
	s, _, _, _ := liveSession(t)
	for _, line := range []string{"# 标题", "- [x] 完成", "中文 **加粗** 和 *斜体*", "`代码`", "[链接](https://example.com)", "| 列一 | 列二 |", "---", "```go"} {
		runs := s.highlightNoteMarkdown("markdown", line, nil)
		if len(runs) == 0 {
			t.Fatalf("no highlighting for %q", line)
		}
		end := 0
		for _, run := range runs {
			if run.Start < end || run.End <= run.Start || run.End > len(line) || !utf8.ValidString(line[run.Start:run.End]) {
				t.Fatalf("invalid byte range in %q: %+v", line, run)
			}
			end = run.End
		}
	}
	for _, line := range []string{"普通中文正文", `\*不是斜体\*`} {
		if runs := s.highlightNoteMarkdown("markdown", line, nil); len(runs) != 0 {
			t.Fatalf("plain or escaped text highlighted: %q", line)
		}
	}
}
