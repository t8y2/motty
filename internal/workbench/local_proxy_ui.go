package workbench

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/systemproxy"
)

const (
	defaultLocalProxyPort  uint16  = 7897
	dockerProxyDocsURL             = "https://docs.docker.com/engine/daemon/proxy/"
	localProxyMainHeight   float32 = 190
	localProxyDockerHeight float32 = 620
)

var localProxyHits = tree.Feature("local-proxy")

var (
	hitLocalProxyOpen          = tree.ID(localProxyHits, "open")
	hitLocalProxyDismiss       = tree.ID(localProxyHits, "dismiss")
	hitLocalProxyClose         = tree.ID(localProxyHits, "close")
	hitLocalProxyPort          = tree.ID(localProxyHits, "port")
	hitLocalProxyStart         = tree.ID(localProxyHits, "start")
	hitLocalProxyTest          = tree.ID(localProxyHits, "test")
	hitLocalProxyStop          = tree.ID(localProxyHits, "stop")
	hitLocalProxyEnableCopy    = tree.ID(localProxyHits, "enable-copy")
	hitLocalProxyDisableCopy   = tree.ID(localProxyHits, "disable-copy")
	hitLocalProxyDocker        = tree.ID(localProxyHits, "docker")
	hitLocalProxyDockerBack    = tree.ID(localProxyHits, "docker-back")
	hitLocalProxyDockerDocs    = tree.ID(localProxyHits, "docker-docs")
	hitLocalProxyDockerConfig  = tree.ID(localProxyHits, "docker-config-copy")
	hitLocalProxyDockerRestore = tree.ID(localProxyHits, "docker-restore-copy")
	hitLocalProxyScroll        = tree.ID(localProxyHits, "scroll")
	hitLocalProxyMore          = tree.ID(localProxyHits, "more")
)

type localProxyUI struct {
	session        uint32
	port           text.Editor
	docker         bool
	more           bool
	copyTarget     localProxyTarget
	returnFocus    gfx.ID
	scroll         float32
	scrollGeometry components.ScrollAreaGeometry
	lastRemotePort uint16
	lastStopped    bool
	copied         gfx.ID
	copyError      string
	retryCopy      gfx.ID
	// The dialog's body is part of the window page; these are the slots its
	// declaration leaves for the hand-painted field, phase line and buttons.
	portField, phaseLine, dockerButton, moreButton, testButton, restoreButton ui.Box
	configButton, docsButton, dockerRestoreButton                             ui.Box
	// viewportDraw and controlsDraw are the dialog's kept painters, made once
	// per open so a repaint declares the body without allocating.
	viewportDraw, controlsDraw func(*ui.DrawContext)
	viewportTitle              string
}

func (state *State) openLocalProxy(session *Session) {
	if _, ok := state.localProxyTarget(session); !ok || session.host.isDesktop() {
		return
	}
	state.localProxyUI = localProxyUI{session: session.ID}
	port := state.localProxyPort
	if !state.localProxyPortSet {
		port = localProxySystemPort(state.lookupSystemProxy)
	}
	if snapshot := state.localProxySnapshot(session); snapshot.LocalPort != 0 {
		port = snapshot.LocalPort
	}
	state.localProxyUI.port.Insert(strconv.Itoa(int(port)))
	state.localProxyUI.port.SetSelection(text.Range{End: state.localProxyUI.port.RuneCount()})
	state.modal, state.modalSession = modalLocalProxy, session.ID
}

func localProxySystemPort(lookup func() systemproxy.Settings) uint16 {
	if lookup == nil {
		return defaultLocalProxyPort
	}
	settings := lookup()
	raw := settings.HTTP
	if raw == "" {
		raw = settings.HTTPS
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil {
		return defaultLocalProxyPort
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" {
		return defaultLocalProxyPort
	}
	port, err := strconv.ParseUint(parsed.Port(), 10, 16)
	if err != nil || port == 0 {
		return defaultLocalProxyPort
	}
	return uint16(port)
}

func (state *State) localProxySession() *Session {
	session, ok := state.sessionByIDPointer(state.localProxyUI.session)
	if !ok {
		return nil
	}
	return session
}

func (state *State) localProxyPortValue() (uint16, bool) {
	value := strings.TrimSpace(state.localProxyUI.port.String())
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return uint16(port), true
}

func (state *State) localProxyUIPortLocked() bool {
	phase := state.localProxySnapshot(state.localProxySession()).Phase
	return phase == localProxyConnecting || phase == localProxyConnected
}

func (state *State) localProxyActionVisible(session *Session) bool {
	if session == nil || !session.host.isSSH() || session.host.mosh() || session.isLocal() || session.host.isDesktop() {
		return false
	}
	_, ok := state.localProxyTarget(session)
	return ok
}

func (state *State) clickLocalProxy(id gfx.ID) {
	switch id {
	case hitLocalProxyOpen:
		session, _ := state.sessionByIDPointer(state.activeSessionID)
		state.openLocalProxy(session)
	case hitLocalProxyDismiss, hitLocalProxyClose:
		state.closeModal()
	case hitLocalProxyDocker:
		state.localProxyUI.copyTarget = localProxyTarget{}
		state.localProxyUI.docker = true
		state.localProxyUI.scroll = 0
	case hitLocalProxyDockerBack:
		state.localProxyUI.docker = false
		state.localProxyUI.scroll = 0
	case hitLocalProxyMore:
		state.localProxyUI.more = !state.localProxyUI.more
		state.localProxyUI.scroll = 0
	case hitLocalProxyDockerDocs:
		if err := state.options.OpenURL(dockerProxyDocsURL); err != nil {
			state.localProxyUI.copyError = state.messages.LocalProxy.OpenDocsFailed
		}
	case hitLocalProxyStart:
		if state.startLocalProxy() {
			state.localProxyUI.copyTarget, _ = state.localProxyTarget(state.localProxySession())
		}
	case hitLocalProxyTest:
		if target, ok := state.localProxyTarget(state.localProxySession()); ok {
			state.localProxyManager().Test(target)
		}
	case hitLocalProxyStop:
		connected := state.localProxySnapshot(state.localProxySession()).Phase == localProxyConnected
		state.stopLocalProxy()
		if connected {
			state.copyLocalProxyAndReturn(id, localProxyDisableCommand(), state.messages.LocalProxy.StoppedNotice)
		}
	case hitLocalProxyEnableCopy:
		if port := state.localProxyCommandPort(); port != 0 {
			state.copyLocalProxyAndReturn(id, localProxyEnableCommand(port), state.messages.LocalProxy.SetupNotice)
		}
	case hitLocalProxyDisableCopy:
		state.copyLocalProxyAndReturn(id, localProxyDisableCommand(), state.messages.LocalProxy.RestoreNotice)
	case hitLocalProxyDockerConfig:
		if port := state.localProxyCommandPort(); port != 0 {
			state.copyLocalProxyText(id, localProxyDockerConfigCommand(port))
		}
	case hitLocalProxyDockerRestore:
		state.copyLocalProxyText(id, localProxyDockerRestoreCommand())
	}
}

func (state *State) localProxyPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if state.modal != modalLocalProxy || event.Kind != gfx.PointerUp || event.Button != gfx.PointerButtonPrimary ||
		event.Target != hitLocalProxyDismiss && event.Target != hitLocalProxyClose {
		return gfx.EventResult{}, false
	}
	state.clickLocalProxy(event.Target)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: state.activeTerminalFocusHit()}, true
}

func (state *State) localProxyCommandPort() uint16 {
	snapshot := state.localProxySnapshot(state.localProxySession())
	if snapshot.Phase == localProxyConnected {
		return snapshot.RemotePort
	}
	return 0
}

func (state *State) startLocalProxy() bool {
	session := state.localProxySession()
	target, ok := state.localProxyTarget(session)
	if !ok {
		state.localProxyUI.copyError = state.messages.LocalProxy.Unavailable
		return false
	}
	port, ok := state.localProxyPortValue()
	if !ok {
		state.localProxyUI.copyError = state.messages.LocalProxy.InvalidPort
		return false
	}
	state.localProxyPort = port
	state.localProxyPortSet = true
	state.localProxyUI.copyError = ""
	state.localProxyUI.retryCopy = gfx.ID{}
	state.localProxyUI.copied = gfx.ID{}
	state.localProxyUI.lastRemotePort = 0
	state.localProxyUI.lastStopped = false
	session.localProxy, _ = state.localProxyManager().Start(target, session.conn, port, session.localProxy)
	state.joinLocalProxyUsers(target)
	state.save()
	return true
}

// Only the panel that initiated this attempt may copy its result. Closing or
// replacing the panel must not overwrite the clipboard when startup finishes.
func (state *State) finishLocalProxyStart() {
	if state.modal != modalLocalProxy || state.localProxyUI.copyTarget.ConnectionGeneration == 0 {
		return
	}
	session := state.localProxySession()
	target, ok := state.localProxyTarget(session)
	if !ok || target != state.localProxyUI.copyTarget || session.ID != state.activeSessionID {
		state.localProxyUI.copyTarget = localProxyTarget{}
		return
	}
	snapshot := state.localProxyManager().Snapshot(target)
	if snapshot.Phase == localProxyConnecting {
		return
	}
	state.localProxyUI.copyTarget = localProxyTarget{}
	if snapshot.Phase == localProxyConnected {
		state.copyLocalProxyAndReturn(hitLocalProxyEnableCopy, localProxyEnableCommand(snapshot.RemotePort), state.messages.LocalProxy.SetupNotice)
	}
}

func (state *State) copyLocalProxyAndReturn(id gfx.ID, value, notice string) {
	if !state.copyLocalProxyText(id, value) {
		return
	}
	state.RevealSession(state.localProxyUI.session)
	focus := state.activeTerminalFocusHit()
	state.closeModal()
	state.localProxyUI.returnFocus = focus
	state.showActionInfo(state.messages.LocalProxy.Title, notice)
}

func (state *State) paintLocalProxyFocus(canvas *gfx.Canvas) {
	focus := state.localProxyUI.returnFocus
	state.localProxyUI.returnFocus = gfx.ID{}
	if !focus.IsZero() && state.modal == modalNone && state.activeSection == sectionTerminal && focus == state.activeTerminalFocusHit() {
		canvas.RequestFocus(focus)
	}
}

func (state *State) stopLocalProxy() {
	state.localProxyUI.copyTarget = localProxyTarget{}
	session := state.localProxySession()
	target, ok := state.localProxyTarget(session)
	if !ok {
		return
	}
	snapshot := state.localProxyManager().Snapshot(target)
	if snapshot.RemotePort != 0 {
		state.localProxyUI.lastRemotePort = snapshot.RemotePort
	}
	state.localProxyUI.lastStopped = snapshot.Phase != localProxyDisconnected
	state.localProxyManager().Stop(target)
	state.localProxyUI.copied = gfx.ID{}
	state.localProxyUI.copyError = ""
}

func (state *State) copyLocalProxyText(id gfx.ID, value string) bool {
	if value == "" {
		return false
	}
	if state.options.SetClipboardText == nil {
		state.localProxyUI.copyError = state.messages.LocalProxy.CopyUnavailable
		state.localProxyUI.retryCopy = id
		return false
	}
	if err := state.options.SetClipboardText(value); err != nil {
		state.localProxyUI.copied = gfx.ID{}
		state.localProxyUI.copyError = fmt.Sprintf(state.messages.LocalProxy.CopyFailedFmt, err)
		state.localProxyUI.retryCopy = id
		return false
	}
	state.localProxyUI.copied = id
	state.localProxyUI.copyError = ""
	state.localProxyUI.retryCopy = gfx.ID{}
	return true
}

func (state *State) localProxyKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalLocalProxy {
		return gfx.EventResult{}, false
	}
	if event.Release || event.Repeat {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape {
		if state.localProxyUI.docker {
			state.localProxyUI.docker = false
			state.localProxyUI.scroll = 0
			return gfx.EventResult{Handled: true, Repaint: true}, true
		} else {
			state.closeModal()
			return gfx.EventResult{Handled: true, Repaint: true, Focus: state.activeTerminalFocusHit()}, true
		}
	}
	if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && (event.Target == hitLocalProxyDismiss || event.Target == hitLocalProxyClose) {
		state.clickLocalProxy(event.Target)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: state.activeTerminalFocusHit()}, true
	}
	if event.Key == gfx.KeyEnter {
		switch event.Target {
		case hitLocalProxyPort, hitLocalProxyStart:
			if !state.localProxyUIPortLocked() {
				state.clickLocalProxy(hitLocalProxyStart)
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
		case hitLocalProxyTest:
			if state.localProxySnapshot(state.localProxySession()).Phase == localProxyConnected {
				state.clickLocalProxy(hitLocalProxyTest)
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
		case hitLocalProxyStop:
			state.clickLocalProxy(hitLocalProxyStop)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	area := components.BeginScrollArea(nil, state.Theme(), state.localProxyUI.scrollGeometry.Bounds, state.localProxyContentSize())
	x, y, handled := area.ScrollForKey(0, state.localProxyUI.scroll, event.Key, 32)
	if handled {
		state.localProxyUI.scroll = y
		return gfx.EventResult{Handled: true, Repaint: x != 0 || y != 0}, true
	}
	return gfx.EventResult{}, false
}

func (state *State) scrollLocalProxy(event gfx.ScrollEvent) gfx.EventResult {
	before := state.localProxyUI.scroll
	area := components.BeginScrollArea(nil, state.Theme(), state.localProxyUI.scrollGeometry.Bounds, state.localProxyContentSize())
	state.localProxyUI.scroll = area.Offset(0, before+event.DeltaY).Geometry().OffsetY
	return gfx.EventResult{Handled: true, Repaint: before != state.localProxyUI.scroll}
}

func (state *State) localProxyContentSize() gfx.Size {
	geometry := state.localProxyUI.scrollGeometry
	return gfx.Size{Width: geometry.Content.Width, Height: max(geometry.Content.Height, localProxyMainHeight)}
}

func (state *State) localProxyContentHeight(snapshot localProxySnapshot) float32 {
	if state.localProxyUI.docker {
		return localProxyDockerHeight
	}
	height := localProxyMainHeight
	if snapshot.Phase == localProxyConnected || snapshot.Phase == localProxyConnecting {
		height += 100
	}
	if state.localProxyUI.more {
		height += 450
	}
	if state.localProxyUI.copyError != "" {
		height += 45
	}
	return height
}

func (state *State) localProxyPhaseLine(snapshot localProxySnapshot) (string, gfx.Color) {
	m, pal := state.messages.LocalProxy, state.pal
	switch snapshot.Phase {
	case localProxyConnecting:
		return m.Starting, pal.accent
	case localProxyConnected:
		return m.Connected, pal.success
	case localProxyFailed:
		return state.localProxyErrorText(snapshot.Err), pal.danger
	default:
		if state.localProxyUI.lastStopped && state.localProxyUI.lastRemotePort != 0 {
			return fmt.Sprintf(m.StoppedFmt, state.localProxyUI.lastRemotePort), pal.warning
		}
		return m.StartHint, pal.muted
	}
}

func (state *State) localProxyErrorText(err error) string {
	m := state.messages.LocalProxy
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errLocalProxyUnavailable):
		return m.Unavailable
	case errors.Is(err, errLocalProxyInvalidPort):
		return m.InvalidPort
	case errors.Is(err, errLocalProxyProbe):
		return m.ProxyDialFailed
	case errors.Is(err, errLocalProxyForwarding):
		return m.ForwardingRefused
	case errors.Is(err, errLocalProxyBinding):
		return m.BindingRejected
	case errors.Is(err, errLocalProxySSHDisconnected), errors.Is(err, sshconn.ErrDisconnected):
		return m.SSHDisconnected
	case errors.Is(err, errLocalProxyAuth):
		return m.ProxyAuthRequired
	case errors.Is(err, errLocalProxyTunnel):
		return m.TestFailed
	default:
		return m.ProtocolMismatch
	}
}

func (state *State) localProxyServerText(session *Session) string {
	if session == nil {
		return state.messages.LocalProxy.Unavailable
	}
	return fmt.Sprintf(state.messages.LocalProxy.ServerFmt, state.sessionAddress(session.host))
}

// declareLocalProxyDialog declares the proxy dialog in the window page: the
// scrolling body and its rows are page elements, and the field, phase line and
// buttons are painted into the slots they leave.
func (state *State) declareLocalProxyDialog(c ui.C) {
	m, theme := state.messages.LocalProxy, state.Theme()
	session := state.localProxySession()
	snapshot := state.localProxySnapshot(session)
	if snapshot.RemotePort != 0 {
		state.localProxyUI.lastRemotePort = snapshot.RemotePort
	}
	title, description, closeID := m.Title, state.localProxyServerText(session), hitLocalProxyClose
	if state.localProxyUI.docker {
		title, description, closeID = m.DockerTitle, m.DockerHint, hitLocalProxyDockerBack
	}
	contentHeight := state.localProxyContentHeight(snapshot)
	width := float32(520)
	if state.localProxyUI.docker {
		width = 620
	}
	initialFocus := hitLocalProxyPort
	if snapshot.Phase == localProxyConnected {
		initialFocus = hitLocalProxyEnableCopy
	} else if snapshot.Phase == localProxyConnecting {
		initialFocus = hitLocalProxyStop
	}
	if state.localProxyUI.docker {
		initialFocus = hitLocalProxyDockerBack
	}
	viewport := dialogViewport(c)
	width = min(width, max(float32(360), viewport.Width-32))
	bodyHeight := min(contentHeight, max(float32(180), viewport.Height-210))
	height := dialogContentHeight(c.Canvas(), theme, viewport, width, description, bodyHeight)
	// The body scrolls inside the dialog; its geometry is what the declared
	// content and the scrollbar painted after the page both use.
	body := dialogContentRect(c.Canvas(), theme, viewport, width, description, bodyHeight).Body
	contentSize := gfx.Size{Width: max(body.Width-10, 0), Height: contentHeight}
	area := components.BeginScrollArea(nil, theme, body, contentSize).Offset(0, state.localProxyUI.scroll).ReserveScrollbars()
	proxy := &state.localProxyUI
	proxy.scrollGeometry = area.Geometry()
	proxy.scroll = proxy.scrollGeometry.OffsetY
	proxy.viewportTitle = title
	if proxy.viewportDraw == nil {
		proxy.viewportDraw, proxy.controlsDraw = state.drawLocalProxyViewport, state.drawLocalProxyControls
	}
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: description, Width: width, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitLocalProxyDismiss, CloseID: closeID, InitialFocusID: initialFocus,
	})
	dialog.Body(func() {
		bodyViewport := ui.Column(c).Fill().Clip()
		bodyViewport.Draw(proxy.viewportDraw)
		bodyViewport.Children(func() {
			content := ui.Column(c).Width(proxy.scrollGeometry.Content.Width).Height(contentHeight).
				MarginTop(-proxy.scrollGeometry.OffsetY).PadSides(2, 4, 10, 6).Gap(8)
			content.Children(func() {
				if proxy.docker {
					state.declareLocalProxyDockerBody(c, snapshot)
					return
				}
				state.declareLocalProxyMainBody(c, snapshot)
			})
			content.DrawOver(proxy.controlsDraw)
		})
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c, state.localProxyActions(snapshot)...)
	})
}

// drawLocalProxyViewport names the dialog body's scroll viewport for the
// keyboard and the wheel, under the controls the page paints into it.
func (state *State) drawLocalProxyViewport(d *ui.DrawContext) {
	geometry := state.localProxyUI.scrollGeometry
	if geometry.Viewport.Width <= 0 || geometry.Viewport.Height <= 0 {
		return
	}
	d.Canvas.HitWithOptions(hitLocalProxyScroll, geometry.Viewport, state.localProxyUI.viewportTitle, gfx.HitOptions{Role: gfx.RoleGroup})
}

// drawLocalProxyControls paints the field, phase line and buttons into the slots
// the body's declaration left.
func (state *State) drawLocalProxyControls(d *ui.DrawContext) {
	snapshot := state.localProxySnapshot(state.localProxySession())
	if state.localProxyUI.docker {
		state.paintLocalProxyDockerControls(d.Canvas, snapshot)
		return
	}
	state.paintLocalProxyMainControls(d.Canvas, snapshot)
}

// paintLocalProxyScrollbar paints the body's scrollbar over the page: the thumb
// is a hit of its own, outside the content.
func (state *State) paintLocalProxyScrollbar(canvas *gfx.Canvas) {
	proxy := &state.localProxyUI
	geometry := proxy.scrollGeometry
	if geometry.Bounds.Width <= 0 || geometry.Bounds.Height <= 0 {
		return
	}
	area := components.BeginScrollArea(canvas, state.Theme(), geometry.Bounds, state.localProxyContentSize()).
		Offset(0, proxy.scroll).VerticalScrollbar(hitLocalProxyScroll, proxy.viewportTitle, "").ReserveScrollbars()
	area.EndContent()
}

func (state *State) localProxyActions(snapshot localProxySnapshot) []components.ButtonAction {
	m := state.messages.LocalProxy
	if state.localProxyUI.docker {
		return []components.ButtonAction{
			{ID: hitLocalProxyDockerBack, Label: m.TerminalUsage, Variant: components.ButtonSecondary},
			{ID: hitLocalProxyClose, Label: m.BackToTerminal, Variant: components.ButtonOutline},
		}
	}
	switch snapshot.Phase {
	case localProxyConnecting:
		return []components.ButtonAction{{ID: hitLocalProxyStop, Label: state.messages.Cancel, Variant: components.ButtonSecondary}}
	case localProxyConnected:
		return []components.ButtonAction{
			{ID: hitLocalProxyStop, Label: m.Stop, Variant: components.ButtonDestructiveOutline},
			{ID: hitLocalProxyEnableCopy, Label: m.EnableCopy, Variant: components.ButtonDefault},
		}
	default:
		if state.localProxyUI.retryCopy == hitLocalProxyStop || state.localProxyUI.retryCopy == hitLocalProxyDisableCopy {
			return []components.ButtonAction{
				{ID: hitLocalProxyStart, Label: m.Start, Variant: components.ButtonSecondary},
				{ID: hitLocalProxyDisableCopy, Label: m.DisableCopy, Variant: components.ButtonDefault},
			}
		}
		return []components.ButtonAction{{ID: hitLocalProxyStart, Label: m.Start, Variant: components.ButtonDefault}}
	}
}

// declareLocalProxyMainBody declares the main view's rows in the dialog body and
// keeps the slots its hand-painted controls go in.
func (state *State) declareLocalProxyMainBody(c ui.C, snapshot localProxySnapshot) {
	m, pal, theme := state.messages.LocalProxy, state.pal, state.Theme()
	proxy := &state.localProxyUI
	proxy.portField, proxy.phaseLine = ui.Box{}, ui.Box{}
	proxy.dockerButton, proxy.moreButton, proxy.testButton, proxy.restoreButton = ui.Box{}, ui.Box{}, ui.Box{}, ui.Box{}
	fieldHeight := float32(60)
	if snapshot.Phase == localProxyConnecting || snapshot.Phase == localProxyConnected {
		fieldHeight = 90
	}
	ui.Row(c).Height(fieldHeight).Children(func() { proxy.portField = ui.Slot(c).Grow(1) })
	ui.Wrap(c, m.ScopeWarning).FontSize(theme.Metrics.FontSizeS).Color(pal.muted).LineHeight(18)
	ui.Wrap(c, m.AccessWarning).FontSize(theme.Metrics.FontSizeS).Color(pal.muted).LineHeight(18)
	if snapshot.Phase != localProxyDisconnected || state.localProxyUI.lastStopped {
		ui.Row(c).Height(24).Children(func() { proxy.phaseLine = ui.Slot(c).Grow(1) })
	}
	if snapshot.Phase == localProxyConnected {
		ui.Wrap(c, m.StopHint).FontSize(theme.Metrics.FontSizeS).Color(pal.muted).LineHeight(18)
	}
	if state.localProxyUI.copyError != "" {
		ui.Wrap(c, state.localProxyUI.copyError).FontSize(theme.Metrics.FontSizeS).Color(pal.danger).LineHeight(18)
	}
	ui.Row(c).Height(32).Gap(8).Children(func() {
		proxy.dockerButton = ui.Slot(c).Grow(1)
		proxy.moreButton = ui.Slot(c).Grow(1)
	})
	if state.localProxyUI.more {
		ui.Row(c).Height(34).Gap(8).Children(func() {
			proxy.testButton = ui.Slot(c).Grow(1)
			proxy.restoreButton = ui.Slot(c).Grow(1)
		})
		if snapshot.TestPhase != localProxyTestIdle {
			line, color := state.localProxyTestLine(snapshot)
			ui.Wrap(c, line).FontSize(theme.Metrics.FontSizeS).Color(color).LineHeight(18)
		}
		ui.Text(c, m.HowItWorks).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text)
		ui.Wrap(c, m.Flow).FontSize(theme.Metrics.FontSizeS).Color(pal.text).LineHeight(18)
		ui.Wrap(c, m.TerminalHint).FontSize(theme.Metrics.FontSizeS).Color(pal.muted).LineHeight(18)
		ui.Wrap(c, m.ProtocolHint).FontSize(theme.Metrics.FontSizeS).Color(pal.muted).LineHeight(18)
		ui.Wrap(c, m.GitSSHWarning).FontSize(theme.Metrics.FontSizeS).Color(pal.muted).LineHeight(18)
		if snapshot.RemotePort != 0 {
			ui.Wrap(c, fmt.Sprintf(m.RemoteAddressFmt, snapshot.RemotePort)).FontSize(theme.Metrics.FontSizeS).Color(pal.text).LineHeight(18)
		}
	}
}

// paintLocalProxyMainControls paints the main view's field, phase line and
// buttons into the slots its declaration left.
func (state *State) paintLocalProxyMainControls(canvas *gfx.Canvas, snapshot localProxySnapshot) {
	m, theme := state.messages.LocalProxy, state.Theme()
	proxy := &state.localProxyUI
	state.paintLocalProxyPortField(canvas, proxy.portField.Rect(), snapshot, m.PortLabel, m.PortHint, theme)
	if snapshot.Phase != localProxyDisconnected || state.localProxyUI.lastStopped {
		line, color := state.localProxyPhaseLine(snapshot)
		state.paintLocalProxyPhase(canvas, proxy.phaseLine.Rect(), line, color, theme)
	}
	components.BeginButton(canvas, theme, hitLocalProxyDocker, proxy.dockerButton.Rect()).Link(m.DockerTitle)
	label, icon := m.More, uint8(iconChevronRight)
	if state.localProxyUI.more {
		label, icon = m.Less, iconChevronDown
	}
	components.BeginButton(canvas, theme, hitLocalProxyMore, proxy.moreButton.Rect()).WithIcon(lucideIcons[icon]).Link(label)
	if state.localProxyUI.more {
		button := components.BeginButton(canvas, theme, hitLocalProxyTest, proxy.testButton.Rect())
		if snapshot.Phase != localProxyConnected || snapshot.TestPhase == localProxyTestRunning {
			button = button.Disabled()
		}
		button.Outline(m.Test)
		components.BeginButton(canvas, theme, hitLocalProxyDisableCopy, proxy.restoreButton.Rect()).Outline(m.DisableCopy)
	}
}

func (state *State) localProxyTestLine(snapshot localProxySnapshot) (string, gfx.Color) {
	switch snapshot.TestPhase {
	case localProxyTestRunning:
		return state.messages.LocalProxy.Testing, state.pal.accent
	case localProxyTestSucceeded:
		return state.messages.LocalProxy.TestSucceeded, state.pal.success
	default:
		return state.localProxyErrorText(snapshot.TestErr), state.pal.danger
	}
}

func (state *State) paintLocalProxyPortField(canvas *gfx.Canvas, bounds gfx.Rect, snapshot localProxySnapshot, label, hint string, theme gfx.Theme) {
	locked := snapshot.Phase == localProxyConnecting || snapshot.Phase == localProxyConnected
	description := ""
	if locked {
		description = state.messages.LocalProxy.PortLockedHint
		hint = description
	}
	field := components.BeginField(canvas, theme, bounds).For(hitLocalProxyPort).Paint(label, description, "")
	value := state.localProxyUI.port.String()
	focused := canvas.Focused() == hitLocalProxyPort && !locked
	components.PaintInput(canvas, theme, field, components.InputSnapshot{
		ID: hitLocalProxyPort, Label: label, Value: value, Placeholder: strconv.Itoa(int(defaultLocalProxyPort)),
		Hint: hint, Focused: focused, PlatformEditing: focused && value != "",
		Disabled: locked,
	})
}

func (state *State) paintLocalProxyPhase(canvas *gfx.Canvas, bounds gfx.Rect, line string, color gfx.Color, theme gfx.Theme) {
	icon := uint8(iconCircleCheck)
	if color == state.pal.warning || color == state.pal.danger || color == state.pal.muted {
		icon = iconAlert
	}
	canvas.DrawIcon(lucideIcons[icon], gfx.Rect{X: bounds.X, Y: bounds.Y + 3, Width: 16, Height: 16}, color)
	canvas.Label(canvas.FitLine(line, theme.Metrics.FontSizeS, gfx.WeightMedium, max(bounds.Width-22, 0)), gfx.Rect{
		X: bounds.X + 22, Y: bounds.Y, Width: max(bounds.Width-22, 0), Height: bounds.Height,
	}, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Weight: gfx.WeightMedium, Color: color})
}

func (state *State) localProxyCopyLabel(id gfx.ID, normal, copied string) string {
	if state.localProxyUI.copied == id {
		return copied
	}
	return normal
}

// declareLocalProxyDockerBody declares the Docker view's rows in the dialog body
// and keeps the slots its hand-painted buttons go in.
func (state *State) declareLocalProxyDockerBody(c ui.C, snapshot localProxySnapshot) {
	m, pal, theme := state.messages.LocalProxy, state.pal, state.Theme()
	proxy := &state.localProxyUI
	proxy.configButton, proxy.docsButton, proxy.dockerRestoreButton = ui.Box{}, ui.Box{}, ui.Box{}
	port := state.localProxyCommandPort()
	ui.Wrap(c, m.DockerScope).FontSize(theme.Metrics.FontSizeS).Color(pal.warning).LineHeight(18)
	ui.Wrap(c, m.DockerWarning).FontSize(theme.Metrics.FontSizeS).Color(pal.danger).LineHeight(18)
	ui.Text(c, m.DockerSteps).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text)
	if port != 0 {
		for _, line := range strings.Split(fmt.Sprintf(m.DockerConfigFmt, port, port), "\n") {
			ui.Text(c, line).Mono().FontSize(11).Color(pal.text).LineHeight(16)
		}
	}
	ui.Row(c).Height(34).Gap(8).Children(func() {
		proxy.configButton = ui.Slot(c).Grow(1)
		proxy.docsButton = ui.Slot(c).Grow(1)
	})
	ui.Wrap(c, m.DockerRestoreFmt).FontSize(theme.Metrics.FontSizeS).Color(pal.muted).LineHeight(18)
	for _, line := range strings.Split(localProxyDockerRestoreCommand(), "\n") {
		ui.Text(c, line).Mono().FontSize(11).Color(pal.text).LineHeight(16)
	}
	ui.Row(c).Height(34).Children(func() { proxy.dockerRestoreButton = ui.Slot(c).Grow(1) })
	if state.localProxyUI.copyError != "" {
		ui.Text(c, state.localProxyUI.copyError).FontSize(theme.Metrics.FontSizeS).Color(pal.danger)
	}
}

// paintLocalProxyDockerControls paints the Docker view's buttons into the slots
// its declaration left.
func (state *State) paintLocalProxyDockerControls(canvas *gfx.Canvas, snapshot localProxySnapshot) {
	m, theme := state.messages.LocalProxy, state.Theme()
	proxy := &state.localProxyUI
	port := state.localProxyCommandPort()
	if port != 0 {
		components.BeginButton(canvas, theme, hitLocalProxyDockerConfig, proxy.configButton.Rect()).Outline(state.localProxyCopyLabel(hitLocalProxyDockerConfig, m.DockerCopy, m.DockerCopied))
	}
	components.BeginButton(canvas, theme, hitLocalProxyDockerDocs, proxy.docsButton.Rect()).Outline(m.OpenDockerDocs)
	components.BeginButton(canvas, theme, hitLocalProxyDockerRestore, proxy.dockerRestoreButton.Rect()).Outline(state.localProxyCopyLabel(hitLocalProxyDockerRestore, m.DockerCopy, m.DockerCopied))
}

func localProxyDockerConfigCommand(port uint16) string {
	if port == 0 {
		return ""
	}
	return fmt.Sprintf("sudo mkdir -p /etc/systemd/system/docker.service.d\nsudo tee /etc/systemd/system/docker.service.d/motty-proxy.conf >/dev/null <<'EOF'\n[Service]\nEnvironment=\"HTTP_PROXY=http://127.0.0.1:%d\"\nEnvironment=\"HTTPS_PROXY=http://127.0.0.1:%d\"\nEOF\nsudo systemctl daemon-reload\nsudo systemctl restart docker", port, port)
}

func localProxyDockerRestoreCommand() string {
	return "sudo rm -f /etc/systemd/system/docker.service.d/motty-proxy.conf\nsudo systemctl daemon-reload\nsudo systemctl restart docker"
}
