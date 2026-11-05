package workbench

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/localshell"
	"motty/internal/shellkind"
	"motty/internal/term"
)

func pathCompletionWorld(t *testing.T) *completionWorld {
	t.Helper()
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	w.session.term.reportedDirectory = term.Cwd{Path: "/home/user"}
	w.state.completion.probeEnabled = true
	return w
}

func TestCompletionPathUsesReadOnlyHostResultAndSuffix(t *testing.T) {
	w := pathCompletionWorld(t)
	conn := w.session.conn.(*fakeConn)
	conn.runCommand = func(_ context.Context, command string, limit int) ([]byte, error) {
		if limit != 4096 || !strings.Contains(command, "dir='/var'") || !strings.Contains(command, "prefix='lo'") {
			t.Fatalf("wrong read-only path query: %q limit=%d", command, limit)
		}
		return []byte("dlogs\x00flog.txt\x00"), nil
	}
	w.type_("tail -f /var/lo")
	w.state.openCompletions()
	if !w.state.completion.list.active || len(w.state.completion.list.candidates) != 0 {
		t.Fatal("path result was not awaited from an empty static list")
	}
	waitCompletionProbe(t, w.state, func() bool { return len(w.state.completion.list.candidates) == 2 })
	candidate := w.state.completion.list.candidates[0]
	if candidate.Text != "/var/logs/" || candidate.Insert != "gs/" || !candidate.IsDirectory {
		t.Fatalf("directory candidate=%+v", candidate)
	}
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: terminalHit(w.state, 0)})
	if w.shell.typed() != before+"gs/" {
		t.Fatalf("path insertion changed the shell line: %q", w.shell.typed())
	}
}

func TestCompletionPathDoesNotQueryUnsupportedContexts(t *testing.T) {
	for _, test := range []struct {
		name string
		line string
		edit func(*completionWorld)
	}{
		{"opt-out", "tail -f /var/lo", func(w *completionWorld) { w.state.completion.probeEnabled = false }},
		{"no CWD", "tail -f /var/lo", func(w *completionWorld) { w.session.term.reportedDirectory = term.Cwd{} }},
		{"unknown shell", "tail -f /var/lo", func(w *completionWorld) { w.session.term.shell = w.shell }},
		{"PowerShell", "tail -f /var/lo", func(w *completionWorld) { w.session.term.shell = completionPowerShell{w.shell} }},
		{"NoHistory", "tail -f /var/lo", func(w *completionWorld) { w.session.host.NoHistory = true }},
		{"quoted path", `tail -f "/var/log file"`, func(*completionWorld) {}},
		{"secret", "sudo -S password", func(*completionWorld) {}},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := pathCompletionWorld(t)
			w.session.conn.(*fakeConn).runCommand = func(context.Context, string, int) ([]byte, error) {
				t.Fatal("unsupported path queried the host")
				return nil, nil
			}
			test.edit(w)
			w.type_(test.line)
			w.state.openCompletions()
			if w.state.completion.list.active && len(w.state.completion.list.candidates) == 0 {
				t.Fatal("unsupported path left an empty list open")
			}
		})
	}
}

type completionPowerShell struct{ *fakeShell }

func (completionPowerShell) Kind() shellkind.Kind { return shellkind.PowerShell }

func TestCompletionPathDropsStaleConnectionOrCWD(t *testing.T) {
	for _, change := range []string{"connection", "CWD"} {
		t.Run(change, func(t *testing.T) {
			w := pathCompletionWorld(t)
			started := make(chan struct{})
			w.session.conn.(*fakeConn).runCommand = func(ctx context.Context, _ string, _ int) ([]byte, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			w.type_("tail -f /var/lo")
			w.state.openCompletions()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("path query did not start")
			}
			if change == "connection" {
				w.session.conn = &fakeConn{}
			} else {
				w.session.term.reportedDirectory = term.Cwd{Path: "/different"}
			}
			w.state.Wake()
			if len(w.state.completion.list.candidates) != 0 {
				t.Fatal("stale path result was offered")
			}
		})
	}
}

func TestCompletionPathFailurePreservesStaticAndShellTab(t *testing.T) {
	w := pathCompletionWorld(t)
	w.session.conn.(*fakeConn).runCommand = func(context.Context, string, int) ([]byte, error) {
		return nil, errors.New("extra command channel refused")
	}
	w.type_("tail -f /var/lo")
	w.state.openCompletions()
	waitCompletionProbe(t, w.state, func() bool { return !w.state.completion.list.active })
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyTab, Target: terminalHit(w.state, 0)})
	if w.shell.typed() != before+"\t" {
		t.Fatalf("fallback Tab was swallowed: %q", w.shell.typed())
	}
}

func TestCompletionPathWSLLocalSessionDoesNotWaitOnUnsupportedAdapter(t *testing.T) {
	w := pathCompletionWorld(t)
	w.type_("tail -f /var/lo")
	w.session.conn = &localConn{shell: localshell.WSLShell("Ubuntu")}
	w.state.openCompletions()
	if w.state.completion.list.active {
		t.Fatal("unsupported WSL adapter left an empty path list")
	}
}
