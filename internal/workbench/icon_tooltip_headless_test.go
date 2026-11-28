//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// Pressing Tab inside a terminal (for example, shell completion while typing
// "kubectl get po<Tab>" and running the command) must not leave Viva's
// keyboard-focus ring stuck on the terminal or suppress hover tooltips on the
// SSH tab and right toolbar.
func TestTerminalTabDoesNotDisableHoverTooltips(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.history.inlineOff = true
	var canvas gfx.Canvas
	view, err := headless.Open(app.Config{
		Title: "Terminal Tab Hover Tooltip", Width: 1400, Height: 900, Tree: Tree(),
		Paint: w.state.Paint, TextInput: w.state.TerminalInput,
		OnPointer: w.state.Pointer, OnKey: w.state.Key,
		OnShortcut: w.state.Shortcut, OnScroll: w.state.Scroll, OnResize: w.state.Resize,
	}, &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}

	termID := terminalControlHit(w.session.ID)
	termHit, ok := hitByIDSnapshot(view.Snapshot().Hits, termID)
	if !ok {
		t.Fatal("terminal hit not found")
	}
	if err := view.Click(termHit.Bounds.X+50, termHit.Bounds.Y+50); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}

	w.type_("kubectl get po")
	if err := view.Driver().Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	w.state.Wake()
	if err := view.Driver().Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	w.state.Wake()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}

	if fv := canvas.FocusVisible(); !fv.IsZero() {
		t.Fatalf("Tab handled by terminal left FocusVisible = %v, want zero", fv)
	}

	tooltipShown := func(want string) bool {
		for _, text := range canvas.Texts() {
			if text.Layer == tooltipLayer && text.Value == want {
				return true
			}
		}
		return false
	}

	tabID := hitTabList.At(int(w.state.deck.ActiveWorkspace()))
	tabHit, ok := hitByIDSnapshot(view.Snapshot().Hits, tabID)
	if !ok || tabHit.Hint == "" {
		t.Fatalf("SSH tab hit = %+v, %v; want non-empty hint", tabHit, ok)
	}
	if err := view.Move(tabHit.Bounds.X+tabHit.Bounds.Width/2, tabHit.Bounds.Y+tabHit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if w.state.tooltip.Showing() != tabID || !tooltipShown(tabHit.Hint) {
		t.Fatalf("hovering SSH tab after terminal Tab+Enter did not show tooltip %q (showing=%v)", tabHit.Hint, w.state.tooltip.Showing())
	}

	railHit, ok := hitByIDSnapshot(view.Snapshot().Hits, hitSFTPOpen)
	if !ok {
		t.Fatal("right toolbar Files hit not found")
	}
	if err := view.Move(railHit.Bounds.X+railHit.Bounds.Width/2, railHit.Bounds.Y+railHit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if w.state.tooltip.Showing() != hitSFTPOpen || !tooltipShown(w.state.messages.TipFiles) {
		t.Fatalf("hovering right toolbar after terminal Tab+Enter did not show tooltip %q (showing=%v)", w.state.messages.TipFiles, w.state.tooltip.Showing())
	}
}
