//go:build darwin || windows || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
)

// The hosts page's menus close with Escape, with a press elsewhere, and with a
// choice; the keyboard reaches them from their trigger.
func TestHostsToolbarMenusCloseWithEscape(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Hosts", Width: 1200, Height: 800, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
	}), nil, headless.Options{Scale: 1, ReferenceText: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for _, menu := range []struct {
		trigger string
		open    func() bool
	}{
		{state.messages.ImportMenu, func() bool { return state.importMenu }},
		{state.messages.HostCatalog.Order, func() bool { return state.hostOrderMenu }},
		{state.messages.Tools.HostPrivacy, func() bool { return state.hostPrivacy.menu }},
	} {
		if err := view.ClickLabel(menu.trigger); err != nil {
			t.Fatalf("%s: %v", menu.trigger, err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if !menu.open() {
			t.Fatalf("%s did not open its menu", menu.trigger)
		}
		if err := view.Keys("Escape"); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if menu.open() {
			t.Fatalf("Escape left the %s menu open", menu.trigger)
		}
	}
}
