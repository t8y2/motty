package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sessionimport"
)

// Importing Tabby's configuration. Its sessions live in a YAML file, and what it
// saved beside them — passwords, key passphrases, and the private keys the user
// chose to keep there — live in a "vault": one blob encrypted with a passphrase
// the user types. So this import is the only one that asks for something: the
// passphrase, once, and only when the file actually has a vault.
//
// Everything else goes through the same plan as the other clients (see
// session_import.go); this file is the picking, the passphrase and the keys.

// tabbyImportHits is the place of the controls this file paints.
var tabbyImportHits = tree.Feature("tabby-import")

var (
	hitTabbyPassphraseField   = tree.ID(tabbyImportHits, "tabby-passphrase")
	hitTabbyPassphraseSubmit  = tree.ID(tabbyImportHits, "tabby-passphrase-submit")
	hitTabbyPassphraseCancel  = tree.ID(tabbyImportHits, "tabby-passphrase-cancel")
	hitTabbyPassphraseClose   = tree.ID(tabbyImportHits, "tabby-passphrase-close")
	hitTabbyPassphraseDismiss = tree.ID(tabbyImportHits, "tabby-passphrase-dismiss")
)

// tabbyImportUI is a Tabby import in progress: the files it read and has yet to
// add, and the dialog that asks for the passphrase of their vault.
type tabbyImportUI struct {
	files []*sessionimport.TabbyFile
	// failures are the files that could not be read at all, kept so they can be
	// told beside what the others brought.
	failures    []string
	passphrase  text.Editor
	dialogError string
	// layout lays out the body of the passphrase dialog, so painting it keeps no
	// frame of its own.
	layout flex.View
}

func (imported *tabbyImportUI) reset() { *imported = tabbyImportUI{} }

// needsPassphrase reports whether any file keeps its sessions or its secrets in
// a vault that has not been opened yet.
func (imported *tabbyImportUI) needsPassphrase() bool {
	for _, file := range imported.files {
		if file.NeedsPassphrase() {
			return true
		}
	}
	return false
}

// importTabby asks for Tabby's configuration files and adds what they hold.
func (state *State) importTabby() {
	m := state.messages.Tools
	state.modal = modalNone
	title := fmt.Sprintf(m.ImportPickerFmt, importClientName(sessionimport.Tabby))
	paths, err := state.options.OpenFiles(app.OpenFileOptions{Title: title, AllowMultiple: true})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		state.status = m.ImportCanceled
		return
	}
	if err != nil {
		state.showActionFailure(title, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	imported := &state.tabby
	imported.reset()
	for _, path := range paths {
		file, err := sessionimport.ParseTabby(path)
		if err != nil {
			imported.failures = append(imported.failures, fmt.Sprintf("%s: %v", filepath.Base(path), err))
			continue
		}
		imported.files = append(imported.files, file)
	}
	if len(imported.files) == 0 {
		failure := errors.New(strings.Join(imported.failures, "; "))
		imported.reset()
		state.showFileReadFailure(title, failure, fmt.Sprintf(state.messages.ActionFeedback.ImportSessionsFailedFmt, importClientName(sessionimport.Tabby)))
		return
	}
	if imported.needsPassphrase() {
		imported.dialogError = ""
		imported.passphrase = text.Editor{}
		state.modal = modalTabbyPassphrase
		return
	}
	state.applyTabbyImport()
}

func (state *State) clickTabbyPassphrase(id ui.ID) {
	switch id {
	case hitTabbyPassphraseSubmit:
		state.submitTabbyPassphrase()
	case hitTabbyPassphraseCancel, hitTabbyPassphraseClose, hitTabbyPassphraseDismiss:
		// What can be read without the passphrase is still worth adding, so the
		// import goes on and says what it could not read.
		state.closeModal()
		state.applyTabbyImport()
	}
}

// submitTabbyPassphrase opens the vault of every file the passphrase fits. One
// that opens none is wrong as far as the user is concerned, so the dialog stays
// and asks again; one that opens some is taken, and the rest are told.
func (state *State) submitTabbyPassphrase() {
	imported := &state.tabby
	passphrase := imported.passphrase.String()
	if passphrase == "" {
		imported.dialogError = state.messages.Tools.ImportTabbyPassphraseEmpty
		return
	}
	opened := 0
	for _, file := range imported.files {
		if !file.NeedsPassphrase() {
			continue
		}
		if err := file.Unlock(passphrase); err != nil {
			continue
		}
		opened++
	}
	if opened == 0 {
		imported.dialogError = state.messages.Tools.ImportTabbyPassphraseWrong
		imported.passphrase = text.Editor{}
		return
	}
	imported.dialogError = ""
	state.closeModal()
	state.applyTabbyImport()
}

// applyTabbyImport adds everything the files gave: the hosts, their routes and
// tunnels, the keys Tabby kept in its vault and the secrets the user unlocked.
func (state *State) applyTabbyImport() {
	m := state.messages.Tools
	imported := &state.tabby
	defer imported.reset()

	results := make([]sessionimport.Result, 0, len(imported.files))
	warnings := append([]string(nil), imported.failures...)
	for _, file := range imported.files {
		results = append(results, file.Result())
		if file.NeedsPassphrase() {
			warnings = append(warnings, fmt.Sprintf(m.ImportTabbyLockedFmt, filepath.Base(file.Path())))
		}
	}
	plan := state.sessionImportPlan(results...)
	for _, result := range results {
		if result.Skipped > 0 {
			plan.warnings = append(plan.warnings, fmt.Sprintf(m.ImportWarningSkippedFmt, result.Skipped))
		}
		if result.PlainSecrets > 0 {
			plan.warnings = append(plan.warnings, fmt.Sprintf(m.ImportWarningCleartextFmt, result.PlainSecrets))
		}
		if sessionImportHasLoopback(result) {
			plan.warnings = append(plan.warnings, m.ImportWarningLoopback)
		}
		plan.warnings = append(plan.warnings, result.Warnings...)
	}
	plan.warnings = append(plan.warnings, warnings...)
	// Tabby's keys and passwords are imported rather than reported, so the two
	// lines that say the opposite for the other clients are not added here.
	if len(plan.hosts)+len(plan.identities)+len(plan.proxies)+len(plan.forwards) == 0 {
		state.status = m.ImportTabbyNothing
		state.hostImportResult.set(state.messages, m.ImportTabbyNothing, plan.warnings)
		state.modal = modalHostImportResult
		return
	}
	summary := fmt.Sprintf(m.ImportSummaryFmt, len(plan.hosts), importClientName(sessionimport.Tabby), plan.duplicateHosts)
	state.applyHostImportSummary(plan, summary)
}

func (state *State) paintTabbyPassphraseDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitTabbyPassphraseDismiss, CloseID: hitTabbyPassphraseClose, InitialFocusID: hitTabbyPassphraseField,
		ContentSize: ui.Size{Width: 480}, FitBody: true, BodyHeight: withError(66, state.tabby.dialogError),
	})
	layout := dialog.Begin(m.Tools.ImportTabbyPassphraseTitle, m.Tools.ImportTabbyPassphraseBody)
	var field flex.Box
	f := state.tabby.layout.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Gap(12).Children(func() {
		field = f.Slot().Height(62)
		state.problemLine(f, state.tabby.dialogError)
	})
	f.End()
	content := components.BeginField(canvas, theme, field.Rect()).Paint(m.Tools.ImportTabbyPassphraseLabel, "", "")
	value := state.tabby.passphrase.String()
	focused := canvas.Focused() == hitTabbyPassphraseField
	state.paintSecretInput(canvas, content, components.InputSnapshot{
		ID: hitTabbyPassphraseField, Label: m.Tools.ImportTabbyPassphraseLabel, Value: value, Secure: true,
		Focused: focused, PlatformEditing: focused && value != "", Invalid: state.tabby.dialogError != "",
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: hitTabbyPassphraseCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitTabbyPassphraseSubmit, Label: m.Tools.ImportTabbyPassphraseSubmit},
	)
	dialog.End()
}

// writeImportedKey writes a private key a client kept inside its own vault
// beside the state file, so it can be used like any other key, and answers its
// path. The name is the client's own id for the key, so importing the same
// configuration twice lands on the same file and the key is not added twice.
func (state *State) writeImportedKey(id string, data []byte) (string, error) {
	directory := filepath.Join(filepath.Dir(state.options.StatePath), "keys")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, importedKeyFileName(id, data))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// importedKeyFileName is the name a key taken from another client's vault gets:
// its id, cut to what a file name can hold, or a digest of the key itself when
// the client named none.
func importedKeyFileName(id string, data []byte) string {
	var name strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			name.WriteRune(r)
		case r == '.', r == '/', r == ' ', r == '\\':
			name.WriteByte('-')
		}
		if name.Len() >= 64 {
			break
		}
	}
	if name.Len() == 0 {
		sum := sha256.Sum256(data)
		return "tabby-" + hex.EncodeToString(sum[:8]) + ".key"
	}
	return "tabby-" + name.String() + ".key"
}
