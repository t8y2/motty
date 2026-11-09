package workbench

import (
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

func recentHostHint(t *testing.T, state *State, label string) string {
	t.Helper()
	state.Click(hitNewTab)
	state.Click(hitConnectSortRecent)
	canvas, _ := paintedTexts(state)
	for _, hit := range canvas.Hits() {
		if hit.ID.Scope() == hitCommandPaletteItems && hit.Label == label {
			return hit.Hint
		}
	}
	t.Fatalf("no picker row for %q", label)
	return ""
}

func TestRecentPickerSwitchTakesTheKeyboardAndHandsItBack(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitNewTab)
	if state.modal != modalCommandPalette {
		t.Fatal("the picker did not open")
	}
	result := state.Key(gfx.KeyEvent{Target: hitConnectSortDefault, Key: gfx.KeyRight})
	if !result.Handled || !state.commandPalette.workspaces {
		t.Fatalf("right on the switch = %+v, workspaces=%t", result, state.commandPalette.workspaces)
	}
	if result.Focus != hitCommandPaletteSearch {
		t.Fatalf("the keyboard stayed on the switch: %+v", result)
	}
	result = state.Key(gfx.KeyEvent{Target: hitConnectSortWorkspaces, Key: gfx.KeyRight})
	if !result.Handled || !state.recentConnectSort || state.commandPalette.workspaces {
		t.Fatalf("right from workspaces = %+v, recent=%t workspaces=%t", result, state.recentConnectSort, state.commandPalette.workspaces)
	}
	result = state.Key(gfx.KeyEvent{Target: hitConnectSortRecent, Key: gfx.KeyEnter})
	if !result.Handled || result.Focus != hitCommandPaletteSearch {
		t.Fatalf("enter on the switch = %+v", result)
	}
}

func TestRecentPickerRecordsOnlyTargetSuccessAndNotStatusRefresh(t *testing.T) {
	for _, test := range []struct {
		name    string
		phase   sshconn.Phase
		role    sshconn.EndpointRole
		success bool
	}{
		{"target success", sshconn.PhaseConnected, sshconn.EndpointTarget, true},
		{"failed login", sshconn.PhaseFailed, sshconn.EndpointTarget, false},
		{"jump success", sshconn.PhaseConnected, sshconn.EndpointProxyJump, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			world, options := newWorld(t)
			now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
			options.WallNow = func() time.Time { return now }
			state := newTestState(t, options)
			connection := connect(t, state, world)
			connection.set(func(status *sshconn.Status) { status.Phase = test.phase; status.EndpointRole = test.role })
			state.Wake()
			successAt := now
			now = now.Add(time.Hour)
			// A new status version without a new user dial cannot move the timestamp.
			connection.set(func(status *sshconn.Status) { status.ServerVersion = "SSH-test" })
			state.Wake()
			want := ""
			if test.success {
				want = successAt.Local().Format("2006-01-02 15:04:05 MST")
			}
			if got := recentHostHint(t, state, state.hosts[0].Label); got != want {
				t.Fatalf("time = %q, want %q", got, want)
			}
		})
	}
}

func TestRecentPickerDoesNotRecordAutomaticRestore(t *testing.T) {
	world, options := restoredWorld(t, restoreConnectAll, false)
	state := newTestState(t, options)
	state.Wake()
	for _, conn := range world.conns[2:] {
		conn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	}
	state.Wake()
	if got := recentHostHint(t, state, state.hosts[0].Label); got != "" {
		t.Fatalf("automatic restore recorded %q", got)
	}
}

func TestRecentPickerDoesNotRecordAutomaticReconnect(t *testing.T) {
	state, world, connection, clock := reconnectingSession(t, reconnectThree)
	// The first manual success has a timestamp; the automatic one must keep it.
	before := recentHostHint(t, state, state.hosts[0].Label)
	state.Click(hitCommandPaletteClose)
	state.options.WallNow = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	disconnectEstablished(connection, state)
	*clock += time.Second
	state.Wake()
	world.conns[len(world.conns)-1].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	if got := recentHostHint(t, state, state.hosts[0].Label); got != before {
		t.Fatalf("automatic retry changed %q -> %q", before, got)
	}
}

func TestRecentPickerRelativeTimeUnits(t *testing.T) {
	for _, test := range []struct {
		age  time.Duration
		want string
	}{
		{0, "Just now"}, {time.Second, "1s ago"}, {59 * time.Second, "59s ago"}, {time.Minute, "1m ago"},
		{time.Hour, "1h ago"}, {24 * time.Hour, "1d ago"}, {7 * 24 * time.Hour, "1w ago"},
		{30 * 24 * time.Hour, "1mo ago"}, {365 * 24 * time.Hour, "1y ago"},
	} {
		t.Run(test.want, func(t *testing.T) {
			options := testOptions(t)
			now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
			options.WallNow = func() time.Time { return now }
			if err := saveState(options.StatePath, savedState{RecentConnectSort: true, RecentConnections: map[string]time.Time{"local-terminal": now.Add(-test.age)}}); err != nil {
				t.Fatal(err)
			}
			state := newTestState(t, options)
			state.Click(hitNewTab)
			_, texts := paintedTexts(state)
			if !slices.Contains(texts, test.want) {
				t.Fatalf("want %q in %v", test.want, texts)
			}
		})
	}
}

func TestRecentPickerLocalInstancesShareSuccessfulStartupTime(t *testing.T) {
	skipWithoutUnixShell(t)
	options := testOptions(t)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	options.WallNow = func() time.Time { return now }
	state := newTestState(t, options)
	state.networkProxy = networkProxyDirect
	state.localShell = "/bin/sh"
	closeLocalShells(t, state)
	state.Click(hitLocalTerminal)
	state.Click(hitNewTab)
	state.Click(hitConnectSortRecent)
	first := pickerRowLabels(state)
	if first[0] != state.messages.LocalTerminal {
		t.Fatalf("local did not sort first: %v", first)
	}
	state.Click(hitCommandPaletteClose)
	now = now.Add(time.Hour)
	state.Click(hitLocalTerminal)
	state.Click(hitNewTab)
	state.Click(hitConnectSortRecent)
	canvas, _ := paintedTexts(state)
	hit, ok := hitByID(canvas, hitCommandPaletteItems.At(0))
	if !ok || hit.Hint != now.Local().Format("2006-01-02 15:04:05 MST") {
		t.Fatalf("second local time = %q", hit.Hint)
	}
	state.Click(hitCommandPaletteClose)
	state.localShell = "/does-not-exist/motty-test-shell"
	now = now.Add(time.Hour)
	state.Click(hitLocalTerminal)
	state.Click(hitNewTab)
	state.Click(hitConnectSortRecent)
	canvas, _ = paintedTexts(state)
	hit, _ = hitByID(canvas, hitCommandPaletteItems.At(0))
	if hit.Hint != now.Add(-time.Hour).Local().Format("2006-01-02 15:04:05 MST") {
		t.Fatalf("failed shell changed time to %q", hit.Hint)
	}
}

func TestRecentPickerKeepsSuccessesFromOtherWindows(t *testing.T) {
	_, options := newWorld(t)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	options.WallNow = func() time.Time { return now }
	// Persist the shared catalog before opening stale windows; upstream now
	// rejects concurrent creation of the same records with different IDs.
	first := newTestState(t, options)
	if _, err := writeConfig(first.config, first.configBase, first.configDefaults, first.snapshot()); err != nil {
		t.Fatal(err)
	}
	second := newTestState(t, options)
	idle := newTestState(t, options)
	succeed := func(state *State, host int) {
		t.Helper()
		// Exercise successful-connection persistence without competing workspace edits.
		state.rememberRecentConnection(&Session{host: state.hosts[host], recordRecent: true})
	}
	succeed(first, 0)
	productionAt := now
	now = now.Add(time.Hour)
	succeed(second, 1)
	stagingAt := now
	// An old window saving another local choice must keep both records too.
	idle.recentConnectSort = true
	idle.save()
	restarted := newTestState(t, options)
	for index, at := range []time.Time{productionAt, stagingAt} {
		if got := recentHostHint(t, restarted, restarted.hosts[index].Label); got != at.Local().Format("2006-01-02 15:04:05 MST") {
			t.Errorf("recent time for %s = %q, want %v", restarted.hosts[index].Label, got, at)
		}
		restarted.Click(hitCommandPaletteClose)
	}
	// A writer with an older clock must not replace a newer success for the same host.
	now = productionAt.Add(-time.Hour)
	succeed(first, 1)
	restarted = newTestState(t, options)
	if got := recentHostHint(t, restarted, restarted.hosts[1].Label); got != stagingAt.Local().Format("2006-01-02 15:04:05 MST") {
		t.Errorf("older success replaced Staging time: %q", got)
	}
}
