//go:build !windows

package workbench

import (
	"bytes"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
)

// TestReceivingFromARealSzThroughALocalShell runs lrzsz's sz in this machine's
// own shell, on a real pseudo-terminal, and takes the file it sends. It is
// skipped where lrzsz is not installed.
func TestReceivingFromARealSzThroughALocalShell(t *testing.T) {
	szPath, err := exec.LookPath("sz")
	if err != nil {
		t.Skip("lrzsz is not installed")
	}
	state, session := openLocal(t)
	state.Wake()
	source := t.TempDir()
	payload := make([]byte, 300000)
	rand.New(rand.NewSource(9)).Read(payload)
	file := filepath.Join(source, "from-sz.bin")
	os.WriteFile(file, payload, 0o644)
	dest := t.TempDir()
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dest, nil }

	session.term.send([]byte(szPath + " --binary --quiet " + file + "\n"))
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	state.Click(hitTransferChoose)
	waitUntil(t, state, "the end", func() bool { return session.transfer.phase == transferNone })
	got, err := os.ReadFile(filepath.Join(dest, "from-sz.bin"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("received %d bytes, %v; screen %q", len(got), err, screenText(state, 0))
	}
	if !strings.Contains(screenText(state, 0), "Received 1 files") {
		t.Fatalf("screen = %q", screenText(state, 0))
	}
}

// TestSendingToARealRzThroughALocalShell is the other direction.
func TestSendingToARealRzThroughALocalShell(t *testing.T) {
	rzPath, err := exec.LookPath("rz")
	if err != nil {
		t.Skip("lrzsz is not installed")
	}
	state, session := openLocal(t)
	state.Wake()
	source := t.TempDir()
	payload := make([]byte, 200000)
	rand.New(rand.NewSource(10)).Read(payload)
	file := filepath.Join(source, "to-rz.bin")
	os.WriteFile(file, payload, 0o644)
	dest := t.TempDir()
	state.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }

	session.term.send([]byte("cd " + dest + " && " + rzPath + " --binary --quiet --overwrite\n"))
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	state.Click(hitTransferChoose)
	waitUntil(t, state, "the end", func() bool { return session.transfer.phase == transferNone })
	// rz writes the file as it finishes; give the shell a moment to come back.
	waitUntil(t, state, "the file", func() bool {
		got, _ := os.ReadFile(filepath.Join(dest, "to-rz.bin"))
		return bytes.Equal(got, payload)
	})
}
