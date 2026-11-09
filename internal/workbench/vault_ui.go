package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/otp"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

// Saved passwords. The vault package keeps them encrypted; this file is what the
// workbench does with it: offer to save a password or a key passphrase when the
// user types one, answer the same question by itself the next time it is asked,
// and let the user forget everything in Settings. A vault still kept by the master
// password of an earlier version is opened once, by the dialog below, and is an
// ordinary one afterwards.

// secretsUI is the state of saving and using secrets.
type secretsUI struct {
	vault *vault.Vault
	// declined is set when the user dismissed the dialog that asks for the old
	// master password, so the window does not ask again at every prompt until the
	// next start; Settings can ask again.
	declined bool
	// canSave is whether the open password or passphrase dialog offers to save,
	// worked out when it opened, and save whether the box is ticked.
	canSave, save bool

	unlock      text.Editor
	dialogError string
	// after is what to do once the vault is unlocked from Settings.
	after func()
	// actions is where Settings lays out its buttons, so painting them keeps no
	// slice of its own.
	actions [3]components.ButtonAction
	// layout lays out the body of the vault's dialogs, one at a time.
	layout ui.View
}

// secretPurpose says what a secret is for: its name in the vault, and the
// wording of the box the user ticks.
type secretKind uint8

const (
	secretPassword secretKind = iota + 1
	secretPassphrase
)

// The parts of a host's secrets: hostSecretName(catalogID, part). They are kept
// under the host, so editing its address or login moves nothing and deleting it
// deletes them.
const (
	hostPasswordPart = "password"
	hostOTPPart      = "otp"
)

func hostSecretName(catalogID, part string) string { return "host:" + catalogID + ":" + part }

// passwordName and otpSecretName are the secrets of the host the prompt of a
// session is about (see hostCatalog.secretOwner).
func (state *State) passwordName(session *Session) string {
	return state.passwordSecret(session.host, session.endpoint, session.role)
}

func vncSystemPasswordPart(username string) string { return "vnc-system-password:" + username }

func passwordPart(host Host, endpoint sshconn.Endpoint) string {
	if host.Protocol == protocolVNC {
		if username, ok := strings.CutPrefix(endpoint.User, vncAccountPrefix); ok {
			return vncSystemPasswordPart(username)
		}
	}
	return hostPasswordPart
}

func (state *State) otpSecretName(session *Session) string {
	return hostSecretName(state.secretOwner(session.host, session.endpoint, session.role), hostOTPPart)
}

// aiKeySecretName is where the API key of an AI provider is kept. The provider
// ID is random, so the name is the same on every computer the vault syncs to.
func aiKeySecretName(providerID string) string { return "aikey:" + providerID }

func passphraseSecretName(identity string) string { return "passphrase:" + identity }

// initSecrets opens the vault next to the state file. A file that cannot be read
// is set aside, not overwritten: it may hold what a later release can open.
func (state *State) initSecrets() {
	path := filepath.Join(filepath.Dir(state.options.StatePath), "vault.json")
	keys := state.options.Keystore
	if keys == nil {
		keys = defaultKeystore(path)
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

// isPasswordQuestion reports the one secret question that asks for the account
// password: the plain password method, which has no wording, or a
// keyboard-interactive prompt such as PAM's "Password: ", which is how most
// OpenSSH servers take the password. A hidden question that does not read as a
// password (a PIN, a one-time code) is not answered with the saved password: a
// wrong guess would be refused and the refusal would forget the saved one.
func isPasswordQuestion(questions []sshconn.Question) bool {
	if len(questions) != 1 || !questions[0].Secret {
		return false
	}
	wording := questions[0].Text
	return wording == "" || !otp.LooksLikeQuestion(wording) && looksLikePasswordPrompt(wording)
}

// looksLikePasswordPrompt reports whether a server's prompt names the password,
// in the languages servers commonly answer in.
func looksLikePasswordPrompt(wording string) bool {
	lower := strings.ToLower(wording)
	for _, word := range []string{
		"password", "passwd", "passwort", "mot de passe", "contraseña", "senha", "пароль",
		"密码", "密碼", "口令", "パスワード", "비밀번호",
	} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// isOTPQuestion reports a single prompt whose wording asks for a one-time code.
func isOTPQuestion(questions []sshconn.Question) bool {
	return len(questions) == 1 && questions[0].Text != "" && otp.LooksLikeQuestion(questions[0].Text)
}

// promptSecret is the secret an open question could be answered with: its name,
// and what kind. A question that is not a plain password or passphrase, or that
// is asked again because the first answer was refused, has none.
func (state *State) promptSecret(session *Session) (string, secretKind) {
	switch {
	case session.phase == sshconn.PhasePasswordPrompt && session.attempt == 1 && isPasswordQuestion(session.questions):
		return state.passwordName(session), secretPassword
	case session.phase == sshconn.PhaseKeyPassphrasePrompt && session.attempt == 1 && session.identity != "":
		return passphraseSecretName(session.identity), secretPassphrase
	}
	return "", 0
}

type autofillResult uint8

const (
	autofillNone autofillResult = iota
	autofillSent
)

// autofill answers a question from the vault when it has the answer.
func (state *State) autofill(session *Session) autofillResult {
	if session.phase == sshconn.PhasePasswordPrompt && isOTPQuestion(session.questions) {
		return state.autofillOTP(session)
	}
	name, _ := state.promptSecret(session)
	vaultFile := state.secrets.vault
	if name == "" || vaultFile == nil || vaultFile.Len() == 0 {
		return autofillNone
	}
	value, found, err := vaultFile.Get(name)
	switch {
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
func (state *State) canAutofill(session *Session, status sshconn.Status) bool {
	owner := state.secretOwner(session.host, status.Endpoint, status.EndpointRole)
	vaultFile := state.secrets.vault
	if vaultFile != nil && vaultFile.Len() > 0 && status.Phase == sshconn.PhasePasswordPrompt &&
		isOTPQuestion(status.Questions) {
		_, found, err := vaultFile.Get(hostSecretName(owner, hostOTPPart))
		return err == nil && found
	}
	if vaultFile == nil || vaultFile.Len() == 0 || status.Attempt > 1 {
		return false
	}
	var name string
	switch {
	case status.Phase == sshconn.PhasePasswordPrompt && isPasswordQuestion(status.Questions):
		name = state.passwordSecret(session.host, status.Endpoint, status.EndpointRole)
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
	if strings.HasPrefix(name, credentialSecretName("")) {
		// Other hosts use it too, and one host that refuses it says nothing about
		// the rest. The user changes it in Settings.
		state.status = state.messages.Credentials.StatusRefused
		return false
	}
	_ = state.secrets.vault.Delete(name)
	return true
}

// offerToSave works out whether the dialog that is opening can save what is
// typed into it, and starts with the box clear.
func (state *State) offerToSave() {
	state.secrets.save = false
	vaultFile := state.secrets.vault
	state.secrets.canSave = vaultFile != nil && vaultFile.Prepare() == nil
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
	case errors.Is(err, vault.ErrNeedsConversion):
		return state.messages.VaultNeedsConversion
	case errors.Is(err, vault.ErrKeyLost):
		return state.messages.VaultKeyLost
	case errors.Is(err, vault.ErrUnavailable):
		return state.messages.VaultUnavailable
	case errors.Is(err, vault.ErrWrongPassword):
		return state.messages.VaultWrongPassword
	}
	return err.Error()
}

// openVaultUnlock asks for the master password of an earlier version, once, to
// move the saved passwords under the key store. after, if set, runs once it has
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

func (state *State) clickVaultUnlock(id gfx.ID) {
	switch id {
	case hitVaultUnlockSubmit:
		state.submitVaultUnlock()
	case hitVaultUnlockCancel, hitVaultUnlockClose, hitVaultUnlockDismiss:
		state.secrets.declined = true
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
	if err := state.secrets.vault.ConvertLegacy(state.secrets.unlock.String()); err != nil {
		state.secrets.dialogError = state.vaultErrorText(err)
		state.secrets.unlock = text.Editor{}
		return
	}
	state.secrets.declined = false
	state.migrateSecretsToHosts(false)
	state.reloadStoredIdentities() // keys that came by sync while it was locked
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

func (state *State) paintVaultUnlockDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	m, theme := state.messages, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitVaultUnlockDismiss, CloseID: hitVaultUnlockClose, InitialFocusID: hitVaultUnlockField,
		ContentSize: gfx.Size{Width: 480}, FitBody: true, BodyHeight: withError(66, state.secrets.dialogError),
	})
	layout := dialog.Begin(m.VaultUnlockTitle, m.VaultConvertDescription)
	var field ui.Box
	f := state.secrets.layout.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Gap(12).Children(func() {
		field = f.Slot().Height(62)
		state.problemLine(f, state.secrets.dialogError)
	})
	f.End()
	content := components.BeginField(canvas, theme, field.Rect()).Paint(m.VaultUnlockLabel, "", "")
	value := state.secrets.unlock.String()
	focused := canvas.Focused() == hitVaultUnlockField
	state.paintSecretInput(canvas, content, components.InputSnapshot{
		ID: hitVaultUnlockField, Label: m.VaultUnlockLabel, Value: value, Secure: true,
		Focused: focused, PlatformEditing: focused && value != "", Invalid: state.secrets.dialogError != "",
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: hitVaultUnlockCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitVaultUnlockSubmit, Label: m.VaultUnlockButton},
	)
	dialog.End()
}

func (state *State) openVaultForget() {
	state.modal = modalVaultForget
	state.secrets.dialogError = ""
}

func (state *State) clickVaultForget(id gfx.ID) {
	switch id {
	case hitVaultForgetConfirm:
		if state.secrets.vault != nil {
			if err := state.secrets.vault.Clear(); err != nil {
				state.status = fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
			} else {
				state.status = state.messages.VaultCleared
			}
			state.reloadStoredIdentities()
		}
		state.closeModal()
		state.showNextPrompt()
	case hitVaultForgetCancel, hitVaultForgetClose, hitVaultForgetDismiss:
		state.closeModal()
		state.showNextPrompt()
	}
}

func (state *State) paintVaultForgetDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	m := state.messages
	// The text is wrapped to the body's width: 540 less the dialog's padding.
	description := m.VaultForgetDescription
	if stored := state.storedKeyCount(); stored > 0 {
		description += " " + fmt.Sprintf(m.VaultForgetKeysFmt, stored)
	}
	var wrapped [5]string
	lines := canvas.WrapLines(wrapped[:0], description, metrics.FontSizeM, gfx.WeightRegular, 500, len(wrapped))
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitVaultForgetDismiss, CloseID: hitVaultForgetClose, InitialFocusID: hitVaultForgetCancel,
		ContentSize: gfx.Size{Width: 540}, FitBody: true, BodyHeight: float32(len(lines))*21 - 1,
	})
	layout := dialog.Begin(m.VaultForgetTitle, "")
	f := state.secrets.layout.Begin(canvas, state.Theme(), layout.Body)
	f.Paragraph(description, len(wrapped)).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(21)
	f.End()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitVaultForgetCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitVaultForgetConfirm, Label: m.VaultForgetConfirm, Variant: components.ButtonDestructive},
	)
	dialog.End()
}

// clickVaultSettings handles the buttons of the Saved passwords group. It
// reports whether the click was one of them.
func (state *State) clickVaultSettings(id gfx.ID) bool {
	vaultFile := state.secrets.vault
	switch id {
	case hitSettingsVaultForget:
		if vaultFile != nil {
			state.openVaultForget()
		}
	case hitSettingsVaultConvert:
		if vaultFile != nil && vaultFile.NeedsConversion() {
			state.secrets.declined = false
			state.openVaultUnlock(nil, nil)
		}
	default:
		return false
	}
	return true
}

// paintSecuritySettings is the Security category: the local session journal
// first, then the Saved passwords group when this computer has a vault. The
// journal group must paint even without a vault, so the two are not guarded
// together.
func (state *State) paintSecuritySettings(page *settingsPage) {
	m := state.messages
	page.groups(func() {
		state.paintCredentialSettings(page)
		state.paintSessionLogSettingsGroup(page)
		vaultFile := state.secrets.vault
		if vaultFile == nil {
			return
		}
		convert, count := vaultFile.NeedsConversion(), vaultFile.Len()
		description := m.SettingsVaultEmpty
		switch {
		case count > 0 && convert:
			description = fmt.Sprintf(m.SettingsVaultDescriptionLockedFmt, count)
		case count > 0:
			description = fmt.Sprintf(m.SettingsVaultDescriptionFmt, count)
		}
		actions := state.secrets.actions[:0]
		if convert {
			actions = append(actions, components.ButtonAction{ID: hitSettingsVaultConvert, Label: m.SettingsVaultConvert})
		}
		actions = append(actions, components.ButtonAction{ID: hitSettingsVaultForget, Label: m.SettingsVaultForgetAll, Variant: components.ButtonOutline})
		page.group(m.SettingsGroupSecurity, func() {
			page.actionRow(m.SettingsVault, description, count == 0, actions...)
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
func (state *State) paintSaveBox(canvas *gfx.Canvas, body gfx.Rect, id gfx.ID, label string) {
	state.paintSaveBoxAt(canvas, gfx.Rect{X: body.X, Y: body.Y + 99, Width: body.Width, Height: 20}, id, label, "", gfx.ID{})
}

// paintSaveBoxAt draws the box where a dialog laid it out. A hint becomes a
// help icon at its right, so a password error can use the line under the field
// without colliding with the instruction.
func (state *State) paintSaveBoxAt(canvas *gfx.Canvas, bounds gfx.Rect, id gfx.ID, label, hint string, hintID gfx.ID) {
	if !state.secrets.canSave || bounds == (gfx.Rect{}) {
		return
	}
	var help ui.Box
	f := state.secrets.layout.Begin(canvas, state.Theme(), bounds)
	f.Row().Fill().PadX(0).Gap(2).Items(ui.Center).Children(func() {
		// The checkbox slot is 40px wide with the 16px box centered in it. Pull
		// the slot left so the box starts where the password field border does.
		f.Checkbox(id, state.secrets.save).Label(label).MarginSides(0, 0, 0, -8)
		f.Text(label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(state.pal.text).LineHeight(20).MarginSides(0, 0, 0, -6)
		if hint != "" {
			help = f.Slot().Size(16, 16).MarginSides(0, 6, 0, 0).Shrink(0)
		}
		// Keep the help icon beside the label; the rest of the row stays empty
		// instead of pushing it to the far edge.
		f.Spacer().Grow(1)
	})
	f.End()
	if hint != "" && help.Rect().Width > 0 && help.Rect().Height > 0 {
		canvas.HitWithOptions(hintID, help.Rect(), hint, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true, Hint: hint})
		paintIcon(canvas, iconHelp, insetRect(help.Rect(), 0.08), state.pal.muted)
	}
}

// problemLine declares the one-line error under a dialog's fields, when
// there is one.
func (state *State) problemLine(f *ui.Frame, problem string) {
	if problem != "" {
		f.Text(problem).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(18)
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
	name := state.otpSecretName(session)
	value, found, err := vaultFile.Get(name)
	switch {
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
