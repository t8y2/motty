package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// clickPaintedLabel presses the visible button with the given label through the
// driver's real pointer event pass. A page-owned button has no application ID
// to name it by, so the label and role are its address.
func clickPaintedLabel(t *testing.T, driver *app.Driver, label string) {
	t.Helper()
	hits := driver.Snapshot().Hits
	for index := len(hits) - 1; index >= 0; index-- {
		hit := hits[index]
		if hit.Label == label && hit.Role == gfx.RoleButton && hit.Bounds.Width > 0 {
			if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("the painted button %q was not found", label)
}

// The form-family dialogs are declared by the workbench page now: their
// buttons and bodies are page-owned, and the window's real event pass reaches
// them as it reaches any other page control. These tests drive that pass rather
// than calling the click dispatcher.
func TestDialogPageOwnsItsControls(t *testing.T) {
	cases := []struct {
		name string
		open func(*State)
		// id names a control that is still hand-painted with the application's
		// identity; label names a page-owned button.
		id    gfx.ID
		label string
	}{
		{"host", func(state *State) { state.openHostDialog() }, gfx.ID{}, "Cancel"},
		{"close-tabs", func(state *State) {
			state.tabsClose = tabsClose{count: 2, running: []string{"one", "two"}}
			state.modal = modalCloseTabs
		}, hitTabsCloseCancel, ""},
		{"forward", func(state *State) { state.openForwardDialog() }, gfx.ID{}, "Cancel"},
		{"highlight", func(state *State) { state.openHighlightDialog() }, gfx.ID{}, "Cancel"},
		{"vault-unlock", func(state *State) { state.openVaultUnlock(nil, nil) }, gfx.ID{}, "Cancel"},
		{"vault-forget", func(state *State) { state.openVaultForget() }, hitVaultForgetCancel, ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			test.open(state)
			driver := modalEventDriver(t, state)
			// The dialog's elements are declared by the workbench page, so the
			// page's tree owns what ui.Dialog names in its scope even though the
			// application's own identities live under their feature.
			if !dialogPageOwns(dialogScope) {
				t.Fatal("the dialog scope is not declared in the application's dialog scope")
			}
			if test.label != "" {
				clickPaintedLabel(t, driver, test.label)
			} else {
				clickPaintedHit(t, driver, test.id)
			}
			if state.modal != modalNone {
				t.Fatalf("the event pass did not press the dialog's button: modal=%v", state.modal)
			}
		})
	}
}

// Controls the dialog body paints by hand take the window's real event pass
// too: the page owns their identities, and the click reaches the same handler
// the central dispatcher runs.
func TestPageDialogBodyControlsTakeTheEventPass(t *testing.T) {
	t.Run("close-tabs-checkbox", func(t *testing.T) {
		state := newTestState(t, testOptions(t))
		state.tabsClose = tabsClose{count: 2, running: []string{"one", "two"}}
		state.modal = modalCloseTabs
		driver := modalEventDriver(t, state)
		clickPaintedHit(t, driver, hitTabsCloseQuiet)
		if !state.tabsClose.quiet || state.modal != modalCloseTabs {
			t.Fatalf("the quiet checkbox did not take the press: quiet=%v modal=%v", state.tabsClose.quiet, state.modal)
		}
	})
	t.Run("highlight-color", func(t *testing.T) {
		state := newTestState(t, testOptions(t))
		state.openHighlightDialog()
		before := state.highlighting.slot
		driver := modalEventDriver(t, state)
		clickPaintedHit(t, driver, hitHighlightColorList.At(1))
		if state.highlighting.slot == before {
			t.Fatal("the color choice did not take the press")
		}
	})
	t.Run("host-form-submit", func(t *testing.T) {
		state := newTestState(t, testOptions(t))
		state.openHostDialog()
		state.editor.label.Insert("test host")
		state.editor.address.Insert("192.0.2.42")
		before := len(state.hosts)
		driver := modalEventDriver(t, state)
		clickPaintedLabel(t, driver, state.messages.AddHost)
		if state.modal != modalNone || len(state.hosts) != before+1 {
			t.Fatalf("the submit button did not save: modal=%v hosts=%d, want %d", state.modal, len(state.hosts), before+1)
		}
	})
}

// Escape closes a page dialog through the escape target components.Dialog
// installs, still reporting the application's own close identity.
func TestPageDialogEscapeCloses(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openForwardDialog()
	driver := modalEventDriver(t, state)
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone {
		t.Fatalf("Escape did not close the dialog: modal=%v", state.modal)
	}
}
