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
	state.sftpColumns.setEnabled(sftpColumnModified, false)
	var canvas gfx.Canvas
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
	}, &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	var hint gfx.Hit
	for _, hit := range view.Snapshot().Hits {
		if hit.Hint == long {
			hint = hit
			break
		}
	}
	if hint.Bounds.Width <= 0 {
		t.Fatal("a truncated name with a fixed column hidden had no full-text hint")
	}
	if text := sftpNameText(&canvas, long); text.Bounds.Width <= 0 || hint.Bounds.Width != text.Bounds.Width {
		t.Fatalf("the full-text hit %+v does not cover the truncated name %+v", hint, text)
	}
	for _, item := range session.files.rows {
		if item.entry.Name != long && item.entry.Name != "" {
			for _, hit := range view.Snapshot().Hits {
				if hit.Hint == item.entry.Name {
					t.Fatalf("a fitted name has an unnecessary tooltip: %+v", hit)
				}
			}
		}
	}
	if err := view.Move(hint.Bounds.X+hint.Bounds.Width/2, hint.Bounds.Y+hint.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.tooltip.Showing() != hint.ID {
		t.Fatal("hovering the truncated name did not show its full text")
	}
	var lines int
	var tooltip string
	for _, text := range canvas.Texts() {
		if text.Layer == tooltipLayer {
			lines++
			if strings.TrimSpace(text.Value) == "" {
				t.Fatal("the tooltip painted an empty line")
			}
			tooltip += text.Value
		}
	}
	if lines == 0 || tooltip != long {
		t.Fatalf("tooltip = %q, want every part of %q", tooltip, long)
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

// sftpNameText finds the filename's list label, before a tooltip repeats it.
func sftpNameText(canvas *gfx.Canvas, long string) gfx.Text {
	for _, text := range canvas.Texts() {
		if text.Layer == 0 && strings.HasSuffix(text.Value, "…") && strings.HasPrefix(long, strings.TrimSuffix(text.Value, "…")) {
			return text
		}
	}
	return gfx.Text{}
}

func TestSFTPTruncatedNameTooltipWithoutResizableColumns(t *testing.T) {
	state, w, remote, host, _ := dualSFTPFixture(t, 1)
	long := strings.Repeat("wide-", 80) + "name.txt"
	remote.mu.Lock()
	remote.entries["/home/test"] = append(remote.entries["/home/test"],
		workbenchFileInfo{name: long, size: 1, mode: 0o644, when: time.Unix(1_700_000_300, 0)})
	remote.mu.Unlock()
	w.navigate(false, "/home/test")
	dualWait(t, state, w, host.windows[0].view, func() bool { return len(w.view.rows) == 3 })
	state.sftpColumns.setEnabled(sftpColumnModified, false)
	w.invalidate()
	view := host.windows[0].view
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	var hint gfx.Hit
	for _, hit := range view.Snapshot().Hits {
		if hit.Hint == long {
			hint = hit
			break
		}
	}
	if hint.Bounds.Width <= 0 {
		t.Fatal("a truncated window filename with a fixed column hidden had no hint")
	}
	if err := view.Move(hint.Bounds.X+hint.Bounds.Width/2, hint.Bounds.Y+hint.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if w.ui.tooltip.Showing() != hint.ID {
		t.Fatal("hovering the truncated window filename did not show its hint")
	}
	var tooltip string
	for _, text := range view.Snapshot().Texts {
		if text.Layer == tooltipLayer {
			if strings.TrimSpace(text.Value) == "" {
				t.Fatal("the tooltip painted an empty line")
			}
			tooltip += text.Value
		}
	}
	if tooltip != long {
		t.Fatalf("tooltip = %q, want every part of %q", tooltip, long)
	}
}
