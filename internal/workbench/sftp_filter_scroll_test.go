package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

// filterLineX paints the page and returns the x the filter drew the given query at.
// The field's own left edge is not that x: the field has a leading magnifier and its
// text sits inset from it, so a line is compared against the origin an unscrolled
// query takes, which layout alone decides.
func filterLineX(t *testing.T, state *State, query string) float32 {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, painted := range canvas.Texts() {
		if painted.Value == query {
			return painted.Bounds.X
		}
	}
	t.Fatalf("the query %q was not painted at all", query)
	return 0
}

// issue t8y2/motty#1722: the file window's filter keeps its query in view of the caret.
// Before this, the field drew its glyphs from the line's own origin however long the
// query was, so the panel clipped the tail: everything typed past the edge, and the
// caret itself, were out of sight, and End did not bring them back.
func TestSFTPFilterScrollsToKeepTheCaretInView(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.Click(hitSFTPFilterToggle)

	// The origin a query that fits sits at: what the line's own start is, with no
	// scrolling. The layout alone decides it, not the value.
	const short = "a"
	replaceSFTPFilter(t, state, session, false, short)
	origin := filterLineX(t, state, short)

	// A query longer than the panel's filter field, matching nothing: the listing is
	// irrelevant here, the field's own paint is what is under test.
	const query = "a-very-long-file-name-filter-query-that-outgrows-the-box-1234567890"
	replaceSFTPFilter(t, state, session, false, query)
	if got := session.files.sftpFolder.filterInputEditor().String(); got != query {
		t.Fatalf("the filter holds %q, want the whole query", got)
	}
	if x := filterLineX(t, state, query); x >= origin {
		t.Fatalf("the query is painted from %v, the line's own origin %v: a value longer than the box did not scroll, so its tail is out of sight", x, origin)
	}
}

// A caret back at the start of the query puts its beginning back in view: the scroll
// follows the caret instead of staying where the longest value left it.
func TestSFTPFilterScrollReturnsToTheStartWithTheCaret(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.Click(hitSFTPFilterToggle)

	const short = "a"
	replaceSFTPFilter(t, state, session, false, short)
	origin := filterLineX(t, state, short)

	const query = "a-very-long-file-name-filter-query-that-outgrows-the-box"
	replaceSFTPFilter(t, state, session, false, query)
	if x := filterLineX(t, state, query); x >= origin {
		t.Fatalf("setup: the long query is painted from %v, not scrolled from the origin %v", x, origin)
	}

	session.files.sftpFolder.filterEditor.SetSelection(text.Range{Start: 0, End: 0})
	if x := filterLineX(t, state, query); x != origin {
		t.Fatalf("the query is painted from %v with the caret at its start, want the line's own origin %v", x, origin)
	}
}
