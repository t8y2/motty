package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
)

func TestWinSCPImportMenuPreservesFoldersAndSkipsDuplicates(t *testing.T) {
	ini := "[Sessions\\team/staging]\r\nHostName=staging.example\r\nPortNumber=2222\r\nUserName=deploy\r\n"
	path := filepath.Join(t.TempDir(), "winscp.ini")
	if err := os.WriteFile(path, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)
	for range 2 {
		state.Click(hitImportMenu)
		clickPageMenuRow(t, state, state.messages.Tools.ImportSessions)
		if _, texts := paintedTexts(state); !contains(texts, "WinSCP") {
			t.Fatal("WinSCP is missing from the import source chooser")
		}
		state.Click(hitImportWinSCP)
		state.Click(hitHostImportDone)
	}
	restarted := newTestState(t, options)
	host := mustHostLabel(t, restarted, "staging")
	if len(restarted.hosts) != 1 || host.Group != "team" || host.Address != "staging.example" || host.Port != 2222 || host.User != "deploy" {
		t.Fatalf("imported hosts = %+v", restarted.hosts)
	}
	if got := readSyncFile(t, path); got != ini {
		t.Fatal("one-time import edited the INI")
	}
}
