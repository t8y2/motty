package workbench

import (
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

// A path candidate whose text is wider than the list may grow keeps its end:
// the row cuts the middle instead, so the names that tell candidates apart stay
// readable (#875). The list is capped at completionMaxWidth, so a directory
// deep enough to exceed it must still paint every row inside the row.
func TestLongPathCandidatesKeepTheirEndsInsideTheList(t *testing.T) {
	w, remote := newPathWorld(t)
	parent := strings.Repeat("deeply-nested-project/", 5)
	dir := path.Join("/home/test", parent)
	names := []string{"release-candidate-alpha-tarball", "release-candidate-beta-tarball", "release-candidate-gamma-tarball"}
	for index, name := range names {
		remote.entries[dir] = append(remote.entries[dir], workbenchFileInfo{
			name: name, mode: os.ModeDir | 0o755, when: time.Unix(1_700_000_000+int64(index), 0),
		})
	}
	w.state.Resize(gfx.Size{Width: 1400, Height: 900})
	w.type_("cd " + parent)
	waitCompletionList(t, w)
	if got := len(w.state.completion.list.candidates); got != len(names) {
		t.Fatalf("the list offered %d candidates, want %d", got, len(names))
	}

	var canvas gfx.Canvas
	w.state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	rows := make([]gfx.Text, 0, len(names))
	for _, text := range canvas.Texts() {
		if strings.Contains(text.Value, "release-candidate") {
			rows = append(rows, text)
		}
	}
	if len(rows) != len(names) {
		t.Fatalf("painted %d candidate rows, want %d: %v", len(rows), len(names), canvas.Texts())
	}
	seen := map[string]bool{}
	for _, row := range rows {
		candidate := ""
		for _, name := range names {
			if strings.HasSuffix(row.Value, name+"/") {
				candidate = parent + name + "/"
				seen[name] = true
			}
		}
		if candidate == "" {
			t.Fatalf("row %q does not end with any candidate's name", row.Value)
		}
		before, after, cut := strings.Cut(row.Value, "…")
		if !cut || before == "" || after == "" {
			t.Fatalf("row %q was cut at one end, not in its middle", row.Value)
		}
		if !strings.HasPrefix(candidate, before) || !strings.HasSuffix(candidate, after) {
			t.Fatalf("row %q is not made of the candidate's two ends", row.Value)
		}
		if width := canvas.TextWidthStyle(row.Value, row.Style); width > row.Bounds.Width+1 {
			t.Fatalf("row %q is %g wide in a %g wide box", row.Value, width, row.Bounds.Width)
		}
	}
	if len(seen) != len(names) {
		t.Fatalf("the painted rows do not show every candidate: %v", seen)
	}
}
