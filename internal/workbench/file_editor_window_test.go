package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// A save in flight must look like work in progress, not like a finished save:
// the footer names the state and animates a spinner beside it.
func TestDocumentFooterAnimatesWhileSaving(t *testing.T) {
	d := documentFixture(t)
	d.busy = true
	d.status = d.owner.messages.FileWindow.Saving
	var saving gfx.Canvas
	d.paint(&saving, gfx.Size{Width: 900, Height: 640})
	status, animating := documentFooterState(&saving, d.owner.messages.FileWindow.Saving)
	if !status {
		t.Fatal("the saving footer does not name the save")
	}
	if !animating {
		t.Fatal("the saving footer does not animate a spinner")
	}

	d.busy = false
	d.status = d.owner.messages.FileWindow.Ready
	var idle gfx.Canvas
	d.paint(&idle, gfx.Size{Width: 900, Height: 640})
	if status, animating := documentFooterState(&idle, d.owner.messages.FileWindow.Saving); status || animating {
		t.Fatalf("an idle footer still shows progress: status=%v animating=%v", status, animating)
	}
}

// documentFooterState reports whether the painted footer names the given state
// and whether anything in it asks for the next frame, which only the spinner
// does when nothing is focused.
func documentFooterState(canvas *gfx.Canvas, status string) (named, animating bool) {
	for _, text := range canvas.Texts() {
		if strings.Contains(text.Value, status) {
			named = true
		}
	}
	_, animating = canvas.NextFrame()
	return named, animating
}

// The comparison window is the other place a save starts; its footer shows the
// same in-progress state.
func TestComparisonFooterAnimatesWhileSaving(t *testing.T) {
	d := documentFixture(t)
	d.local = false
	d.comparison.active = true
	d.comparison.baseline = d.original
	d.comparison.result = components.DiffCodeLines(d.original, d.editor.String())
	d.busy = true
	d.comparison.saving = true
	var canvas gfx.Canvas
	d.paintComparison(&canvas, gfx.Size{Width: 1120, Height: 720})
	if status, animating := documentFooterState(&canvas, d.owner.messages.FileWindow.SavingChanges); !status || !animating {
		t.Fatalf("the comparison save footer: status=%v animating=%v", status, animating)
	}
}

// A document and its comparison are windows of their own now, so each declares
// the same titlebar the tools window does: the file it shows at the top, and
// nothing of the content under that strip.
func TestDocumentAndComparisonWindowsUseTheContentTitlebar(t *testing.T) {
	d := documentFixture(t)
	var canvas gfx.Canvas
	d.paint(&canvas, gfx.Size{Width: 900, Height: 640})
	if box := d.ui.contentBox.Rect(); box.Y < TitlebarHeight || box.Width <= 0 {
		t.Fatalf("editor content starts at %+v, inside the titlebar", box)
	}
	movable := false
	for _, hit := range canvas.Hits() {
		movable = movable || hit.MovesWindow && hit.Bounds.Y == 0 && hit.Bounds.Height == TitlebarHeight
	}
	if !movable {
		t.Fatal("document window has no draggable titlebar")
	}
	named := map[string]bool{}
	for _, text := range canvas.Texts() {
		if text.Bounds.Y+text.Bounds.Height <= TitlebarHeight {
			named[text.Value] = true
		}
	}
	if !named[d.name] || !named[d.source] {
		t.Fatalf("document titlebar does not name the file and its source: %v", named)
	}
	if config := d.config(); config.WindowMode != app.WindowCustomTitlebar || config.TitlebarHeight != TitlebarHeight {
		t.Fatalf("document window lost its custom titlebar: %+v", config)
	}

	d.comparison.active = true
	d.comparison.baseline = "server\n"
	d.comparison.result = components.DiffCodeLines(d.comparison.baseline, d.editor.String())
	var compare gfx.Canvas
	d.paintComparison(&compare, gfx.Size{Width: 1000, Height: 720})
	if box := d.comparison.ui.contentBox.Rect(); box.Y < TitlebarHeight || box.Width <= 0 {
		t.Fatalf("comparison content starts at %+v, inside the titlebar", box)
	}
	if config := d.comparisonConfig(); config.WindowMode != app.WindowCustomTitlebar || config.TitlebarHeight != TitlebarHeight {
		t.Fatalf("comparison window lost its custom titlebar: %+v", config)
	}
}

func TestASlowDocumentDoesNotTakeOverTheTabOpenedAfterIt(t *testing.T) {
	host := &documentTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() {
		host.Quit()
		app.UseApplicationRunner(nil)
	})
	if err := app.RunApplication(func(application *app.Application) error {
		state := newTestState(t, testOptions(t))
		state.options.OpenContentWindow = application.OpenContentWindow
		manager := &state.fileWindows
		slow := &fileDocument{owner: state, cacheKey: "1:false:/slow.txt", location: "/slow.txt", name: "slow.txt"}
		fast := &fileDocument{owner: state, cacheKey: "1:false:/fast.txt", location: "/fast.txt", name: "fast.txt"}
		manager.documents = map[string]*fileDocument{slow.cacheKey: slow, fast.cacheKey: fast}
		// The document opened last finishes loading first.
		manager.pending = fast.cacheKey
		if err := manager.present(fast); err != nil {
			return err
		}
		// The earlier, slower load finishing afterwards must not take the tab.
		if err := manager.present(slow); err != nil {
			return err
		}
		if manager.active != fast.cacheKey {
			t.Fatalf("active tab = %q, want the document opened last", manager.active)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
