package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// hostAuth is the authentication policy of one SSH host. The zero value keeps
// the long-standing automatic order for existing catalogs.
type hostAuth uint8

const (
	authAutomatic hostAuth = iota
	authPassword
	authPrivateKey
	authAgent
	authCount
)

var hostAuthUiHits = tree.Feature("host-auth-ui")

var (
	hitDialogAuth             = tree.ID(hostAuthUiHits, "dialog-auth")
	hitDialogAuthDismiss      = tree.ID(hostAuthUiHits, "dialog-auth-dismiss")
	hitDialogAuthItemList     = tree.Sub(hostAuthUiHits, tree.Name("dialog-auth-item"))
	hitDialogIdentity         = tree.ID(hostAuthUiHits, "dialog-identity")
	hitDialogIdentityDismiss  = tree.ID(hostAuthUiHits, "dialog-identity-dismiss")
	hitDialogIdentityAll      = tree.ID(hostAuthUiHits, "dialog-identity-all")
	hitDialogIdentityHelp     = tree.ID(hostAuthUiHits, "dialog-identity-help")
	hitDialogIdentityImport   = tree.ID(hostAuthUiHits, "dialog-identity-import")
	hitDialogIdentityItemList = tree.Sub(hostAuthUiHits, tree.Name("dialog-identity-item"))
)

func (authentication hostAuth) saved() string {
	switch authentication {
	case authPassword:
		return "password"
	case authPrivateKey:
		return "key"
	case authAgent:
		return "agent"
	default:
		return ""
	}
}

func parseHostAuth(value string) (hostAuth, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return authAutomatic, true
	case "password":
		return authPassword, true
	case "key":
		return authPrivateKey, true
	case "agent":
		return authAgent, true
	default:
		return authAutomatic, false
	}
}

func (state *State) authLabel(authentication hostAuth) string {
	switch authentication {
	case authPassword:
		return state.messages.AuthPassword
	case authPrivateKey:
		return state.messages.AuthPrivateKey
	case authAgent:
		return state.messages.AuthAgent
	default:
		return state.messages.AuthAutomatic
	}
}

func (state *State) paintHostAuthenticationChoice(canvas *gfx.Canvas, bounds gfx.Rect) {
	editor, theme := &state.editor, state.Theme()
	content := components.BeginField(canvas, theme, bounds).Paint(state.messages.FieldAuthentication, "", "")
	state.rememberHostControl(hitDialogAuth, content)
	portal := state.beginHostFormPopup(canvas, editor.authOpen)
	defer state.endHostFormPopup(canvas, portal)
	list := state.hostAuthListBounds(content, int(authCount))
	selectControl := components.BeginSelect(canvas, theme, content, components.SelectOptions{
		TriggerID: hitDialogAuth, Label: state.messages.FieldAuthentication, Value: state.authLabel(editor.auth),
		Open: editor.authOpen, Content: list,
		Layer: dialogLayer + 1, Scope: dialogScope, DismissID: hitDialogAuthDismiss, Viewport: state.windowRect(),
	})
	defer selectControl.End()
	if editor.authOpen {
		state.menuPlaced(list)
	}
	for authentication := authAutomatic; authentication < authCount; authentication++ {
		selectControl.AddItem(components.SelectItem{
			ID: hitDialogAuthItemList.At(int(authentication)), Label: state.authLabel(authentication), Selected: editor.auth == authentication,
		})
	}
}

func (state *State) paintHostIdentityChoice(canvas *gfx.Canvas, bounds gfx.Rect) {
	editor, theme := &state.editor, state.Theme()
	value := state.messages.AuthAllKeys
	if editor.identity != "" {
		value = state.messages.AuthKeyUnavailable
		if identity, ok := state.identityForFingerprint(editor.identity); ok {
			value = identity.Name
		}
	}
	content := components.BeginField(canvas, theme, bounds).Paint(state.messages.FieldIdentity, "", "")
	if state.hostKeyNeedsPassphrase(editor.identity) && !state.hostAsksKeyPassphrase() {
		// A question mark after the label: a line under the field would crowd the row below.
		labelWidth := canvas.TextWidthWeight(state.messages.FieldIdentity, theme.Metrics.FontSizeM, gfx.WeightMedium)
		titleHeight := content.Y - bounds.Y - theme.Metrics.GapItem/2
		state.paintHelpIcon(canvas, hitDialogIdentityHelp, state.messages.KeyPassphraseHostHint,
			gfx.Rect{X: bounds.X + labelWidth + 6, Y: bounds.Y + (titleHeight-16)/2, Width: 16, Height: 16})
	}
	state.rememberHostControl(hitDialogIdentity, content)
	portal := state.beginHostFormPopup(canvas, editor.identityOpen)
	defer state.endHostFormPopup(canvas, portal)
	rows := min(len(state.identities)+2, 6)
	list := state.hostAuthListBounds(content, rows)
	var scroll *components.MenuScroll
	if editor.identityOpen {
		scroll = state.menuScrollFor(hitDialogIdentityDismiss)
		state.menuPlaced(list)
	}
	selectControl := components.BeginSelect(canvas, theme, content, components.SelectOptions{
		TriggerID: hitDialogIdentity, Label: state.messages.FieldIdentity, Value: value,
		Open: editor.identityOpen, Content: list, Scroll: scroll,
		Layer: dialogLayer + 1, Scope: dialogScope, DismissID: hitDialogIdentityDismiss, Viewport: state.windowRect(),
	})
	defer selectControl.End()
	selectControl.AddItem(components.SelectItem{
		ID: hitDialogIdentityAll, Label: state.messages.AuthAllKeys, Selected: editor.identity == "",
	})
	for _, identity := range state.identities {
		selectControl.AddItem(components.SelectItem{
			ID: hitDialogIdentityItemList.At(int(identity.ID)), Label: identity.Name,
			// A key whose fingerprint is unknown until it is unlocked (an encrypted
			// PKCS#8 or old PEM file) cannot be told apart from "any key" here.
			Selected: identity.Fingerprint != "" && editor.identity == identity.Fingerprint,
			Disabled: identity.Error != "" || identity.Fingerprint == "",
		})
	}
	selectControl.AddItem(components.SelectItem{ID: hitDialogIdentityImport, Label: state.messages.AuthImportKey})
}

func (state *State) hostAuthListBounds(content gfx.Rect, rows int) gfx.Rect {
	height := float32(8 + max(rows, 1)*32)
	top := content.Y + content.Height + 4
	window := state.windowRect()
	if top+height > window.Y+window.Height-8 {
		top = content.Y - height - 4
	}
	return gfx.Rect{X: content.X, Y: top, Width: content.Width, Height: height}
}

func (state *State) clickHostAuthentication(id gfx.ID) bool {
	editor := &state.editor
	switch {
	case id == hitDialogAuth:
		editor.authOpen = !editor.authOpen
		editor.identityOpen, editor.credentialOpen = false, false
	case id == hitDialogAuthDismiss:
		editor.authOpen = false
	case id.Scope() == hitDialogAuthItemList:
		authentication := hostAuth(item(id))
		if authentication >= authCount {
			return false
		}
		editor.auth = authentication
		editor.authOpen = false
		editor.identityOpen = false
	case id == hitDialogIdentity:
		editor.identityOpen = !editor.identityOpen
		editor.authOpen, editor.credentialOpen = false, false
	case id == hitDialogIdentityDismiss:
		editor.identityOpen = false
	case id == hitDialogIdentityAll:
		editor.identity = ""
		editor.identityOpen = false
	case id == hitDialogIdentityImport:
		before := len(state.identities)
		state.importKeys()
		if len(state.identities) > before {
			if identity, ok := state.identityByID(state.selectedIdentityID); ok {
				editor.identity = identity.Fingerprint
			}
		}
		editor.identityOpen = false
	case id.Scope() == hitDialogIdentityItemList:
		identity, ok := state.identityByID(uint32(item(id)))
		if !ok || identity.Error != "" {
			return false
		}
		editor.identity = identity.Fingerprint
		editor.identityOpen = false
	default:
		return false
	}
	state.dialogError = ""
	state.prefillKeyPassphrase()
	return true
}

// hostKeyNeedsPassphrase reports whether connecting with the key pinned by
// fingerprint, or with any imported key when it is empty, may ask for a
// passphrase.
func (state *State) hostKeyNeedsPassphrase(fingerprint string) bool {
	for _, identity := range state.identities {
		if identity.Error == "" && identity.Encrypted && (fingerprint == "" || identity.Fingerprint == fingerprint) {
			return true
		}
	}
	return false
}
