package workbench

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/localshell"
)

func TestCompletionDiscoveryOpensFromAnEmptyStaticCatalog(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	w.type_("zqmotty")
	w.state.openCompletions()
	if w.state.completion.list.active {
		t.Fatal("host-only query opened before opt-in")
	}
	conn := w.session.conn.(*fakeConn)
	conn.runCommand = func(_ context.Context, command string, limit int) ([]byte, error) {
		if !strings.Contains(command, "path=$PATH:") || limit != 4096 {
			t.Fatalf("unexpected discovery command=%q limit=%d", command, limit)
		}
		return []byte("zqmotty-local\x00zqmotty-unsafe;run\x00"), nil
	}
	w.state.completion.probeEnabled = true
	w.state.openCompletions()
	if !w.state.completion.list.active || len(w.state.completion.list.candidates) != 0 {
		t.Fatal("an empty static prefix did not wait for the host query")
	}
	waitCompletionProbe(t, w.state, func() bool { return len(w.state.completion.list.candidates) == 1 })
	candidate := w.state.completion.list.candidates[0]
	if candidate.Text != "zqmotty-local" || candidate.Insert != "-local" {
		t.Fatalf("host-only candidate = %+v", candidate)
	}
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: terminalHit(w.state, 0)})
	if w.shell.typed() != before+"-local" || w.state.completion.list.active {
		t.Fatalf("host-only acceptance changed the shell: %q", w.shell.typed())
	}
}

func TestCompletionDiscoveryNeverQueriesUnsupportedContexts(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*completionWorld)
	}{
		{"unknown shell", func(w *completionWorld) {}},
		{"NoHistory", func(w *completionWorld) {
			w.session.term.shell = completionPOSIXShell{w.shell}
			w.session.host.NoHistory = true
		}},
		{"Telnet", func(w *completionWorld) {
			w.session.term.shell = completionPOSIXShell{w.shell}
			w.session.host.Protocol = protocolTelnet
		}},
		{"Off", func(w *completionWorld) {
			w.session.term.shell = completionPOSIXShell{w.shell}
			w.state.setCompletionMode(completionOff)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := newCompletionWorld(t)
			w.type_("zqmotty")
			w.state.completion.probeEnabled = true
			w.session.conn.(*fakeConn).runCommand = func(context.Context, string, int) ([]byte, error) {
				t.Fatal("unsupported context queried the host")
				return nil, nil
			}
			test.edit(w)
			w.state.openCompletions()
			w.state.Wake()
			if w.state.completion.list.active {
				t.Fatal("unsupported context left an empty list open")
			}
		})
	}
}

func TestCompletionDiscoveryRejectsChangedConnectionAndLine(t *testing.T) {
	for _, changed := range []string{"connection", "line"} {
		t.Run(changed, func(t *testing.T) {
			w := newCompletionWorld(t)
			w.session.term.shell = completionPOSIXShell{w.shell}
			w.type_("zqmotty")
			w.state.completion.probeEnabled = true
			started := make(chan struct{})
			w.session.conn.(*fakeConn).runCommand = func(ctx context.Context, _ string, _ int) ([]byte, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			w.state.openCompletions()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("discovery did not start")
			}
			if changed == "connection" {
				w.session.conn = &fakeConn{}
			} else {
				w.type_("x")
			}
			w.state.Wake()
			if len(w.state.completion.list.candidates) != 0 {
				t.Fatal("obsolete host-only result entered the current list")
			}
		})
	}
}

func TestCompletionDiscoveryEmptyWaitingListForwardsEnter(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	w.type_("zqmotty")
	w.state.completion.probeEnabled = true
	w.session.conn.(*fakeConn).runCommand = func(ctx context.Context, _ string, _ int) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	w.state.openCompletions()
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: terminalHit(w.state, 0)})
	if w.state.completion.list.active || w.shell.typed() != before+"\r" {
		t.Fatalf("empty waiting list swallowed shell Enter: %q", w.shell.typed())
	}
}

func TestCompletionDiscoveryCachesTheExactPrefixUntilExpiry(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	w.type_("zqmotty")
	w.state.completion.probeEnabled = true
	conn := w.session.conn.(*fakeConn)
	conn.runCommand = func(context.Context, string, int) ([]byte, error) {
		return []byte("zqmotty-local\x00"), nil
	}
	w.state.openCompletions()
	waitCompletionProbe(t, w.state, func() bool { return len(w.state.completion.list.candidates) == 1 })
	w.state.closeCompletionList()
	w.state.openCompletions()
	conn.mu.Lock()
	before := len(conn.commandCalls)
	conn.mu.Unlock()
	if before != 1 || len(w.state.completion.list.candidates) != 1 {
		t.Fatalf("cache failed: calls=%d candidates=%v", before, w.state.completion.list.candidates)
	}
	w.clock += completionProbeCache + time.Millisecond
	w.state.openCompletions()
	waitCompletionProbe(t, w.state, func() bool {
		conn.mu.Lock()
		defer conn.mu.Unlock()
		return len(conn.commandCalls) == 2
	})
}

func TestCompletionDiscoveryDropsQueuedPasswordOutputWithoutWake(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	w.type_("zqmotty")
	w.state.completion.probeEnabled = true
	release := make(chan struct{})
	w.session.conn.(*fakeConn).runCommand = func(ctx context.Context, _ string, _ int) ([]byte, error) {
		select {
		case <-release:
			return []byte("zqmotty-local\x00"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	w.state.openCompletions()
	w.shell.say("\r\nPassword: ")
	close(release)
	waitCompletionProbe(t, w.state, func() bool { return !w.state.completion.list.active })
	if len(w.state.completion.list.candidates) != 0 {
		t.Fatal("password prompt adopted stale host names")
	}
}

func TestCompletionDiscoveryLongNameDoesNotClipItsSource(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("ca")
	w.state.openCompletions()
	w.state.completion.probeEnabled = true
	w.state.completion.discovery = completionDiscovery{
		sessionID: w.session.ID, owner: w.session.term, conn: w.session.conn,
		prefix: "ca", expiresAt: w.clock + completionProbeCache,
	}
	w.state.completion.list.candidates[0].Text = "cat-super-long-custom-executable"
	w.state.completion.list.candidates[0].FromHost = true
	var canvas ui.Canvas
	w.state.Paint(&canvas, ui.Size{Width: 900, Height: 600})
	found := false
	for _, text := range canvas.Texts() {
		if strings.Contains(text.Value, w.state.messages.Completion.SourceAvailable) {
			found = true
			if canvas.TextWidth(text.Value, metrics.FontSizeS) > text.Bounds.Width {
				t.Fatalf("host source detail clipped: %q inside width %.1f", text.Value, text.Bounds.Width)
			}
		}
	}
	if !found {
		t.Fatal("host source detail was not painted")
	}
}

func TestCompletionDiscoveryLongNameFitsItsRowAndRemainsAccessible(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("ca")
	w.state.openCompletions()
	name := "cat-super-long-custom-executable-with-many-extra-characters-for-testing"
	w.state.completion.list.candidates[0].Text = name
	var canvas ui.Canvas
	w.state.Paint(&canvas, ui.Size{Width: 320, Height: 240})
	for _, text := range canvas.Texts() {
		if text.Value == name && canvas.TextWidth(name, metrics.FontSizeS) > text.Bounds.Width {
			t.Fatal("long command name clipped without a visible abbreviation")
		}
	}
	hit, ok := hitByID(&canvas, hitCompletionList.At(0))
	if !ok || hit.Label != name {
		t.Fatalf("full command name unavailable to accessibility: %+v", hit)
	}
}

func TestCompletionDiscoveryWSLLocalSessionDoesNotWaitOnUnsupportedAdapter(t *testing.T) {
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	w.type_("zqmotty")
	w.state.completion.probeEnabled = true
	w.session.conn = &localConn{shell: localshell.WSLShell("Ubuntu")}
	w.state.openCompletions()
	if w.state.completion.list.active {
		t.Fatal("unsupported WSL adapter left an empty discovery list")
	}
}
