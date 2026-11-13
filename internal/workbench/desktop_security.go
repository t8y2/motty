package workbench

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/desktop"
)

var (
	hitDesktopSecurityClose = tree.ID(desktopHits, "security-close")
	hitDesktopSecurityCopy  = tree.ID(desktopHits, "security-copy")
	hitDesktopSecurityText  = tree.ID(desktopHits, "security-text")
)

type desktopSecurityDialog struct {
	text   string
	layout components.TextareaLayout
	height float32
	err    string
}

func (state *State) vncProtection(info desktop.Info) string {
	m := state.messages.Desktop
	if info.ProtocolEncrypted {
		return m.StripVNCEncrypted
	}
	if info.Security == "RA2ne" || info.Security == "RA256ne" {
		return m.SecurityAuthOnly
	}
	return m.StripPlain
}

// vncSecurityStatus formats stable connection metadata only when it or the
// language changes. The picture's paint path then reuses the same string.
func (state *State) vncSecurityStatus(view *desktopView) string {
	info, m := view.session.Info(), state.messages.Desktop
	if info != view.securityInfo || view.securityLocale != m.SecurityDetails {
		view.securityInfo, view.securityLocale = info, m.SecurityDetails
		view.securityStatus = state.vncProtection(info)
		if info.Security != "" {
			view.securityStatus = info.Security + " (" + view.securityStatus + ")"
		}
	}
	return view.securityStatus
}

func (state *State) vncSecurityDetails(session *Session) string {
	m := state.messages.Desktop
	info := session.desk.session.Info()
	auth := m.SecurityNotReported
	switch info.Authentication {
	case "none":
		auth = m.SecurityAuthNone
	case "vnc-password":
		auth = m.VNCPassword
	case "user-password":
		auth = m.VNCSystemPassword
	}
	fingerprint := ""
	key := session.hostKey
	if conn, ok := session.conn.(*desktopConn); ok {
		if verified := conn.verifiedServerIdentity(); verified.Fingerprint != "" {
			key = verified
		}
	}
	if vncIdentityKind(key.Type) && key.Fingerprint != "" {
		fingerprint = key.Type + " " + key.Fingerprint
	}
	endpoint := net.JoinHostPort(session.host.Address, strconv.Itoa(int(session.host.Port)))
	rows := [...]struct{ label, value string }{
		{m.SecurityEndpoint, endpoint},
		{m.SecurityMode, info.Security},
		{m.SecurityProtection, state.vncProtection(info)},
		{m.SecurityAuthentication, auth},
		{m.SecurityCipher, info.Cipher},
		{m.SecurityTLSVersion, info.TLSVersion},
		{m.SecurityOffered, info.SecurityTypes},
		{m.SecuritySubtypes, info.SecuritySubtypes},
		{m.SecurityIdentity, fingerprint},
	}
	var result strings.Builder
	for _, row := range rows {
		value := row.value
		if value == "" {
			value = m.SecurityNotReported
		}
		result.WriteString(row.label + ": " + value + "\n\n")
	}
	if session.host.tunneled() || session.desktopTunnel {
		result.WriteString(m.StripEncrypted + "\n")
	}
	return strings.TrimSpace(result.String())
}

func (state *State) openDesktopSecurity(session *Session) {
	if session == nil || session.desk == nil || session.host.Protocol != protocolVNC {
		return
	}
	// Opening local controls releases any held remote keys. Closing the dialog
	// leaves capture released until the user deliberately resumes it.
	if !session.desk.released {
		state.toggleCapture(session.desk)
	}
	state.desktopSecurity = desktopSecurityDialog{text: state.vncSecurityDetails(session)}
	state.modal = modalDesktopSecurity
}

func (state *State) clickDesktopSecurity(id gfx.ID) {
	switch id {
	case hitDesktopSecurityClose:
		state.desktopSecurity = desktopSecurityDialog{}
		state.closeModal()
	case hitDesktopSecurityCopy:
		copy := state.options.SetClipboardText
		if copy == nil {
			return
		}
		if err := copy(state.desktopSecurity.text); err != nil {
			state.desktopSecurity.err = fmt.Sprintf(state.messages.StatusClipboardFailedFmt, err)
			return
		}
		state.desktopSecurity.err = ""
		state.status = state.messages.Desktop.SecurityCopied
	}
}

func (state *State) paintDesktopSecurity(canvas *gfx.Canvas, viewport gfx.Rect) {
	m, details := state.messages.Desktop, &state.desktopSecurity
	width := min(float32(620), max(viewport.Width-32, 240))
	details.height = min(float32(460), max(viewport.Height-230, 100))
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitDesktopSecurityClose, CloseID: hitDesktopSecurityClose,
		InitialFocusID: hitDesktopSecurityText,
		ContentSize:    gfx.Size{Width: width}, FitBody: true, BodyHeight: details.height + 28,
	})
	layout := dialog.Begin(m.SecurityDetails, "")
	textRect := layout.Body
	textRect.Height = details.height
	components.PaintTextareaContent(canvas, state.Theme(), textRect, textRect, components.TextareaSnapshot{
		Value: details.text, ReadOnly: true, Layout: &details.layout,
	})
	canvas.HitWithOptions(hitDesktopSecurityText, textRect, m.SecurityDetails, gfx.HitOptions{
		Role: gfx.RoleGroup, Value: details.text, TextBounds: textRect,
	})
	if details.err != "" {
		canvas.Label(details.err, gfx.Rect{X: textRect.X, Y: textRect.Y + textRect.Height + 4, Width: textRect.Width, Height: 24}, gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.danger})
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitDesktopSecurityClose, Label: state.messages.ConnClose, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitDesktopSecurityCopy, Label: m.SecurityCopy},
	)
	dialog.End()
}

func (state *State) desktopSecurityKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalDesktopSecurity {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape && !event.Repeat {
		state.clickDesktopSecurity(hitDesktopSecurityClose)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Key == gfx.KeyEnter && !event.Repeat && (event.Target == hitDesktopSecurityClose || event.Target == hitDesktopSecurityCopy) {
		state.clickDesktopSecurity(event.Target)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Target != hitDesktopSecurityText {
		return gfx.EventResult{}, false
	}
	var step float32
	switch event.Key {
	case gfx.KeyUp:
		step = -36
	case gfx.KeyDown:
		step = 36
	case gfx.KeyPageUp:
		step = -state.desktopSecurity.height * 0.9
	case gfx.KeyPageDown:
		step = state.desktopSecurity.height * 0.9
	case gfx.KeyHome:
		step = -1e9
	case gfx.KeyEnd:
		step = 1e9
	default:
		return gfx.EventResult{}, false
	}
	state.desktopSecurity.layout.ScrollBy(step)
	return gfx.EventResult{Handled: true, Repaint: true}, true
}
