package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/diag"
	"motty/internal/sshconn"
)

func TestTheDiagnosticsReportNamesNoHostAccountOrAddress(t *testing.T) {
	world, options := newWorld(t)
	log := diag.NewLog(50, 1<<16)
	fmt.Fprintln(log, "motty: dial prod.example.com: connection refused (deploy@192.168.1.1)")
	options.Log = log
	state := newTestState(t, options)
	connection := connect(t, state, world)
	connection.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseFailed
		status.Err = fmt.Errorf("dial tcp 203.0.113.9:22 for deploy on prod.example.com: %w", sshconn.ErrDisconnected)
		status.ServerVersion = "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13"
	})
	state.Wake()

	report := state.diagnosticReport()
	for _, private := range []string{"prod.example.com", "staging.example.com", "192.168.1.1", "203.0.113.9", "deploy", "admin", "Production", "Router"} {
		if strings.Contains(report, private) {
			t.Errorf("the report names %q:\n%s", private, report)
		}
	}
	for _, want := range []string{"== Hosts ==", "host-1 protocol=ssh port=22", "phase: failed", "error kind: disconnected", "OpenSSH_9.6p1 Ubuntu", "== Recent log ==", "connection refused"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q:\n%s", want, report)
		}
	}
}

func TestExportingDiagnosticsWritesAPrivateFileAndReportsWhereOrWhy(t *testing.T) {
	_, options := newWorld(t)
	path := filepath.Join(t.TempDir(), "report.txt")
	options.SaveFile = func(app.SaveFileOptions) (string, error) { return path, nil }
	state := newTestState(t, options)
	state.openExportDiagnostics()
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), "== Motty diagnostics ==") || !strings.Contains(state.status, path) {
		t.Fatalf("err=%v status=%q data=%.60q", err, state.status, data)
	}

	cancel := filepath.Join(t.TempDir(), "never.txt")
	options.SaveFile = func(app.SaveFileOptions) (string, error) { return cancel, app.ErrCanceled }
	state = newTestState(t, options)
	state.openExportDiagnostics()
	if _, err := os.Stat(cancel); err == nil {
		t.Fatal("a canceled picker still wrote a file")
	}

	options.SaveFile = func(app.SaveFileOptions) (string, error) {
		return filepath.Join(t.TempDir(), "no", "such", "dir", "x.txt"), nil
	}
	state = newTestState(t, options)
	state.openExportDiagnostics()
	if state.status == "" || strings.Contains(state.status, "x.txt") {
		t.Fatalf("a failed save left status %q, want the failure and no success path", state.status)
	}
}
