//go:build darwin || windows || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/sshconn"
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
	// A context menu opened at a point of the window closes the same way.
	state.openHostBlankMenu(gfx.Point{X: 300, Y: 300})
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !state.hostBlankMenu.open {
		t.Fatal("the blank-area menu did not open")
	}
	if err := view.Keys("Escape"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.hostBlankMenu.open {
		t.Fatal("Escape left the blank-area menu open")
	}
}

// A terminal is renamed by typing into the dialog's field and pressing Enter.
func TestTerminalRenameDialogTakesTypedNameWithEnter(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.openTerminalRename(session)
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Rename", Width: 900, Height: 600, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
	}), nil, headless.Options{Scale: 1, ReferenceText: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("cmd+a"); err != nil {
		t.Fatal(err)
	}
	if err := view.Type("web-logs"); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone || session.Title != "web-logs" {
		t.Fatalf("modal %d, title %q", state.modal, session.Title)
	}
}

// A password is typed into the dialog's field, shown by the eye button, and
// answered with Enter.
func TestPasswordDialogTakesTypedAnswerAndReveals(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) {
		status.Phase, status.Attempt = sshconn.PhasePasswordPrompt, 1
		status.Questions = []sshconn.Question{{Text: "Password: ", Secret: true}}
		status.Endpoint = sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}
	})
	state.Wake()
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Password", Width: 900, Height: 600, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
	}), nil, headless.Options{Scale: 1, ReferenceText: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := view.Type("hunter2"); err != nil {
		t.Fatal(err)
	}
	if got := state.passwordFields[0].String(); got != "hunter2" {
		t.Fatalf("typed answer = %q", got)
	}
	if err := view.ClickLabel(state.messages.ShowSecret); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !state.passwordInputs[0].shown {
		t.Fatal("the eye button did not show the answer")
	}
	if err := view.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.modal == modalPassword {
		t.Fatal("Enter did not answer the question")
	}
}
