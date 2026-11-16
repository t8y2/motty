package workbench

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A local terminal's files panel starts where its shell is, not in the home
// folder, and follows the shell as it moves.
func TestLocalFilesPanelOpensAtAndFollowsTheShellsFolder(t *testing.T) {
	for _, name := range []string{"bash", "zsh"} {
		t.Run(name, func(t *testing.T) { localFilesPanelFollowsShell(t, name) })
	}
}

func localFilesPanelFollowsShell(t *testing.T, name string) {
	skipWithoutUnixShell(t)
	shellPath, err := exec.LookPath(name)
	if err != nil || runtime.GOOS == "windows" {
		t.Skipf("needs %s", name)
	}
	home, err := filepath.EvalSymlinks(localFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SHELL", shellPath)
	t.Setenv("ZDOTDIR", home)
	state := newTestState(t, testOptions(t))
	state.options.Invalidate = func() {}
	state.Click(hitLocalTerminal)
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("no local session opened")
	}
	t.Cleanup(func() {
		if session.term != nil && session.term.shell != nil {
			session.term.shell.Close()
		}
	})

	reported := func(want string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			state.Wake()
			if cwd, ok := session.term.screen.WorkingDirectory(); ok {
				if resolved, err := filepath.EvalSymlinks(cwd.Path); err == nil && resolved == want {
					return
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		cwd, _ := session.term.screen.WorkingDirectory()
		t.Fatalf("the shell never reported %q; last report %q; screen %q", want, cwd.Path, screenText(state, 0))
	}
	photos := filepath.Join(home, "photos")

	reported(home)
	session.term.send([]byte("cd photos\n"))
	reported(photos)

	state.Click(hitSFTPOpen)
	view := session.files
	if view == nil || !view.localOnly || view.lfolder.path != photos {
		t.Fatalf("the panel opened at %q, want the shell's folder %q", pathOf(view), photos)
	}

	session.term.send([]byte("cd ..\n"))
	reported(home)
	state.Wake()
	if view.lfolder.path != home {
		t.Fatalf("the open panel stayed at %q after the shell moved to %q", view.lfolder.path, home)
	}

	state.Click(contextID(hitSFTPClose))
	session.term.send([]byte("cd photos\n"))
	reported(photos)
	if err := os.RemoveAll(photos); err != nil {
		t.Fatal(err)
	}
	state.Click(hitSFTPOpen)
	if session.files == nil || session.files.lfolder.path != home {
		t.Fatalf("with the shell's folder gone the panel opened at %q, want home %q", pathOf(session.files), home)
	}
}

func pathOf(view *sftpView) string {
	if view == nil {
		return "<no panel>"
	}
	return view.lfolder.path
}
