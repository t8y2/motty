//go:build darwin || linux || windows

package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func openSFTPColumnPreview(t *testing.T, state *State) *headless.Session {
	t.Helper()
	view, err := headless.Open(app.Config{
		Title: "Motty SFTP columns", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if input := state.SFTPInput(context); input != nil {
				return input
			}
			if editor := state.EditorFor(context.ID); editor != nil {
				return &app.TextInput{Editor: editor}
			}
			return nil
		},
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	return view
}

func TestSFTPPanelColumnDividerDragsAndFits(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := openSFTPColumnPreview(t, state)
	divider, ok := hitByIDSnapshot(view.Snapshot().Hits, hitSFTPPanelColumnDivider.At(0))
	if !ok {
		t.Fatal("the name divider was not painted")
	}
	if divider.Role != gfx.RoleSeparator || divider.Cursor != gfx.PointerCursorResizeHorizontal {
		t.Fatalf("divider semantics = %+v", divider)
	}
	before := state.sftpColumnSizeWidth
	if before <= 0 {
		t.Fatalf("the default size width was not resolved: %v", before)
	}
	x, y := divider.Bounds.X+divider.Bounds.Width/2, divider.Bounds.Y+divider.Bounds.Height/2
	if err := view.Driver().Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Move(x-40, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Release(x-40, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !state.sftpColumnWidthsChosen || state.sftpColumnSizeWidth < before+39 || state.sftpColumnSizeWidth > before+41 {
		t.Fatalf("dragging the name divider left left size=%v chosen=%v, want about %v", state.sftpColumnSizeWidth, state.sftpColumnWidthsChosen, before+40)
	}

	// A double click fits the name column to its content; the fixture's short
	// names make it narrower than the default.
	divider, ok = hitByIDSnapshot(view.Snapshot().Hits, hitSFTPPanelColumnDivider.At(0))
	if !ok {
		t.Fatal("the name divider disappeared after the drag")
	}
	x, y = divider.Bounds.X+divider.Bounds.Width/2, divider.Bounds.Y+divider.Bounds.Height/2
	if err := view.DoubleClick(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if session.files.columnFitPending {
		t.Fatal("the fit did not run on the next paint")
	}
	if state.sftpColumnSizeWidth <= before+40 {
		t.Fatalf("fitting the name left the size at %v, want it grown back", state.sftpColumnSizeWidth)
	}
}

func TestSFTPTruncatedNameCarriesItsFullTextAsHint(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	long := strings.Repeat("wide-", 80) + "name.txt"
	remote.mu.Lock()
	remote.entries["/home/test"] = append(remote.entries["/home/test"],
		workbenchFileInfo{name: long, size: 1, mode: 0o644, when: time.Unix(1_700_000_300, 0)})
	remote.mu.Unlock()
	state.Click(hitSFTPRefresh)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.rows) == 3 })
	view := openSFTPColumnPreview(t, state)
	var hint gfx.Hit
	for _, hit := range view.Snapshot().Hits {
		if hit.Hint == long {
			hint = hit
			break
		}
	}
	if hint.Bounds.Width <= 0 {
		t.Fatal("a truncated name had no full-text hint")
	}
	// The hint hit carries the row's identity, so clicking it still chooses the
	// row rather than becoming a dead overlay.
	before := session.files.rows[sftpRowIndex(t, session.files, long)].selected
	if err := view.Click(hint.Bounds.X+hint.Bounds.Width/2, hint.Bounds.Y+hint.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !session.files.rows[sftpRowIndex(t, session.files, long)].selected {
		t.Fatalf("clicking the name hint did not choose the row (was selected: %v)", before)
	}
}
