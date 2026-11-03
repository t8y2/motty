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
	state.Click(hitBulkApplyPassword)
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
	if state.modal != modalBulkHostPassword || state.bulkPassword.eligible != 2 || state.bulkPassword.skipped != 1 {
		t.Fatalf("eligible=%d skipped=%d", state.bulkPassword.eligible, state.bulkPassword.skipped)
	}
	state.Click(hitBulkApplyPassword)
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
	state.Click(hitBulkApplyPassword)
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
	state.Click(hitBulkApplyPassword)
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

func TestBulkAuthenticationEnterConfirmsUsernameEdit(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	state.Click(hitHostCheckList.At(int(host.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyUser)
	typeInto(state, hitBulkUserField, "enter-user")
	if result := state.Key(ui.KeyEvent{Target: hitBulkUserField, Key: ui.KeyEnter}); !result.Handled {
		t.Fatal("Enter from username did not submit the batch")
	}
	if state.modal != modalNone || state.hosts[0].User != "enter-user" {
		t.Fatalf("username edit was not submitted: modal=%d user=%q error=%q", state.modal, state.hosts[0].User, state.dialogError)
	}
}

func TestBulkAuthenticationChangesOnlyEnabledSSHFields(t *testing.T) {
	state := newTestState(t, testOptions(t))
	first, second, untouched := state.hosts[0], state.hosts[1], state.hosts[2]
	state.Click(hitHostCheckList.At(int(first.ID)))
	state.Click(hitHostCheckList.At(int(second.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyUser)
	typeInto(state, hitBulkUserField, "batch-user")
	state.Click(hitBulkApplyAuth)
	state.Click(hitBulkAuthItemList.At(int(authAgent)))
	state.Click(hitBulkApplyAgentForward)
	state.Click(hitBulkAgentForwardValue)
	state.Click(hitBulkPasswordConfirm)
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("batch authentication failed: %q", state.dialogError)
	}
	for _, host := range state.hosts[:2] {
		if host.User != "batch-user" || host.Auth != authAgent || !host.AgentForward || host.Address != first.Address && host.Address != second.Address {
			t.Fatalf("host fields = %+v", host)
		}
	}
	if state.hosts[2] != untouched {
		t.Fatal("unselected host changed")
	}
	if restarted := newTestState(t, state.options); restarted.hosts[0].Auth != authAgent || restarted.hosts[0].User != "batch-user" {
		t.Fatal("batch authentication was not persisted")
	}
}

func TestBulkAuthenticationDialogOffersFieldsWithoutExposingSecrets(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	state.Click(hitBulkHostPassword)
	canvas, _ := paintedTexts(state)
	for _, id := range []ui.ID{
		hitBulkApplyUser, hitBulkUserField, hitBulkApplyAuth, hitBulkAuthTrigger,
		hitBulkApplyIdentity, hitBulkIdentityTrigger, hitBulkApplyPassword, hitBulkPasswordField,
		hitBulkApplyOTP, hitBulkOTPField, hitBulkApplyPassphrase, hitBulkPassphraseField,
		hitBulkApplyAgentForward, hitBulkAgentForwardValue,
	} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Errorf("missing batch authentication control %d", id)
		}
	}
	for _, id := range []ui.ID{hitBulkPasswordField, hitBulkOTPField, hitBulkPassphraseField} {
		if field, _ := hitByID(canvas, id); !field.Secure || field.Value != "" {
			t.Errorf("secret field %d is not masked", id)
		}
	}
	state.Click(hitBulkAuthTrigger)
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitBulkAuthItemList.At(int(authPrivateKey))); !ok {
		t.Fatal("authentication menu does not offer private-key mode")
	}
	state.Click(hitBulkAuthDismiss)
	state.Click(hitBulkIdentityTrigger)
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitBulkIdentityAll); !ok {
		t.Fatal("identity menu does not offer all keys")
	}
}

func TestBulkAuthPreviewCountsPasswordEligibleHosts(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Auth = authAgent
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	state.Click(hitHostCheckList.At(int(state.hosts[1].ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyPassword)
	canvas, texts := paintedTexts(state)
	if _, ok := hitByID(canvas, hitBulkPasswordConfirm); !ok || !contains(texts, "1 eligible") || !contains(texts, "1 skipped") {
		t.Fatalf("password scope count is misleading: %v", texts)
	}
}

func TestBulkAuthenticationBlankOTPDoesNotEraseSavedSeeds(t *testing.T) {
	state := newTestState(t, testOptions(t))
	first, second := state.hosts[0], state.hosts[1]
	for _, host := range []Host{first, second} {
		if err := state.secrets.vault.Set(otpName(host), "otpauth://totp/Motty?secret=JBSWY3DPEHPK3PXP"); err != nil {
			t.Fatal(err)
		}
	}
	state.Click(hitHostCheckList.At(int(first.ID)))
	state.Click(hitHostCheckList.At(int(second.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyOTP)
	state.Click(hitBulkPasswordConfirm)
	if state.dialogError == state.messages.Tools.ErrOTPInvalid {
		t.Fatalf("blank OTP field was rejected as an invalid seed: %q", state.dialogError)
	}
	for _, host := range []Host{first, second} {
		if value, found := vaultValue(t, state, otpName(host)); !found || value == "" {
			t.Fatalf("%s lost its OTP seed", host.Label)
		}
	}
}

func TestBulkAuthenticationClearsOnlySelectedHostSecrets(t *testing.T) {
	state := newTestState(t, testOptions(t))
	key := writeWorkbenchIdentity(t, "id_shared", "secret")
	state.restoreIdentities([]string{key})
	first, second := state.hosts[0], state.hosts[1]
	for index := 0; index < 2; index++ {
		state.hosts[index].Auth = authPrivateKey
		state.hosts[index].Identity = state.identities[0].Fingerprint
	}
	for _, host := range []Host{first, second} {
		if err := state.secrets.vault.Set(otpName(host), "otpauth://totp/Motty?secret=JBSWY3DPEHPK3PXP"); err != nil {
			t.Fatal(err)
		}
		if err := state.secrets.vault.Set(hostPassphraseSecretName(host.CatalogID), encodeHostPassphrase(key, "secret")); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.secrets.vault.Set(passphraseSecretName(key), "shared"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitHostCheckList.At(int(first.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyOTP)
	state.Click(hitBulkOTPRemove)
	state.Click(hitBulkApplyPassphrase)
	state.Click(hitBulkPassphraseKeyItemList.At(int(state.identities[0].ID)))
	state.Click(hitBulkPassphraseRemove)
	state.Click(hitBulkPasswordConfirm)
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("clearing selected credentials failed: %q", state.dialogError)
	}
	for _, name := range []string{otpName(first), hostPassphraseSecretName(first.CatalogID)} {
		if _, found := vaultValue(t, state, name); found {
			t.Fatalf("selected host still has %s", name)
		}
	}
	for _, name := range []string{otpName(second), hostPassphraseSecretName(second.CatalogID), passphraseSecretName(key)} {
		if _, found := vaultValue(t, state, name); !found {
			t.Fatalf("unselected or shared secret %s was deleted", name)
		}
	}
}

func TestBulkAuthenticationSwitchingModesKeepsSavedPassword(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	name := hostSecretName(host.CatalogID, hostPasswordPart)
	if err := state.secrets.vault.Set(name, "saved-before-key-mode"); err != nil {
		t.Fatal(err)
	}
	key := writeWorkbenchIdentity(t, "id_mode", "")
	state.restoreIdentities([]string{key})
	state.Click(hitHostCheckList.At(int(host.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyAuth)
	state.Click(hitBulkAuthItemList.At(int(authPrivateKey)))
	state.Click(hitBulkApplyIdentity)
	state.Click(hitBulkIdentityItemList.At(int(state.identities[0].ID)))
	state.Click(hitBulkPasswordConfirm)
	if state.dialogError != "" || state.hosts[0].Auth != authPrivateKey || state.hosts[0].Identity != state.identities[0].Fingerprint {
		t.Fatalf("key mode failed: %q %+v", state.dialogError, state.hosts[0])
	}
	if _, eligible := hostPasswordSecretName(state.hosts[0]); eligible {
		t.Fatal("key-only authentication still offers the old password")
	}
	if value, found := vaultValue(t, state, name); !found || value != "saved-before-key-mode" {
		t.Fatal("key mode erased the saved password")
	}
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyAuth)
	state.Click(hitBulkAuthItemList.At(int(authAgent)))
	state.Click(hitBulkPasswordConfirm)
	if state.dialogError != "" || state.hosts[0].Auth != authAgent || state.hosts[0].Identity != state.identities[0].Fingerprint {
		t.Fatalf("agent mode failed: %q %+v", state.dialogError, state.hosts[0])
	}
	if value, found := vaultValue(t, state, name); !found || value != "saved-before-key-mode" {
		t.Fatal("agent mode erased the saved password")
	}
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyAuth)
	state.Click(hitBulkAuthItemList.At(int(authPassword)))
	state.Click(hitBulkPasswordConfirm)
	if state.dialogError != "" || state.hosts[0].Auth != authPassword {
		t.Fatalf("password mode failed: %q", state.dialogError)
	}
	if value, found := vaultValue(t, state, name); !found || value != "saved-before-key-mode" {
		t.Fatal("switching back to password mode lost its saved value")
	}
}

func TestBulkAuthenticationMethodOnlyPreservesPinnedKeyAcrossRestart(t *testing.T) {
	state := newTestState(t, testOptions(t))
	key := writeWorkbenchIdentity(t, "id_preserve", "")
	state.restoreIdentities([]string{key})
	host := state.hosts[0]
	host.Auth, host.Identity = authPrivateKey, state.identities[0].Fingerprint
	state.hosts[0] = host
	state.Click(hitHostCheckList.At(int(host.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyAuth)
	state.Click(hitBulkAuthItemList.At(int(authAgent)))
	state.Click(hitBulkPasswordConfirm)
	if state.dialogError != "" || state.hosts[0].Identity != host.Identity {
		t.Fatalf("method-only update changed the unselected key: %q %+v", state.dialogError, state.hosts[0])
	}
	restarted := newTestState(t, state.options)
	if restarted.hosts[0].Identity != host.Identity {
		t.Fatalf("restart discarded the dormant key: %+v", restarted.hosts[0])
	}
	restarted.openEditHostID(restarted.hosts[0].ID)
	restarted.Click(hitDialogSubmit)
	if restarted.hosts[0].Identity != host.Identity {
		t.Fatalf("editing the host discarded the dormant key: %+v", restarted.hosts[0])
	}
}

func TestBulkAuthenticationRejectsEffectiveSystemLoginDuplicate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hostCatalog.systemUser = "local-user"
	first, second := state.hosts[0], state.hosts[1]
	first.Address, first.Port, first.User = "same.example.com", 22, "other-user"
	second.Address, second.Port, second.User = first.Address, first.Port, "local-user"
	state.hosts[0], state.hosts[1] = first, second
	state.Click(hitHostCheckList.At(int(first.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyUser)
	state.Click(hitBulkPasswordConfirm)
	if state.modal != modalBulkHostPassword || state.dialogError != state.messages.BulkAuthDuplicate {
		t.Fatalf("new default login duplicate was accepted: modal=%d error=%q", state.modal, state.dialogError)
	}
	if state.hosts[0].User != "other-user" {
		t.Fatal("duplicate rejection partially changed the username")
	}
}

func TestBulkAuthenticationRejectsBareOTPSeed(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	state.Click(hitHostCheckList.At(int(host.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyOTP)
	typeInto(state, hitBulkOTPField, "JBSWY3DPEHPK3PXP")
	state.Click(hitBulkPasswordConfirm)
	if state.modal != modalBulkHostPassword || state.dialogError != state.messages.Tools.ErrOTPInvalid {
		t.Fatalf("bare OTP seed was accepted: modal=%d error=%q", state.modal, state.dialogError)
	}
	if _, found := vaultValue(t, state, otpName(host)); found {
		t.Fatal("invalid seed was saved")
	}
}

func TestBulkAuthenticationSavesValidOTPForSelectedHostOnly(t *testing.T) {
	state := newTestState(t, testOptions(t))
	first, second := state.hosts[0], state.hosts[1]
	state.Click(hitHostCheckList.At(int(first.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyOTP)
	typeInto(state, hitBulkOTPField, "otpauth://totp/Motty:deploy?secret=JBSWY3DPEHPK3PXP")
	state.Click(hitBulkPasswordConfirm)
	if state.dialogError != "" || state.modal != modalNone {
		t.Fatalf("valid OTP seed was rejected: %q", state.dialogError)
	}
	if value, found := vaultValue(t, state, otpName(first)); !found || value == "" {
		t.Fatal("selected host has no OTP seed")
	}
	if _, found := vaultValue(t, state, otpName(second)); found {
		t.Fatal("unselected host received an OTP seed")
	}
}

func TestBulkAuthenticationRejectsWrongPrivateKeyPassphrase(t *testing.T) {
	state := newTestState(t, testOptions(t))
	key := writeWorkbenchIdentity(t, "id_wrong", "correct")
	state.restoreIdentities([]string{key})
	host := state.hosts[0]
	host.Auth, host.Identity = authPrivateKey, state.identities[0].Fingerprint
	state.hosts[0] = host
	state.Click(hitHostCheckList.At(int(host.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyPassphrase)
	state.Click(hitBulkPassphraseKeyItemList.At(int(state.identities[0].ID)))
	typeInto(state, hitBulkPassphraseField, "wrong")
	state.Click(hitBulkPasswordConfirm)
	if state.dialogError != state.messages.KeyPassphraseWrong || state.modal != modalBulkHostPassword {
		t.Fatalf("wrong passphrase was accepted: modal=%d error=%q", state.modal, state.dialogError)
	}
	if _, found := vaultValue(t, state, hostPassphraseSecretName(host.CatalogID)); found {
		t.Fatal("wrong passphrase was stored")
	}
}

func TestBulkAuthenticationNarrowDialogShowsSaveError(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyOTP)
	typeInto(state, hitBulkOTPField, "invalid-otp")
	state.Click(hitBulkPasswordConfirm)
	canvas, texts := paintAt(state, ui.Size{Width: 640, Height: 480})
	visible := false
	for _, drawn := range canvas.Texts() {
		if drawn.Value == state.messages.Tools.ErrOTPInvalid && drawn.Bounds.Y >= state.bulkPassword.viewport.Y &&
			drawn.Bounds.Y+drawn.Bounds.Height <= state.bulkPassword.viewport.Y+state.bulkPassword.viewport.Height {
			visible = true
		}
	}
	if !visible {
		t.Fatalf("save error outside narrow dialog viewport: %v", texts)
	}
}

func TestBulkAuthenticationRejectsDuplicateEndpointAndInvalidOTP(t *testing.T) {
	state := newTestState(t, testOptions(t))
	first := state.hosts[0]
	state.Click(hitHostCheckList.At(int(first.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyUser)
	typeInto(state, hitBulkUserField, state.hosts[1].User)
	state.hosts[0].Address = state.hosts[1].Address
	state.Click(hitBulkPasswordConfirm)
	if state.modal != modalBulkHostPassword || state.dialogError == "" {
		t.Fatal("duplicate endpoint was saved")
	}
	state.hosts[0].Address = first.Address
	state.Click(hitBulkApplyUser)
	state.Click(hitBulkApplyOTP)
	typeInto(state, hitBulkOTPField, "not an otp URI")
	state.Click(hitBulkPasswordConfirm)
	if state.modal != modalBulkHostPassword || state.dialogError == "" {
		t.Fatal("invalid OTP was accepted")
	}
	if _, found := vaultValue(t, state, otpName(first)); found {
		t.Fatal("invalid OTP created a secret")
	}
}

func TestBulkAuthenticationSavesPassphraseOnlyForSelectedHosts(t *testing.T) {
	state := newTestState(t, testOptions(t))
	key := writeWorkbenchIdentity(t, "id_shared", "secret")
	state.restoreIdentities([]string{key})
	first, second := state.hosts[0], state.hosts[1]
	for index := 0; index < 2; index++ {
		state.hosts[index].Auth = authPrivateKey
		state.hosts[index].Identity = state.identities[0].Fingerprint
	}
	global := passphraseSecretName(key)
	if err := state.secrets.vault.Set(global, "old-global"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitHostCheckList.At(int(first.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyPassphrase)
	state.Click(hitBulkPassphraseKeyItemList.At(int(state.identities[0].ID)))
	typeInto(state, hitBulkPassphraseField, "secret")
	state.Click(hitBulkPasswordConfirm)
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("passphrase save failed: %q", state.dialogError)
	}
	if got := savedPassphraseName(state.secrets.vault, first.CatalogID, key); got != hostPassphraseSecretName(first.CatalogID) {
		t.Fatal("selected host did not get a local passphrase")
	}
	if got := savedPassphraseName(state.secrets.vault, second.CatalogID, key); got != global {
		t.Fatal("unselected host stopped using shared passphrase")
	}
	if value, found := vaultValue(t, state, global); !found || value != "old-global" {
		t.Fatal("shared key passphrase was overwritten")
	}
	restarted := newTestState(t, state.options)
	if _, value, found, err := savedPassphrase(restarted.secrets.vault, first.CatalogID, key); err != nil || !found || value != "secret" {
		t.Fatalf("selected host override lost after restart: value=%q found=%v err=%v", value, found, err)
	}
	if _, value, found, err := savedPassphrase(restarted.secrets.vault, second.CatalogID, key); err != nil || !found || value != "old-global" {
		t.Fatalf("unselected host fallback changed after restart: value=%q found=%v err=%v", value, found, err)
	}
}

func TestBulkAuthenticationRefusesBlockedConfigurationBeforeWritingSecrets(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	state.Click(hitHostCheckList.At(int(host.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyUser)
	typeInto(state, hitBulkUserField, "changed")
	state.Click(hitBulkApplyPassword)
	typeInto(state, hitBulkPasswordField, "secret")
	state.saveBlocked = true
	state.Click(hitBulkPasswordConfirm)
	if state.modal != modalBulkHostPassword || state.dialogError == "" || state.hosts[0].User != host.User {
		t.Fatalf("blocked config partially applied: modal=%d error=%q user=%q", state.modal, state.dialogError, state.hosts[0].User)
	}
	if _, found := vaultValue(t, state, hostSecretName(host.CatalogID, hostPasswordPart)); found {
		t.Fatal("blocked config wrote a password")
	}
}

func TestBulkAuthenticationConfigFailureRestoresHostAndVault(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	state.Click(hitHostCheckList.At(int(host.ID)))
	state.Click(hitBulkHostPassword)
	state.Click(hitBulkApplyUser)
	typeInto(state, hitBulkUserField, "changed")
	state.Click(hitBulkApplyPassword)
	typeInto(state, hitBulkPasswordField, "new-password")
	state.lastSaved, state.hasSaved = state.snapshot(), true
	before := append([]byte(nil), state.configBase.local...)
	state.configBase.local = []byte("{invalid local state")
	t.Cleanup(func() { state.configBase.local = before })
	state.Click(hitBulkPasswordConfirm)
	if state.modal != modalBulkHostPassword || state.dialogError == "" || state.hosts[0].User != host.User {
		t.Fatalf("config failure partially applied: modal=%d error=%q user=%q", state.modal, state.dialogError, state.hosts[0].User)
	}
	if _, found := vaultValue(t, state, hostSecretName(host.CatalogID, hostPasswordPart)); found {
		t.Fatal("config failure left an orphan saved password")
	}
}

func TestBulkAuthenticationIsAvailableForKeyOnlySelection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Auth = authAgent
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	canvas, _ := paintedTexts(state)
	if action, ok := hitByID(canvas, hitBulkHostPassword); !ok || action.Disabled {
		t.Fatal("agent-only host cannot edit its authentication")
	}
	state.Click(hitBulkHostPassword)
	if state.modal != modalBulkHostPassword {
		t.Fatal("SSH selection did not open the authentication editor")
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

func TestBulkAuthenticationContextMenuIncludesKeyOnlySelection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Auth = authAgent
	state.hosts[1].Auth = authPrivateKey
	state.hostSelections = []uint32{state.hosts[0].ID, state.hosts[1].ID}
	state.hostMenu = hostMenu{open: true, host: state.hosts[1].ID, checked: true}
	canvas, _ := paintedTexts(state)
	if action, ok := hitByID(canvas, hitHostMenuPassword); !ok || action.Disabled {
		t.Fatal("key-only context selection cannot edit authentication")
	}
}
