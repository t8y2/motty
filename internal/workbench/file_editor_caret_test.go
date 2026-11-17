//go:build darwin || windows || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/headless"
)

func TestDocumentWindowCaretBlinksWithoutASecondApplicationPhase(t *testing.T) {
	document := documentFixture(t)
	view, err := headless.Open(document.config(), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	for _, hit := range view.Snapshot().Hits {
		if hit.ID == hitDocumentInput {
			if err := view.Click(hit.Bounds.X+30, hit.Bounds.Y+20); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	caretVisible := func() bool {
		caret, shown := document.ui.code.CaretBounds()
		if !shown {
			return false
		}
		for _, primitive := range view.Snapshot().Primitives {
			if primitive.Kind == app.RectanglePrimitive && primitive.Color == document.owner.pal.cursor &&
				primitive.Bounds.Width == caret.Width && primitive.Bounds.Height > caret.Width {
				return true
			}
		}
		return false
	}
	if view.Snapshot().Focused != hitDocumentInput || !caretVisible() {
		t.Fatal("document caret must start lit when clicked")
	}
	if err := view.Driver().Advance(components.CaretBlinkHalf + time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if caretVisible() {
		t.Fatal("document caret stayed lit after a half period")
	}
	if err := view.Driver().Advance(components.CaretBlinkHalf); err != nil {
		t.Fatal(err)
	}
	if !caretVisible() {
		t.Fatal("document caret did not return after a full period")
	}
}
