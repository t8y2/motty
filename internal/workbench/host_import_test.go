package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func TestSSHConfigImportHitChoosesOneFileAndShowsResult(t *testing.T) {
	config := writeWorkbenchSSHConfig(t, `
Host imported
    HostName imported.example.com
    User deploy
    Port 2200
`)
	options := freshOptions(t)
	var request app.OpenFileOptions
	options.OpenFiles = func(options app.OpenFileOptions) ([]string, error) {
		request = options
		return []string{config}, nil
	}
	state := newTestState(t, options)
	state.Click(hitImportMenu)
	canvas, _ := paintedTexts(state)
	if hit, ok := hitByID(canvas, hitImportSSHConfig); !ok || hit.Disabled {
		t.Fatalf("import hit = %+v, %v", hit, ok)
	}

	state.Click(hitImportSSHConfig)
	if request.Title != state.messages.SSHConfigPickerTitle || request.AllowMultiple {
		t.Fatalf("OpenFiles request = %+v", request)
	}
	if state.modal != modalHostImportResult || len(state.hosts) != 1 {
		t.Fatalf("modal=%d hosts=%+v", state.modal, state.hosts)
	}
	host := state.hosts[0]
	if host.Label != "imported" || host.Address != "imported.example.com" || host.User != "deploy" || host.Port != 2200 {
		t.Fatalf("imported host = %+v", host)
	}
	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.SSHConfigResultTitle) || !contains(texts, state.hostImportResult.summary) || !hasHit(canvas, hitHostImportDone) {
		t.Fatalf("result texts=%v hits=%+v", texts, canvas.Hits())
	}
	state.Click(hitHostImportDone)
	if state.modal != modalNone {
		t.Fatalf("result modal remained open: %d", state.modal)
	}
}

func TestSSHConfigImportDeduplicatesMapsMultiHopAndPersistsAtomically(t *testing.T) {
	identity := writeWorkbenchIdentity(t, "id_imported", "")
	config := writeWorkbenchSSHConfig(t, fmt.Sprintf(`
Host existing
    HostName prod.example.com
    User deploy
    Port 22
Host outer
    HostName outer.example.com
    User gateway
Host inner
    HostName inner.example.com
    User relay
    ProxyJump outer
Host target
    HostName target.example.com
    User application
    Port 2200
    IdentityFile %q
    ProxyJump outer,inner
Host warning
    HostName warning.example.com
    ProxyCommand nc gateway %%h
`, identity))
	world, options := newWorld(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{config}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSSHConfig)

	if len(state.hosts) != len(sampleHosts)+4 || len(state.identities) != 1 || len(state.proxies) != 2 {
		t.Fatalf("hosts=%d identities=%d proxies=%+v", len(state.hosts), len(state.identities), state.proxies)
	}
	if got := state.hostImportResult.summary; got != "Imported 4 hosts, 1 keys and 2 jump routes; skipped 1 duplicate hosts." {
		t.Fatalf("summary = %q", got)
	}
	if state.hostImportResult.visible == 0 || !strings.Contains(state.hostImportResult.warnings[0], "proxycommand") {
		t.Fatalf("warnings = %+v", state.hostImportResult)
	}
	target := mustHostLabel(t, state, "target")
	inner := mustHostLabel(t, state, "inner")
	outer := mustHostLabel(t, state, "outer")
	chain, valid := state.proxyChainFor(target)
	if !valid || len(chain) != 2 || chain[0].ID != inner.ID || chain[1].ID != outer.ID {
		t.Fatalf("proxy chain = %+v, valid=%v", chain, valid)
	}

	saved, report, err := loadState(state.options.StatePath)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load state = outcome %d, err %v", report.outcome, err)
	}
	if len(saved.Hosts) != len(sampleHosts)+4 || !slices.Equal(saved.IdentityFiles, []string{identity}) || len(saved.Proxies) != 2 {
		t.Fatalf("saved hosts=%d identities=%v proxies=%+v", len(saved.Hosts), saved.IdentityFiles, saved.Proxies)
	}

	restarted := newTestState(t, options)
	if len(restarted.hosts) != len(sampleHosts)+4 || len(restarted.identities) != 1 || len(restarted.proxies) != 2 {
		t.Fatalf("restored hosts=%d identities=%d proxies=%+v", len(restarted.hosts), len(restarted.identities), restarted.proxies)
	}
	restoredTarget := mustHostLabel(t, restarted, "target")
	restarted.selectedHostID = restoredTarget.ID
	openHost(restarted)
	if len(world.configs) != 1 {
		t.Fatalf("dials = %d", len(world.configs))
	}
	wantJumps := []sshconn.Endpoint{
		{Host: outer.Address, Port: outer.Port, User: outer.User},
		{Host: inner.Address, Port: inner.Port, User: inner.User},
	}
	if !slices.Equal(world.configs[0].ProxyJumps, wantJumps) || !slices.Equal(world.configs[0].AdditionalIdentityFiles, []string{identity}) {
		t.Fatalf("dial config = %+v", world.configs[0])
	}
}

func TestSSHConfigImportMakesUnsafeProxyMappingVisible(t *testing.T) {
	config := writeWorkbenchSSHConfig(t, `
Host dynamic-jump
    HostName %h.internal
Host target
    HostName target.example.com
    ProxyJump dynamic-jump
`)
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{config}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSSHConfig)

	if len(state.hosts) != 1 || state.hosts[0].Label != "target" || len(state.proxies) != 0 {
		t.Fatalf("hosts=%+v proxies=%+v", state.hosts, state.proxies)
	}
	var proxyWarning bool
	for _, warning := range state.hostImportResult.warnings[:state.hostImportResult.visible] {
		proxyWarning = proxyWarning || strings.Contains(warning, "ProxyJump for target")
	}
	if !proxyWarning {
		t.Fatalf("unsafe proxy warning is not visible: %+v", state.hostImportResult)
	}
	_, texts := paintedTexts(state)
	if !contains(texts, state.hostImportResult.warningsTitle) {
		t.Fatalf("warning heading is not painted: %v", texts)
	}
}

func TestSSHConfigImportCancelAndFailuresDoNotMutateOrSave(t *testing.T) {
	for _, test := range []struct {
		name  string
		paths []string
		err   error
	}{
		{name: "cancel", err: app.ErrCanceled},
		{name: "picker failure", err: errors.New("picker failed")},
		{name: "parse failure", paths: []string{filepath.Join(t.TempDir(), "missing")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := freshOptions(t)
			options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return test.paths, test.err }
			state := newTestState(t, options)
			state.Click(hitImportSSHConfig)
			if len(state.hosts) != 0 || len(state.identities) != 0 || len(state.proxies) != 0 || state.modal != modalNone {
				t.Fatalf("state mutated: hosts=%v identities=%v proxies=%v modal=%d", state.hosts, state.identities, state.proxies, state.modal)
			}
			if saved, _, err := loadState(state.options.StatePath); err != nil || len(saved.Hosts) != 0 || len(saved.Forwards) != 0 || len(saved.Proxies) != 0 {
				t.Fatalf("a failed import was saved: %+v (%v)", saved, err)
			}
			if state.status == "" {
				t.Fatal("failed import did not report status")
			}
		})
	}
}

func TestSSHConfigImportResultPaintDoesNotAllocate(t *testing.T) {
	config := writeWorkbenchSSHConfig(t, `
Host imported
    HostName imported.example.com
    ProxyCommand nc gateway %h
`)
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{config}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSSHConfig)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(50, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("import result steady-state paint allocations = %v", allocations)
	}
}

func writeWorkbenchSSHConfig(t testing.TB, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustHostLabel(t testing.TB, state *State, label string) Host {
	t.Helper()
	for _, host := range state.hosts {
		if host.Label == label {
			return host
		}
	}
	t.Fatalf("host %q not found in %+v", label, state.hosts)
	return Host{}
}

// The toolbar has one Import button; its menu holds the three sources, and a
// press anywhere else, or Escape, closes it without running one.
func TestImportMenuOpensListsEverySourceAndCloses(t *testing.T) {
	state := newTestState(t, testOptions(t))
	canvas, _ := paintedTexts(state)
	for _, id := range []ui.ID{hitImportSSHConfig, hitImportHostCatalog, hitImportSessions} {
		if _, ok := hitByID(canvas, id); ok {
			t.Fatalf("source %d is painted while the menu is closed", id)
		}
	}
	state.Click(hitImportMenu)
	if !state.importMenu {
		t.Fatal("the Import button did not open its menu")
	}
	canvas, _ = paintedTexts(state)
	for _, id := range []ui.ID{hitImportSSHConfig, hitImportHostCatalog, hitImportSessions} {
		if hit, ok := hitByID(canvas, id); !ok || hit.Disabled {
			t.Fatalf("menu source %d = %+v, %v", id, hit, ok)
		}
	}

	state.Key(ui.KeyEvent{Key: ui.KeyEscape})
	if state.importMenu {
		t.Fatal("Escape left the menu open")
	}

	state.Click(hitImportMenu)
	canvas, _ = paintedTexts(state)
	if hit, ok := hitByID(canvas, hitImportMenuDismiss); !ok || hit.Bounds.Width < 1000 {
		t.Fatalf("the open menu has no window-wide backdrop to press: %+v, %v", hit, ok)
	}
	state.Click(hitImportMenuDismiss)
	if state.importMenu || state.modal != modalNone {
		t.Fatalf("an outside press: menu=%v modal=%d", state.importMenu, state.modal)
	}

	state.Click(hitImportMenu)
	state.Click(hitImportSessions)
	if state.importMenu || state.modal != modalImportSessions {
		t.Fatalf("choosing a source: menu=%v modal=%d", state.importMenu, state.modal)
	}
}
