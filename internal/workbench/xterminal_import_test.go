package workbench

import (
	"github.com/vivaui/viva/ui"
	"testing"
)

func TestXterminalImportChooserAndPasteDialog(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.modal = modalImportSessions
	view := func(c ui.C) { state.declareImportSessions(c); state.declareXterminalImport(c) }
	page, canvas := drawDialogPage(t, state, view)
	clickPageHit(t, state, page, canvas, view, pageHitByLabel(t, canvas, state.messages.Tools.XterminalPasteTitle))
	if state.modal != modalXterminalImport {
		t.Fatal("paste dialog did not open")
	}
	state.xterminal.editor.Insert("host=192.0.2.1 port=2222 user=root authType=password password=test-secret sshProxy=ignored title=Test")
	page, canvas = drawDialogPage(t, state, view)
	clickPageHit(t, state, page, canvas, view, pageHitByLabel(t, canvas, "Import"))
	if state.modal != modalSessionImportPreview || len(state.sessionImport.passwords) != 1 {
		t.Fatal("password preview not opened")
	}
	if state.xterminal.editor.String() != "" {
		t.Fatal("paste contents retained")
	}
	host := state.sessionImport.plan.hosts[0]
	if host.Address != "192.0.2.1" || host.Port != 2222 || host.Label != "Test" || host.User != "root" || len(state.sessionImport.plan.proxies) != 0 {
		t.Fatal("incorrect import plan")
	}
	state.clickSessionImportPreview(hitImportConnectionsOnly)
	if len(state.hosts) != 1 || state.modal != modalHostImportResult {
		t.Fatal("host not imported")
	}
	state.xterminal.editor.Insert("host=192.0.2.1 port=2222 user=root title=Test")
	state.submitXterminalImport()
	if len(state.hosts) != 1 {
		t.Fatal("duplicate imported")
	}
	state.xterminal.editor.Insert("host=192.0.2.1 port=2222 user=root title=Another name")
	state.submitXterminalImport()
	if len(state.hosts) != 2 {
		t.Fatal("distinct named connection was not retained")
	}
}

func TestXterminalImportInvalidAndCancel(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.clickImportSessions(hitImportXterminalPaste)
	state.xterminal.editor.Insert("invalid")
	state.submitXterminalImport()
	if state.modal != modalXterminalImport || state.dialogError == "" || len(state.hosts) != 0 {
		t.Fatal("invalid input not retained for correction")
	}
	view := func(c ui.C) { state.declareXterminalImport(c) }
	page, canvas := drawDialogPage(t, state, view)
	clickPageHit(t, state, page, canvas, view, pageHitByLabel(t, canvas, state.messages.Cancel))
	if state.modal != modalNone || state.xterminal.editor.String() != "" {
		t.Fatal("cancel did not clear paste contents")
	}
}

func TestXterminalImportSavesPasswordsAndHostsAcrossRestart(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.clickImportSessions(hitImportXterminalPaste)
	state.xterminal.editor.Insert("host=192.0.2.2 port=22 user=root authType=password password=test-secret sshProxy=ignored title=Saved\nhost=192.0.2.3 user=deploy title=Second")
	state.submitXterminalImport()
	state.Click(hitImportSavePasswords)
	if len(state.hosts) != 2 || state.modal != modalHostImportResult {
		t.Fatalf("import failed: %s", state.dialogError)
	}
	restarted := newTestState(t, options)
	if len(restarted.hosts) != 2 {
		t.Fatal("hosts not persisted")
	}
	value, found := vaultValue(t, restarted, hostSecretName(restarted.hosts[0].CatalogID, hostPasswordPart))
	if !found || value != "test-secret" {
		t.Fatal("password not persisted in vault")
	}
}
