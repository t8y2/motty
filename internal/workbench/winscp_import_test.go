package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/sshconn"
)

func TestWinSCPImportMenuConfirmsBeforeSavingAndSkipsDuplicates(t *testing.T) {
	ini := "[Sessions\\team/staging]\r\nHostName=staging.example\r\nPortNumber=2222\r\nUserName=deploy\r\n" +
		"[Sessions\\ftp]\r\nHostName=ftp.example\r\nFSProtocol=5\r\n"
	path := filepath.Join(t.TempDir(), "winscp.ini")
	if err := os.WriteFile(path, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)
	openPreview := func() {
		t.Helper()
		state.Click(hitImportMenu)
		clickPageMenuRow(t, state, state.messages.Tools.ImportSessions)
		if _, texts := paintedTexts(state); !contains(texts, "WinSCP") {
			t.Fatal("WinSCP is missing from the import source chooser")
		}
		state.Click(hitImportWinSCP)
		if state.modal != modalSessionImportPreview {
			t.Fatalf("modal = %v, want session import preview", state.modal)
		}
		// WinSCP carries no passwords, so the preview must not talk about them.
		if _, texts := paintedTexts(state); !contains(texts, fmt.Sprintf(state.messages.Tools.ImportConnectionsBodyFmt, len(state.sessionImport.plan.hosts))) {
			t.Fatalf("preview does not state the host count without password wording: %v", texts)
		}
	}
	openPreview()
	if len(state.hosts) != 0 || len(newTestState(t, options).hosts) != 0 {
		t.Fatal("opening the preview imported a host")
	}
	state.clickSessionImportPreview(hitImportPreviewCancel)
	if state.status != state.messages.Tools.ImportCanceled || state.modal != modalNone || len(state.hosts) != 0 {
		t.Fatal("cancelling the preview did not discard the import")
	}
	for range 2 {
		openPreview()
		state.clickSessionImportPreview(hitImportConnectionsOnly)
		_, texts := paintedTexts(state)
		if !contains(texts, fmt.Sprintf(state.messages.Tools.ImportWarningSkippedFmt, 1)) {
			t.Fatalf("expected the standard skipped-session warning, got %v", texts)
		}
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

func TestWinSCPImportRetainsProxyAndTunnelAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "winscp.ini")
	ini := "[Sessions\\through-gateway]\nHostName=target.example\nProxyMethod=2\nProxyHost=proxy.example\n" +
		"ProxyUsername=proxy-user\nTunnel=1\nTunnelHostName=jump.example\nTunnelUserName=jump-user\n"
	if err := os.WriteFile(path, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)
	state.Click(hitImportMenu)
	clickPageMenuRow(t, state, state.messages.Tools.ImportSessions)
	state.Click(hitImportWinSCP)
	if state.modal != modalSessionImportPreview || len(state.hosts) != 0 || len(state.proxies) != 0 {
		t.Fatal("WinSCP route import did not wait for confirmation")
	}
	state.clickSessionImportPreview(hitImportConnectionsOnly)
	restarted := newTestState(t, options)
	target := mustHostLabel(t, restarted, "through-gateway")
	config, _, valid := restarted.hostCatalog.sshConfigFor(target, target, restarted.connectionEnv(nil))
	if !valid || config.Proxy == nil || config.Proxy.Kind != sshconn.ProxySOCKS5 ||
		config.Proxy.Address != "proxy.example:80" || config.Proxy.Username != "proxy-user" {
		t.Fatalf("unexpected restored outbound proxy: valid=%v, proxy=%+v", valid, config.Proxy)
	}
	want := sshconn.Endpoint{Host: "jump.example", User: "jump-user", Port: 22}
	if len(config.ProxyJumps) != 1 || config.ProxyJumps[0] != want {
		t.Fatalf("restored jumps = %+v, want [%+v]", config.ProxyJumps, want)
	}
}
