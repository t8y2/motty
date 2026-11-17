package workbench

import "testing"

// The menu bar speaks the application's language, shows the shortcuts the user
// kept, and disables what needs a session when there is none.
func TestMenuBarFollowsTheShortcutTableAndSession(t *testing.T) {
	state := newTestState(t, Options{})
	menus := state.MenuBar()
	if len(menus) != 3 || menus[0].Title == "" || menus[1].Title == "" || menus[2].Title == "" {
		t.Fatalf("menus = %+v", menus)
	}
	newWindow := menus[0].Items[0]
	if newWindow.Title == "" || newWindow.OnSelect == nil || newWindow.Disabled {
		t.Fatalf("New Window = %+v", newWindow)
	}
	edit := menus[1]
	if !edit.Items[0].Disabled || !edit.Items[1].Disabled || !edit.Items[3].Disabled {
		t.Fatalf("Copy, Paste and Find must need a session: %+v", edit.Items)
	}
	if menus[2].Items[1].Disabled {
		t.Fatalf("the assistant needs no session: %+v", menus[2].Items[1])
	}
	// A separator carries no title and no action.
	if !edit.Items[2].Separator || edit.Items[2].OnSelect != nil {
		t.Fatalf("separator = %+v", edit.Items[2])
	}
}

// A native menu runs the entry outside the window's event handlers, so the
// action has to hand the runtime what it did. Find opens the bar; the result
// must name the field whose keyboard focus the runtime then applies, or the
// bar opens while the keyboard stays in the terminal.
func TestMenuBarFindReturnsTheFieldFocus(t *testing.T) {
	state, _, _, _ := liveSession(t)
	result := state.MenuBar()[1].Items[3].OnSelect()
	if !result.Handled || !result.Repaint || result.Focus != hitTerminalFindField || !result.FocusVisible {
		t.Fatalf("Find result = %+v, want focus on %v", result, hitTerminalFindField)
	}
	if session := &state.sessions[0]; !session.term.find.open {
		t.Fatal("the menu action did not open the bar")
	}
}
