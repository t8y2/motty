package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

func TestBulkPasswordReplacesSelectedHostsWithOneVaultBatch(t *testing.T) {
	state := newTestState(t, testOptions(t))
	first, second, third := state.hosts[0], state.hosts[1], state.hosts[2]
	for _, host := range []Host{first, second, third} {
		if err := state.secrets.vault.Set(hostSecretName(host.CatalogID, hostPasswordPart), "old"); err != nil {
			t.Fatal(err)
		}
	}
	state.Click(hitHostCheckList.At(int(first.ID)))
	state.Click(hitHostCheckList.At(int(second.ID)))
	canvas, _ := paintedTexts(state)
	if button, ok := hitByID(canvas, hitBulkHostPassword); !ok || button.Disabled {
		t.Fatal("bulk password command is not available with a selection")
	}
	state.Click(hitBulkHostPassword)
	if state.modal != modalBulkHostPassword || state.bulkPassword.eligible != 2 || state.bulkPassword.skipped != 0 {
		t.Fatalf("modal=%d eligible=%d skipped=%d", state.modal, state.bulkPassword.eligible, state.bulkPassword.skipped)
	}
	canvas, _ = paintedTexts(state)
	if field, ok := hitByID(canvas, hitBulkPasswordField); !ok || !field.Secure || field.Value != "" {
		t.Fatal("bulk password field is not masked")
	}
	typeInto(state, hitBulkPasswordField, "new-secret-711")
	canvas, texts := paintedTexts(state)
	if field, ok := hitByID(canvas, hitBulkPasswordField); !ok || !field.Secure || field.Value != "" {
		t.Fatal("typed bulk password was exposed in the input hit")
	}
	for _, text := range texts {
		if text == "new-secret-711" {
			t.Fatal("typed bulk password was rendered as plain text")
		}
	}
	state.Click(hitBulkPasswordConfirm)
	if state.modal != modalNone {
		t.Fatalf("bulk update failed: %s", state.dialogError)
	}
	for _, item := range []struct {
		host Host
		want string
	}{{first, "new-secret-711"}, {second, "new-secret-711"}, {third, "old"}} {
		if got, found := vaultValue(t, state, hostSecretName(item.host.CatalogID, hostPasswordPart)); !found || got != item.want {
			t.Fatalf("host %s password not preserved/replaced", item.host.Label)
		}
	}
	if state.status == "" || state.status == "new-secret-711" || state.bulkPassword.editor.String() != "" {
		t.Fatal("result text exposed or retained the password")
	}
}

func TestBulkPasswordSkipsKeyAndNonPasswordHosts(t *testing.T) {
	state := newTestState(t, testOptions(t))
	key := state.hosts[0]
	key.Auth = authPrivateKey
	state.hosts[0] = key
	state.hosts[1].Protocol = protocolTelnet
	desktop := Host{ID: state.nextHostID, Label: "Desktop", Protocol: protocolVNC, Address: "desk.example.com", Port: 5900}
	desktop.finish()
	state.hosts = append(state.hosts, desktop)
	for _, host := range []Host{key, state.hosts[1], desktop} {
		state.Click(hitHostCheckList.At(int(host.ID)))
	}
	state.Click(hitBulkHostPassword)
	if state.modal != modalBulkHostPassword || state.bulkPassword.eligible != 1 || state.bulkPassword.skipped != 2 {
		t.Fatalf("eligible=%d skipped=%d", state.bulkPassword.eligible, state.bulkPassword.skipped)
	}
	typeInto(state, hitBulkPasswordField, "desktop-only")
	state.Click(hitBulkPasswordConfirm)
	if value, found := vaultValue(t, state, hostSecretName(desktop.CatalogID, hostPasswordPart)); !found || value != "desktop-only" {
		t.Fatal("eligible desktop secret was not saved")
	}
	for _, host := range []Host{key, state.hosts[1]} {
		if _, found := vaultValue(t, state, hostSecretName(host.CatalogID, hostPasswordPart)); found {
			t.Fatalf("ineligible host %s received a password", host.Label)
		}
	}
}

func TestBulkPasswordCancelEmptyAndVaultFailureAreNoOps(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	name := hostSecretName(host.CatalogID, hostPasswordPart)
	if err := state.secrets.vault.Set(name, "original"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitHostCheckList.At(int(host.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkPasswordCancel)
	if value, _ := vaultValue(t, state, name); value != "original" {
		t.Fatal("cancel changed the password")
	}
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkPasswordConfirm)
	if state.modal != modalBulkHostPassword || state.dialogError == "" {
		t.Fatal("empty password was accepted")
	}
	typeInto(state, hitBulkPasswordField, "replacement")
	vaultFile := state.secrets.vault
	state.secrets.vault = nil
	state.Click(hitBulkPasswordConfirm)
	state.secrets.vault = vaultFile
	if state.modal != modalBulkHostPassword || state.dialogError == "" {
		t.Fatal("unavailable vault completed the operation")
	}
	if value, _ := vaultValue(t, state, name); value != "original" {
		t.Fatal("failed update partially replaced the password")
	}
	state.Click(hitBulkPasswordCancel)
	if state.bulkPassword.editor.String() != "" {
		t.Fatal("secret editor remained populated after cancel")
	}
}

func TestBulkPasswordEnterConfirmsAndDoesNotRetargetSelection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	first, second := state.hosts[0], state.hosts[1]
	state.Click(hitHostCheckList.At(int(first.ID)))
	state.Click(hitBulkHostPassword)
	state.hostSelections = []uint32{second.ID}
	typeInto(state, hitBulkPasswordField, "one-only")
	if result := state.Key(ui.KeyEvent{Target: hitBulkPasswordField, Key: ui.KeyEnter}); !result.Handled {
		t.Fatal("Enter did not confirm the password")
	}
	if _, found := vaultValue(t, state, hostSecretName(second.CatalogID, hostPasswordPart)); found {
		t.Fatal("changed selection affected the batch target")
	}
	if value, found := vaultValue(t, state, hostSecretName(first.CatalogID, hostPasswordPart)); !found || value != "one-only" {
		t.Fatal("captured host did not receive the password")
	}
}

func TestBulkPasswordIsUnavailableForKeyOnlySelection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Auth = authAgent
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	canvas, _ := paintedTexts(state)
	if action, ok := hitByID(canvas, hitBulkHostPassword); !ok || !action.Disabled {
		t.Fatal("agent-only host enabled password replacement")
	}
	state.Click(hitBulkHostPassword)
	if state.modal == modalBulkHostPassword {
		t.Fatal("ineligible selection opened a password editor")
	}
}

func TestBulkPasswordContextMenuUsesCheckedHosts(t *testing.T) {
	state := newTestState(t, testOptions(t))
	first, second := state.hosts[0], state.hosts[1]
	state.hostSelections = []uint32{first.ID, second.ID}
	state.hostMenu = hostMenu{open: true, host: second.ID, checked: true}
	canvas, _ := paintedTexts(state)
	if action, ok := hitByID(canvas, hitHostMenuPassword); !ok || action.Disabled {
		t.Fatal("checked-host menu lacks a batch password command")
	}
	state.Click(hitHostMenuPassword)
	if state.modal != modalBulkHostPassword || state.bulkPassword.eligible != 2 {
		t.Fatal("context command lost the checked host selection")
	}
}

func TestBulkPasswordContextMenuDisablesKeyOnlySelection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Auth = authAgent
	state.hosts[1].Auth = authPrivateKey
	state.hostSelections = []uint32{state.hosts[0].ID, state.hosts[1].ID}
	state.hostMenu = hostMenu{open: true, host: state.hosts[1].ID, checked: true}
	canvas, _ := paintedTexts(state)
	if action, ok := hitByID(canvas, hitHostMenuPassword); !ok || !action.Disabled {
		t.Fatal("key-only context selection enabled password replacement")
	}
}
