package workbench

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"

	"motty/internal/otp"
)

// One-time code secrets are kept in the saved-passwords vault, one per account,
// as an otpauth:// address. The host dialog is where they are given and taken
// away; the connection (vault_ui.go) answers the server's question with them.

// otpName is where the one-time code secret of a host is kept: under the host,
// so that it follows the host wherever its address or login is edited.
func otpName(host Host) string { return hostSecretName(host.CatalogID, hostOTPPart) }

// prepareOTPState tells the host dialog whether the host being edited has a
// secret saved. The secret itself is never read into the form.
func (state *State) prepareOTPState(host Host) {
	editor, vaultFile := &state.editor, state.secrets.vault
	editor.otpSaved, editor.otpRemove = false, false
	if vaultFile == nil || vaultFile.Len() == 0 || vaultFile.NeedsConversion() || !host.isSSH() {
		return
	}
	_, editor.otpSaved, _ = vaultFile.Get(otpName(host))
}

// otpPrecheck says why the secret cannot be kept, before anything is saved, so a
// host is not stored with its secret silently dropped.
func (state *State) otpPrecheck() string {
	editor := &state.editor
	if editor.protocol != protocolSSH || (editor.value(fieldOTP) == "" && !editor.otpRemove) {
		return ""
	}
	vaultFile := state.secrets.vault
	switch {
	case vaultFile == nil:
		return state.messages.Tools.ErrOTPNoVault
	case vaultFile.NeedsConversion():
		return state.messages.Tools.ErrOTPLocked
	case vaultFile.Prepare() != nil:
		return state.messages.Tools.ErrOTPNoVault
	}
	return ""
}

// commitOTP applies the form's secret to the vault once the host is saved:
// store a new one, or remove the old one.
func (state *State) commitOTP(host Host) {
	editor := &state.editor
	vaultFile := state.secrets.vault
	if vaultFile == nil || host.Protocol != protocolSSH {
		return
	}
	name := otpName(host)
	label := state.login(host) + "@" + host.Address
	switch typed := editor.value(fieldOTP); {
	case typed != "":
		secret, err := otp.Parse(typed)
		if err != nil {
			return
		}
		if err := vaultFile.Set(name, secret.String()); err != nil {
			state.status = fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
			return
		}
		state.status = fmt.Sprintf(state.messages.Tools.OTPSavedFmt, label)
	case editor.otpRemove:
		_ = vaultFile.Delete(name)
		state.status = fmt.Sprintf(state.messages.Tools.OTPRemovedFmt, label)
	}
}

// scanOTPImage asks for an image of a QR code and puts the secret in it into the
// form's field, where it is shown, checked and saved like a pasted address. The
// image is read once; neither it nor the text in it is kept or logged.
func (state *State) scanOTPImage() {
	m := state.messages.Tools
	paths, err := state.options.OpenFiles(app.OpenFileOptions{
		Title:   m.OTPScanTitle,
		Filters: []app.FileFilter{{Name: m.OTPScanFilter, Pattern: "*.png;*.jpg;*.jpeg;*.gif"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		state.dialogError = fmt.Sprintf(m.ErrOTPScanReadFmt, err)
		return
	}
	secrets, err := otp.FromImageFile(paths[0])
	state.takeScannedSecrets(secrets, err)
}

// pasteOTPImage reads the QR code from an image on the clipboard, as scanOTPImage
// does from a file. The clipboard is read when the button is pressed and not
// before: asking another program whether it holds an image can take a while, and a
// picture copied after the dialog opened must still be found.
func (state *State) pasteOTPImage() {
	m := state.messages.Tools
	data, err := state.options.ClipboardImagePNG()
	switch {
	case errors.Is(err, app.ErrNoImage):
		state.dialogError = m.ErrOTPPasteNoImage
		return
	case errors.Is(err, app.ErrTooLarge):
		state.dialogError = m.ErrOTPScanTooLarge
		return
	case err != nil:
		state.dialogError = fmt.Sprintf(m.ErrOTPScanReadFmt, err)
		return
	}
	secrets, err := otp.FromImage(data)
	state.takeScannedSecrets(secrets, err)
}

// takeScannedSecrets puts what a QR code carried into the form's field, or says
// why it could not.
func (state *State) takeScannedSecrets(secrets []otp.Secret, err error) {
	m := state.messages.Tools
	if err != nil {
		state.dialogError = state.otpScanErrorText(err)
		return
	}
	secret, ok := state.pickScannedSecret(secrets)
	if !ok {
		state.dialogError = fmt.Sprintf(m.ErrOTPScanManyFmt, len(secrets))
		return
	}
	state.editor.otp = text.Editor{}
	state.editor.otp.Insert(secret.String())
	state.editor.otpRemove = false
	state.dialogError = ""
	state.status = fmt.Sprintf(m.OTPScannedFmt, scannedLabel(secret))
}

// pickScannedSecret chooses the secret for this host. An export QR code holds
// every account of the authenticator, so with several the one named like the
// host's user is taken, and when none is, the choice is left to the person.
func (state *State) pickScannedSecret(secrets []otp.Secret) (otp.Secret, bool) {
	if len(secrets) == 1 {
		return secrets[0], true
	}
	user := strings.ToLower(state.editor.value(fieldUser))
	address := strings.ToLower(state.editor.value(fieldAddress))
	var found []otp.Secret
	for _, secret := range secrets {
		account := strings.ToLower(secret.Account)
		local, _, _ := strings.Cut(account, "@")
		if user != "" && (account == user || local == user) || address != "" && strings.Contains(account, address) {
			found = append(found, secret)
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return otp.Secret{}, false
}

func scannedLabel(secret otp.Secret) string {
	switch {
	case secret.Issuer != "" && secret.Account != "":
		return secret.Issuer + " · " + secret.Account
	case secret.Issuer != "":
		return secret.Issuer
	}
	return secret.Account
}

func (state *State) otpScanErrorText(err error) string {
	m := state.messages.Tools
	switch {
	case errors.Is(err, otp.ErrImageTooLarge):
		return m.ErrOTPScanTooLarge
	case errors.Is(err, otp.ErrImageDimension):
		return m.ErrOTPScanDimension
	case errors.Is(err, otp.ErrNotImage):
		return m.ErrOTPScanNotImage
	case errors.Is(err, otp.ErrNoQRCode):
		return m.ErrOTPScanNoCode
	case errors.Is(err, otp.ErrNotOTPQRCode):
		return m.ErrOTPScanNotOTP
	case errors.Is(err, otp.ErrMigration):
		return m.ErrOTPScanMigration
	case errors.Is(err, os.ErrNotExist), errors.Is(err, os.ErrPermission):
		return fmt.Sprintf(m.ErrOTPScanReadFmt, err)
	case errors.Is(err, otp.ErrSecret), errors.Is(err, otp.ErrScheme), errors.Is(err, otp.ErrParameter), errors.Is(err, otp.ErrNoSecretKey), errors.Is(err, otp.ErrEmpty):
		return m.ErrOTPInvalid
	}
	return fmt.Sprintf(m.ErrOTPScanReadFmt, err)
}
