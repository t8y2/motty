package workbench

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/remotetools"
)

func TestRemoteProcessRefreshFailurePreservesStaleRowsAndRecovers(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	stamp := state.remote.collectedAt
	selectRemoteToolsRow(state, 0)
	selected := state.remote.selected
	server.mu.Lock()
	server.failures["processes"] = errors.New("listing failed")
	server.mu.Unlock()
	state.Click(hitRemoteRefresh)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if len(state.remote.rows) != 3 || state.remote.selected != selected || !state.remote.stale || state.remote.collectedAt != stamp || stamp.IsZero() {
		t.Fatalf("lost successful snapshot: %+v", state.remote)
	}
	actions, count := state.remoteToolActions()
	for _, action := range actions[1:count] {
		if !action.Disabled {
			t.Fatalf("stale mutation is enabled: %+v", action)
		}
	}
	for _, id := range []gfx.ID{hitRemoteKill, hitRemoteTerminate, hitRemoteNiceLower} {
		state.Click(id)
		if state.modal != modalRemoteTools || server.ran("kill") != 0 || server.ran("renice") != 0 {
			t.Fatal("direct dispatch bypassed stale-state guard")
		}
	}
	state.Click(hitRemoteRowList.At(0))
	canvas := &gfx.Canvas{}
	driver, err := app.NewDriver(app.Config{Title: "stale processes", Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer}, canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if hit, ok := hitByID(canvas, hitRemoteSnapshotStatus); !ok || !strings.Contains(hit.Label, stamp.Format("15:04:05")) {
		t.Fatalf("native stale status is missing timestamp: %q found=%v", state.remote.snapshotStatus, ok)
	}
	kill, ok := hitByID(canvas, hitRemoteKill)
	if !ok || !kill.Disabled {
		t.Fatal("native stale signal button is not disabled")
	}
	if err := driver.Click(kill.Bounds.X+kill.Bounds.Width/2, kill.Bounds.Y+kill.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalRemoteTools || server.ran("kill") != 0 {
		t.Fatal("native pointer activated a stale mutation")
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape}); !result.Handled {
		t.Fatal("Escape did not dismiss the stale process details")
	}
	server.mu.Lock()
	delete(server.failures, "processes")
	server.mu.Unlock()
	state.Click(hitRemoteRefresh)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if state.remote.stale || state.remote.failed || !state.remote.collectedAt.After(stamp) {
		t.Fatal("successful refresh did not restore freshness")
	}
}

func TestRemoteProcessSelectionNeverRetargetsAReusedPID(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	selectRemoteToolsRow(state, 0)
	selected := state.remote.selected
	state.remote.procs[0].StartIdentity = "linux:aaaaaaaa-1234-1234-1234-123456789abc:999"
	state.rebuildRemoteRows()
	if state.remote.selected != "" || state.remote.rows[0].key == selected {
		t.Fatal("refresh rebound selection to a replacement process")
	}
}

func TestRemoteProcessConfirmationCannotEscapeCapturedScope(t *testing.T) {
	for _, change := range []string{"selection", "session", "stale", "identity"} {
		t.Run(change, func(t *testing.T) {
			state, server, _ := openRemoteToolsDialog(t)
			selectRemoteToolsRow(state, 0)
			state.Click(hitRemoteKill)
			if state.modal != modalForm {
				t.Fatal("missing process confirmation")
			}
			switch change {
			case "selection":
				state.remote.selected = ""
			case "session":
				state.remote.session++
			case "stale":
				state.remote.stale = true
			case "identity":
				state.remote.procs[0].StartIdentity = ""
			}
			state.Click(hitFormSubmit)
			if server.ran("kill") != 0 || state.remote.loading {
				t.Fatal("confirmation acted after target scope changed")
			}
		})
	}
}

func TestRemoteIdentityUnavailableKeepsSearchButDisablesProcessActions(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	for i := range state.remote.procs {
		state.remote.procs[i].StartIdentity = ""
	}
	state.updateRemoteSnapshotStatus()
	state.rebuildRemoteRows()
	if !strings.Contains(state.remote.snapshotStatus, state.messages.Tools.RemoteProcessUnverifiable) {
		t.Fatal("read-only snapshot did not explain disabled process actions")
	}
	selectRemoteToolsRow(state, 0)
	actions, count := state.remoteToolActions()
	for _, action := range actions[1:count] {
		if !action.Disabled {
			t.Fatalf("unverifiable process action enabled: %+v", action)
		}
	}
	rows := state.remoteProcessMenuRows(state.remote.procs[0])
	for _, entry := range rows.entries[:rows.count] {
		if entry.item.ID == hitRemoteProcessMenuTerminate ||
			entry.item.ID == hitRemoteProcessMenuKill ||
			entry.item.ID == hitRemoteProcessMenuNiceLower ||
			entry.item.ID == hitRemoteProcessMenuNiceNormal ||
			entry.item.ID == hitRemoteProcessMenuNiceHigher {
			if !entry.item.Disabled {
				t.Fatalf("unverifiable process action enabled: %+v", entry.item)
			}
		}
	}
	state.Click(hitRemoteKill)
	if state.modal != modalRemoteTools || state.remote.status != state.messages.Tools.RemoteProcessUnverifiable {
		t.Fatal("unsupported identity did not explain read-only state")
	}
}

func TestRemoteGuardFailureRequiresRefreshAndReselection(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	selectRemoteToolsRow(state, 0)
	state.runRemoteAction("should not succeed", func(context.Context, remotetools.Runner) error { return remotetools.ErrProcessChanged })
	waitFor(t, state, func() bool { return !state.remote.loading })
	if !state.remote.stale || state.remote.selected != "" || server.ran("kill") != 0 || !strings.Contains(state.remote.status, state.messages.Tools.RemoteProcessChanged) {
		t.Fatal("identity rejection allowed another action without refresh")
	}
}

func TestRemoteProcessActionTimeoutRequiresFreshSnapshot(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	selectRemoteToolsRow(state, 0)
	state.runRemoteAction("unknown outcome", func(context.Context, remotetools.Runner) error { return context.DeadlineExceeded })
	waitFor(t, state, func() bool { return !state.remote.loading })
	if !state.remote.stale || state.remote.selected != "" {
		t.Fatal("timeout left the uncertain process target actionable")
	}
}

func TestRemoteInitialFailureHasNoSuccessfulTimestamp(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	fake.runCommand = func(context.Context, string, int) ([]byte, error) { return nil, errors.New("offline") }
	state.remote.tab = remoteVolumes
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.openRemoteTools(session)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if len(state.remote.rows) != 0 || !state.remote.collectedAt.IsZero() || !state.remote.failed {
		t.Fatal("initial error pretended to retain a successful snapshot")
	}
	actions, count := state.remoteToolActions()
	for _, action := range actions[1:count] {
		if !action.Disabled {
			t.Fatal("initial failed listing enabled mutation without a fresh snapshot")
		}
	}
}

func TestRemotePIDReuseBetweenConfirmationAndExecutionNeverSignals(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	selectRemoteToolsRow(state, 0)
	state.Click(hitRemoteKill)
	if state.modal != modalForm {
		t.Fatal("no confirmation")
	}
	server.mu.Lock()
	server.identities = strings.ReplaceAll(processIdentityFixture, "2211 221101", "2211 221102")
	server.mu.Unlock()
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if server.ran("kill") != 0 || !state.remote.stale || state.remote.selected != "" || state.remote.status != state.messages.Tools.RemoteProcessChanged {
		t.Fatal("confirmation signaled a reused PID or left it actionable")
	}
	state.Click(hitRemoteRefresh)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if state.remote.stale || state.remote.selected != "" {
		t.Fatal("refresh silently selected a replacement")
	}
}

func TestRemoteFailedTabSwitchNeverShowsAnotherTabsTimestamp(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	previous := state.remote.snapshotStatus
	server.mu.Lock()
	server.failures["docker ps"] = errors.New("missing docker")
	server.mu.Unlock()
	state.Click(hitRemoteTabContainers)
	if !state.remote.collectedAt.IsZero() || state.remote.snapshotStatus != "" || strings.Contains(state.remote.searchSummary, previous) {
		t.Fatal("tab switch painted the previous tab's collection timestamp")
	}
	waitFor(t, state, func() bool { return !state.remote.loading })
	if len(state.remote.rows) != 0 || !state.remote.collectedAt.IsZero() || state.remote.snapshotStatus != "" {
		t.Fatal("failed tab switch retained unrelated successful rows/timestamp")
	}
}

func TestRemoteProcessActionPaintStateDoesNotAllocate(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	selectRemoteToolsRow(state, 0)
	// The completion catalog is loaded by a background wake. Let it finish so
	// the allocation sample observes this paint path rather than unrelated I/O.
	waitFor(t, state, func() bool { return !state.completion.catalogLoading })
	if allocations := testing.AllocsPerRun(100, func() { state.remoteToolActions() }); allocations != 0 {
		t.Fatalf("process action paint state allocates: %g", allocations)
	}
	var canvas gfx.Canvas
	// Keep the auto-refresh clock frame deterministic. A wall clock advances
	// between allocation samples and intentionally tessellates the next hand
	// phase, which measures animation-cache growth rather than list painting.
	canvas.SetClock(func() time.Duration { return time.Second })
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("unchanged process snapshot paint allocates: %g", allocations)
	}
	state.remote.auto.enabled = true
	state.remote.auto.restart()
	canvas.Reset()
	state.Paint(&canvas, size)
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("auto-refresh process snapshot paint allocates: %g", allocations)
	}
}

func BenchmarkRemoteProcessSnapshotSafetyPaint(b *testing.B) {
	state, _, _, _ := liveSession(b)
	state.modal = modalRemoteTools
	state.remote = remoteToolsView{session: state.activeSessionID, collectedAt: time.Unix(1000, 0)}
	for i := 0; i < 5000; i++ {
		state.remote.procs = append(state.remote.procs, remotetools.Process{PID: i + 2, User: "alice", Command: "worker", StartIdentity: "linux:12345678-1234-1234-1234-123456789abc:100"})
	}
	state.remote.selected = remotetools.ProcessKey(state.remote.procs[4999])
	state.updateRemoteSnapshotStatus()
	state.rebuildRemoteRows()
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func TestRemoteTruncationHintRemainsReachableThroughDriverHitTest(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.remote.processesTruncated = true
	state.rebuildRemoteRows()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(app.Config{Title: "narrow snapshot status", Width: 600, Height: 700, Tree: Tree(), Paint: state.Paint, OnKey: state.Key}, &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	summary, ok := hitByID(&canvas, hitRemoteSearchScope)
	if !ok {
		t.Fatal("no search summary")
	}
	id, ok := driver.HitTest(summary.Bounds.X+summary.Bounds.Width/2, summary.Bounds.Y+summary.Bounds.Height/2)
	if !ok {
		t.Fatal("summary has no pointer target")
	}
	hit, ok := hitByID(&canvas, id)
	if !ok || hit.Hint != state.remote.searchSummary || !strings.Contains(hit.Hint, state.messages.Tools.RemoteSearchLimited) {
		t.Fatalf("actual summary pointer target hides bounded search scope: id=%v hint=%q", id, hit.Hint)
	}
}

func TestRemoteSnapshotStatusDoesNotBlameIdentityForAnEmptyList(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.remote.tab = remoteProcesses
	state.remote.collectedAt = time.Now()
	state.remote.procs = nil
	state.updateRemoteSnapshotStatus()
	if strings.Contains(state.remote.snapshotStatus, state.messages.Tools.RemoteProcessUnverifiable) {
		t.Fatalf("an empty process list was blamed on process identity: %q", state.remote.snapshotStatus)
	}
}

func TestRemoteLogsWaitsForAFreshSnapshot(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.remote.tab = remoteContainers
	state.remote.stale = true
	state.remote.collectedAt = time.Now()
	if remoteMutation(hitRemoteLogs) == false {
		t.Fatal("opening container logs bypasses the snapshot gate")
	}
}
