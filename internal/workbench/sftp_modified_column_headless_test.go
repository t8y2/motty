//go:build darwin || linux || windows

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
)

// The modification time is as wide as its text at the current interface font
// and text size, not as wide as one font's metrics. A larger interface text
// size used to cut the last digits off the column.
func TestSFTPWindowModifiedTimeFitsTheInterfaceTextSize(t *testing.T) {
	defer applyMetrics(interfaceTextSizeDefault.factor(), cornersRounded)
	for _, resizable := range []bool{true, false} {
		name := "both-fixed-columns"
		if !resizable {
			name = "modified-column-only"
		}
		t.Run(name, func(t *testing.T) {
			state, _, _, host, _ := dualSFTPFixture(t, 1)
			if !resizable {
				state.sftpColumns.setEnabled(sftpColumnSize, false)
			}
			state.setInterfaceTextSize(interfaceTextSizeMax)
			view := host.windows[0].view
			if err := view.Driver().Pump(); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			requireSFTPModifiedTimeNotClipped(t, view.Snapshot())
		})
	}
}

func TestSFTPPanelModifiedTimeFitsTheInterfaceTextSize(t *testing.T) {
	defer applyMetrics(interfaceTextSizeDefault.factor(), cornersRounded)
	for _, resizable := range []bool{true, false} {
		name := "both-fixed-columns"
		if !resizable {
			name = "modified-column-only"
		}
		t.Run(name, func(t *testing.T) {
			state, _, _, _ := openTestSFTP(t)
			if !resizable {
				state.sftpColumns.setEnabled(sftpColumnSize, false)
			}
			// Keep the panel wide enough for the full date, the case the
			// clipped column was reported in.
			workspace := state.deck.ActiveWorkspace()
			context, _ := state.deck.Context(workspace)
			context.Width = 420
			if err := state.deck.SetContext(workspace, context); err != nil {
				t.Fatal(err)
			}
			state.setInterfaceTextSize(interfaceTextSizeMax)
			view := openSFTPColumnPreview(t, state)
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			requireSFTPModifiedTimeNotClipped(t, view.Snapshot())
		})
	}
}

// requireSFTPModifiedTimeNotClipped checks the painted frame: every line that
// is a modification time must be shown whole. It reads the frame's text
// widths, so it holds for any font the platform falls back to.
func requireSFTPModifiedTimeNotClipped(t *testing.T, snapshot app.Snapshot) {
	t.Helper()
	// A narrow list drops the year rather than the time, so both forms count.
	layouts := []string{sftpModifiedLayout, sftpModifiedLayout[5:]}
	modifiedTime := func(value string) bool {
		for _, layout := range layouts {
			if _, err := time.ParseInLocation(layout, value, time.Local); err == nil {
				return true
			}
		}
		return false
	}
	shown := 0
	for _, text := range snapshot.Texts {
		if modifiedTime(text.Value) {
			shown++
		}
	}
	if shown == 0 {
		t.Fatal("the frame paints no modification time to check")
	}
	for _, problem := range headless.CheckLayout(snapshot) {
		if problem.Kind == headless.LayoutClipped && modifiedTime(problem.Text) {
			t.Fatalf("the modification time %q is cut off: %s", problem.Text, problem.Detail)
		}
	}
}
