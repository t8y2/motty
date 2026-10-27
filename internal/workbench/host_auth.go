package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
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

func (state *State) authenticationFor(host Host) sshconn.Authentication {
	switch host.Auth {
	case authPassword:
		return sshconn.Authentication{
			Methods:       sshconn.AuthKeyboardInteractive | sshconn.AuthPassword,
			IdentityFiles: []string{},
		}
	case authPrivateKey:
		if fingerprint := strings.TrimSpace(host.Identity); fingerprint != "" {
			if identity, ok := state.identityForFingerprint(fingerprint); ok {
				return sshconn.Authentication{Methods: sshconn.AuthPublicKey, IdentityFiles: []string{identity.Path}}
			}
			return sshconn.Authentication{Methods: sshconn.AuthPublicKey, IdentityFiles: []string{}}
		}
		return sshconn.Authentication{
			Methods:                 sshconn.AuthPublicKey,
			AdditionalIdentityFiles: state.identityPaths(),
		}
	case authAgent:
		return sshconn.Authentication{
			Methods:       sshconn.AuthPublicKey,
			IdentityFiles: []string{},
			Agent:         true,
		}
	default:
		return sshconn.Authentication{
			Methods:                 sshconn.AuthAll,
			AdditionalIdentityFiles: state.identityPaths(),
			Agent:                   true,
		}
	}
}

func (state *State) identityForFingerprint(fingerprint string) (identityRecord, bool) {
	for _, identity := range state.identities {
		if identity.Error == "" && identity.Fingerprint == fingerprint {
			return identity, true
		}
	}
	return identityRecord{}, false
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

func (state *State) paintHostAuthenticationChoice(canvas *ui.Canvas, bounds ui.Rect) {
	editor, theme := &state.editor, state.Theme()
	content := components.BeginField(canvas, theme, bounds).LabelGap(hostLabelGap).Paint(state.messages.FieldAuthentication, "", "")
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

func (state *State) paintHostIdentityChoice(canvas *ui.Canvas, bounds ui.Rect) {
	editor, theme := &state.editor, state.Theme()
	value := state.messages.AuthAllKeys
	if editor.identity != "" {
		value = state.messages.AuthKeyUnavailable
		if identity, ok := state.identityForFingerprint(editor.identity); ok {
			value = identity.Name
		}
	}
	content := components.BeginField(canvas, theme, bounds).LabelGap(hostLabelGap).Paint(state.messages.FieldIdentity, "", "")
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
			Selected: editor.identity == identity.Fingerprint, Disabled: identity.Error != "",
		})
	}
	selectControl.AddItem(components.SelectItem{ID: hitDialogIdentityImport, Label: state.messages.AuthImportKey})
}

func (state *State) hostAuthListBounds(content ui.Rect, rows int) ui.Rect {
	height := float32(8 + max(rows, 1)*32)
	top := content.Y + content.Height + 4
	window := state.windowRect()
	if top+height > window.Y+window.Height-8 {
		top = content.Y - height - 4
	}
	return ui.Rect{X: content.X, Y: top, Width: content.Width, Height: height}
}

func (state *State) clickHostAuthentication(id ui.ID) bool {
	editor := &state.editor
	switch {
	case id == hitDialogAuth:
		editor.authOpen = !editor.authOpen
		editor.identityOpen = false
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
		editor.authOpen = false
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
	return true
}
