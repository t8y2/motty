package workbench

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"motty/internal/sshconn"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// Identities. One login name and password kept once and used by several hosts: a
// host names one (Host.Credential) instead of keeping a password of its own, so
// changing it changes every host that uses it. The password lives in the vault
// under the credential's id, never in the settings file or a record that syncs
// in the clear; the record holds the id, the name the user gave it and the login
// name. A host's own login name wins over the identity's; a host with neither
// signs in as the system user. The login name is resolved in one place
// (hostCatalog.login), so forwards, jump routes and SSH-config entries follow it.

const (
	maxCredentialName = 80
	maxCredentialUser = 128
	maxCredentials    = 256
)

// credential is one identity: a shared password and, optionally, the login name
// that goes with it. Its secret is in the vault. User is empty when the identity
// only carries the password; a host then keeps its own login name.
type credential struct {
	ID   string
	Name string
	User string
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
	// User is optional: records saved before identities carried a login name
	// have none.
	User string `json:"user,omitempty"`
}

func (state *State) savedCredentials() []savedCredential {
	var saved []savedCredential
	for _, entry := range state.credentials {
		saved = append(saved, savedCredential{ID: entry.ID, Name: entry.Name, User: entry.User})
	}
	return saved
}

// decodeCredentials turns saved records into identities. A record without an id
// or a name, or whose id repeats, is dropped; an unusable login name is dropped
// alone, which leaves the identity a password-only one.
func decodeCredentials(saved []savedCredential) []credential {
	var credentials []credential
	for _, entry := range saved {
		name := cleanSnippetText(entry.Name, maxCredentialName)
		if entry.ID == "" || name == "" || len(credentials) >= maxCredentials ||
			slices.ContainsFunc(credentials, func(have credential) bool { return have.ID == entry.ID }) {
			continue
		}
		credentials = append(credentials, credential{ID: entry.ID, Name: name, User: cleanSnippetText(entry.User, maxCredentialUser)})
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
	hitCredentialUser        = tree.ID(credentialHits, "credential-user")
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
	editing              string
	name, user, password text.Editor
	dialogError          string
	dialogErrorSelection ui.TextSelectionState // what the user selected of dialogError, to copy it
	// passwordShown swaps the password's bullets for its text, as the field's
	// eye button does; closing the dialog forgets it.
	passwordShown bool
	// nameField and passwordField are the page's handles on the same editors, so
	// the platform edits and the dialog reads one text.
	nameField, userField, passwordField ui.Field
}

func (state *State) openCredentialDialog(id string) {
	form := &state.credentialForm
	*form = credentialUI{editing: id}
	// The handles point at the form's own editors, which live as long as it does.
	form.nameField.Bind(&form.name)
	form.userField.Bind(&form.user)
	form.passwordField.Bind(&form.password)
	if entry, ok := state.credentialByID(id); ok {
		form.name.Insert(entry.Name)
		form.user.Insert(entry.User)
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
	user := cleanSnippetText(form.user.String(), maxCredentialUser)
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
		state.credentials[index].User = user
	} else {
		state.credentials = append(state.credentials, credential{ID: id, Name: name, User: user})
	}
	form.password = text.Editor{}
	returning := state.credentialReturn
	state.closeModal()
	if returning == modalNewHost {
		state.editor.credential = id
	}
	state.save()
}

// deleteCredential removes an identity and its secret. The hosts that used it
// keep no password of their own, so they ask for one at the next connection. They
// keep its login name too, as their own, so they do not silently sign in as the
// system user instead.
func (state *State) deleteCredential(id string) {
	index := slices.IndexFunc(state.credentials, func(entry credential) bool { return entry.ID == id })
	if index < 0 {
		return
	}
	gone := state.credentials[index]
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
			if updated.User == "" {
				updated.User = gone.User
			}
			state.replaceHost(hostIndex, previous, updated)
		}
	}
	state.hostDataChanged()
	state.save()
}

// declareCredentialDialog declares the shared-password dialog in the window
// page. The page names its two fields and their buttons, so typing, focus and
// the initial focus are the page's; the dialog keeps no body page of its own.
func (state *State) declareCredentialDialog(c ui.C) {
	if state.modal != modalCredential {
		return
	}
	form, m := &state.credentialForm, state.messages.Credentials
	title := m.DialogAddTitle
	if form.editing != "" {
		title = m.DialogEditTitle
	}
	const width float32 = 520
	bodyHeight := withError(float32(68*3+8), form.dialogError)
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: width, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitCredentialClose, InitialFocusID: hitCredentialName,
	})
	dialog.Body(func() {
		ui.Column(c).Height(bodyHeight).PadSides(2, 4, 0, 4).Children(func() {
			state.credentialField(c, hitCredentialName, &form.nameField, m.Name, m.NamePlaceholder, 62, false)
			state.credentialField(c, hitCredentialUser, &form.userField, m.User, m.UserPlaceholder, 62, false)
			placeholder := ""
			if form.editing != "" {
				placeholder = m.PasswordKeep
			}
			state.credentialField(c, hitCredentialPassword, &form.passwordField, m.Password, placeholder, 62, true)
			if form.dialogError != "" {
				ui.TextSelectionArea(c, &form.dialogErrorSelection).Children(func() {
					ui.Text(c, form.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).
						Color(state.pal.danger).LineHeight(18).MarginSides(15, 0, 0, 0)
				})
			}
		})
	})
	dialog.Footer(func() {
		// The page owns its buttons; the window's dispatch runs after the paint,
		// as it did when the dialog was painted by hand.
		if ui.Button(c, state.messages.Cancel).Secondary().Shrink(0).Clicked() {
			c.After(func() { state.clickCredentialDialog(hitCredentialCancel) })
		}
		if ui.Button(c, m.Save).Shrink(0).Clicked() {
			c.After(func() { state.clickCredentialDialog(hitCredentialSubmit) })
		}
	})
	if dialog.Dismissed() {
		state.closeModal()
	}
}

// credentialField declares one labelled field of the shared-password dialog in
// the same row rhythm components.BeginField gives it. A secret field gets the
// eye button that swaps bullets for text.
func (state *State) credentialField(c ui.C, id gfx.ID, field *ui.Field, title, placeholder string, height float32, secret bool) {
	theme := c.Theme()
	m := theme.Metrics
	gap := m.GapItem / 2
	form := &state.credentialForm
	field.Secure = secret && !form.passwordShown
	ui.Column(c).Height(height).MarginSides(0, 0, 6, 0).Children(func() {
		ui.Text(c, title).FontSize(m.FontSizeM).Weight(gfx.WeightMedium).Color(theme.Foreground).LineHeight(m.FontSizeM + 4).Ellipsis()
		box := ui.TextField(c, field, title, placeholder, ui.WithID(id)).Height(m.ControlHeight).MarginSides(gap, 0, 0, 0)
		if secret {
			icon, label := lucideIcons[iconEye], state.messages.ShowSecret
			if form.passwordShown {
				icon, label = lucideIcons[iconEyeOff], state.messages.HideSecret
			}
			box.Action(icon, label)
			if box.ActionClicked() {
				form.passwordShown = !form.passwordShown
				if state.options.Invalidate != nil {
					state.options.Invalidate()
				}
			}
		}
	})
}

func (state *State) paintCredentialSettings(page *settingsPage) {
	m := state.messages.Credentials
	page.group(m.Group, func() {
		for index, entry := range state.credentials {
			uses := m.UsedByNone
			if count := state.credentialUses(entry.ID); count > 0 {
				uses = fmt.Sprintf(m.UsedByFmt, count)
			}
			if entry.User != "" {
				uses = entry.User + " · " + uses
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
	hitDialogCredentialOwn      = tree.ID(credentialChoiceHits, "dialog-credential-own")
	hitDialogCredentialNew      = tree.ID(credentialChoiceHits, "dialog-credential-new")
	hitDialogCredentialValue    = tree.ID(credentialChoiceHits, "dialog-credential-value")
	hitDialogCredentialItemList = tree.Sub(credentialChoiceHits, tree.Name("dialog-credential-item")) // + credential index
)

// hostOffersCredential says the form has a choice of password source: the host
// signs in with a password. The choice is offered even with no identity saved,
// because it is where the first one is made.
func (state *State) hostOffersCredential() bool {
	editor := &state.editor
	switch editor.protocol {
	case protocolSSH:
		return editor.auth.usesPassword()
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

// hostUserHint is the hint of the host form's login field when the form's host
// takes its login name from an identity: the identity's name, shown where the
// field is empty, so the user sees the login the host will sign in as. Otherwise
// it is the hint the field would show anyway.
func (state *State) hostUserHint(hint string) string {
	if !state.hostTakesCredential() {
		return hint
	}
	if entry, _ := state.credentialByID(state.editor.credential); entry.User != "" {
		return entry.User
	}
	return hint
}

// paintHostPasswordWithCredential is the password field of a form that has
// shared passwords to choose from. Clicking the field types this host's own
// password; the arrow at its end opens the list. The reveal button moves to the
// start to make room. With a shared password chosen the field is a read-only
// line naming it: a button at its start goes back to this host's own, and the
// arrow stays at its end.
func (state *State) paintHostPasswordWithCredential(canvas *gfx.Canvas, content gfx.Rect, title, placeholder string) {
	editor, theme, m := &state.editor, state.Theme(), state.messages.Credentials
	state.revealHostField(canvas, hitDialogPassword, content)
	editor.credentialAnchor = content
	arrow := components.InputGroupSlot{
		Kind: components.InputGroupSlotButton, ID: hitDialogCredential, Icon: lucideIcons[iconChevronDown], Label: m.FieldSource,
		ButtonSize: components.InputGroupButtonIconXS,
	}
	entry, taken := state.credentialByID(editor.credential)
	if !state.hostTakesCredential() {
		focused := canvas.Focused() == hitDialogPassword
		value := editor.password.String()
		state.paintSecretInputSlots(canvas, content, components.InputSnapshot{
			ID: hitDialogPassword, Label: title, Value: value, Placeholder: placeholder, Secure: true,
			Focused: focused, PlatformEditing: focused && value != "",
		}, arrow, true)
		return
	}
	_ = taken
	components.PaintInputGroup(canvas, theme, content, components.InputGroupSnapshot{
		ID: hitDialogCredentialValue, Label: title, Value: entry.Name, ReadOnly: true,
		InlineStart: components.InputGroupSlot{
			Kind: components.InputGroupSlotButton, ID: hitDialogCredentialOwn, Icon: lucideIcons[iconClose], Label: m.SourceOwn,
			ButtonSize: components.InputGroupButtonIconXS,
		},
		InlineEnd: arrow,
	})
}

// declareHostCredentialMenu declares the list of shared passwords under the
// password field as a page menu.
func (state *State) declareHostCredentialMenu(c ui.C) {
	editor, m := &state.editor, state.messages.Credentials
	if !editor.credentialOpen || !state.hostOffersCredential() {
		return
	}
	anchor := editor.credentialAnchor
	if anchor.Width <= 0 {
		return
	}
	var rows menuRows
	rows.add(components.ContextMenuItem{
		ID: hitDialogCredentialOwn, Label: m.SourceOwn, Kind: components.ContextMenuRadio, Checked: !state.hostTakesCredential(), Selected: !state.hostTakesCredential(),
	})
	for index, entry := range state.credentials {
		rows.add(components.ContextMenuItem{
			ID: hitDialogCredentialItemList.At(index), Label: entry.Name, Kind: components.ContextMenuRadio,
			Checked: entry.ID == editor.credential, Selected: entry.ID == editor.credential,
		})
	}
	rows.add(components.ContextMenuItem{ID: hitDialogCredentialNew, Label: m.Add})
	ui.PopupLayer(c, dialogLayer)
	ui.MenuAt(c, &editor.credentialOpen, anchor, func(menu *ui.MenuItems) {
		menu.WidthRange(anchor.Width, anchor.Width)
		rows.declare(c, menu, state.chooseHostCredential)
	})
}

// chooseHostCredential runs a choice of the shared-password menu, which closed
// with it.
func (state *State) chooseHostCredential(id gfx.ID) {
	state.clickHostCredential(id)
}

func (state *State) clickHostCredential(id gfx.ID) bool {
	editor := &state.editor
	switch {
	case id == hitDialogCredential, id == hitDialogCredentialValue:
		editor.credentialOpen = !editor.credentialOpen
		editor.authOpen, editor.identityOpen = false, false
	case id == hitDialogCredentialOwn:
		editor.credential, editor.credentialOpen = "", false
	case id == hitDialogCredentialNew:
		// The identity dialog goes over the host form; saving or cancelling it
		// brings the form back, with the new identity chosen.
		editor.credentialOpen = false
		state.credentialReturn = modalNewHost
		state.openCredentialDialog("")
		return true
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

// hostSystemUserHint is the hint of a login field whose empty value signs in as
// the system account: it names that account instead of leaving the user to guess
// what an empty field means. It is built again only when the language changes,
// because it is asked for on every painted frame.
func (state *State) hostSystemUserHint() string {
	m := state.messages
	if state.hostCatalog.systemUser == "" {
		return m.FieldUserPlaceholder
	}
	if cache := &state.systemUserHint; cache.format != m.FieldUserDefaultFmt || cache.user != state.hostCatalog.systemUser {
		*cache = systemUserHintCache{format: m.FieldUserDefaultFmt, user: state.hostCatalog.systemUser}
		cache.text = fmt.Sprintf(cache.format, cache.user)
	}
	return state.systemUserHint.text
}

type systemUserHintCache struct{ format, user, text string }
