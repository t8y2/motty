package workbench

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/shellkind"
)

type completionPOSIXShell struct{ *fakeShell }

func (completionPOSIXShell) Kind() shellkind.Kind { return shellkind.POSIX }

func waitCompletionProbe(t *testing.T, state *State, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("completion probe did not settle")
}

func completionAvailabilityCalls(conn *fakeConn) int {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	calls := 0
	for _, command := range conn.commandCalls {
		if strings.Contains(command, `command -v "$name"`) {
			calls++
		}
	}
	return calls
}

func TestCompletionProbeOptInRanksAvailableRoots(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	conn := w.session.conn.(*fakeConn)
	conn.runCommand = func(_ context.Context, command string, _ int) ([]byte, error) {
		if !strings.Contains(command, `command -v "$name"`) {
			return nil, nil
		}
		return []byte("cat\n"), nil
	}
	w.type_("ca")
	w.state.openCompletions()
	if len(conn.commandCalls) != 0 {
		t.Fatal("the host was probed before opt-in")
	}
	w.state.closeCompletionList()
	w.state.completion.probeEnabled = true
	w.state.openCompletions()
	waitCompletionProbe(t, w.state, func() bool {
		return w.state.completion.list.active && !w.state.completion.probe.pending && w.state.completion.probe.available["cat"]
	})
	calls := completionAvailabilityCalls(conn)
	if calls != 1 {
		t.Fatalf("probe ran %d times, want one", calls)
	}
}

func TestCompletionProbeIgnoresStaleConnectionAndDeduplicates(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	conn := w.session.conn.(*fakeConn)
	started := make(chan struct{})
	release := make(chan struct{})
	conn.runCommand = func(ctx context.Context, command string, _ int) ([]byte, error) {
		if !strings.Contains(command, `command -v "$name"`) {
			return nil, nil
		}
		close(started)
		select {
		case <-release:
			return []byte("cat\n"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	w.state.completion.probeEnabled = true
	w.type_("ca")
	w.state.openCompletions()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("opted-in probe did not start")
	}
	w.state.openCompletions()
	calls := completionAvailabilityCalls(conn)
	if calls != 1 {
		t.Fatalf("duplicate in-flight probe calls = %d", calls)
	}
	w.session.conn = &fakeConn{}
	close(release)
	for index := 0; index < 5; index++ {
		w.state.Wake()
	}
	for _, candidate := range w.state.completion.list.candidates {
		if candidate.Text == "cat" && w.state.completionAvailable(w.session, candidate) {
			t.Fatal("old connection's availability marked the new connection")
		}
	}
}

func TestCompletionProbeRespectsOffAndNoHistory(t *testing.T) {
	for _, test := range []struct {
		name      string
		noHistory bool
	}{
		{"Off", false},
		{"NoHistory", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := newCompletionWorld(t)
			w.session.term.shell = completionPOSIXShell{w.shell}
			conn := w.session.conn.(*fakeConn)
			conn.runCommand = func(context.Context, string, int) ([]byte, error) {
				t.Fatal("disabled completion ran a command")
				return nil, nil
			}
			w.type_("ca")
			w.state.completion.probeEnabled = true
			w.session.host.NoHistory = test.noHistory
			if !test.noHistory {
				w.state.setCompletionMode(completionOff)
			}
			w.state.openCompletions()
			w.state.Wake()
		})
	}
}

func TestCompletionProbeAvailabilityExpiresWithItsConnectionCache(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	conn := w.session.conn.(*fakeConn)
	conn.runCommand = func(_ context.Context, command string, _ int) ([]byte, error) {
		if !strings.Contains(command, `command -v "$name"`) {
			return nil, nil
		}
		return []byte("cat\n"), nil
	}
	w.state.completion.probeEnabled = true
	w.type_("ca")
	w.state.openCompletions()
	waitCompletionProbe(t, w.state, func() bool { return !w.state.completion.probe.pending && w.state.completion.probe.available["cat"] })
	w.state.openCompletions()
	before := completionAvailabilityCalls(conn)
	if before != 1 {
		t.Fatalf("cache did not avoid a repeated probe: %d calls", before)
	}
	w.clock += completionProbeCache + time.Millisecond
	w.state.openCompletions()
	waitCompletionProbe(t, w.state, func() bool {
		return completionAvailabilityCalls(conn) == 2
	})
}

func TestCompletionProbeCancelsWhenConnectionChanges(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	conn := w.session.conn.(*fakeConn)
	started := make(chan struct{})
	canceled := make(chan struct{})
	conn.runCommand = func(ctx context.Context, command string, _ int) ([]byte, error) {
		if !strings.Contains(command, `command -v "$name"`) {
			return nil, nil
		}
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}
	w.state.completion.probeEnabled = true
	w.type_("ca")
	w.state.openCompletions()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	w.session.conn = &fakeConn{}
	w.state.Wake()
	select {
	case <-canceled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("connection replacement did not cancel the pending probe")
	}
}

func TestCompletionProbeFailureKeepsStaticCandidates(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	conn := w.session.conn.(*fakeConn)
	conn.runCommand = func(context.Context, string, int) ([]byte, error) {
		return nil, errors.New("server refused another channel")
	}
	w.state.completion.probeEnabled = true
	w.type_("ca")
	w.state.openCompletions()
	waitCompletionProbe(t, w.state, func() bool { return !w.state.completion.probe.pending })
	if !w.state.completion.list.active {
		t.Fatal("failed probe closed the static list")
	}
	for _, name := range []string{"cat", "cargo"} {
		found := false
		for _, candidate := range w.state.completion.list.candidates {
			found = found || candidate.Text == name
		}
		if !found {
			t.Errorf("failed probe dropped %q", name)
		}
	}
}

func TestCompletionProbeLateResultPreservesPreviewBaseline(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	w.session.term.integrated = true
	w.session.term.previewClearBound = true
	w.state.completion.previewEnabled = true
	w.shell.onWrite = func(data []byte) {
		if len(data) > 0 && data[0] == 0x15 {
			w.shell.say("\r\x1b[2K$ " + string(data[1:]))
			return
		}
		w.shell.say(string(data))
	}
	w.type_("ca")
	conn := w.session.conn.(*fakeConn)
	release := make(chan struct{})
	conn.runCommand = func(ctx context.Context, command string, _ int) ([]byte, error) {
		if !strings.Contains(command, `command -v "$name"`) {
			return nil, nil
		}
		select {
		case <-release:
			return []byte("cat\n"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	w.state.completion.probeEnabled = true
	w.state.openCompletions()
	for index, candidate := range w.state.completion.list.candidates {
		if candidate.Text == "cat" {
			w.state.completion.list.selected = index
			break
		}
	}
	w.state.previewCompletion(w.session, w.state.completion.list.selected)
	w.state.Wake()
	if w.state.completion.preview.phase != previewed {
		t.Fatal("fixture did not reach confirmed physical preview")
	}
	close(release)
	waitCompletionProbe(t, w.state, func() bool { return !w.state.completion.probe.pending })
	if !w.state.completion.list.active || w.state.completion.preview.baseline != "ca" {
		t.Fatal("availability result discarded the preview baseline")
	}
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: terminalHit(w.state, 0)})
	w.state.Wake()
	if w.line(t) != "ca" || w.shell.typed() == before {
		t.Fatal("late availability result prevented baseline restoration")
	}
}

func TestCompletionProbeDetailDoesNotLeakAnotherHostsAvailability(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	w.type_("ca")
	w.state.openCompletions()
	w.state.completion.probeEnabled = true
	probe := &w.state.completion.probe
	probe.sessionID, probe.owner, probe.conn = w.session.ID, w.session.term, &fakeConn{}
	probe.expiresAt = w.clock + completionProbeCache
	probe.available = map[string]bool{"cat": true}
	for index, candidate := range w.state.completion.list.candidates {
		if candidate.Text == "cat" {
			w.state.completion.list.selected = index
			break
		}
	}
	var canvas ui.Canvas
	w.state.Paint(&canvas, ui.Size{Width: 900, Height: 600})
	reference := false
	for _, text := range canvas.Texts() {
		if strings.HasPrefix(text.Value, "cat - ") && strings.Contains(text.Value, w.state.messages.Completion.SourceAvailable) {
			t.Fatal("another host's probe marked this host available")
		}
		reference = reference || strings.HasPrefix(text.Value, "cat - ") && strings.Contains(text.Value, w.state.messages.Completion.SourceCatalog)
	}
	if !reference {
		t.Fatal("expected a visible reference label for an unproven command")
	}
}
