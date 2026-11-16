package workbench

import (
	"github.com/vivaui/viva/text"
	"strings"
	"testing"
)

func TestNotesTableAndRuleFormatting(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	e := s.EditorFor(hitNoteBody)
	e.Insert("前文甲|乙\n丙后文")
	original := e.String()
	e.SetSelection(text.Range{Start: 2, End: 7})
	s.Click(hitNoteFormats.At(10))
	if !strings.Contains(e.String(), "前文\n\n| ") || !strings.Contains(e.String(), "| --- | --- | --- |") || !strings.Contains(e.String(), `甲\|乙<br>丙`) || !strings.HasSuffix(e.String(), "\n\n后文") {
		t.Fatalf("table block %q", e.String())
	}
	rows := 0
	for _, line := range strings.Split(e.String(), "\n") {
		if strings.HasPrefix(line, "| ") {
			rows++
		}
	}
	if rows != 4 || !strings.Contains(e.String(), "|  |  |  |") {
		t.Fatal("table should have three columns and three visible rows")
	}
	if !e.Undo() || e.String() != original {
		t.Fatal("table insertion did not undo as one edit")
	}
	e.MoveTo(e.RuneCount(), false)
	s.Click(hitNoteFormats.At(11))
	if e.String() != original+"\n\n---\n\n" {
		t.Fatalf("rule block %q", e.String())
	}
	if e.Selection().Start != e.RuneCount() || e.Selection().End != e.RuneCount() {
		t.Fatal("rule did not leave cursor after block")
	}
	if !e.Undo() || e.String() != original {
		t.Fatal("rule insertion did not undo as one edit")
	}
	e.SetMarkedText("输入", text.Range{End: 2})
	before := e.String()
	s.Click(hitNoteFormats.At(10))
	s.Click(hitNoteFormats.At(11))
	if e.String() != before {
		t.Fatal("block formatting modified composition")
	}
}
