//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/ai/agent"
	"motty/internal/i18n"
	layoutcore "motty/internal/workspace"
)

// TestMultiTargetScreenshots paints the target list with several connections
// ticked, the trigger that summarizes them, and the split-terminal rename. It
// only writes pictures, and only when MOTTY_AI_TARGET_SCREENSHOTS names a
// directory.
func TestMultiTargetScreenshots(t *testing.T) {
	dir := os.Getenv("MOTTY_AI_TARGET_SCREENSHOTS")
	if dir == "" {
		t.Skip("set MOTTY_AI_TARGET_SCREENSHOTS to write the pictures")
	}
	state := multiTargetWorld(t, quietModel{"ok"})
	state.setLanguage(i18n.Chinese)
	state.setAppearance(appearanceLight)
	state.renameSession(&state.sessions[0], "web-01")
	state.renameSession(&state.sessions[1], "db-primary")
	state.renameSession(&state.sessions[2], "db-replica")
	state.rebuildTabOptions()
	state.ai.mode, state.ai.open = agent.ModeAsk, true
	state.activeSection = sectionTerminal
	size := gfx.Size{Width: 1400, Height: 820}
	view, err := headless.Open(app.Config{Width: int(size.Width), Height: int(size.Height), Tree: Tree(), Paint: state.Paint, OnResize: state.Resize,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll,
		TextInput: state.AIPanelInput}, nil, headless.Options{Scale: shotScale()})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	find := func(id gfx.ID) gfx.Hit {
		t.Helper()
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == id {
				return hit
			}
		}
		t.Fatalf("missing %v", id)
		return gfx.Hit{}
	}
	click := func(id gfx.ID) {
		t.Helper()
		hit := find(id)
		must(view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2))
		must(view.Rest())
	}
	must(os.MkdirAll(dir, 0o700))
	shot := func(name string) { must(view.Screenshot(filepath.Join(dir, name))) }

	state.aiChooseOnlyTarget(hitAITargets.At(int(state.sessions[0].ID)))
	must(view.Rest())
	click(hitAITarget)
	click(hitAITargets.At(int(state.sessions[1].ID)))
	shot("target-list-two-ticked.png")
	workspace, _, _ := state.deck.PaneForSession(layoutcore.SessionID(state.sessions[1].ID))
	click(hitAITargetTabs.At(int(workspace)))
	shot("target-list-whole-tab.png")
	click(hitAITargetDismiss)
	shot("target-trigger-summary.png")

	pane, ok := state.paneForSession(state.sessions[2].ID)
	if !ok {
		t.Fatal("no pane for the third terminal")
	}
	bounds := pixelBounds(state.workspacePixelBounds(size), pane.Bounds)
	must(view.RightClick(bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2))
	must(view.Rest())
	shot("split-terminal-menu.png")
	click(hitTerminalMenuRename)
	shot("split-terminal-rename.png")
}

// shotScale is the display scale of the pictures, 2 unless MOTTY_SHOT_SCALE says otherwise.
func shotScale() float64 {
	if value, err := strconv.ParseFloat(os.Getenv("MOTTY_SHOT_SCALE"), 64); err == nil && value > 0 {
		return value
	}
	return 2
}
