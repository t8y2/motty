//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// The window bar is a move region. A click on the home button with a tab's menu
// open must only close the menu: the menu's backdrop covers the bar, so the click
// reaches it and the home button does not navigate, and a press on the bar's empty
// space under the backdrop is the application's until the pointer drags (see
// app.PressMoveOnDrag).
func TestClickingTheHomeButtonWithATabMenuOpenOnlyClosesTheMenu(t *testing.T) {
	state, tabs := fourTabs(t)
	session, err := headless.Open(app.Config{
		Title: "tab menu", Width: 1400, Height: 900,
		Paint:     state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.Rest(); err != nil {
		t.Fatal(err)
	}
	home, ok := session.Hit(state.messages.NavHosts)
	if !ok {
		t.Fatal("no home button")
	}
	section := state.activeSection

	// The window moves from the bar's empty space, and its buttons are controls.
	center := func(hit gfx.Hit) (float32, float32) {
		return hit.Bounds.X + hit.Bounds.Width/2, hit.Bounds.Y + hit.Bounds.Height/2
	}
	homeX, homeY := center(home)
	if kind := session.PressAt(homeX, homeY); kind != app.PressControl {
		t.Fatalf("a press on the home button is %d, want a control", kind)
	}
	var empty float32 = -1
	for x := float32(0); x < 1400; x += 4 {
		if session.PressAt(x, 18) == app.PressMove {
			empty = x
			break
		}
	}
	if empty < 0 {
		t.Fatal("no place on the window bar moves the window")
	}
	if kind := session.PressAt(700, 400); kind == app.PressMove || kind == app.PressMoveOnDrag {
		t.Fatalf("a press in the page is %d, which would move the window", kind)
	}

	tab, ok := hitByIDSnapshot(session.Snapshot().Hits, hitTabList.At(int(tabs[1])))
	if !ok {
		t.Fatal("no tab")
	}
	if err := session.RightClick(tab.Bounds.X+tab.Bounds.Width/2, tab.Bounds.Y+tab.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.tabMenu.open {
		t.Fatal("the tab menu did not open")
	}
	if kind := session.PressAt(empty, 18); kind != app.PressMoveOnDrag {
		t.Fatalf("a press on the bar under the menu's backdrop is %d, want the application's, moving the window on a drag", kind)
	}
	if err := session.Click(home.Bounds.X+home.Bounds.Width/2, home.Bounds.Y+home.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.tabMenu.open {
		t.Fatal("a click in the titlebar strip left the tab menu open")
	}
	if state.activeSection != section {
		t.Fatalf("the click went through to the home button: section %v -> %v", section, state.activeSection)
	}
}

func hitByIDSnapshot(hits []gfx.Hit, id gfx.ID) (gfx.Hit, bool) {
	for index := len(hits) - 1; index >= 0; index-- {
		if hits[index].ID == id {
			return hits[index], true
		}
	}
	return gfx.Hit{}, false
}
