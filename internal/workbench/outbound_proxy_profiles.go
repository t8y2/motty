package workbench

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// A proxy profile is one outbound route kept once and used by several hosts. A
// host names one (Host.ProxyProfile) instead of keeping its own proxy, so
// changing the profile changes every host that uses it. The route's password is
// kept in the vault under the profile's id, never in the settings file; a host
// keeps its own direct list (Host.ProxyBypass), because the profile is shared.
// Profiles are a setting, so they sync with the rest of the connection settings.

// proxyProfileAddChoice is the select's last item: make a new profile.
const proxyProfileAddChoice = "\x00add-proxy-profile"

const (
	maxProxyProfiles      = 256
	maxProxyProfileName   = 80
	proxyProfileTestLimit = 8 * time.Second
)

type proxyProfile struct {
	ID      string
	Name    string
	Kind    sshconn.ProxyKind
	Address string
	User    string
}

// proxyProfileSecretName is where a profile's password is kept in the vault. The
// id is random, so the name is the same on every computer the vault syncs to.
func proxyProfileSecretName(id string) string { return "proxy-profile:" + id + ":password" }

func freshProxyProfileID(taken func(string) bool) string {
	for {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			panic("workbench: no randomness for a proxy profile id: " + err.Error())
		}
		if id := "prof_" + hex.EncodeToString(random[:]); !taken(id) {
			return id
		}
	}
}

func (catalog *hostCatalog) proxyProfileByID(id string) (proxyProfile, bool) {
	if id == "" {
		return proxyProfile{}, false
	}
	index := slices.IndexFunc(catalog.proxyProfiles, func(entry proxyProfile) bool { return entry.ID == id })
	if index < 0 {
		return proxyProfile{}, false
	}
	return catalog.proxyProfiles[index], true
}

// proxyProfileUses is how many hosts take their outbound route from a profile.
func (catalog *hostCatalog) proxyProfileUses(id string) int {
	count := 0
	for _, host := range catalog.hosts {
		if host.ProxyProfile == id {
			count++
		}
	}
	return count
}

// outboundProxy is the profile's route as a connection is given it; the host's
// own direct list is added by the caller.
func (profile proxyProfile) outboundProxy() outboundProxy {
	return outboundProxy{kind: profile.Kind, address: profile.Address, user: profile.User}
}

// ---------------------------------------------------------------------------
// The state file

type savedProxyProfile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Address string `json:"address"`
	User    string `json:"user,omitempty"`
}

func (state *State) savedProxyProfiles() []savedProxyProfile {
	var saved []savedProxyProfile
	for _, entry := range state.proxyProfiles {
		saved = append(saved, savedProxyProfile{
			ID: entry.ID, Name: entry.Name, Kind: proxyKindString(entry.Kind), Address: entry.Address, User: entry.User,
		})
	}
	return saved
}

// decodeProxyProfiles turns saved records into profiles. A record without an id,
// a name or a usable route is dropped; an id that repeats keeps the first record.
func decodeProxyProfiles(saved []savedProxyProfile) []proxyProfile {
	var profiles []proxyProfile
	for _, entry := range saved {
		kind := proxyKindFromString(entry.Kind)
		name := cleanSnippetText(entry.Name, maxProxyProfileName)
		if entry.ID == "" || !validCatalogID(entry.ID) || name == "" || len(profiles) >= maxProxyProfiles ||
			(kind != sshconn.ProxySOCKS5 && kind != sshconn.ProxyHTTPConnect) ||
			!validProxyAddress(strings.TrimSpace(entry.Address)) || len(entry.User) > 255 ||
			slices.ContainsFunc(profiles, func(have proxyProfile) bool { return have.ID == entry.ID }) {
			continue
		}
		profiles = append(profiles, proxyProfile{
			ID: entry.ID, Name: name, Kind: kind, Address: strings.TrimSpace(entry.Address), User: strings.TrimSpace(entry.User),
		})
	}
	return profiles
}

func (state *State) restoreProxyProfiles(saved savedState) {
	state.proxyProfiles = decodeProxyProfiles(saved.ProxyProfiles)
}

// ---------------------------------------------------------------------------
// The dialog

var proxyProfileHits = tree.Feature("proxy-profiles")

var (
	hitProxyProfileAdd      = tree.ID(proxyProfileHits, "proxy-profile-add")
	hitProxyProfileName     = tree.ID(proxyProfileHits, "proxy-profile-name")
	hitProxyProfileAddress  = tree.ID(proxyProfileHits, "proxy-profile-address")
	hitProxyProfileUser     = tree.ID(proxyProfileHits, "proxy-profile-user")
	hitProxyProfilePassword = tree.ID(proxyProfileHits, "proxy-profile-password")
	hitProxyProfileTarget   = tree.ID(proxyProfileHits, "proxy-profile-test-target")
	hitProxyProfileTest     = tree.ID(proxyProfileHits, "proxy-profile-test")
	hitProxyProfileSubmit   = tree.ID(proxyProfileHits, "proxy-profile-submit")
	hitProxyProfileCancel   = tree.ID(proxyProfileHits, "proxy-profile-cancel")
	hitProxyProfileClose    = tree.ID(proxyProfileHits, "proxy-profile-close")
)

var (
	hitProxyProfileEditList   = tree.Sub(proxyProfileHits, tree.Name("proxy-profile-edit"))   // + profile index
	hitProxyProfileDeleteList = tree.Sub(proxyProfileHits, tree.Name("proxy-profile-delete")) // + profile index
)

// proxyProfileUI is the dialog that adds or edits a proxy profile.
type proxyProfileUI struct {
	// editing is the id of the profile being edited; empty adds a new one.
	editing          string
	name, address    text.Editor
	user, password   text.Editor
	testTarget       text.Editor
	kind, kindChoice sshconn.ProxyKind
	dialogError      string
	testMessage      string
	testOK           bool
	testRunning      bool
	testRun          uint64
	passwordShown    bool
	nameField        ui.Field
	addressField     ui.Field
	userField        ui.Field
	passwordField    ui.Field
	testTargetField  ui.Field
}

func (state *State) openProxyProfileDialog(id string) {
	state.proxyProfileReturn = modalNone
	state.showProxyProfileDialog(id)
}

// openProxyProfileDialogOverHost opens the dialog over the host proxy dialog,
// so saving a new profile returns there with it chosen.
func (state *State) openProxyProfileDialogOverHost() {
	state.proxyProfileReturn = modalOutboundProxy
	state.showProxyProfileDialog("")
}

func (state *State) showProxyProfileDialog(id string) {
	form := &state.proxyProfileForm
	testRun := form.testRun + 1 // a test still in flight must not land in the reopened dialog
	*form = proxyProfileUI{editing: id, kind: sshconn.ProxySOCKS5, kindChoice: sshconn.ProxySOCKS5, testRun: testRun}
	form.nameField.Bind(&form.name)
	form.addressField.Bind(&form.address)
	form.userField.Bind(&form.user)
	form.passwordField.Bind(&form.password)
	form.testTargetField.Bind(&form.testTarget)
	if entry, ok := state.proxyProfileByID(id); ok {
		form.name.Insert(entry.Name)
		form.address.Insert(entry.Address)
		form.user.Insert(entry.User)
		form.kind, form.kindChoice = entry.Kind, entry.Kind
		// The dialog shows what is saved, as the identity dialog does; a vault
		// that cannot answer leaves the field empty, which saving reads as "keep".
		if vaultFile := state.secrets.vault; vaultFile != nil && vaultFile.Len() > 0 {
			if value, found, err := vaultFile.Get(proxyProfileSecretName(id)); err == nil && found {
				form.password.Insert(value)
			}
		}
	}
	state.modal = modalProxyProfile
}

func (state *State) clickProxyProfileDialog(id gfx.ID) {
	switch id {
	case hitProxyProfileSubmit:
		state.submitProxyProfile()
	case hitProxyProfileTest:
		state.testProxyProfile()
	case hitProxyProfileCancel, hitProxyProfileClose:
		returning := state.proxyProfileReturn
		state.proxyProfileReturn = modalNone
		state.closeModal()
		if returning == modalOutboundProxy {
			state.proxyForm.profile = state.proxyForm.profileBeforeAdd
			state.modal = modalOutboundProxy
		}
	}
}

// proxyProfileFromForm reads the dialog's route, separate from its name and
// secret. It reports a user-visible problem and false when the route is invalid.
func (state *State) proxyProfileFromForm() (proxyProfile, bool) {
	form, m := &state.proxyProfileForm, state.messages.ProxyProfiles
	address := strings.TrimSpace(form.address.String())
	user := strings.TrimSpace(form.user.String())
	if form.kind != sshconn.ProxySOCKS5 && form.kind != sshconn.ProxyHTTPConnect {
		form.kind = sshconn.ProxySOCKS5
	}
	if !validProxyAddress(address) {
		form.dialogError = m.ErrorAddress
		return proxyProfile{}, false
	}
	if len(user) > 255 || len(form.password.String()) > 255 {
		form.dialogError = m.ErrorLogin
		return proxyProfile{}, false
	}
	return proxyProfile{ID: form.editing, Kind: form.kind, Address: address, User: user}, true
}

func (state *State) submitProxyProfile() {
	form, m := &state.proxyProfileForm, state.messages.ProxyProfiles
	name := cleanSnippetText(form.name.String(), maxProxyProfileName)
	switch {
	case name == "":
		form.dialogError = m.ErrorName
		return
	case slices.ContainsFunc(state.proxyProfiles, func(entry proxyProfile) bool {
		return entry.ID != form.editing && strings.EqualFold(entry.Name, name)
	}):
		form.dialogError = m.ErrorNameTaken
		return
	case form.editing == "" && len(state.proxyProfiles) >= maxProxyProfiles:
		form.dialogError = m.ErrorTooMany
		return
	}
	profile, ok := state.proxyProfileFromForm()
	if !ok {
		return
	}
	profile.Name = name
	id := form.editing
	if id == "" {
		id = freshProxyProfileID(func(candidate string) bool {
			_, taken := state.proxyProfileByID(candidate)
			return taken
		})
	}
	profile.ID = id
	vaultFile := state.secrets.vault
	value := form.password.String()
	switch {
	case value != "" && profile.User != "":
		if vaultFile == nil {
			form.dialogError = fmt.Sprintf(m.ErrorVaultFmt, state.messages.VaultUnavailable)
			return
		}
		if err := vaultFile.Prepare(); err != nil {
			form.dialogError = fmt.Sprintf(m.ErrorVaultFmt, state.vaultErrorText(err))
			return
		}
		if err := vaultFile.Set(proxyProfileSecretName(id), value); err != nil {
			form.dialogError = fmt.Sprintf(m.ErrorVaultFmt, state.vaultErrorText(err))
			return
		}
	case profile.User == "" && vaultFile != nil:
		// No login: the profile keeps no password.
		if err := vaultFile.Delete(proxyProfileSecretName(id)); err != nil {
			form.dialogError = fmt.Sprintf(m.ErrorVaultFmt, state.vaultErrorText(err))
			return
		}
	}
	if index := slices.IndexFunc(state.proxyProfiles, func(entry proxyProfile) bool { return entry.ID == id }); index >= 0 {
		state.proxyProfiles[index] = profile
	} else {
		state.proxyProfiles = append(state.proxyProfiles, profile)
	}
	form.password = text.Editor{}
	returning := state.proxyProfileReturn
	state.proxyProfileReturn = modalNone
	state.closeModal()
	if returning == modalOutboundProxy {
		proxyForm := &state.proxyForm
		proxyForm.profile = id
		proxyForm.inherit = false
		state.loadProxyProfileIntoForm(profile)
		proxyForm.password = text.Editor{}
		state.modal = modalOutboundProxy
	}
	state.save()
}

// deleteProxyProfile removes a profile and its secret. Hosts that used it keep
// their own direct list but name no profile any more, so they connect directly:
// deleting a route must not silently send them through the global proxy instead.
func (state *State) deleteProxyProfile(id string) {
	index := slices.IndexFunc(state.proxyProfiles, func(entry proxyProfile) bool { return entry.ID == id })
	if index < 0 {
		return
	}
	if state.secrets.vault != nil {
		if err := state.secrets.vault.Delete(proxyProfileSecretName(id)); err != nil {
			state.status = state.hostPasswordSaveError(err)
			return
		}
	}
	state.proxyProfiles = slices.Delete(state.proxyProfiles, index, index+1)
	for hostIndex := range state.hosts {
		previous := state.hosts[hostIndex]
		if previous.ProxyProfile != id {
			continue
		}
		updated := previous
		updated.ProxyProfile, updated.ProxyBypass = "", ""
		updated.Proxy = &outboundProxy{}
		state.replaceHost(hostIndex, previous, updated)
	}
	state.hostDataChanged()
	state.save()
}

func (state *State) paintProxyProfileSettings(page *settingsPage) {
	m := state.messages.ProxyProfiles
	page.group(m.Group, func() {
		for index, entry := range state.proxyProfiles {
			uses := m.UsedByNone
			if count := state.proxyProfileUses(entry.ID); count > 0 {
				uses = fmt.Sprintf(m.UsedByFmt, count)
			}
			summary := fmt.Sprintf(m.RowFmt, state.proxyKindName(entry.Kind), entry.Address) + " · " + uses
			page.actionRow(entry.Name, summary, false,
				components.ButtonAction{ID: hitProxyProfileEditList.At(index), Label: m.Edit, Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitProxyProfileDeleteList.At(index), Label: m.Delete, Variant: components.ButtonOutline})
		}
		page.actionRow(m.Group, m.RowDescription, len(state.proxyProfiles) >= maxProxyProfiles,
			components.ButtonAction{ID: hitProxyProfileAdd, Label: m.Add, Variant: components.ButtonOutline})
	})
}

// clickProxyProfileSettings handles the Settings group's buttons.
func (state *State) clickProxyProfileSettings(id gfx.ID) bool {
	switch {
	case id == hitProxyProfileAdd:
		state.openProxyProfileDialog("")
	case id.Scope() == hitProxyProfileEditList:
		if index := item(id); index >= 0 && index < len(state.proxyProfiles) {
			state.openProxyProfileDialog(state.proxyProfiles[index].ID)
		}
	case id.Scope() == hitProxyProfileDeleteList:
		state.askDeleteProxyProfile(item(id))
	default:
		return false
	}
	return true
}

// declareProxyProfileDialog declares the profile dialog in the window page, as
// the identity dialog is declared: the page names its fields and buttons.
func (state *State) declareProxyProfileDialog(c ui.C) {
	if state.modal != modalProxyProfile {
		return
	}
	form, m := &state.proxyProfileForm, state.messages.ProxyProfiles
	title := m.DialogAddTitle
	if form.editing != "" {
		title = m.DialogEditTitle
	}
	bodyHeight := withError(420, form.dialogError)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 560, Layer: dialogLayer, BodyHeight: bodyHeight,
		Scope: dialogScope, CloseID: hitProxyProfileClose, InitialFocusID: hitProxyProfileName,
	})
	dialog.Body(func() {
		ui.Column(c).Height(bodyHeight).PadSides(2, 4, 0, 4).Children(func() {
			ui.Column(c).Height(58).MarginSides(0, 0, 6, 0).Children(func() {
				ui.Text(c, m.Kind).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(c.Theme().Foreground).
					LineHeight(metrics.FontSizeM + 4).Ellipsis()
				form.kindChoice = form.kind
				if ui.Choice(c, &form.kindChoice, []sshconn.ProxyKind{sshconn.ProxySOCKS5, sshconn.ProxyHTTPConnect}, state.proxyKindName).Fill().Changed() {
					form.kind = form.kindChoice
				}
			})
			state.proxyProfileField(c, hitProxyProfileName, &form.nameField, m.Name, m.NamePlaceholder, 62, false)
			state.proxyProfileField(c, hitProxyProfileAddress, &form.addressField, m.Address, m.AddressPlaceholder, 62, false)
			state.proxyProfileField(c, hitProxyProfileUser, &form.userField, m.User, m.UserPlaceholder, 62, false)
			placeholder := m.PasswordKeep
			state.proxyProfileField(c, hitProxyProfilePassword, &form.passwordField, m.Password, placeholder, 62, true)
			state.proxyProfileField(c, hitProxyProfileTarget, &form.testTargetField, m.TestTarget, m.TestTargetPlaceholder, 62, false)
			if form.dialogError != "" {
				state.problemLineC(c, form.dialogError)
			} else if form.testMessage != "" {
				color := state.pal.muted
				switch {
				case form.testRunning:
				case form.testOK:
					color = state.pal.success
				default:
					color = state.pal.danger
				}
				ui.Text(c, form.testMessage).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(color).LineHeight(18)
			}
		})
	})
	dialog.Footer(func() {
		if dialogButton(c, m.Test, components.ButtonOutline, form.testRunning).Clicked() {
			c.After(func() { state.clickProxyProfileDialog(hitProxyProfileTest) })
		}
		if dialogButton(c, state.messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickProxyProfileDialog(hitProxyProfileCancel) })
		}
		if dialogButton(c, m.Save, components.ButtonDefault, false).Clicked() {
			c.After(func() { state.clickProxyProfileDialog(hitProxyProfileSubmit) })
		}
	})
	if dialog.Dismissed() {
		state.clickProxyProfileDialog(hitProxyProfileClose)
	}
}

// proxyProfileField declares one labelled field of the profile dialog. A secret
// field gets the eye button that swaps bullets for text.
func (state *State) proxyProfileField(c ui.C, id gfx.ID, field *ui.Field, title, placeholder string, height float32, secret bool) {
	theme := c.Theme()
	m := theme.Metrics
	gap := m.GapItem / 2
	form := &state.proxyProfileForm
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

// proxyProfileFormSignature digests what a test was run against, so a result is
// shown only while the dialog still says the same thing.
func (state *State) proxyProfileFormSignature() string {
	form := &state.proxyProfileForm
	return fmt.Sprint(form.kind, "\x00", form.address.String(), "\x00", form.user.String(), "\x00",
		form.password.String(), "\x00", form.testTarget.String())
}

// testProxyProfile dials the route the dialog describes, without saving it. A
// test target makes the proxy carry a real connection; without one only the
// proxy port is checked, because a SOCKS5 or HTTP CONNECT handshake needs a
// destination.
func (state *State) testProxyProfile() {
	form, m := &state.proxyProfileForm, state.messages.ProxyProfiles
	if form.testRunning {
		return
	}
	profile, ok := state.proxyProfileFromForm()
	if !ok {
		return
	}
	password := form.password.String()
	if password == "" && form.editing != "" && profile.User != "" {
		if vaultFile := state.secrets.vault; vaultFile != nil {
			if value, found, err := vaultFile.Get(proxyProfileSecretName(form.editing)); err == nil && found {
				password = value
			}
		}
	}
	target := strings.TrimSpace(form.testTarget.String())
	if target != "" && !validProxyAddress(target) {
		form.dialogError = m.ErrorTestTarget
		return
	}
	form.dialogError = ""
	testRun := form.testRun + 1
	signature := state.proxyProfileFormSignature()
	form.testRun, form.testRunning, form.testMessage, form.testOK = testRun, true, m.Testing, false
	dial := &sshconn.Proxy{Kind: profile.Kind, Address: profile.Address, Username: profile.User, Password: password}
	go func() {
		message, ok := "", false
		ctx, cancel := context.WithTimeout(context.Background(), proxyProfileTestLimit)
		defer cancel()
		if target == "" {
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", profile.Address)
			if err != nil {
				message = fmt.Sprintf(m.TestFailedFmt, err.Error())
			} else {
				_ = conn.Close()
				message, ok = m.TestEndpointOK, true
			}
		} else {
			conn, err := dial.Dial(ctx, proxyProfileTestLimit, target)
			if err != nil {
				message = fmt.Sprintf(m.TestFailedFmt, err.Error())
			} else {
				_ = conn.Close()
				message, ok = fmt.Sprintf(m.TestOKFmt, target), true
			}
		}
		state.aiPost(func(current *State) {
			form := &current.proxyProfileForm
			if current.modal != modalProxyProfile || form.testRun != testRun || current.proxyProfileFormSignature() != signature {
				return
			}
			form.testRunning, form.testMessage, form.testOK = false, message, ok
		})
	}()
}

// proxyProfileChoiceLabel names one profile in the host form's select.
func (state *State) proxyProfileChoiceLabel(id string) string {
	m := state.messages.ProxyProfiles
	switch id {
	case "":
		return m.Manual
	case proxyProfileAddChoice:
		return m.Add + "…"
	}
	profile, ok := state.proxyProfileByID(id)
	if !ok {
		return ""
	}
	return fmt.Sprintf(m.ChoiceFmt, profile.Name, state.proxyKindName(profile.Kind), profile.Address)
}

// hostProxyProfileChoices fills the host form's select with the manual route
// first and then the saved profiles.
func (state *State) hostProxyProfileChoices() []string {
	form := &state.proxyForm
	form.profileChoices = append(form.profileChoices[:0], "")
	for _, entry := range state.proxyProfiles {
		form.profileChoices = append(form.profileChoices, entry.ID)
	}
	form.profileChoices = append(form.profileChoices, proxyProfileAddChoice)
	return form.profileChoices
}
