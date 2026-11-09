package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/remotetools"
)

func replaceRemoteQuery(t *testing.T, state *State, query string) {
	t.Helper()
	input := state.RemoteToolsInput(app.TextInputContext{ID: hitRemoteSearch})
	if input == nil {
		t.Fatal("remote search has no native input")
	}
	input.OnEvent(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll})
	input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: query})
}

func TestRemoteSearchFindsIdleProcessesAndNeverSendsQueries(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	state.remote.procs = nil
	for i := 1; i <= 100; i++ {
		state.remote.procs = append(state.remote.procs, remotetools.Process{PID: i, User: "root", CPU: float64(100 - i), Command: fmt.Sprintf("service-%d", i)})
	}
	state.remote.procs = append(state.remote.procs, remotetools.Process{PID: 9999, User: "oracle", Command: "audit-NEEDLE"})
	state.rebuildRemoteRows()
	before := server.ran("LC_ALL=C ps")
	state.remote.selected = "1"
	for _, query := range []string{"needle", "ORACLE", "9999"} {
		replaceRemoteQuery(t, state, query)
		if len(state.remote.rows) != 1 || state.remote.rows[0].key != "9999" || state.remote.selected != "" {
			t.Fatalf("query=%q rows=%+v selected=%q", query, state.remote.rows, state.remote.selected)
		}
	}
	replaceRemoteQuery(t, state, "'; $(touch /tmp/not-run) | java")
	if len(state.remote.rows) != 0 || server.ran("LC_ALL=C ps") != before {
		t.Fatal("search text entered remote commands or was not treated literally")
	}
	state.Click(hitRemoteSearchClear)
	if len(state.remote.rows) != 101 || state.remote.searches[remoteProcesses].editor.String() != "" {
		t.Fatal("clearing search did not restore the complete loaded snapshot")
	}
}

func TestRemoteSearchQueriesStayIndependentAcrossTabsAndRefresh(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	replaceRemoteQuery(t, state, "NGINX")
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if len(state.remote.rows) != 2 {
		t.Fatal("process query leaked into containers")
	}
	replaceRemoteQuery(t, state, "old-job")
	if len(state.remote.rows) != 1 {
		t.Fatal("container search failed")
	}
	state.Click(hitRemoteRefresh)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if len(state.remote.rows) != 1 {
		t.Fatal("refresh lost container query")
	}
	state.Click(hitRemoteTabProcesses)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if len(state.remote.rows) != 1 || state.remote.rows[0].cells[0] != "2211" {
		t.Fatal("returning to process tab lost its independent query")
	}
}

func TestRemoteSearchNativeFocusTypingAndEscape(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	driver, err := app.NewDriver(app.Config{Title: "remote search", Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnShortcut: state.Shortcut, OnPointer: state.Pointer, TextInput: state.RemoteToolsInput}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	find := state.shortcuts.chords.Chord(shortcutFind)
	if err := driver.Chord(find.Modifiers, string(find.Char)); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != hitRemoteSearch {
		t.Fatal("find did not focus remote search")
	}
	before := server.ran("LC_ALL=C ps")
	if err := driver.Type("PyThOn"); err != nil {
		t.Fatal(err)
	}
	if len(state.remote.rows) != 1 || state.remote.rows[0].cells[0] != "3000" {
		t.Fatal("native text input did not filter process rows")
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalRemoteTools || len(state.remote.rows) != 3 {
		t.Fatal("Escape closed tools instead of clearing query")
	}
	if server.ran("LC_ALL=C ps") != before {
		t.Fatal("native typing caused remote requests")
	}
	state.remote.processesTruncated, state.remote.commandsTruncated = true, true
	state.rebuildRemoteRows()
	if !strings.Contains(state.remote.searchSummary, state.messages.Tools.RemoteSearchLimited) {
		t.Fatal("truncated search scope is not labeled")
	}
}

func TestRemoteSearchEscapeClearsQueryAfterResultsTakeFocus(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	replaceRemoteQuery(t, state, "nginx")
	result := state.remoteSearchKey(gfx.KeyEvent{Target: hitRemoteRowList.At(0), Key: gfx.KeyEscape})
	if !result.Handled || state.remote.searches[remoteProcesses].editor.String() != "" || len(state.remote.rows) != 3 {
		t.Fatal("Escape from the result list did not clear its query before closing")
	}
}

func TestRemoteSearchZeroMatchesDoesNotMeanNoDockerObjects(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading })
	replaceRemoteQuery(t, state, "no-matching-container")
	state.Click(hitRemoteRefresh)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if state.remote.sourceTotal != 2 || len(state.remote.rows) != 0 || state.remote.status == state.messages.Tools.RemoteEmptyContainers {
		t.Fatalf("filtered snapshot was reported as an empty host: %q", state.remote.status)
	}
}

func TestRemoteSearchNativeUtilityWindowUsesItsOwnInput(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		state, _, fake, _ := liveSession(t)
		server := &remoteServer{}
		fake.mu.Lock()
		fake.runCommand = server.run
		fake.mu.Unlock()
		state.options.OpenContentWindow = application.OpenContentWindow
		state.Click(hitRemoteToolsOpen)
		w := state.remoteWindows[0]
		host.until(t, func() bool { return !w.ui.remote.loading })
		driver, err := app.NewDriver(host.windows[0].config, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer driver.Close()
		state.shortcuts.bind(shortcutFind, keymap.Chord{Key: gfx.KeyF8})
		if err := driver.Key(gfx.KeyF8, 0); err != nil {
			t.Fatal(err)
		}
		if driver.Snapshot().Focused != hitRemoteSearch {
			t.Fatal("named Find binding did not focus the utility window's search")
		}
		state.shortcuts = defaultShortcuts()
		find := state.shortcuts.chords.Chord(shortcutFind)
		if err := driver.Chord(find.Modifiers, string(find.Char)); err != nil {
			t.Fatal(err)
		}
		if err := driver.Type("ALICE"); err != nil {
			t.Fatal(err)
		}
		if len(w.ui.remote.rows) != 1 || w.ui.remote.rows[0].cells[0] != "3000" || state.modal != modalNone {
			t.Fatal("native utility search was routed to the workbench or lost its source")
		}
		if err := driver.Key(gfx.KeyEscape, 0); err != nil {
			t.Fatal(err)
		}
		if w.closed || len(w.ui.remote.rows) != 3 {
			t.Fatal("Escape closed utility instead of clearing its active query")
		}
	})
}
