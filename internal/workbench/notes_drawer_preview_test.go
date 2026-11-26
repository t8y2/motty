package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// TestNotesDrawerPreviewSwitchPaintsChosenNote guards the narrow-card layout,
// where the library is a drawer declared after the editor. Choosing a note
// there changes the editor's text after its preview was laid out for the note
// before it; the draw that follows must lay the preview out again rather than
// paint the old note's layout with the new note's text. A leading fenced block
// in the long note is what a stale layout would slice past the short note's end.
func TestNotesDrawerPreviewSwitchPaintsChosenNote(t *testing.T) {
	long := "```bash\n" + strings.Repeat("command --flag value\n", 520) + "```\n"
	short := "# Short\n\nA short note."
	for _, test := range []struct {
		name  string
		mode  noteMode
		width float32
	}{
		{"preview", notePreview, 600},
		// Split needs a card wide enough to keep its panes, still narrow enough
		// to collapse the library into the drawer.
		{"split", noteSplit, 900},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newTestState(t, freshOptions(t))
			s.activeSection = sectionNotes
			s.notes.list = []savedNote{
				{ID: "long", Title: "Long", Body: long},
				{ID: "short", Title: "Short", Body: short},
			}
			s.selectNote("long")
			s.notes.view.mode = test.mode
			s.viewport = gfx.Size{Width: test.width, Height: 700}
			paintAt(s, s.viewport)

			s.Click(hitNoteTreeToggle)
			if !s.notes.view.drawer {
				t.Fatal("drawer did not open on a narrow card")
			}
			clickNoteTreeRow(t, s, &s.notes, "short")
			if s.notes.selected != "short" || s.notes.body.String() != short {
				t.Fatalf("selected note did not change: id=%q body=%q", s.notes.selected, s.notes.body.String())
			}
			if s.notes.previewLayout.source != short {
				t.Fatal("preview still laid out for the previous note after the switch")
			}
			all := ui.TextRange{Focus: s.notes.rendered.TextLen()}
			if got := s.notes.rendered.SelectedText(all); !strings.Contains(got, "A short note.") {
				t.Fatalf("preview paints the wrong note: %q", got)
			}
		})
	}
}
