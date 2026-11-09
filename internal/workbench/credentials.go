package workbench

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// Shared passwords. One password kept once and used by several hosts: a host
// names it (Host.Credential) instead of keeping a password of its own, so
// changing it changes every host that uses it. The password lives in the vault
// under the credential's id, never in the settings file or a record that syncs
// in the clear; the record holds the id and the name the user gave it.
//
// A shared password is the password alone. The login stays on the host, because
// the host's login also names its forwards, jump routes and SSH-config entries.

const (
	maxCredentialName = 80
	maxCredentials    = 256
)

// credential is one shared password. Its secret is in the vault.
type credential struct {
	ID   string
	Name string
}

// credentialSecretName is where a shared password is kept in the vault. The id is
// random, so the name is the same on every computer the vault syncs to.
func credentialSecretName(id string) string { return "credential:" + id }

func freshCredentialID(taken func(string) bool) string {
	for {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			panic("workbench: no randomness for a credential id: " + err.Error())
		}
		if id := "cred_" + hex.EncodeToString(random[:]); !taken(id) {
			return id
		}
	}
}

func (catalog *hostCatalog) credentialByID(id string) (credential, bool) {
	if id == "" {
		return credential{}, false
	}
	index := slices.IndexFunc(catalog.credentials, func(entry credential) bool { return entry.ID == id })
	if index < 0 {
		return credential{}, false
	}
	return catalog.credentials[index], true
}

// credentialUses is how many hosts take their password from a shared one.
func (catalog *hostCatalog) credentialUses(id string) int {
	count := 0
	for _, host := range catalog.hosts {
		if host.Credential == id {
			count++
		}
	}
	return count
}

// usesCredential says the host's password is a shared one that still exists. A
// host that names one that is gone falls back to a password of its own.
func (catalog *hostCatalog) usesCredential(host Host) bool {
	_, ok := catalog.credentialByID(host.Credential)
	return ok
}

// passwordSecret is the vault name of the password that answers the password
// prompt of a connection: the shared password of the host the prompt is about
// when it uses one, and otherwise the host's own (see secretOwner).
func (catalog *hostCatalog) passwordSecret(target Host, endpoint sshconn.Endpoint, role sshconn.EndpointRole) string {
	owner := target
	if role != sshconn.EndpointTarget {
		owner, _ = catalog.hostByConnection(endpoint.Host, endpoint.User, endpoint.Port)
	}
	if catalog.usesCredential(owner) {
		return credentialSecretName(owner.Credential)
	}
	return hostSecretName(catalog.secretOwner(target, endpoint, role), passwordPart(target, endpoint))
}

// passwordShared reports whether the password a prompt would be answered with
// belongs to a shared password rather than to the host alone.
func (state *State) passwordShared(session *Session) bool {
	return strings.HasPrefix(state.passwordName(session), credentialSecretName(""))
}

// ---------------------------------------------------------------------------
// The state file

type savedCredential struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (state *State) savedCredentials() []savedCredential {
	var saved []savedCredential
	for _, entry := range state.credentials {
		saved = append(saved, savedCredential{ID: entry.ID, Name: entry.Name})
	}
	return saved
}

// decodeCredentials turns saved records into shared passwords. A record without
// an id or a name, or whose id repeats, is dropped.
func decodeCredentials(saved []savedCredential) []credential {
	var credentials []credential
	for _, entry := range saved {
		name := cleanSnippetText(entry.Name, maxCredentialName)
		if entry.ID == "" || name == "" || len(credentials) >= maxCredentials ||
			slices.ContainsFunc(credentials, func(have credential) bool { return have.ID == entry.ID }) {
			continue
		}
		credentials = append(credentials, credential{ID: entry.ID, Name: name})
	}
	return credentials
}

func (state *State) restoreCredentials(saved savedState) {
	state.credentials = decodeCredentials(saved.Credentials)
}

// ---------------------------------------------------------------------------
// The dialog

var credentialHits = tree.Feature("credentials")

var (
	hitSettingsCredentialAdd = tree.ID(credentialHits, "settings-credential-add")
	hitCredentialName        = tree.ID(credentialHits, "credential-name")
	hitCredentialPassword    = tree.ID(credentialHits, "credential-password")
	hitCredentialSubmit      = tree.ID(credentialHits, "credential-submit")
	hitCredentialCancel      = tree.ID(credentialHits, "credential-cancel")
	hitCredentialClose       = tree.ID(credentialHits, "credential-close")
)

var (
	hitCredentialEditList   = tree.Sub(credentialHits, tree.Name("credential-edit"))   // + credential index
	hitCredentialDeleteList = tree.Sub(credentialHits, tree.Name("credential-delete")) // + credential index
)

// credentialUI is the dialog that adds or edits a shared password.
type credentialUI struct {
	// editing is the id of the password being edited; empty adds a new one.
	editing        string
	name, password text.Editor
	dialogError    string
}

func (state *State) openCredentialDialog(id string) {
	form := &state.credentialForm
	*form = credentialUI{editing: id}
	if entry, ok := state.credentialByID(id); ok {
		form.name.Insert(entry.Name)
		// The dialog shows what is saved, as a host's does; a vault that cannot
		// answer leaves the field empty, which saving reads as "keep".
		if vaultFile := state.secrets.vault; vaultFile != nil && vaultFile.Len() > 0 {
			if value, found, err := vaultFile.Get(credentialSecretName(id)); err == nil && found {
				form.password.Insert(value)
			}
		}
	}
	state.modal = modalCredential
}

func (state *State) clickCredentialDialog(id gfx.ID) {
	switch id {
	case hitCredentialSubmit:
		state.submitCredential()
	case hitCredentialCancel, hitCredentialClose:
		state.closeModal()
	}
}

func (state *State) submitCredential() {
	form, m := &state.credentialForm, state.messages.Credentials
	fail := func(message string) { form.dialogError = message }
	name := cleanSnippetText(form.name.String(), maxCredentialName)
	switch {
	case name == "":
		fail(m.ErrorName)
		return
	case slices.ContainsFunc(state.credentials, func(entry credential) bool {
		return entry.ID != form.editing && strings.EqualFold(entry.Name, name)
	}):
		fail(m.ErrorNameTaken)
		return
	case form.editing == "" && len(state.credentials) >= maxCredentials:
		fail(m.ErrorTooMany)
		return
	}
	value := form.password.String()
	id := form.editing
	if id == "" {
		id = freshCredentialID(func(candidate string) bool {
			_, taken := state.credentialByID(candidate)
			return taken
		})
	}
	vaultFile := state.secrets.vault
	if value == "" {
		// An edit that left the field empty keeps the password it has, if it has one.
		kept := false
		if form.editing != "" && vaultFile != nil && vaultFile.Len() > 0 {
			_, found, err := vaultFile.Get(credentialSecretName(id))
			kept = err == nil && found
		}
		if !kept {
			fail(m.ErrorPassword)
			return
		}
	} else {
		if vaultFile == nil {
			fail(fmt.Sprintf(m.ErrorVaultFmt, state.messages.VaultUnavailable))
			return
		}
		if err := vaultFile.Prepare(); err != nil {
			fail(fmt.Sprintf(m.ErrorVaultFmt, state.vaultErrorText(err)))
			return
		}
		if err := vaultFile.Set(credentialSecretName(id), value); err != nil {
			fail(fmt.Sprintf(m.ErrorVaultFmt, state.vaultErrorText(err)))
			return
		}
	}
	if index := slices.IndexFunc(state.credentials, func(entry credential) bool { return entry.ID == id }); index >= 0 {
		state.credentials[index].Name = name
	} else {
		state.credentials = append(state.credentials, credential{ID: id, Name: name})
	}
	form.password = text.Editor{}
	state.closeModal()
	state.save()
}

// deleteCredential removes a shared password and its secret. The hosts that used
// it keep no password of their own, so they ask for one at the next connection.
func (state *State) deleteCredential(id string) {
	index := slices.IndexFunc(state.credentials, func(entry credential) bool { return entry.ID == id })
	if index < 0 {
		return
	}
	if state.secrets.vault != nil {
		if err := state.secrets.vault.Delete(credentialSecretName(id)); err != nil {
			state.status = state.hostPasswordSaveError(err)
			return
		}
	}
	state.credentials = slices.Delete(state.credentials, index, index+1)
	for hostIndex := range state.hosts {
		if previous := state.hosts[hostIndex]; previous.Credential == id {
			updated := previous
			updated.Credential = ""
			state.replaceHost(hostIndex, previous, updated)
		}
	}
	state.hostDataChanged()
	state.save()
}

func (state *State) paintCredentialDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	m, pal, theme := state.messages.Credentials, state.pal, state.Theme()
	form := &state.credentialForm
	title := m.DialogAddTitle
	if form.editing != "" {
		title = m.DialogEditTitle
	}
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		CloseID: hitCredentialClose, InitialFocusID: hitCredentialName,
		ContentSize: gfx.Size{Width: 520}, FitBody: true, BodyHeight: withError(float32(68*2+8), form.dialogError),
	})
	layout := dialog.Begin(title, "")
	var name, password ui.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		name = f.Slot().Height(62).MarginSides(0, 0, 6, 0)
		password = f.Slot().Height(62).MarginSides(0, 0, 6, 0)
		if form.dialogError != "" {
			f.Text(form.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.danger).LineHeight(18).MarginSides(15, 0, 0, 0)
		}
	})
	f.End()
	field := func(box ui.Box, id gfx.ID, label, placeholder, value string, secret bool) {
		content := components.BeginField(canvas, theme, box.Rect()).Paint(label, "", "")
		focused := canvas.Focused() == id
		state.paintSecretInput(canvas, content, components.InputSnapshot{
			ID: id, Label: label, Value: value, Placeholder: placeholder, Secure: secret,
			Focused: focused, PlatformEditing: focused && value != "", Invalid: form.dialogError != "",
		})
	}
	field(name, hitCredentialName, m.Name, m.NamePlaceholder, form.name.String(), false)
	placeholder := ""
	if form.editing != "" {
		placeholder = m.PasswordKeep
	}
	field(password, hitCredentialPassword, m.Password, placeholder, form.password.String(), true)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitCredentialCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitCredentialSubmit, Label: m.Save},
	)
	dialog.End()
}

// paintCredentialSettings is the Shared passwords group of Settings.
func (state *State) paintCredentialSettings(page *settingsPage) {
	m := state.messages.Credentials
	page.group(m.Group, func() {
		for index, entry := range state.credentials {
			uses := m.UsedByNone
			if count := state.credentialUses(entry.ID); count > 0 {
				uses = fmt.Sprintf(m.UsedByFmt, count)
			}
			page.actionRow(entry.Name, uses, false,
				components.ButtonAction{ID: hitCredentialEditList.At(index), Label: m.Edit, Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitCredentialDeleteList.At(index), Label: m.Delete, Variant: components.ButtonOutline})
		}
		page.actionRow(m.Row, m.RowDescription, len(state.credentials) >= maxCredentials,
			components.ButtonAction{ID: hitSettingsCredentialAdd, Label: m.Add, Variant: components.ButtonOutline})
	})
}

// clickCredentialSettings handles the Settings group's buttons.
func (state *State) clickCredentialSettings(id gfx.ID) bool {
	switch {
	case id == hitSettingsCredentialAdd:
		state.openCredentialDialog("")
	case id.Scope() == hitCredentialEditList:
		if index := item(id); index >= 0 && index < len(state.credentials) {
			state.openCredentialDialog(state.credentials[index].ID)
		}
	case id.Scope() == hitCredentialDeleteList:
		state.askDeleteCredential(item(id))
	default:
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// The host form's choice

var credentialChoiceHits = tree.Feature("host-credential")

var (
	hitDialogCredential         = tree.ID(credentialChoiceHits, "dialog-credential")
	hitDialogCredentialDismiss  = tree.ID(credentialChoiceHits, "dialog-credential-dismiss")
	hitDialogCredentialOwn      = tree.ID(credentialChoiceHits, "dialog-credential-own")
	hitDialogCredentialValue    = tree.ID(credentialChoiceHits, "dialog-credential-value")
	hitDialogCredentialItemList = tree.Sub(credentialChoiceHits, tree.Name("dialog-credential-item")) // + credential index
)

// hostOffersCredential says the form has a choice of password source: the host
// signs in with a password, and there is a saved one to choose.
func (state *State) hostOffersCredential() bool {
	editor := &state.editor
	if len(state.credentials) == 0 {
		return false
	}
	switch editor.protocol {
	case protocolSSH:
		return editor.auth != authPrivateKey && editor.auth != authAgent
	case protocolVNC, protocolRDP:
		return true
	}
	return false
}

// hostTakesCredential says the form's host uses a shared password, so the form
// has no password field of its own.
func (state *State) hostTakesCredential() bool {
	if !state.hostOffersCredential() {
		return false
	}
	_, ok := state.credentialByID(state.editor.credential)
	return ok
}

// paintHostPasswordWithCredential is the password field of a form that has
// shared passwords to choose from. The key button at its start opens the list.
// With a shared password chosen the field is a read-only line naming it, and a
// button at its end goes back to this host's own.
func (state *State) paintHostPasswordWithCredential(canvas *gfx.Canvas, content gfx.Rect, title, placeholder string) {
	editor, theme, m := &state.editor, state.Theme(), state.messages.Credentials
	state.revealHostField(canvas, hitDialogPassword, content)
	editor.credentialAnchor = content
	key := components.InputGroupSlot{
		Kind: components.InputGroupSlotButton, ID: hitDialogCredential, Icon: lucideIcons[iconKey], Label: m.FieldSource,
		ButtonSize: components.InputGroupButtonIconXS,
	}
	entry, taken := state.credentialByID(editor.credential)
	if !state.hostTakesCredential() {
		focused := canvas.Focused() == hitDialogPassword
		value := editor.password.String()
		state.paintSecretInputStart(canvas, content, components.InputSnapshot{
			ID: hitDialogPassword, Label: title, Value: value, Placeholder: placeholder, Secure: true,
			Focused: focused, PlatformEditing: focused && value != "",
		}, key)
		return
	}
	_ = taken
	components.PaintInputGroup(canvas, theme, content, components.InputGroupSnapshot{
		ID: hitDialogCredentialValue, Label: title, Value: entry.Name, ReadOnly: true,
		InlineStart: key,
		InlineEnd: components.InputGroupSlot{
			Kind: components.InputGroupSlotButton, ID: hitDialogCredentialOwn, Icon: lucideIcons[iconClose], Label: m.SourceOwn,
			ButtonSize: components.InputGroupButtonIconXS,
		},
	})
}

// paintHostCredentialMenu is the list of shared passwords under the field.
func (state *State) paintHostCredentialMenu(canvas *gfx.Canvas) {
	editor, m := &state.editor, state.messages.Credentials
	if !editor.credentialOpen || !state.hostOffersCredential() {
		return
	}
	portal := state.beginHostFormPopup(canvas, true)
	defer state.endHostFormPopup(canvas, portal)
	list := state.hostAuthListBounds(editor.credentialAnchor, len(state.credentials)+1)
	state.menuPlaced(list)
	menu := components.BeginDropdownMenu(canvas, state.Theme(), list, components.DropdownMenuOptions{
		Layer: dialogLayer + 1, Scope: dialogScope, DismissID: hitDialogCredentialDismiss, Viewport: state.windowRect(),
		Scroll: state.menuScrollFor(hitDialogCredentialDismiss),
	})
	defer menu.End()
	menu.AddItem(components.DropdownMenuItem{
		ID: hitDialogCredentialOwn, Label: m.SourceOwn, Kind: components.DropdownMenuRadio, Checked: !state.hostTakesCredential(), Selected: !state.hostTakesCredential(),
	})
	for index, entry := range state.credentials {
		menu.AddItem(components.DropdownMenuItem{
			ID: hitDialogCredentialItemList.At(index), Label: entry.Name, Kind: components.DropdownMenuRadio, Checked: entry.ID == editor.credential, Selected: entry.ID == editor.credential,
		})
	}
}

func (state *State) clickHostCredential(id gfx.ID) bool {
	editor := &state.editor
	switch {
	case id == hitDialogCredential, id == hitDialogCredentialValue:
		editor.credentialOpen = !editor.credentialOpen
		editor.authOpen, editor.identityOpen = false, false
	case id == hitDialogCredentialDismiss:
		editor.credentialOpen = false
	case id == hitDialogCredentialOwn:
		editor.credential, editor.credentialOpen = "", false
	case id.Scope() == hitDialogCredentialItemList:
		index := item(id)
		if index < 0 || index >= len(state.credentials) {
			return false
		}
		editor.credential, editor.credentialOpen = state.credentials[index].ID, false
	default:
		return false
	}
	state.dialogError = ""
	return true
}
