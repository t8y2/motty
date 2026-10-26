package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/sshconn"
)

func TestConnectionLogContainsSafeStoryAndRetainedTerminalText(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connection := connect(t, state, world)
	connection.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseHostKeyPrompt
		status.HostKey = sshconn.HostKeyInfo{Address: "prod.example.com:22", Fingerprint: "PRIVATE-KEY-MATERIAL"}
	})
	state.Wake()
	connection.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhasePasswordPrompt
		status.Questions = []sshconn.Question{{Text: "password SECRET-PASSWORD", Secret: true}}
		status.IdentityPath = "/private/id_secret"
	})
	state.Wake()
	connection.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.Method = "password"
		status.ServerVersion = "SSH-2.0-Test"
	})
	state.Wake()
	connection.shell(t).say("hello from remote\r\n")
	state.Wake()

	log := string(appendConnectionLog(nil, &state.sessions[0]))
	for _, want := range []string{
		"Motty connection log", "Target: deploy@prod.example.com:22", "Connection started",
		"Waiting for host key verification", "Authenticating", "Connected: password · SSH-2.0-Test",
		"Terminal retained text:", "hello from remote",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("log does not contain %q:\n%s", want, log)
		}
	}
	for _, secret := range []string{"SECRET-PASSWORD", "/private/id_secret", "PRIVATE-KEY-MATERIAL"} {
		if strings.Contains(log, secret) {
			t.Fatalf("log leaked %q:\n%s", secret, log)
		}
	}
}

func TestExportConnectionLogUsesPickerAndPrivateAtomicFile(t *testing.T) {
	world, options := newWorld(t)
	path := filepath.Join(t.TempDir(), "chosen.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	var requested app.SaveFileOptions
	options.SaveFile = func(options app.SaveFileOptions) (string, error) {
		requested = options
		return path, nil
	}
	state := newTestState(t, options)
	connection := connect(t, state, world)
	connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	connection.shell(t).say("retained output\r\n")
	state.Wake()

	canvas, _ := paintedTexts(state)
	if !hasHit(canvas, hitConnExport) {
		t.Fatal("live terminal has no export-log hit")
	}
	state.Click(hitConnExport)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "retained output") || !strings.Contains(state.status, path) {
		t.Fatalf("data=%q status=%q", data, state.status)
	}
	if requested.DefaultExtension != "txt" || requested.SuggestedName != "Production-connection-log.txt" || len(requested.Filters) != 1 {
		t.Fatalf("save request = %+v", requested)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestConnectionLogExportFailureIsVisibleAndCanceledPickerWritesNothing(t *testing.T) {
	world, options := newWorld(t)
	options.SaveFile = func(app.SaveFileOptions) (string, error) { return "", app.ErrCanceled }
	state := newTestState(t, options)
	connect(t, state, world)
	before := state.status
	state.Click(hitConnExport)
	if state.status != before {
		t.Fatalf("canceled export changed status to %q", state.status)
	}

	world, options = newWorld(t)
	options.SaveFile = func(app.SaveFileOptions) (string, error) { return "", errors.New("picker failed") }
	state = newTestState(t, options)
	connect(t, state, world)
	state.Click(hitConnExport)
	if !strings.Contains(state.status, "picker failed") || !strings.Contains(state.sessions[0].term.screen.Text(), "picker failed") {
		t.Fatalf("export error status=%q terminal=%q", state.status, state.sessions[0].term.screen.Text())
	}
}

func TestWritePrivateAtomicRejectsMissingDirectoryWithoutLeavingTemporaryFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "missing", "log.txt")
	if err := writePrivateAtomic(path, []byte("content")); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed write left files: %v", entries)
	}
}
