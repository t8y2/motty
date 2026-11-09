package workbench

import (
	"context"
	"runtime"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/localshell"
)

// withShellChoices makes the state offer these shells, as a Windows computer
// would after finding them, without looking at the real machine.
func withShellChoices(t *testing.T, state *State, shells []localshell.Choice, distros []localshell.Choice) {
	t.Helper()
	previous := localShellsSupported
	localShellsSupported = true
	t.Cleanup(func() { localShellsSupported = previous })
	state.options.Invalidate = func() {}
	state.localShellMenu.discover = func() []localshell.Choice { return shells }
	state.localShellMenu.discoverWSL = func(context.Context) []localshell.Choice { return distros }
}

func closeLocalShells(t *testing.T, state *State) {
	t.Helper()
	t.Cleanup(func() {
		for index := range state.sessions {
			if term := state.sessions[index].term; term != nil && term.shell != nil {
				term.shell.Close()
			}
		}
	})
}

func localShellOf(t *testing.T, session *Session) string {
	t.Helper()
	conn, ok := session.conn.(*localConn)
	if !ok {
		t.Fatalf("session connection is %T, want a local one", session.conn)
	}
	return conn.shell
}

func TestNewTabPageOpensALocalTerminalWithTheChosenShell(t *testing.T) {
	skipWithoutUnixShell(t)
	state := newTabState(t)
	closeLocalShells(t, state)
	withShellChoices(t, state,
		[]localshell.Choice{{Name: "Alpha", Path: "/bin/sh", Default: true}, {Name: "Beta", Path: "/bin/bash"}},
		[]localshell.Choice{localshell.WSLChoices([]string{"Ubuntu"})[0]})
	state.localShell = "/bin/sh"
	tabs, workspace := len(state.workspaceTabs), state.deck.ActiveWorkspace()

	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitNewTabShells); !ok {
		t.Fatal("the Local Terminal row has no way to choose a shell")
	}
	state.Click(hitNewTabShells)
	if !state.localShellPicker.open {
		t.Fatal("the arrow did not open the shell menu")
	}
	// The menu picks the distributions up as it is painted, so the wait goes
	// through a repaint instead of adopting them itself.
	waitUntil(t, state, "the WSL shell choices", func() bool {
		paintedTexts(state)
		return state.localShellMenu.listing == nil
	})
	canvas, _ = paintedTexts(state)
	items := []string{"Alpha", "Beta", "WSL: Ubuntu"}
	for index, name := range items {
		hit, ok := hitByID(canvas, hitLocalShellPickerItems.At(index))
		if !ok || hit.Label != name {
			t.Fatalf("menu item %d = %+v found=%v, want %q", index, hit, ok, name)
		}
	}

	state.Click(hitLocalShellPickerItems.At(1))
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || !session.isLocal() || state.localShellPicker.open {
		t.Fatalf("choosing a shell: session ok=%v open=%v", ok, state.localShellPicker.open)
	}
	if len(state.workspaceTabs) != tabs || state.deck.ActiveWorkspace() != workspace {
		t.Fatalf("tabs %d -> %d: the shell must start in the tab it was chosen in", tabs, len(state.workspaceTabs))
	}
	if got := localShellOf(t, session); got != "/bin/bash" {
		t.Fatalf("tab runs %q, want the chosen /bin/bash rather than the default", got)
	}
	if session.Title != "Beta" {
		t.Fatalf("tab is called %q, want the shell's name", session.Title)
	}
}

func TestSidebarLocalTerminalMenuOpensAnotherShellInANewTab(t *testing.T) {
	skipWithoutUnixShell(t)
	state := newTestState(t, testOptions(t))
	closeLocalShells(t, state)
	withShellChoices(t, state, []localshell.Choice{{Name: "Alpha", Path: "/bin/sh", Default: true}, {Name: "Beta", Path: "/bin/bash"}}, nil)

	pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitLocalTerminal, X: 60, Y: 700})
	if !state.localShellPicker.open || state.localShellPicker.inPane {
		t.Fatalf("picker = %+v", state.localShellPicker)
	}
	tabs := len(state.workspaceTabs)
	paintedTexts(state)
	state.Click(hitLocalShellPickerItems.At(1))
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || localShellOf(t, session) != "/bin/bash" || len(state.workspaceTabs) != tabs+1 {
		t.Fatalf("session ok=%v tabs %d -> %d", ok, tabs, len(state.workspaceTabs))
	}

	// The plain click is still the default shell, which follows the setting.
	state.localShell = "/bin/sh"
	state.Click(hitLocalTerminal)
	plain, _ := state.sessionByIDPointer(state.activeSessionID)
	if plain.host.localShell != "" || localShellOf(t, plain) != "/bin/sh" {
		t.Fatalf("a plain click chose shell %q (host %q), want the default", localShellOf(t, plain), plain.host.localShell)
	}
}

func TestLocalShellPickerIsAbsentWhereTheShellIsNotChosen(t *testing.T) {
	state := newTestState(t, testOptions(t))
	previous := localShellsSupported
	localShellsSupported = false
	t.Cleanup(func() { localShellsSupported = previous })
	pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitLocalTerminal, X: 60, Y: 700})
	if state.localShellPicker.open {
		t.Fatal("a computer with one login shell offered a shell menu")
	}
	openEmptyTab(state)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitNewTabShells); ok {
		t.Fatal("the new-tab page shows a shell arrow where there is nothing to choose")
	}
}

func TestChosenShellIsKeptByReconnectDuplicateAndRestart(t *testing.T) {
	skipWithoutUnixShell(t)
	options := testOptions(t)
	first := newTestState(t, options)
	closeLocalShells(t, first)
	withShellChoices(t, first, []localshell.Choice{{Name: "Alpha", Path: "/bin/sh", Default: true}, {Name: "Beta", Path: "/bin/bash"}}, nil)
	first.localShell = "/bin/sh"
	first.openLocalShellPicker(gfx.Point{X: 10, Y: 10}, false)
	first.Click(hitLocalShellPickerItems.At(1))
	session, _ := first.sessionByIDPointer(first.activeSessionID)

	// Changing the default later must not move a tab that was given a shell.
	first.localShell = "/bin/sh"
	first.reconnect(session)
	if got := localShellOf(t, session); got != "/bin/bash" {
		t.Fatalf("reconnect started %q, want the tab's own /bin/bash", got)
	}
	copied := first.newHostSession(session.host)
	if got := localShellOf(t, copied); got != "/bin/bash" {
		t.Fatalf("duplicate started %q, want /bin/bash", got)
	}

	first.save()
	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Sessions) == 0 || saved.Sessions[0].LocalShell != "/bin/bash" {
		t.Fatalf("saved sessions = %+v, want the shell kept", saved.Sessions)
	}
	second := newTestState(t, options)
	if len(second.sessions) == 0 || second.sessions[0].host.localShell != "/bin/bash" || second.sessions[0].Title != "Beta" {
		t.Fatalf("restored session = %+v, want the tab's shell and name back", second.sessions)
	}
}

func TestSavedLocalShellThatCannotBeRunIsIgnored(t *testing.T) {
	for _, shell := range []string{"relative/pwsh.exe", "wsl:not a distro", "/bin/sh\x00--evil", ""} {
		if validLocalShell(shell) {
			t.Errorf("%q was accepted as a local shell", shell)
		}
	}
	program := "/bin/sh"
	if runtime.GOOS == "windows" {
		program = `C:\Program Files\PowerShell\7\pwsh.exe`
	}
	for _, shell := range []string{program, localshell.WSLShell("Ubuntu-22.04")} {
		if !validLocalShell(shell) {
			t.Errorf("%q was refused as a local shell", shell)
		}
	}
}

func TestNewTabPickerExpandsShellsInsideTheSearchableList(t *testing.T) {
	skipWithoutUnixShell(t)
	state := newTestState(t, testOptions(t))
	closeLocalShells(t, state)
	withShellChoices(t, state, []localshell.Choice{{Name: "Alpha", Path: "/bin/sh", Default: true}, {Name: "Beta", Path: "/bin/bash"}}, nil)
	state.localShell = "/bin/sh"

	state.openConnectPicker()
	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.LocalTerminal) {
		t.Fatalf("the default Local Terminal entry is gone: %q", texts)
	}
	for _, hidden := range []string{state.messages.LocalTerminal + " · Alpha", state.messages.LocalTerminal + " · Beta"} {
		if contains(texts, hidden) {
			t.Fatalf("the unfiltered picker listed %q twice: %q", hidden, texts)
		}
	}
	if _, ok := hitByID(canvas, hitConnectPickShellPicker); !ok {
		t.Fatal("the Local Terminal row has no way to choose a shell")
	}

	state.Click(hitConnectPickShellPicker)
	if state.modal != modalCommandPalette || !state.commandPalette.connect || !state.commandPalette.localShells || state.localShellPicker.open {
		t.Fatalf("the arrow left the searchable list: modal=%d connect=%v shells=%v picker=%+v", state.modal, state.commandPalette.connect, state.commandPalette.localShells, state.localShellPicker)
	}
	_, texts = paintedTexts(state)
	for _, want := range []string{state.messages.LocalTerminal + " · Alpha", state.messages.LocalTerminal + " · Beta"} {
		if !contains(texts, want) {
			t.Fatalf("the expanded list does not offer %q; it shows %q", want, texts)
		}
	}

	// The search above the list filters the shells without closing it.
	state.commandPalette.search.Insert("beta")
	_, texts = paintedTexts(state)
	if contains(texts, state.messages.LocalTerminal+" · Alpha") || !contains(texts, state.messages.LocalTerminal+" · Beta") {
		t.Fatalf("searching beta shows %q", texts)
	}
	tabs := len(state.workspaceTabs)
	state.Click(hitCommandPaletteItems.At(0)) // the one row the search left
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || localShellOf(t, session) != "/bin/bash" || len(state.workspaceTabs) != tabs+1 {
		t.Fatalf("session ok=%v tabs %d -> %d", ok, tabs, len(state.workspaceTabs))
	}
}

func TestNewTabPickerHasNoShellArrowWhenThereIsOneShell(t *testing.T) {
	state := newTestState(t, testOptions(t))
	withShellChoices(t, state, []localshell.Choice{{Name: "Alpha", Path: "/bin/sh", Default: true}}, nil)
	state.openConnectPicker()
	canvas, texts := paintedTexts(state)
	if contains(texts, state.messages.LocalTerminal+" · Alpha") {
		t.Fatalf("a lone shell was listed twice: %q", texts)
	}
	if _, ok := hitByID(canvas, hitConnectPickShellPicker); ok {
		t.Fatal("a lone shell left an empty shell menu")
	}
}

func TestNewTabPickerSearchFindsShellsWhileCollapsed(t *testing.T) {
	skipWithoutUnixShell(t)
	for _, recent := range []bool{false, true} {
		name := "default"
		if recent {
			name = "recent"
		}
		t.Run(name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			closeLocalShells(t, state)
			withShellChoices(t, state, []localshell.Choice{
				{Name: "zsh", Path: "/bin/sh", Default: true},
				{Name: "bash", Path: "/bin/bash"},
			}, nil)
			state.localShell = "/bin/sh"
			state.recentConnectSort = recent

			state.openConnectPicker()
			_, texts := paintedTexts(state)
			if state.commandPalette.localShells {
				t.Fatal("the picker opened expanded")
			}
			if contains(texts, state.messages.LocalTerminal+" · bash") {
				t.Fatalf("the collapsed picker listed a shell before it was searched for: %q", texts)
			}

			state.commandPalette.search.Insert("bash")
			canvas, texts := paintedTexts(state)
			want := state.messages.LocalTerminal + " · bash"
			if !contains(texts, want) {
				t.Fatalf("searching bash while collapsed shows %q, want %q", texts, want)
			}
			if contains(texts, state.messages.LocalTerminal+" · zsh") {
				t.Fatalf("searching bash also shows zsh: %q", texts)
			}
			if _, ok := hitByID(canvas, hitCommandPaletteItems.At(0)); !ok {
				t.Fatal("the matching shell has no row to click")
			}

			tabs := len(state.workspaceTabs)
			state.Click(hitCommandPaletteItems.At(0))
			session, ok := state.sessionByIDPointer(state.activeSessionID)
			if !ok {
				t.Fatal("choosing the searched shell did not open a tab")
			}
			if got := localShellOf(t, session); got != "/bin/bash" || len(state.workspaceTabs) != tabs+1 {
				t.Fatalf("shell=%q tabs %d -> %d", got, tabs, len(state.workspaceTabs))
			}
		})
	}
}

func TestNewTabPickerKeyboardExpandsAndCollapsesShells(t *testing.T) {
	state := newTestState(t, testOptions(t))
	withShellChoices(t, state, []localshell.Choice{{Name: "Alpha", Path: "/bin/sh", Default: true}, {Name: "Beta", Path: "/bin/bash"}}, nil)
	state.openConnectPicker()
	paintedTexts(state)
	local := hitCommandPaletteItems.At(0)
	if result := state.Key(gfx.KeyEvent{Target: local, Key: gfx.KeyRight}); !result.Handled || !state.commandPalette.localShells {
		t.Fatalf("Right did not expand the shells: result=%+v expanded=%v", result, state.commandPalette.localShells)
	}
	if result := state.Key(gfx.KeyEvent{Target: hitCommandPaletteItems.At(0), Key: gfx.KeyLeft}); !result.Handled || state.commandPalette.localShells {
		t.Fatalf("Left did not collapse the shells: result=%+v expanded=%v", result, state.commandPalette.localShells)
	}
}
