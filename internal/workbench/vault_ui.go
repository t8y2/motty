package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/otp"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

// Saved passwords. The vault package keeps them encrypted; this file is what the
// workbench does with it: offer to save a password or a key passphrase when the
// user types one, answer the same question by itself the next time it is asked,
// and let the user set a master password or forget everything in Settings.

// secretsUI is the state of saving and using secrets.
type secretsUI struct {
	vault *vault.Vault
	// declined is set when the user dismissed the unlock dialog, so the window
	// does not ask again at every prompt; unlocking from Settings clears it.
	declined bool
	// canSave is whether the open password or passphrase dialog offers to save,
	// worked out when it opened, and save whether the box is ticked.
	canSave, save bool

	unlock, newPassword, confirmPassword text.Editor
	dialogError                          string
	changing                             bool
	// after is what to do once the vault is unlocked from Settings.
	after func()
	// actions is where Settings lays out its buttons, so painting them keeps no
	// slice of its own.
	actions [3]components.ButtonAction
	// layout lays out the body of the vault's dialogs, one at a time.
	layout flex.View
}

// secretPurpose says what a secret is for: its name in the vault, and the
// wording of the box the user ticks.
type secretKind uint8

const (
	secretPassword secretKind = iota + 1
	secretPassphrase
)

func passwordSecretName(endpoint sshconn.Endpoint) string {
	return "password:" + endpoint.User + "@" + endpoint.Host + ":" + strconv.Itoa(int(endpoint.Port))
}

// aiKeySecretName is where the API key of an AI provider is kept. The provider
// ID is random, so the name is the same on every computer the vault syncs to.
func aiKeySecretName(providerID string) string { return "aikey:" + providerID }

func passphraseSecretName(identity string) string { return "passphrase:" + identity }

// otpSecretName is where the one-time code secret of an account is kept.
func otpSecretName(endpoint sshconn.Endpoint) string {
	return "otp:" + endpoint.User + "@" + endpoint.Host + ":" + strconv.Itoa(int(endpoint.Port))
}

// initSecrets opens the vault next to the state file. A file that cannot be read
// is set aside, not overwritten: it may hold what a later release can open.
func (state *State) initSecrets() {
	path := filepath.Join(filepath.Dir(state.options.StatePath), "vault.json")
	keys := state.options.Keystore
	if keys == nil {
		keys = systemKeystore{}
	}
	opened, err := vault.OpenShared(path, keys)
	if errors.Is(err, vault.ErrCorrupt) {
		aside := path + ".bad-" + time.Now().Format("20060102-150405")
		if os.Rename(path, aside) == nil {
			vault.Forget(path)
			opened, err = vault.OpenShared(path, keys)
			state.status = fmt.Sprintf(state.messages.VaultCorruptFmt, filepath.Base(aside))
		}
	}
	if err != nil {
		return // no saved passwords this run; the rest of the application is unaffected
	}
	state.secrets.vault = opened
}

// isPlainPassword reports the one secret question with no wording: the plain
// password method.
func isPlainPassword(questions []sshconn.Question) bool {
	return len(questions) == 1 && questions[0].Secret && questions[0].Text == ""
}

// isOTPQuestion reports a single prompt whose wording asks for a one-time code.
func isOTPQuestion(questions []sshconn.Question) bool {
	return len(questions) == 1 && questions[0].Text != "" && otp.LooksLikeQuestion(questions[0].Text)
}

// promptSecret is the secret an open question could be answered with: its name,
// and what kind. A question that is not a plain password or passphrase, or that
// is asked again because the first answer was refused, has none.
func promptSecret(session *Session) (string, secretKind) {
	switch {
	case session.phase == sshconn.PhasePasswordPrompt && session.attempt == 1 && isPlainPassword(session.questions):
		return passwordSecretName(session.endpoint), secretPassword
	case session.phase == sshconn.PhaseKeyPassphrasePrompt && session.attempt == 1 && session.identity != "":
		return passphraseSecretName(session.identity), secretPassphrase
	}
	return "", 0
}

type autofillResult uint8

const (
	autofillNone autofillResult = iota
	autofillSent
	autofillNeedsUnlock
)

// autofill answers a question from the vault when it has the answer. It tells the
// caller when the vault has to be unlocked first.
func (state *State) autofill(session *Session) autofillResult {
	if session.phase == sshconn.PhasePasswordPrompt && isOTPQuestion(session.questions) {
		return state.autofillOTP(session)
	}
	name, _ := promptSecret(session)
	vaultFile := state.secrets.vault
	if name == "" || vaultFile == nil || vaultFile.Len() == 0 {
		return autofillNone
	}
	value, found, err := vaultFile.Get(name)
	switch {
	case errors.Is(err, vault.ErrLocked):
		if state.secrets.declined {
			return autofillNone
		}
		return autofillNeedsUnlock
	case err != nil || !found:
		return autofillNone
	}
	switch session.phase {
	case sshconn.PhasePasswordPrompt:
		session.conn.SubmitPassword(value)
	case sshconn.PhaseKeyPassphrasePrompt:
		session.conn.SubmitKeyPassphrase(value)
	}
	session.answered = promptKey(session.phase, session.attempt, session.identity, session.endpoint, session.role)
	session.filledSecret = name
	return autofillSent
}

// canAutofill reports whether the question a connection is about to ask is one
// the vault answers with no one's help, as a restore needs to know before it
// decides to give up on the tab.
func (state *State) canAutofill(status sshconn.Status) bool {
	vaultFile := state.secrets.vault
	if vaultFile != nil && vaultFile.Len() > 0 && status.Phase == sshconn.PhasePasswordPrompt &&
		isOTPQuestion(status.Questions) {
		_, found, err := vaultFile.Get(otpSecretName(status.Endpoint))
		return err == nil && found
	}
	if vaultFile == nil || vaultFile.Len() == 0 || status.Attempt > 1 {
		return false
	}
	var name string
	switch {
	case status.Phase == sshconn.PhasePasswordPrompt && isPlainPassword(status.Questions):
		name = passwordSecretName(status.Endpoint)
	case status.Phase == sshconn.PhaseKeyPassphrasePrompt && status.IdentityPath != "":
		name = passphraseSecretName(status.IdentityPath)
	default:
		return false
	}
	_, found, err := vaultFile.Get(name)
	return err == nil && found
}

// forgetRefusedSecret drops a saved secret the server has just refused, so the
// next question is the user's and the stale one is not tried again.
func (state *State) forgetRefusedSecret(session *Session, name string) bool {
	if session.filledSecret == "" || session.filledSecret != name || state.secrets.vault == nil {
		return false
	}
	session.filledSecret = ""
	_ = state.secrets.vault.Delete(name)
	return true
}

// offerToSave works out whether the dialog that is opening can save what is
// typed into it, and starts with the box clear.
func (state *State) offerToSave() {
	state.secrets.save = false
	vaultFile := state.secrets.vault
	switch {
	case vaultFile == nil:
		state.secrets.canSave = false
	case vaultFile.Unlocked():
		state.secrets.canSave = true
	case vaultFile.HasMasterPassword():
		state.secrets.canSave = false
	default:
		err := vaultFile.Prepare()
		state.secrets.canSave = err == nil
	}
}

// stageSecret keeps what the user typed, if the box to save it is ticked, until the
// server has accepted it: a wrong password is not worth saving.
func (state *State) stageSecret(session *Session, name, value string) {
	session.pendingSecretName, session.pendingSecretValue = "", ""
	if state.secrets.canSave && state.secrets.save && value != "" {
		session.pendingSecretName, session.pendingSecretValue = name, value
	}
}

// rememberSecret saves what the user asked to be saved, once the server has
// accepted it. A failure is told, and never stops the connection.
func (state *State) rememberSecret(session *Session) {
	name, value := session.pendingSecretName, session.pendingSecretValue
	session.pendingSecretName, session.pendingSecretValue = "", ""
	if name == "" || state.secrets.vault == nil {
		return
	}
	if err := state.secrets.vault.Set(name, value); err != nil {
		state.status = fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
		return
	}
	state.status = fmt.Sprintf(state.messages.VaultSavedFmt, session.endpoint.User+"@"+session.endpoint.Host)
}

// vaultErrorText says why the vault refused, in the current language.
func (state *State) vaultErrorText(err error) string {
	switch {
	case errors.Is(err, vault.ErrKeyLost):
		return state.messages.VaultKeyLost
	case errors.Is(err, vault.ErrUnavailable):
		return state.messages.VaultUnavailable
	case errors.Is(err, vault.ErrWrongPassword):
		return state.messages.VaultWrongPassword
	}
	return err.Error()
}

// openVaultUnlock asks for the master password. after, if set, runs once it has
// been typed correctly; otherwise the prompt that was waiting goes on.
func (state *State) openVaultUnlock(session *Session, after func()) {
	state.modal = modalVaultUnlock
	if session != nil {
		state.modalSession = session.ID
	}
	state.secrets.unlock = text.Editor{}
	state.secrets.dialogError = ""
	state.secrets.after = after
}

func (state *State) clickVaultUnlock(id ui.ID) {
	switch id {
	case hitVaultUnlockSubmit:
		state.submitVaultUnlock()
	case hitVaultUnlockCancel, hitVaultUnlockClose, hitVaultUnlockDismiss:
		state.secrets.declined = state.secrets.after == nil
		state.secrets.after = nil
		state.closeModal()
		state.showNextPrompt()
	}
}

func (state *State) submitVaultUnlock() {
	if state.secrets.vault == nil {
		state.closeModal()
		return
	}
	if err := state.secrets.vault.Unlock(state.secrets.unlock.String()); err != nil {
		state.secrets.dialogError = state.vaultErrorText(err)
		state.secrets.unlock = text.Editor{}
		return
	}
	state.secrets.declined = false
	if state.sync.waitingForVault {
		state.sync.waitingForVault = false
		state.scheduleSync()
	}
	after := state.secrets.after
	state.secrets.after = nil
	state.closeModal()
	if after != nil {
		after()
	}
	state.showNextPrompt()
}

func (state *State) paintVaultUnlockDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitVaultUnlockDismiss, CloseID: hitVaultUnlockClose, InitialFocusID: hitVaultUnlockField,
		ContentSize: ui.Size{Width: 480}, FitBody: true, BodyHeight: withError(66, state.secrets.dialogError),
	})
	layout := dialog.Begin(m.VaultUnlockTitle, "")
	var field flex.Box
	f := state.secrets.layout.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Gap(12).Children(func() {
		field = f.Slot().Height(62)
		state.problemLine(f, state.secrets.dialogError)
	})
	f.End()
	content := components.BeginField(canvas, theme, field.Rect()).Paint(m.VaultUnlockLabel, "", "")
	value := state.secrets.unlock.String()
	focused := canvas.Focused() == hitVaultUnlockField
	components.PaintInput(canvas, theme, content, components.InputSnapshot{
		ID: hitVaultUnlockField, Label: m.VaultUnlockLabel, Value: value, Secure: true,
		Focused: focused, PlatformEditing: focused && value != "", Invalid: state.secrets.dialogError != "",
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: hitVaultUnlockCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitVaultUnlockSubmit, Label: m.VaultUnlockButton},
	)
	dialog.End()
}

// openMasterPassword opens the dialog that sets or changes the master password.
func (state *State) openMasterPassword(changing bool) {
	state.modal = modalMasterPassword
	state.secrets.changing = changing
	state.secrets.newPassword, state.secrets.confirmPassword = text.Editor{}, text.Editor{}
	state.secrets.dialogError = ""
}

func (state *State) clickMasterPassword(id ui.ID) {
	switch id {
	case hitMasterPasswordSubmit:
		state.submitMasterPassword()
	case hitMasterPasswordCancel, hitMasterPasswordClose, hitMasterPasswordDismiss:
		state.closeModal()
		state.showNextPrompt()
	}
}

func (state *State) submitMasterPassword() {
	secrets, m := &state.secrets, state.messages
	password := secrets.newPassword.String()
	switch {
	case secrets.vault == nil:
		state.closeModal()
		return
	case len([]rune(password)) < vault.MinMasterPasswordLength:
		secrets.dialogError = fmt.Sprintf(m.MasterPasswordTooShortFmt, vault.MinMasterPasswordLength)
		return
	case password != secrets.confirmPassword.String():
		secrets.dialogError = m.MasterPasswordMismatch
		return
	}
	if err := secrets.vault.SetMasterPassword(password); err != nil {
		secrets.dialogError = state.vaultErrorText(err)
		return
	}
	state.closeModal()
	state.status = m.VaultMasterSet
	state.showNextPrompt()
}

func (state *State) paintMasterPasswordDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitMasterPasswordDismiss, CloseID: hitMasterPasswordClose, InitialFocusID: hitMasterPasswordNew,
		ContentSize: ui.Size{Width: 520}, FitBody: true, BodyHeight: withError(132, state.secrets.dialogError), // two fields
	})
	title := m.MasterPasswordTitleSet
	if state.secrets.changing {
		title = m.MasterPasswordTitleChange
	}
	layout := dialog.Begin(title, m.MasterPasswordDescription)
	entries := [...]struct {
		id    ui.ID
		label string
		value string
	}{
		{hitMasterPasswordNew, m.MasterPasswordNewLabel, state.secrets.newPassword.String()},
		{hitMasterPasswordConfirm, m.MasterPasswordConfirmLabel, state.secrets.confirmPassword.String()},
	}
	var fields [len(entries)]flex.Box
	f := state.secrets.layout.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Gap(6).Children(func() {
		for index := range fields {
			fields[index] = f.Slot().Height(62)
		}
		if problem := state.secrets.dialogError; problem != "" {
			f.Text(problem).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(state.pal.danger).LineHeight(18).MarginSides(4, 0, 0, 0)
		}
	})
	f.End()
	for index, entry := range entries {
		content := components.BeginField(canvas, theme, fields[index].Rect()).Paint(entry.label, "", "")
		focused := canvas.Focused() == entry.id
		components.PaintInput(canvas, theme, content, components.InputSnapshot{
			ID: entry.id, Label: entry.label, Value: entry.value, Secure: true,
			Focused: focused, PlatformEditing: focused && entry.value != "", Invalid: state.secrets.dialogError != "",
		})
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitMasterPasswordCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitMasterPasswordSubmit, Label: m.MasterPasswordSave},
	)
	dialog.End()
}

func (state *State) openVaultForget() {
	state.modal = modalVaultForget
	state.secrets.dialogError = ""
}

func (state *State) clickVaultForget(id ui.ID) {
	switch id {
	case hitVaultForgetConfirm:
		if state.secrets.vault != nil {
			if err := state.secrets.vault.Clear(); err != nil {
				state.status = fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
			} else {
				state.status = state.messages.VaultCleared
			}
		}
		state.closeModal()
		state.showNextPrompt()
	case hitVaultForgetCancel, hitVaultForgetClose, hitVaultForgetDismiss:
		state.closeModal()
		state.showNextPrompt()
	}
}

func (state *State) paintVaultForgetDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m := state.messages
	// The text is wrapped to the body's width: 540 less the dialog's padding.
	var wrapped [3]string
	lines := canvas.WrapLines(wrapped[:0], m.VaultForgetDescription, metrics.FontSizeM, ui.WeightRegular, 500, 3)
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitVaultForgetDismiss, CloseID: hitVaultForgetClose, InitialFocusID: hitVaultForgetCancel,
		ContentSize: ui.Size{Width: 540}, FitBody: true, BodyHeight: float32(len(lines))*21 - 1,
	})
	layout := dialog.Begin(m.VaultForgetTitle, "")
	f := state.secrets.layout.Begin(canvas, state.Theme(), layout.Body)
	f.Paragraph(m.VaultForgetDescription, len(wrapped)).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(21)
	f.End()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitVaultForgetCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitVaultForgetConfirm, Label: m.VaultForgetConfirm, Variant: components.ButtonDestructive},
	)
	dialog.End()
}

// clickVaultSettings handles the buttons of the Saved passwords group. It
// reports whether the click was one of them.
func (state *State) clickVaultSettings(id ui.ID) bool {
	vaultFile := state.secrets.vault
	switch id {
	case hitSettingsVaultForget:
		if vaultFile != nil {
			state.openVaultForget()
		}
	case hitSettingsMasterSet:
		if vaultFile != nil {
			state.openMasterPassword(false)
		}
	case hitSettingsMasterChange:
		if vaultFile == nil {
			break
		}
		if vaultFile.Unlocked() {
			state.openMasterPassword(true)
		} else {
			state.openVaultUnlock(nil, func() { state.openMasterPassword(true) })
		}
	case hitSettingsMasterRemove:
		if vaultFile == nil {
			break
		}
		remove := func() {
			if err := vaultFile.RemoveMasterPassword(); err != nil {
				state.status = fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
				return
			}
			state.status = state.messages.VaultMasterRemoved
		}
		if vaultFile.Unlocked() {
			remove()
		} else {
			state.openVaultUnlock(nil, remove)
		}
	case hitSettingsMasterLock:
		if vaultFile != nil {
			vaultFile.Lock()
			state.secrets.declined = false
		}
	case hitSettingsMasterUnlock:
		if vaultFile != nil && !vaultFile.Unlocked() {
			state.openVaultUnlock(nil, nil)
		}
	default:
		return false
	}
	return true
}

// paintSecuritySettings is the Saved passwords group of Settings.
func (state *State) paintSecuritySettings(page *settingsPage) {
	m := state.messages
	vaultFile := state.secrets.vault
	if vaultFile == nil {
		return
	}
	hasPassword, unlocked, count := vaultFile.HasMasterPassword(), vaultFile.Unlocked(), vaultFile.Len()
	description := m.SettingsVaultEmpty
	switch {
	case count > 0 && hasPassword:
		description = fmt.Sprintf(m.SettingsVaultDescriptionLockedFmt, count)
	case count > 0:
		description = fmt.Sprintf(m.SettingsVaultDescriptionFmt, count)
	}
	forget := components.ButtonAction{ID: hitSettingsVaultForget, Label: m.SettingsVaultForgetAll, Variant: components.ButtonOutline}

	actions := state.secrets.actions[:0]
	switch {
	case !hasPassword:
		actions = append(actions, components.ButtonAction{ID: hitSettingsMasterSet, Label: m.SettingsMasterPasswordSet, Variant: components.ButtonOutline})
	case unlocked:
		actions = append(actions,
			components.ButtonAction{ID: hitSettingsMasterLock, Label: m.SettingsMasterPasswordLock, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitSettingsMasterChange, Label: m.SettingsMasterPasswordChange, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitSettingsMasterRemove, Label: m.SettingsMasterPasswordRemove, Variant: components.ButtonOutline},
		)
	default:
		actions = append(actions,
			components.ButtonAction{ID: hitSettingsMasterUnlock, Label: m.SettingsMasterPasswordUnlock},
			components.ButtonAction{ID: hitSettingsMasterChange, Label: m.SettingsMasterPasswordChange, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitSettingsMasterRemove, Label: m.SettingsMasterPasswordRemove, Variant: components.ButtonOutline},
		)
	}
	page.groups(func() {
		page.group(m.SettingsGroupSecurity, func() {
			page.actionRow(m.SettingsVault, description, count == 0 && !hasPassword, forget)
			page.actionRow(m.SettingsMasterPassword, m.SettingsMasterPasswordDescription, false, actions...)
		})
	})
}

// withError is a dialog body's height with the one-line error under it, when
// there is one.
func withError(height float32, problem string) float32 {
	if problem != "" {
		return height + 30
	}
	return height
}

// secretFieldBody is the height of a dialog body that is one secret field, the
// error under it when there is one, and the box offering to save what is typed
// when the vault can.
func (state *State) secretFieldBody(problem string) float32 {
	if state.secrets.canSave {
		return 134
	}
	return withError(66, problem)
}

// paintSaveBox draws the box under a dialog body that offers to save what is
// typed, where a one-field dialog has always put it.
func (state *State) paintSaveBox(canvas *ui.Canvas, body ui.Rect, id ui.ID, label string) {
	state.paintSaveBoxAt(canvas, ui.Rect{X: body.X, Y: body.Y + 99, Width: body.Width, Height: 20}, id, label, "", ui.ID{})
}

// paintSaveBoxAt draws the box where a dialog laid it out. A hint becomes a
// help icon at its right, so a password error can use the line under the field
// without colliding with the instruction.
func (state *State) paintSaveBoxAt(canvas *ui.Canvas, bounds ui.Rect, id ui.ID, label, hint string, hintID ui.ID) {
	if !state.secrets.canSave || bounds == (ui.Rect{}) {
		return
	}
	var help flex.Box
	f := state.secrets.layout.Begin(canvas, state.Theme(), bounds)
	f.Row().Fill().PadX(0).Gap(2).Items(flex.Center).Children(func() {
		// The checkbox slot is 40px wide with the 16px box centered in it. Pull
		// the slot left so the box starts where the password field border does.
		f.Checkbox(id, state.secrets.save).Label(label).MarginSides(0, 0, 0, -8)
		f.Text(label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(state.pal.text).LineHeight(20).MarginSides(0, 0, 0, -6)
		if hint != "" {
			help = f.Slot().Size(16, 16).MarginSides(0, 6, 0, 0).Shrink(0)
		}
		// Keep the help icon beside the label; the rest of the row stays empty
		// instead of pushing it to the far edge.
		f.Spacer().Grow(1)
	})
	f.End()
	if hint != "" && help.Rect().Width > 0 && help.Rect().Height > 0 {
		canvas.HitWithOptions(hintID, help.Rect(), hint, ui.HitOptions{Role: ui.RoleButton, SkipFocus: true, Hint: hint})
		paintIcon(canvas, iconHelp, insetRect(help.Rect(), 0.08), state.pal.muted)
	}
}

// problemLine declares the one-line error under a dialog's fields, when
// there is one.
func (state *State) problemLine(f *flex.Frame, problem string) {
	if problem != "" {
		f.Text(problem).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(state.pal.danger).LineHeight(18)
	}
}

// autofillOTP answers a question for a one-time code from the account's secret.
// It does so once per connection: a code the server refused is the user's to
// retype, not something to send again until the account locks.
func (state *State) autofillOTP(session *Session) autofillResult {
	vaultFile := state.secrets.vault
	if session.otpSent || vaultFile == nil || vaultFile.Len() == 0 {
		return autofillNone
	}
	name := otpSecretName(session.endpoint)
	value, found, err := vaultFile.Get(name)
	switch {
	case errors.Is(err, vault.ErrLocked):
		if state.secrets.declined {
			return autofillNone
		}
		return autofillNeedsUnlock
	case err != nil || !found:
		return autofillNone
	}
	secret, err := otp.Parse(value)
	if err != nil {
		return autofillNone
	}
	session.conn.SubmitPassword(secret.Code(state.options.WallNow()))
	session.otpSent = true
	session.answered = promptKey(session.phase, session.attempt, session.identity, session.endpoint, session.role)
	if secret.Kind == otp.HOTP {
		// The counter moves with every code used, or the next one would repeat.
		if err := vaultFile.Set(name, secret.Next().String()); err != nil {
			state.status = fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
		}
	}
	return autofillSent
}
