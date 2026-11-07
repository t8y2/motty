//go:build darwin || linux

package workbench

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/cmdhistory"
)

func TestControlFAcceptsInALocalShellWithoutExecuting(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("requires bash")
	}
	options := testOptions(t)
	started := time.Now()
	options.Now = func() time.Duration { return time.Since(started) }
	options.Invalidate = func() {}
	options.HistoryPath = filepath.Join(t.TempDir(), "history.db")
	state := newTestState(t, options)
	state.localShell = bash
	state.history.on = true
	state.setCompletionMode(completionBoth)
	state.Click(hitLocalTerminal)
	local, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("no local session")
	}
	t.Cleanup(func() {
		if local.term != nil && local.term.shell != nil {
			local.term.shell.Close()
		}
	})
	state.Wake()
	if local.term == nil || local.term.shell == nil {
		t.Fatal("no local shell")
	}
	local.term.send([]byte("PS1='ghost$ '; printf 'ready-%s\\n' ghost\r"))
	localShellOutput(t, state, local, "ready-ghost")

	marker := filepath.Join(t.TempDir(), "accepted")
	command := "printf accepted > '" + strings.ReplaceAll(marker, "'", "'\\''") + "'"
	key := historyKey(local)
	if err := state.historyStore().Record(key, command, time.Now()); err != nil {
		t.Fatal(err)
	}
	entries, err := state.historyStore().List(key)
	if err != nil {
		t.Fatal(err)
	}
	state.forgetCompletionHistory()
	state.history.ghost = map[string][]cmdhistory.Entry{key: entries}
	view, err := headless.Open(app.Config{
		Title: "local inline suggestion", Width: 1400, Height: 900,
		Paint: state.Paint, TextInput: state.TerminalInput,
		OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.ClickLabel(fmt.Sprintf(state.messages.LabelTerminalFmt, local.Address)); err != nil {
		t.Fatal(err)
	}
	// History only learns lines begun after a quiet prompt; wait for the real
	// PTY to finish startup and echo the setup command before typing.
	version, quietSince := local.term.screen.Version(), time.Now()
	if err := view.WaitFor(func(app.Snapshot) bool {
		state.Wake()
		if next := local.term.screen.Version(); next != version {
			version, quietSince = next, time.Now()
		}
		return time.Since(quietSince) >= 2*cmdhistory.QuietBefore
	}, waitBudget()); err != nil {
		t.Fatal(err)
	}
	if err := view.Type("printf accepted > "); err != nil {
		t.Fatal(err)
	}
	if err := view.WaitFor(func(app.Snapshot) bool {
		state.Wake()
		rest, offered := state.commandGhost(local)
		return offered && rest == strings.TrimPrefix(command, "printf accepted > ")
	}, waitBudget()); err != nil {
		t.Fatalf("local shell did not offer the recorded command: %v; screen=%q", err, screenText(state, 0))
	}
	if err := view.Keys("ctrl+f"); err != nil {
		t.Fatal(err)
	}
	if err := view.WaitFor(func(app.Snapshot) bool {
		state.Wake()
		line, ok := local.term.recorder.Line(cmdhistory.TermScreen{Term: local.term.screen})
		return ok && line == command
	}, waitBudget()); err != nil {
		t.Fatalf("Ctrl+F did not fill the local command line: %v; screen=%q", err, screenText(state, 0))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("accepting executed the command: stat=%v", err)
	}
	if err := view.Keys("enter"); err != nil {
		t.Fatal(err)
	}
	if err := view.WaitFor(func(app.Snapshot) bool {
		state.Wake()
		data, err := os.ReadFile(marker)
		return err == nil && string(data) == "accepted"
	}, waitBudget()); err != nil {
		t.Fatalf("Enter did not execute the accepted command: %v", err)
	}
}
