//go:build darwin || linux

package workbench

import (
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

func realCompletionLocal(t *testing.T, profile string) (*State, *Session, *headless.Session) {
	t.Helper()
	program, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash unavailable: %v", err)
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte("PS1='preview> '\n"+profile), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", program)
	t.Setenv("INPUTRC", "/dev/null")
	state := newTestState(t, testOptions(t))
	state.localShell = program
	state.setCompletionMode(completionList)
	state.completion.previewEnabled = true
	state.Click(hitLocalTerminal)
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.term == nil || session.term.shell == nil {
		t.Fatal("real local shell did not start")
	}
	var invalidator app.Invalidator
	state.options.Invalidate = invalidator.Invalidate
	view, err := headless.Open(app.Config{
		Width: 900, Height: 600, Tree: Tree(), Invalidator: &invalidator,
		Paint: state.Paint, TextInput: state.TerminalInput, OnKey: state.Key,
		OnPointer: state.Pointer, OnResize: state.Resize, OnWake: state.Wake,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.term.shell.Close() })
	t.Cleanup(view.Close)
	return state, session, view
}

func waitRealCompletion(t *testing.T, state *State, session *Session, want string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		line, ok := session.term.recorder.Line(cmdhistory.TermScreen{Term: session.term.screen})
		if ok && line == want && check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	line, ok := session.term.recorder.Line(cmdhistory.TermScreen{Term: session.term.screen})
	t.Fatalf("expected line %q, got %q (confirmed=%v, bound=%v, phase=%v, screen=%q)", want, line, ok,
		session.term.previewClearBound, state.completion.preview.phase, session.term.screen.Text())
}

func TestHeadlessCompletionPreviewWithRealLocalBash(t *testing.T) {
	for _, test := range []struct {
		name, profile string
		preview       bool
	}{
		{"safe binding", "", true},
		{"rebound binding", "bind '\"\\C-u\": accept-line'\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, session, view := realCompletionLocal(t, test.profile)
			deadline := time.Now().Add(8 * time.Second)
			for time.Now().Before(deadline) {
				state.Wake()
				if session.term.promptReady && session.term.inputGuard.known {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !session.term.promptReady || session.term.previewClearBound != test.preview {
				t.Fatalf("real prompt ready=%v, safe binding=%v; screen=%q", session.term.promptReady,
					session.term.previewClearBound, session.term.screen.Text())
			}
			time.Sleep(cmdhistory.QuietBefore + 20*time.Millisecond)
			if err := view.Type("cat -"); err != nil {
				t.Fatal(err)
			}
			waitRealCompletion(t, state, session, "cat -", func() bool { return true })
			state.openCompletions()
			if !state.completion.list.active {
				t.Fatal("real shell has no static completion list")
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if err := view.Keys("down"); err != nil {
				t.Fatal(err)
			}
			if !test.preview {
				waitRealCompletion(t, state, session, "cat -", func() bool { return state.completion.preview.phase == previewIdle })
			} else {
				waitRealCompletion(t, state, session, "cat -e", func() bool { return state.completion.preview.phase == previewed })
				state.options.Invalidate()
				if err := view.Driver().Pump(); err != nil {
					t.Fatal(err)
				}
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(strings.Join(view.Texts(), " "), state.messages.Completion.Title) {
					t.Fatalf("preview hid its own popup: active=%v eligible=%v guard=%+v phase=%v texts=%q",
						state.completion.list.active, state.completionEligible(session), session.term.inputGuard,
						state.completion.preview.phase, view.Texts())
				}
				if directory := os.Getenv("MOTTY_COMPLETION_SCREENSHOTS"); directory != "" {
					if err := os.MkdirAll(directory, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := view.Screenshot(filepath.Join(directory, "real-local-preview.png")); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(strings.Join(view.Texts(), " "), state.messages.Completion.Title) {
						t.Fatalf("preview popup disappeared on the next Wake: active=%v phase=%v ready=%v started=%v guard=%+v status=%q",
							state.completion.list.active, state.completion.preview.phase, session.term.promptReady,
							session.term.promptStarted, session.term.inputGuard, state.status)
					}
					picture, err := view.Image()
					if err != nil {
						t.Fatal(err)
					}
					if picture.RGBAAt(320, 150) == picture.RGBAAt(320, 900) {
						t.Fatal("the screenshot shows no visible preview popup")
					}
				}
			}
			if err := view.Keys("escape"); err != nil {
				t.Fatal(err)
			}
			waitRealCompletion(t, state, session, "cat -", func() bool { return !state.completion.list.active })
			if strings.Contains(session.term.screen.Text(), "Password:") {
				t.Fatal("preview crossed into a password prompt")
			}
		})
	}
}

func TestHeadlessCompletionProbeWithRealLocalBash(t *testing.T) {
	state, session, view := realCompletionLocal(t, "")
	state.completion.previewEnabled = false
	state.completion.probeEnabled = true
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if session.term.promptReady {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !session.term.promptReady {
		t.Fatal("real Bash prompt was not ready")
	}
	time.Sleep(cmdhistory.QuietBefore + 20*time.Millisecond)
	if err := view.Type("ca"); err != nil {
		t.Fatal(err)
	}
	waitRealCompletion(t, state, session, "ca", func() bool { return true })
	state.openCompletions()
	if !state.completion.list.active {
		t.Fatal("no catalog candidate to probe")
	}
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if !state.completion.probe.pending && state.completion.probe.available["cat"] {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !state.completion.probe.available["cat"] {
		t.Fatalf("local shell did not confirm cat: known=%v, available=%v", state.completion.probe.known, state.completion.probe.available)
	}
	for index, candidate := range state.completion.list.candidates {
		if candidate.Text == "cat" {
			state.completion.list.selected = index
			state.completion.list.clamp()
			break
		}
	}
	state.options.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	label := state.messages.Completion.SourceAvailable
	found := false
	for _, text := range view.Texts() {
		found = found || strings.Contains(text, "cat - "+label)
	}
	if !found {
		t.Fatalf("installed command label not visible: %q", view.Texts())
	}
	if directory := os.Getenv("MOTTY_COMPLETION_SCREENSHOTS"); directory != "" {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(directory, "real-local-probe.png")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHeadlessCompletionDiscoversRealLocalExecutable(t *testing.T) {
	directory := t.TempDir()
	name := "zqmotty-local-command"
	if err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\nprintf unexpected-execution\\n\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	state, session, view := realCompletionLocal(t, "")
	state.completion.probeEnabled = true
	state.completion.previewEnabled = false
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if session.term.promptReady {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !session.term.promptReady {
		t.Fatal("real Bash prompt was not ready")
	}
	time.Sleep(cmdhistory.QuietBefore + 20*time.Millisecond)
	if err := view.Type("zqmotty"); err != nil {
		t.Fatal(err)
	}
	waitRealCompletion(t, state, session, "zqmotty", func() bool { return true })
	state.openCompletions()
	if !state.completion.list.active || len(state.completion.list.candidates) != 0 {
		t.Fatal("host-only name was not awaited from an empty static list")
	}
	waitRealCompletion(t, state, session, "zqmotty", func() bool { return len(state.completion.list.candidates) == 1 })
	if state.completion.list.candidates[0].Text != name {
		t.Fatalf("host-only candidate = %+v", state.completion.list.candidates[0])
	}
	state.options.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("MOTTY_COMPLETION_SCREENSHOTS"); directory != "" {
		if err := view.Screenshot(filepath.Join(directory, "real-local-discovery.png")); err != nil {
			t.Fatal(err)
		}
	}
	if err := view.Keys("enter"); err != nil {
		t.Fatal(err)
	}
	waitRealCompletion(t, state, session, name, func() bool { return !state.completion.list.active })
	if strings.Contains(session.term.screen.Text(), "unexpected-execution") {
		t.Fatal("accepting a discovered command executed it")
	}
}

func TestHeadlessCompletionListsRealLocalPaths(t *testing.T) {
	state, session, view := realCompletionLocal(t, "")
	state.completion.previewEnabled = false
	state.completion.probeEnabled = true
	directory := os.Getenv("HOME")
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "log.txt"), []byte("safe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "lo bad"), []byte("unsafe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if session.term.promptReady && session.term.reportedDirectory.Path == resolved {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !session.term.promptReady || session.term.reportedDirectory.Path != resolved {
		t.Fatalf("real CWD not confirmed: %+v", session.term.reportedDirectory)
	}
	time.Sleep(cmdhistory.QuietBefore + 20*time.Millisecond)
	if err := view.Type("tail -f ./lo"); err != nil {
		t.Fatal(err)
	}
	waitRealCompletion(t, state, session, "tail -f ./lo", func() bool { return true })
	state.openCompletions()
	if !state.completion.list.active || len(state.completion.list.candidates) != 0 {
		t.Fatal("path candidates did not wait for the host query")
	}
	waitRealCompletion(t, state, session, "tail -f ./lo", func() bool { return len(state.completion.list.candidates) == 2 })
	directoryIndex := -1
	for index, candidate := range state.completion.list.candidates {
		if candidate.Text == "./logs/" && candidate.IsDirectory {
			directoryIndex = index
		}
	}
	if directoryIndex < 0 {
		t.Fatalf("real directory candidate missing: %+v", state.completion.list.candidates)
	}
	state.completion.list.selected = directoryIndex
	state.completion.list.clamp()
	state.options.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("MOTTY_COMPLETION_SCREENSHOTS"); directory != "" {
		if err := view.Screenshot(filepath.Join(directory, "real-local-path.png")); err != nil {
			t.Fatal(err)
		}
	}
	if err := view.Keys("enter"); err != nil {
		t.Fatal(err)
	}
	waitRealCompletion(t, state, session, "tail -f ./logs/", func() bool { return !state.completion.list.active })
	if strings.Contains(session.term.screen.Text(), "safe\n") {
		t.Fatal("accepting a path executed tail")
	}
}
