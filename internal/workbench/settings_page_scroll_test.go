//go:build darwin || windows || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// openSettingsPageScroll opens the settings tab on the terminal category in a
// window short enough that the page scrolls, and returns the session.
func openSettingsPageScroll(t *testing.T, state *State) *headless.Session {
	t.Helper()
	session, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Settings page scroll", Width: 1000, Height: 520, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	if state.settingsContent <= state.settings.pageView {
		t.Fatalf("the page (%v) fits its view (%v), so the test would prove nothing", state.settingsContent, state.settings.pageView)
	}
	return session
}

// settingsHitOf is the painted hit with that identity.
func settingsHitOf(t *testing.T, session *headless.Session, id gfx.ID) (gfx.Hit, bool) {
	t.Helper()
	for _, hit := range session.Snapshot().Hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// TestSettingsPageKeysScrollItToItsLastRow pins the keyboard's way into a long
// settings page: PageUp and PageDown page it while one of the page's own
// controls has the keyboard, the arrows and Home/End move it once its viewport
// has it, and the last row of the category, which a short window leaves below the
// fold, is reached that way rather than only by the wheel.
func TestSettingsPageKeysScrollItToItsLastRow(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	session := openSettingsPageScroll(t, state)

	// A row below the fold is not painted, so the keyboard would not reach it
	// before the page moves.
	if _, painted := settingsHitOf(t, session, hitFindSettingsReset); painted {
		t.Fatal("the last row of the category is already painted, so the test would prove nothing")
	}

	// The page keys page the page from the category row, the control the pointer
	// and the keyboard enter the page through.
	category, ok := settingsHitOf(t, session, hitSettingsCategoryList.At(int(settingsTerminal)))
	if !ok {
		t.Fatal("the terminal category row is not painted")
	}
	if err := session.Click(category.Bounds.X+category.Bounds.Width/2, category.Bounds.Y+category.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	before := state.settingsScroll
	if err := session.Keys("pagedown"); err != nil {
		t.Fatal(err)
	}
	if state.settingsScroll <= before {
		t.Fatalf("PageDown left the page at %v, where it was", state.settingsScroll)
	}

	// The arrows are the keyboard control's own: they do not page the page.
	arrows := state.settingsScroll
	for _, key := range []string{"down", "up", "home", "end"} {
		if err := session.Keys(key); err != nil {
			t.Fatal(err)
		}
		if state.settingsScroll != arrows {
			t.Fatalf("%s moved the page to %v while a control had the keyboard, from %v", key, state.settingsScroll, arrows)
		}
	}

	// A press on the page's empty room gives the viewport the keyboard, and from
	// there the ends and the arrows move the page.
	page, ok := settingsHitOf(t, session, hitSettingsViewport)
	if !ok {
		t.Fatal("the page's viewport is not a pointer target")
	}
	if err := session.Click(page.Bounds.X+8, page.Bounds.Y+8); err != nil {
		t.Fatal(err)
	}
	if focused := session.Snapshot().Focused; focused != hitSettingsViewport {
		t.Fatalf("a press on the page's empty room focused %v, want its viewport", focused)
	}
	if err := session.Keys("end"); err != nil {
		t.Fatal(err)
	}
	maxScroll := max(state.settingsContent-state.settings.pageView, 0)
	if state.settingsScroll < maxScroll-1 {
		t.Fatalf("End left the page at %v of %v", state.settingsScroll, maxScroll)
	}
	last, painted := settingsHitOf(t, session, hitFindSettingsReset)
	if !painted {
		t.Fatal("the last row of the category is still not painted at the page's end")
	}
	if last.Bounds.Y < page.Bounds.Y || last.Bounds.Bottom() > page.Bounds.Bottom() {
		t.Fatalf("the last row %+v is not inside the page's view %+v", last.Bounds, page.Bounds)
	}
	if err := session.Keys("home"); err != nil {
		t.Fatal(err)
	}
	if state.settingsScroll != 0 {
		t.Fatalf("Home left the page at %v", state.settingsScroll)
	}
}

// TestSettingsPageBarDragsIt pins the bar the page draws: a drag on it moves the
// page and stays where it was dropped, though the page keeps its own offset.
func TestSettingsPageBarDragsIt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	session := openSettingsPageScroll(t, state)

	page, ok := settingsHitOf(t, session, hitSettingsViewport)
	if !ok {
		t.Fatal("the page's viewport is not painted")
	}
	// The pointer over the page reveals its bar, as every scroll area in the app
	// reveals its own.
	if err := session.Move(page.Bounds.X+page.Bounds.Width/2, page.Bounds.Y+page.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	bar, ok := settingsHitOf(t, session, hitSettingsPageBar)
	if !ok {
		t.Fatal("the page paints no scrollbar under the pointer")
	}
	x := bar.Bounds.X + bar.Bounds.Width/2
	if err := session.Press(x, bar.Bounds.Y+40); err != nil {
		t.Fatal(err)
	}
	if err := session.Move(x, bar.Bounds.Y+140); err != nil {
		t.Fatal(err)
	}
	if err := session.Release(x, bar.Bounds.Y+140); err != nil {
		t.Fatal(err)
	}
	dragged := state.settingsScroll
	if dragged <= 0 {
		t.Fatal("dragging the page's bar did not scroll the page")
	}
	// The page keeps its own offset: the next paints must take the dragged one,
	// not put the thumb back where the page was.
	if err := session.Settle(10*time.Millisecond, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := session.Frame(); err != nil {
		t.Fatal(err)
	}
	if state.settingsScroll != dragged {
		t.Fatalf("the page went back to %v after the drag left it at %v", state.settingsScroll, dragged)
	}
}
