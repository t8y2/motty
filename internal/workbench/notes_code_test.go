package workbench

import (
	"errors"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
)

func TestNoteCodeCopiesOriginalAndRejectsStaleTargets(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	code := "\t中文 := 123\n\n  // trailing spaces  \n"
	s.notes.body.Insert("```go\n" + code + "```\n\n```go\n" + code + "```\n")
	s.notes.view.mode = notePreview
	var c gfx.Canvas
	paint := func() { c.Reset(); s.Paint(&c, gfx.Size{Width: 1400, Height: 900}) }
	paint()
	n := &s.notes
	targets := n.view.codeActions.targets
	if len(targets) != 2 {
		t.Fatalf("copy targets: %d", len(targets))
	}
	first, second := hitNoteCodeCopies.At(targets[0].token), hitNoteCodeCopies.At(targets[1].token)
	var copied string
	s.options.SetClipboardText = func(value string) error { copied = value; return nil }
	before := n.body.String()
	if !s.clickNotes(first) || copied != code || n.body.String() != before {
		t.Fatalf("wrong code copied: %q", copied)
	}
	if n.view.codeActions.copied != first || n.view.codeActions.copied == second {
		t.Fatal("copy feedback not scoped to one block")
	}
	s.options.SetClipboardText = func(string) error { return errors.New("clipboard failed") }
	s.clickNotes(second)
	if n.error != "clipboard failed" || !n.view.codeActions.copied.IsZero() {
		t.Fatal("failed copy showed success")
	}
	n.body.Insert("more")
	copied = "unchanged"
	s.options.SetClipboardText = func(value string) error { copied = value; return nil }
	s.clickNotes(first)
	if copied != "unchanged" {
		t.Fatal("copied a stale block before repaint")
	}
	paint()
	s.clickNotes(first)
	if copied != "unchanged" {
		t.Fatal("reused old block token after repaint")
	}
	// Different previews have independent targets and feedback.
	side := s.noteSidebarState()
	side.content.selected = n.selected
	s.syncNoteSidebar()
	c.Reset()
	s.paintNotePreviewContent(&c, gfx.Rect{Width: 600, Height: 500}, &side.content)
	id := hitNoteCodeCopies.At(side.content.view.codeActions.targets[0].token)
	s.clickNoteCodeCopy(id, &side.content)
	if copied != code || side.content.view.codeActions.copied.IsZero() {
		t.Fatal("side preview copy failed")
	}
}

func TestNoteCodeHighlightAndCopyAffordance(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.body.Insert("```go\nvar value = 123\n```\n\n```unknown\nplain\n```\n")
	s.notes.view.mode = notePreview
	var c gfx.Canvas
	paint := func() { c.Reset(); s.Paint(&c, gfx.Size{Width: 1400, Height: 900}) }
	paint()
	n := &s.notes
	target := hitNoteCodeCopies.At(n.view.codeActions.targets[0].token)
	if hit, ok := hitByID(&c, target); !ok || !hit.Focusable {
		t.Fatal("hidden copy not keyboard reachable")
	}
	// Hover inside the block must request a paint even within the same preview region.
	b := n.previewBounds
	s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: b.X + 15, Y: b.Y + 15})
	if r := s.notesPointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: b.X + 25, Y: b.Y + 20}); !r.Repaint {
		t.Fatal("block hover did not repaint")
	}
	if len(highlightCode("go", "var value = 123", nil)) == 0 || len(highlightCode("unknown", "var value = 123", nil)) != 0 || len(highlightCode("", "var value = 123", nil)) != 0 {
		t.Fatal("language fallback changed")
	}
	s.options.SetClipboardText = func(string) error { return nil }
	now := time.Second
	s.options.Now = func() time.Duration { return now }
	s.notesKey(gfx.KeyEvent{Target: target, Key: gfx.KeyEnter})
	if n.view.codeActions.copied != target {
		t.Fatal("keyboard copy failed")
	}
	now += 2 * time.Second
	paint()
	if hit, ok := hitByID(&c, target); !ok || hit.Label != s.noteText("Copy code", "复制代码") {
		t.Fatal("copy feedback did not expire")
	}
}

func TestNoteCodeClipboardPreservesLineEndings(t *testing.T) {
	for _, tt := range []struct{ source, want string }{
		{"```go\n  value\n\n```\n", "  value\n\n"},
		{"```\r\nvalue\r\nnext\r\n```\r\n", "value\r\nnext\r\n"},
		{"```\nunfinished\n", "unfinished\n"},
		{"```\nunfinished", "unfinished"},
		{"```\n\n```", "\n"},
		{"```\n```", ""},
		{"> ```go\n> value\n> ```", "value\n"},
		{"  ~~~sh\n  echo ok\n  ~~~", "echo ok\n"},
	} {
		var doc markdown.Document
		doc.Parse(tt.source)
		if len(doc.Blocks) != 1 || doc.Blocks[0].Kind != markdown.Code {
			t.Fatalf("fixture: %q", tt.source)
		}
		if got := noteCodeText(tt.source, doc.Blocks[0]); got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.source, got, tt.want)
		}
	}
}
