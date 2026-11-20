//go:build darwin || linux || windows

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// The columns editor keeps the painted reorder row: a 48px row, a 28px
// six-dot handle at its start and the contents 40px in.
func TestSFTPColumnSettingsKeepsThePaintedReorderRow(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openSFTPColumnSettings()
	config := state.WindowConfig(app.Config{
		Width: 1200, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll,
	})
	view, err := headless.Open(config, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	var row, handle gfx.Rect
	for _, h := range view.Snapshot().Hits {
		if h.Role == gfx.RoleOption && h.Bounds.Height == 48 && row.Width == 0 {
			row = h.Bounds
		}
		if h.Role == gfx.RoleButton && h.Bounds.Width == 28 && h.Bounds.Height == 40 {
			handle = h.Bounds
		}
	}
	if row.Width == 0 || handle.Width == 0 {
		t.Fatalf("no 48px row with a 28x40 handle: row=%+v handle=%+v", row, handle)
	}
	if handle.X != row.X+4 {
		t.Fatalf("handle x=%v, want the row's start at %v", handle.X, row.X+4)
	}
}
