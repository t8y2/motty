package workbench

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// A connection goes through four stages. The page shown while it is made keeps
// one row for each, so the user always sees where it is, what it is waiting
// for, and, when it fails, which stage that was.
const (
	stageConnect = iota // reach the server and exchange keys
	stageHostKey        // check the key the server presented
	stageLogin          // prove who the user is
	stageShell          // ask for a terminal and wait for it to speak
	stageCount
)

// shellGrace is how long an open shell may stay silent before the terminal
// replaces the page anyway: a shell that prints nothing until it is typed to
// must not keep the user away from its keyboard.
const shellGrace = 1500 * time.Millisecond

// stepState is how one row of the page looks.
type stepState uint8

const (
	stepPending stepState = iota
	stepActive            // working on it
	stepWaiting           // waiting for the user
	stepDone
	stepFailed
)

// now is the workbench's monotonic clock. A State built by hand, without
// options, has none and lives at time zero.
func (state *State) now() time.Duration {
	if state.options.Now == nil {
		return 0
	}
	return state.options.Now()
}

// failed reports whether the connection, or its shell, ended in an error: the
// page then says why and offers another try.
func (session *Session) failed() bool {
	return session.phase == sshconn.PhaseFailed || session.phase == sshconn.PhaseClosed && session.err != nil || session.term != nil && session.term.ended && !session.term.live
}

// stepOf is how row stage looks for a session.
func (session *Session) stepOf(stage int) stepState {
	failedAt := session.stage
	switch {
	case session.failed():
		switch {
		case stage < failedAt:
			return stepDone
		case stage == failedAt:
			return stepFailed
		}
		return stepPending
	case stage < session.stage:
		return stepDone
	case stage > session.stage:
		return stepPending
	case session.phase == sshconn.PhaseHostKeyPrompt || session.phase == sshconn.PhaseKeyPassphrasePrompt || session.phase == sshconn.PhasePasswordPrompt:
		return stepWaiting
	}
	return stepActive
}

// showsProgress reports whether a session shows its connection page rather
// than its terminal: from the dial until the shell first speaks, and for a
// connection that failed. The log, which is the terminal's screen, is one
// button away either way.
func (state *State) showsProgress(session *Session) bool {
	t := session.term
	if t == nil || session.showLog || t.live {
		return t != nil && !session.showLog && session.reconnectPending
	}
	if session.isLocal() && session.quietOK() {
		return false // a shell on this machine starts at once: there is nothing to show progress of
	}
	if state.paneInSplit(session.ID) && session.quietOK() {
		return false
	}
	return t.shell == nil || state.now()-t.shellOpened < shellGrace
}

// quietOK reports whether a session in a split may stay on its terminal. A
// connection that is waiting for the user (a host key, a password, a passphrase)
// or that failed needs its page to say so.
func (session *Session) quietOK() bool {
	return !session.failed() && !isPromptPhase(session.phase)
}

// describe writes what each row of the page says, in the current language. It
// runs when the connection moves on and when the language changes, so painting
// only reads the result.
func (state *State) describe(session *Session) {
	m := state.messages
	hostPort := fmt.Sprintf("%s:%d", session.host.Address, session.host.Port)
	if session.host.Protocol == protocolSerial {
		hostPort = session.host.Address
	}
	jump := ""
	if session.role == sshconn.EndpointProxyJump && session.endpoint.Host != "" {
		jump = formatEndpoint(session.endpoint)
		if session.jumpPath != "" {
			jump += " · " + session.jumpPath
		}
	}
	session.elapsed = "" // rebuilt by the next paint, in this language

	switch {
	case session.role == sshconn.EndpointProxyJump:
		switch session.phase {
		case sshconn.PhaseConnecting:
			session.details[stageConnect] = fmt.Sprintf(m.ConnDetailJumpDialingFmt, jump)
		case sshconn.PhaseHostKeyPrompt:
			session.details[stageConnect] = fmt.Sprintf(m.ConnDetailJumpKeyFmt, jump)
		case sshconn.PhaseAuthenticating, sshconn.PhaseKeyPassphrasePrompt, sshconn.PhasePasswordPrompt:
			session.details[stageConnect] = fmt.Sprintf(m.ConnDetailJumpLoginFmt, jump)
		default:
			session.details[stageConnect] = jump
		}
	case session.serverVersion != "" && !session.failed():
		session.details[stageConnect] = fmt.Sprintf(m.ConnDetailServerFmt, session.serverVersion)
	case session.failed() && session.stage == stageConnect:
		if session.jumpPath != "" {
			hostPort = fmt.Sprintf(m.ConnDetailConnectingViaFmt, hostPort, session.jumpPath)
		}
		session.details[stageConnect] = hostPort // no longer "connecting": it did not work
	case session.jumpPath != "" && session.stage > stageConnect:
		session.details[stageConnect] = fmt.Sprintf(m.ConnDetailJumpReadyFmt, session.jumpPath)
	default:
		// The card's header already names the address; this line says what is
		// being waited for, and the way there when there is a jump host.
		session.details[stageConnect] = m.ConnDetailDialing
		if session.jumpPath != "" {
			session.details[stageConnect] = fmt.Sprintf(m.ConnDetailDialingViaFmt, session.jumpPath)
		}
	}
	if session.reconnectAttempt > 0 && !session.reconnectPending && session.phase != sshconn.PhaseConnected {
		retrying := fmt.Sprintf(m.ConnReconnectTryingFmt, session.reconnectAttempt)
		if session.details[stageConnect] != "" {
			retrying += " · " + session.details[stageConnect]
		}
		session.details[stageConnect] = retrying
	}

	session.details[stageHostKey] = ""
	if !session.host.isSSH() && !session.host.tunneled() {
		// Nothing to verify and no one to log in to: whatever asks for a login
		// does so in the terminal.
		session.details[stageLogin] = ""
		session.details[stageShell] = ""
		session.failure = ""
		if session.failed() {
			session.failure = state.failureLine(session)
		}
		return
	}
	if session.role == sshconn.EndpointProxyJump {
		session.details[stageLogin] = ""
		session.details[stageShell] = ""
		session.failure = ""
		if session.failed() {
			session.failure = state.failureLine(session)
		}
		return
	}
	switch key := session.hostKey; {
	case session.phase == sshconn.PhaseHostKeyPrompt:
		session.details[stageHostKey] = fmt.Sprintf(m.ConnDetailKeyAskFmt, key.Type, key.Fingerprint)
	case key.Fingerprint != "" && session.stage > stageHostKey:
		session.details[stageHostKey] = key.Type + " " + key.Fingerprint
	case session.stage > stageHostKey:
		session.details[stageHostKey] = m.ConnDetailKnownHost
	}

	session.details[stageLogin] = ""
	switch {
	case session.phase == sshconn.PhasePasswordPrompt:
		session.details[stageLogin] = m.ConnDetailPasswordAsk
	case session.phase == sshconn.PhaseKeyPassphrasePrompt:
		session.details[stageLogin] = fmt.Sprintf(m.ConnDetailKeyPassphraseFmt, state.identityLabel(session.identity))
	case session.phase == sshconn.PhaseConnected && session.method != "":
		session.details[stageLogin] = fmt.Sprintf(m.ConnDetailMethodFmt, session.method)
	case session.stage == stageLogin:
		session.details[stageLogin] = m.ConnDetailAgent
	case session.stage > stageLogin && session.method != "":
		session.details[stageLogin] = fmt.Sprintf(m.ConnDetailMethodFmt, session.method)
	}

	session.details[stageShell] = ""
	if session.stage == stageShell && !session.failed() {
		session.details[stageShell] = m.ConnDetailShellOpening
		if session.term != nil && session.term.shell != nil {
			session.details[stageShell] = m.ConnDetailShellWaiting
		}
	}

	session.failure = ""
	if session.failed() {
		session.failure = state.failureLine(session)
	}
}

// failureLine is one line saying why a connection failed.
func (state *State) failureLine(session *Session) string {
	m, err := state.messages, session.err
	switch {
	case err == nil:
		return ""
	case session.shellFailed:
		return fmt.Sprintf(m.ConnShellFailedFmt, err)
	case errors.Is(err, sshconn.ErrHostKeyChanged):
		return m.ConnChangedShort
	case errors.Is(err, sshconn.ErrHostKeyRejected):
		return m.ConnRejected
	case errors.Is(err, sshconn.ErrCanceled):
		return m.ConnCanceled
	case errors.Is(err, sshconn.ErrAuthFailed):
		return m.ConnAuthFailed
	}
	// The page and the desktop overlay both say what ended; this is only why,
	// so it is the error as the system reported it, with no second "failed".
	return err.Error()
}

// Card metrics, in logical pixels.
const (
	cardWidth  = float32(508)
	cardPad    = float32(28)
	cardHeader = float32(106)
	cardFooter = float32(64)

	// While a connection is being made the title's ellipsis bounces, and the
	// rail eases to each new stage. One frame per connectionSignalStep keeps both
	// smooth; nothing asks for frames once they settle or the user prefers
	// reduced motion.
	connectionDotsPeriod = 1200 * time.Millisecond
	connectionSignalStep = 16 * time.Millisecond

	// The reason for a failure sits in a tinted box of up to failureLines lines.
	failureLines  = 3
	failureLeadin = float32(17)
	failureInset  = float32(11)
)

// A session pane is part of the workbench page: the connection, restored and
// desktop bodies are declared in the pane's keyed container, so the page names
// their controls and the window paints the few parts they leave by hand. The
// scratch each declaration leaves is kept per pane slot, because a split
// declares every visible pane's body in one paint.

// paneBody is the declaration scratch of the session's pane.
func (state *State) paneBody(session *Session) *sessionPaneUI {
	slot := int(session.paneSlot)
	if slot < 0 || slot >= len(state.sessionBodies) {
		slot = 0
	}
	return &state.sessionBodies[slot]
}

// panePartFor is the part naming the session's pane container. It is cached per
// pane because a new name is a string built from the session number, and the
// container is declared on every paint.
func (state *State) panePartFor(session *Session) string {
	s := state.paneBody(session)
	if s.part == "" || s.partSession != session.ID {
		s.part, s.partSession = terminalPageRoot(session.ID), session.ID
	}
	return s.part
}

// sessionButton is one button of a session pane: its identity as the pane paints
// it, label, size and variant, painted into the slot the declaration left.
type sessionButton struct {
	id      gfx.ID
	label   string
	size    components.ButtonSize
	variant components.ButtonVariant
}

// sessionPaneUI is the declaration scratch of one session's pane body: the boxes
// the page laid out and the window paints into.
type sessionPaneUI struct {
	// part and partSession name the pane's container in the page's identity tree.
	part        string
	partSession uint32
	// declared says the page declared this pane's body in the current paint. A
	// click delivered during the paint can create a session after the pane loop
	// ran; its body appears on the repaint, and until then the window must not
	// paint boxes from an older paint.
	declared bool
	// buttons and buttonBoxes are the footer's buttons and their slots, reset
	// when the body is declared.
	buttons     []sessionButton
	buttonBoxes []ui.Box
	// avatar and failing are the connection card's badges; track and dots its
	// rail; rail, railTarget and railIcon the rail's own layout.
	avatar, failing, track, dots ui.Box
	rail, railTarget, railIcon   ui.Box
	// keysClose and keysConnect are the restored pane's key hints.
	keysClose, keysConnect ui.Box
	closeHint, connectHint []string
	closeKeys, connectKeys [6]string
	// light, tools, capture and latency are the desktop bars' slots, and
	// desktopTools the controls they hold.
	light, tools, capture, latency ui.Box
	desktopTools                   []components.ButtonAction
	latencyBadge                   desktopLatencyUI
	latencyShown                   bool
}

// declareSessionPane declares a session the terminal control does not show: its
// connection, restored or desktop body, in the pane's container.
func (state *State) declareSessionPane(c ui.C, session *Session, bounds gfx.Rect) {
	switch {
	case session.restored:
		state.declareRestoredSessionBody(c, session, bounds)
	case state.showsProgress(session):
		state.declareConnectionBody(c, session, bounds)
	case session.desk != nil:
		state.declareDesktopBody(c, session, bounds)
	}
}

// paintSessionPaneDetails paints what a session pane's declaration left by hand:
// its badges, buttons, rail and, for a remote desktop, the picture.
func (state *State) paintSessionPaneDetails(canvas *gfx.Canvas, session *Session, bounds gfx.Rect) {
	if !state.paneBody(session).declared {
		return
	}
	switch {
	case session.restored:
		state.paintRestoredSessionDetails(canvas, session)
	case state.showsProgress(session):
		state.paintConnectionDetails(canvas, session)
	case session.desk != nil:
		state.paintDesktopDetails(canvas, session, bounds)
	}
}

func (state *State) paintConnectionDetails(canvas *gfx.Canvas, session *Session) {
	pal := state.pal
	s := state.paneBody(session)

	step := session.stepOf(connectionDisplayStage(session))
	if session.failed() {
		step = stepFailed
	}
	state.paintSessionBadge(canvas, session, s.avatar.Rect())
	state.paintSessionButtons(canvas, session)
	if step == stepFailed {
		paintIcon(canvas, iconAlert, s.failing.Rect(), pal.danger)
	}
	state.easeRail(canvas, session, step)
	if step != stepFailed {
		state.paintConnectionTrack(canvas, session, step, session.railShown, session.host.System)
	}
	if step == stepActive && !canvas.ReduceMotion() {
		paintBouncingDots(canvas, s.dots.Rect(), pal.muted)
		canvas.RequestFrameIn(connectionSignalStep)
	}
	// A shell that stays silent hands the window over to the terminal.
	if t := session.term; t != nil && t.shell != nil {
		canvas.RequestFrameIn(max(shellGrace-(state.now()-t.shellOpened), time.Millisecond))
	}
}

// declareConnectionBody declares the connection card and the rail it holds.
// The inputs are the session's, and the boxes it leaves are painted after the
// page is laid out.
func (state *State) declareConnectionBody(c ui.C, session *Session, frame gfx.Rect) {
	canvas := c.Canvas()
	pal, m := state.pal, state.messages
	s := state.paneBody(session)
	s.declared = true
	s.avatar, s.failing, s.track, s.dots, s.rail, s.railTarget, s.railIcon = ui.Box{}, ui.Box{}, ui.Box{}, ui.Box{}, ui.Box{}, ui.Box{}, ui.Box{}

	failed := session.failed()
	stage := connectionDisplayStage(session)
	step := session.stepOf(stage)
	if failed {
		step = stepFailed
	}

	// A short window trades air for room: these are the sizes the card has at each
	// height, not positions.
	width := min(cardWidth, max(frame.Width-32, 240))
	innerWidth := width - 2*cardPad
	headerHeight, statusHeight := cardHeader, float32(126)
	footerGap, footerHeight := float32(7), cardFooter
	reasonLines, reasonInset, reasonLeadin := failureLines, failureInset, failureLeadin
	avatarSize, avatarTop := float32(46), float32(24)
	// A failure ends the journey the rail describes, so the panel becomes the
	// result: a mark, what failed, the endpoint, and how long it took.
	failureHeight := float32(86)
	if frame.Height < 460 {
		headerHeight, statusHeight = 88, 104
		footerGap, footerHeight = 5, 52
		reasonLines, reasonInset, reasonLeadin = 2, 8, 16
		avatarSize, avatarTop = 40, 18
		failureHeight = 76
	}
	if frame.Height < 360 {
		headerHeight, statusHeight = 78, 92
		footerGap, footerHeight = 4, 48
		reasonLines, reasonInset, reasonLeadin = 1, 6, 15
		avatarSize, avatarTop = 36, 16
		failureHeight = 66
	}
	var reason []string
	if failed && session.failure != "" {
		reason = canvas.WrapLines(state.wrapped[:0], session.failure, metrics.FontSizeS, gfx.WeightRegular, innerWidth-24, reasonLines)
	}

	titles := [stageCount]string{m.ConnStepConnect, m.ConnStepHostKey, m.ConnStepLogin, m.ConnStepShell}
	// Working and waiting stay in the page's own ink; only an ending is colored,
	// and only faintly, so the panel is never a patch of amber.
	var ending gfx.Color
	switch step {
	case stepFailed:
		ending = withAlpha(pal.danger, 0.06)
	case stepDone:
		ending = withAlpha(pal.success, 0.06)
	}
	panelFill := pal.surfaceSoft
	panelFill.Alpha *= 0.72
	statusTitle := m.HostStateConnecting
	switch step {
	case stepWaiting:
		statusTitle = titles[stage]
	case stepFailed:
		statusTitle = m.HostStateFailed
	case stepDone:
		statusTitle = m.HostStateConnected
	}
	detail := elide(canvas, session.details[stage], metrics.FontSizeS, gfx.WeightRegular, innerWidth-36)
	footerText := state.reconnectStatusText(canvas, session)
	if footerText == "" && !failed {
		footerText = state.elapsedText(canvas, session)
	}
	labelWidth := max(innerWidth-avatarSize-16, 0)
	title := elide(canvas, session.Title, 16, gfx.WeightSemibold, labelWidth)
	address := elide(canvas, session.Address, metrics.FontSizeS, gfx.WeightRegular, labelWidth)
	cardEdge := pal.windowStroke
	cardEdge.Alpha = 0.72
	reasonEdge, reasonTint := pal.danger, pal.danger
	reasonEdge.Alpha, reasonTint.Alpha = 0.35, 0.12

	// Generous buttons when the card has room for them; the row shrinks them
	// when it must, as it does for the four buttons of a pending reconnect.
	size := components.ButtonSizeDefault
	switch {
	case footerHeight < 52:
		size = components.ButtonSizeXSmall
	case footerHeight < 64 || session.reconnectPending:
		size = components.ButtonSizeSmall
	}
	s.buttonBoxes, s.buttons = s.buttonBoxes[:0], s.buttons[:0]
	addButton := func(id gfx.ID, label string, variant components.ButtonVariant) {
		probe := components.BeginButton(canvas, *c.Theme(), gfx.ID{}, gfx.Rect{}).Size(size).SizeToFit(label)
		box := ui.Slot(c).Size(probe.BoundsWidth(), probe.BoundsHeight()).Shrink(1)
		s.buttonBoxes = append(s.buttonBoxes, box)
		s.buttons = append(s.buttons, sessionButton{id: id, label: label, size: size, variant: variant})
	}
	ui.Column(c).Fill().Background(pal.workspace).Items(ui.Center).Children(func() {
		ui.Spacer(c).Grow(0.42)
		// The card is a stack so its edge is a layer of its own: a border drawn by
		// the content column would take a pixel from the layout.
		ui.Stack(c).Width(width).Shrink(0).Children(func() {
			ui.Column(c).Fill().Background(pal.surface).Radius(18).Border(1, cardEdge)
			ui.Column(c).PadX(cardPad).Children(func() {
				ui.Row(c).Height(headerHeight).Shrink(0).PadSides(avatarTop, 0, 0, 0).Gap(16).Children(func() {
					s.avatar = ui.Slot(c).Size(avatarSize, avatarSize).Shrink(0)
					ui.Column(c).Gap(2).MarginSides(1, 0, 0, 0).Children(func() {
						ui.Text(c, title).FontSize(16).Bold().Color(pal.text)
						ui.Text(c, address).FontSize(metrics.FontSizeS).Color(pal.muted)
					})
				})

				if step == stepFailed {
					// There is no journey left to draw: the rail says nothing once
					// the connection has ended, so the panel gives its width to the
					// result instead, with the time it took on the other edge.
					ui.Stack(c).Height(failureHeight).Shrink(0).Background(panelFill).Radius(14).Children(func() {
						ui.Column(c).Fill().Background(ending).Radius(14)
						ui.Row(c).Fill().PadSides(16, 16, 16, 16).Gap(12).Items(ui.Center).Children(func() {
							s.failing = ui.Slot(c).Size(20, 20).Shrink(0)
							ui.Column(c).Grow(1).Justify(ui.Center).Gap(3).Children(func() {
								ui.Text(c, statusTitle).FontSize(metrics.FontSizeM).Bold().Color(pal.text).LineHeight(23)
								if detail != "" {
									ui.Text(c, detail).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(17)
								}
							})
							ui.Text(c, state.elapsedText(canvas, session)).FontSize(metrics.FontSizeS).Color(pal.faint).Shrink(0)
						})
					})
				} else {
					ui.Stack(c).Height(statusHeight).Shrink(0).Background(panelFill).Radius(14).Children(func() {
						ui.Column(c).Fill().Background(ending).Radius(14)
						s.track = ui.Slot(c).Height(46).FillWidth().MarginSides(12, 18, 0, 18)
						s.track.Children(func() {
							ui.Row(c).Fill().Items(ui.Center).Children(func() {
								s.rail = ui.Slot(c).Grow(1).Height(3).MarginSides(0, 6, 0, 18)
								s.railTarget = ui.Column(c).Size(30, 30).Center().Shrink(0).Children(func() {
									s.railIcon = ui.Slot(c).Size(14, 14)
								})
							})
						})
						ui.Column(c).Fill().Justify(ui.End).PadSides(0, 0, 8, 0).Children(func() {
							// The badge sits against the panel's edge, the title clipped short of it.
							titleGap := float32(0)
							if step == stepWaiting {
								titleGap = 28
							}
							ui.Row(c).Items(ui.Center).Children(func() {
								ui.Text(c, statusTitle).FontSize(metrics.FontSizeM).Bold().Color(pal.text).LineHeight(25).MarginSides(0, titleGap, 0, 18)
								if step == stepActive && !canvas.ReduceMotion() {
									// The working ellipsis follows the title, resting 6 above its bottom edge.
									s.dots = ui.Slot(c).Size(bouncingDotsWidth, bouncingDotsHeight).Self(ui.End).MarginSides(0, 0, 6, 5).Shrink(0)
								}
								if step == stepWaiting {
									ui.Spacer(c)
									ui.Column(c).Height(22).PadX(9).Center().Background(pal.accentSoft).Radius(11).Shrink(0).Children(func() {
										ui.Text(c, m.ConnNeedsYou).FontSize(metrics.FontSizeS).Bold().Color(pal.accent)
									})
								}
							})
							if detail != "" {
								ui.Text(c, detail).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(20).MarginX(18)
							} else {
								ui.Column(c).Height(20).Shrink(0)
							}
						})
					})
				}

				// Why it failed.
				if len(reason) > 0 {
					ui.Row(c).MarginSides(10, 0, 0, 0).PadSides(reasonInset, 12, reasonInset, 12).Shrink(0).
						Background(reasonTint.Over(pal.surface)).Radius(radius.popup).Border(1, reasonEdge.Over(pal.surface)).Children(func() {
						ui.Column(c).Grow(1).Children(func() {
							for _, line := range reason {
								ui.Text(c, line).FontSize(metrics.FontSizeS).Color(pal.text).LineHeight(reasonLeadin)
							}
						})
					})
				}

				// How long, and what the user can do.
				footerTop := footerGap
				if len(reason) > 0 {
					footerTop += 6
				}
				ui.Stack(c).Height(footerHeight).Shrink(0).MarginSides(footerTop, 0, 0, 0).Children(func() {
					ui.Column(c).Height(1).Background(pal.line)
					ui.Row(c).Fill().Items(ui.Center).Gap(state.theme.Metrics.GapItem).Children(func() {
						ui.Text(c, footerText).FontSize(metrics.FontSizeS).Color(pal.muted).Grow(1)
						addButton(hitConnLog, m.ConnShowLog, components.ButtonGhost)
						if session.reconnectPending {
							addButton(hitConnExport, m.ConnExportLog, components.ButtonGhost)
						}
						addButton(hitConnClose, m.ConnClose, components.ButtonOutline)
						if failed && canForgetServerIdentity(session) {
							addButton(hitConnForget, m.HostKeyForget, components.ButtonOutline)
						}
						if session.reconnectPending {
							addButton(hitConnCancelReconnect, m.ConnCancelReconnect, components.ButtonDefault)
						} else if failed && state.canReconnect(session) {
							addButton(hitConnRetry, m.ConnRetry, components.ButtonDefault)
						}
					})
				})
			})
		})
		ui.Spacer(c).Grow(0.58)
	})
}
func connectionDisplayStage(session *Session) int {
	switch {
	case session.shellFailed:
		return stageShell
	case errors.Is(session.err, sshconn.ErrAuthFailed):
		return stageLogin
	case errors.Is(session.err, sshconn.ErrHostKeyChanged), errors.Is(session.err, sshconn.ErrHostKeyRejected):
		return stageHostKey
	}
	return min(max(session.stage, 0), stageCount-1)
}

func (state *State) paintConnectionTrack(canvas *gfx.Canvas, session *Session, step stepState, fill float32, system sshconn.RemoteSystem) {
	const endpointSize, ballSize, ring = float32(30), float32(26), float32(18)
	pal := state.pal

	// The rail runs from the ring's reach at the left to a little short of the
	// machine it leads to at the right; only the ball on it moves. Its slots were
	// declared with the connection body.
	s := state.paneBody(session)
	track, targetBounds, iconBounds := s.rail.Rect(), s.railTarget.Rect(), s.railIcon.Rect()

	tint := pal.accent
	switch step {
	case stepFailed:
		tint = pal.danger
	case stepDone:
		tint = pal.success
	}

	// The track, filled behind the ball as far as the connection has come.
	canvas.Rounded(track, track.Height/2, withAlpha(pal.line, 0.9))
	if step == stepActive {
		paintConnectionFlow(canvas, track, tint)
	}
	ballX := track.X + track.Width*min(max(fill, 0), 1)
	if filled := ballX - track.X; filled > 0 {
		canvas.Rounded(gfx.Rect{X: track.X, Y: track.Y, Width: filled, Height: track.Height}, track.Height/2, withAlpha(tint, 0.55))
	}

	canvas.Rounded(targetBounds, endpointSize/2, pal.surfaceHot)
	arrived := step == stepDone && fill >= 0.98
	icon, iconColor := iconTerminal, pal.muted
	if arrived {
		icon, iconColor = iconCheck, pal.success
	}
	paintIcon(canvas, icon, iconBounds, iconColor)

	if arrived {
		return // it has arrived: the terminal says so
	}
	centerY := track.Y + track.Height/2
	clear := ballSize + 8
	canvas.Rounded(gfx.Rect{X: ballX - clear/2, Y: centerY - clear/2, Width: clear, Height: clear}, clear/2, pal.surfaceSoft.Over(pal.surface)) // the track under the ball and its ring
	// The ball is the machine: its system's badge, as in the host list.
	paintSystemBadge(canvas, system, gfx.Rect{X: ballX - ballSize/2, Y: centerY - ballSize/2, Width: ballSize, Height: ballSize}, pal)
	if step == stepActive {
		paintConnectionRing(canvas, ballX, centerY, ring, tint)
	}
}

// The light that runs along the rail while the connection is being made: one
// streak, a bright head with a fading tail, that leaves the ball and crosses to
// the machine, then starts again.
const (
	connectionFlowSpeed = float32(700) // pixels a second
	connectionFlowTail  = float32(72)
	connectionFlowSlice = float32(4)
	connectionFlowRest  = 250 * time.Millisecond // dark between two crossings
)

// paintConnectionFlow draws the streak, clipped to the rail. Standing still
// (reduced motion) it draws none: the ring already says it is working.
func paintConnectionFlow(canvas *gfx.Canvas, track gfx.Rect, tint gfx.Color) {
	if canvas.ReduceMotion() {
		return
	}
	// The head goes from the rail's start to a tail's length past its end.
	distance := track.Width + connectionFlowTail
	travel := time.Duration(float64(distance/connectionFlowSpeed) * float64(time.Second))
	phase := canvas.Now() % (travel + connectionFlowRest)
	if phase >= travel {
		return
	}
	head := track.X + distance*float32(phase)/float32(travel)
	slices := int(connectionFlowTail / connectionFlowSlice)
	for index := range slices {
		left := head - float32(index+1)*connectionFlowSlice
		right := min(left+connectionFlowSlice, track.X+track.Width)
		left = max(left, track.X)
		if right <= left {
			continue
		}
		fade := 1 - float32(index)/float32(slices)
		canvas.Fill(gfx.Rect{X: left, Y: track.Y, Width: right - left, Height: track.Height}, withAlpha(tint, 0.6*fade*fade))
	}
}

// connectionRingPeriod is one turn of the ring around the ball.
const connectionRingPeriod = 900 * time.Millisecond

// paintConnectionRing is the working ring: sixteen small dots around the ball
// whose brightness trails behind a head that goes round, so it reads as a smooth
// sweep and costs no path or allocation. With reduced motion it stands still,
// three quarters lit.
func paintConnectionRing(canvas *gfx.Canvas, x, y, radius float32, tint gfx.Color) {
	const dots = 16
	head := float64(0)
	if !canvas.ReduceMotion() {
		head = float64(cycle(canvas.Now(), connectionRingPeriod, 0))
	}
	for index := range dots {
		position := float64(index) / dots
		behind := head - position
		behind -= math.Floor(behind) // 0 at the head, then growing round to 1
		alpha := float32(0)
		if behind < 0.75 {
			alpha = float32(1 - behind/0.75)
			alpha *= alpha
		}
		alpha = 0.12 + 0.88*alpha
		angle := 2*math.Pi*position - math.Pi/2
		const diameter = float32(3)
		cx, cy := x+radius*float32(math.Cos(angle)), y+radius*float32(math.Sin(angle))
		canvas.Rounded(gfx.Rect{X: cx - diameter/2, Y: cy - diameter/2, Width: diameter, Height: diameter}, diameter/2, withAlpha(tint, alpha))
	}
}

// connectionRailTarget is where the ball is: at the start for as long as the
// connection is being made, whatever stage it is in, and at the end once it is
// made. (The stages are in the words under the rail.)
func connectionRailTarget(step stepState) float32 {
	if step == stepDone {
		return 1
	}
	return 0
}

// easeRail moves the ball toward where it belongs. The ease is by time, not by
// frame, so it is as smooth at 30 frames a second as at 120.
func (state *State) easeRail(canvas *gfx.Canvas, session *Session, step stepState) {
	now := canvas.Now()
	target := connectionRailTarget(step)
	if canvas.ReduceMotion() {
		session.railShown, session.railTo = target, target
		return
	}
	if target != session.railTo {
		session.railFrom, session.railTo, session.railStart = session.railShown, target, now
	}
	if session.railShown == target {
		return
	}
	progress := float32(min(max(now-session.railStart, 0), connectionRailDuration)) / float32(connectionRailDuration)
	if progress >= 1 {
		session.railShown = target
		return
	}
	session.railShown = session.railFrom + (target-session.railFrom)*progress*progress*(3-2*progress)
	canvas.RequestFrameIn(connectionSignalStep)
}

// connectionRailDuration is how long the ball takes to its new place.
const connectionRailDuration = 340 * time.Millisecond

func withAlpha(color gfx.Color, alpha float32) gfx.Color {
	color.Alpha = alpha
	return color
}

// cycle is where a repeating animation of the given period stands at now, from
// 0 up to but not including 1, shifted by offset (also 0..1).
func cycle(now, period time.Duration, offset float32) float32 {
	phase := float32(now%period)/float32(period) + offset
	return phase - float32(int(phase))
}

// The working ellipsis: three dots that hop in turn, in a box as wide as they
// are and tall enough for the hop.
const (
	bouncingDotSize    = float32(3.5)
	bouncingDotsWidth  = 3*bouncingDotSize + 2*3.5
	bouncingDotsHeight = 2 * bouncingDotSize
)

// paintBouncingDots draws the working ellipsis in its box: the dots rest on the
// box's bottom edge and hop up.
func paintBouncingDots(canvas *gfx.Canvas, box gfx.Rect, color gfx.Color) {
	now := canvas.Now()
	for dot := float32(0); dot < 3; dot++ {
		wave := float32(math.Sin(math.Pi * float64(cycle(now, connectionDotsPeriod, 1-dot*0.14)) * 2))
		lift := max(wave, 0)
		color.Alpha = 0.45 + 0.55*lift
		canvas.Rounded(gfx.Rect{X: box.X + dot*(bouncingDotSize+3.5), Y: box.Y + box.Height - bouncingDotSize - 3.5*lift, Width: bouncingDotSize, Height: bouncingDotSize}, bouncingDotSize/2, color)
	}
}

func (state *State) paintRestoredSessionDetails(canvas *gfx.Canvas, session *Session) {
	s := state.paneBody(session)
	state.paintSessionBadge(canvas, session, s.avatar.Rect())
	state.paintSessionButtons(canvas, session)
	state.paintKeyHint(canvas, s.keysClose.Rect(), s.closeHint)
	state.paintKeyHint(canvas, s.keysConnect.Rect(), s.connectHint)
}

// declareRestoredSessionBody declares the body of a session that has to be
// restored: who it is, what happened, what to do, and the keys that do it.
func (state *State) declareRestoredSessionBody(c ui.C, session *Session, area gfx.Rect) {
	canvas := c.Canvas()
	pal, messages := state.pal, state.messages
	s := state.paneBody(session)
	s.declared = true
	s.avatar, s.keysClose, s.keysConnect = ui.Box{}, ui.Box{}, ui.Box{}
	s.closeHint, s.connectHint = nil, nil

	// No card: the page is the window's own space, with one column down its
	// middle (who, what happened, what to do) and the keys that do it.
	const avatarSize = float32(52)
	labelMax := max(area.Width-avatarSize-48, 120)
	title := elide(canvas, session.Title, 18, gfx.WeightSemibold, labelMax)
	address := elide(canvas, session.Address, metrics.FontSizeS, gfx.WeightRegular, labelMax)
	body := messages.SessionRestoredBody
	if session.restoreNote != "" {
		body = session.restoreNote
	}
	textWidth := min(max(area.Width-64, 200), 440)
	lines := canvas.WrapLines(state.wrapped[:0], body, metrics.FontSizeM, gfx.WeightRegular, textWidth, 3)

	// Under each button the keys that press it, as wide as their keycaps; they hang
	// below the button without taking room from the page's centring.
	s.closeHint = state.shortcuts.chords.Chord(shortcutCloseTab).Keycaps(s.closeKeys[:0])
	s.connectHint = append(s.connectKeys[:0], "↵")
	hintHeight := state.Theme().Metrics.FontSizeS + 9
	closeHintWidth, connectHintWidth := state.keyHintWidth(canvas, s.closeHint, hintHeight), state.keyHintWidth(canvas, s.connectHint, hintHeight)

	s.buttonBoxes, s.buttons = s.buttonBoxes[:0], s.buttons[:0]
	addButton := func(id gfx.ID, label string, variant components.ButtonVariant) {
		probe := components.BeginButton(canvas, *c.Theme(), gfx.ID{}, gfx.Rect{}).SizeToFit(label)
		box := ui.Slot(c).Size(max(probe.BoundsWidth(), 96), probe.BoundsHeight())
		s.buttonBoxes = append(s.buttonBoxes, box)
		s.buttons = append(s.buttons, sessionButton{id: id, label: label, size: components.ButtonSizeDefault, variant: variant})
	}

	ui.Column(c).Fill().Background(pal.workspace).PadSides(max(24, area.Height*0.34), 0, 0, 0).Items(ui.Center).Children(func() {
		ui.Row(c).Gap(16).Items(ui.Center).Children(func() {
			s.avatar = ui.Slot(c).Size(avatarSize, avatarSize)
			ui.Column(c).Children(func() {
				ui.Text(c, title).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text)
				ui.Text(c, address).FontSize(metrics.FontSizeS).Color(pal.muted)
			})
		})
		ui.Column(c).Width(textWidth).MarginSides(28, 0, 0, 0).Children(func() {
			ui.Text(c, messages.SessionRestoredTitle).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).TextCenter().MarginSides(0, 0, 4, 0)
			for _, line := range lines {
				ui.Text(c, line).Color(pal.muted).TextCenter()
			}
		})
		ui.Row(c).Gap(14).MarginSides(22, 0, 0, 0).Children(func() {
			ui.Column(c).Items(ui.Center).Children(func() {
				addButton(hitConnClose, messages.ConnClose, components.ButtonOutline)
				s.keysClose = ui.Slot(c).Size(closeHintWidth, hintHeight).MarginSides(10, 0, -(10 + hintHeight), 0)
			})
			ui.Column(c).Items(ui.Center).Children(func() {
				addButton(hitConnRetry, messages.SessionReconnect, components.ButtonDefault)
				s.keysConnect = ui.Slot(c).Size(connectHintWidth, hintHeight).MarginSides(10, 0, -(10 + hintHeight), 0)
			})
		})
	})
}

// paintSessionButtons paints the footer buttons the session body declared into
// their slots, keeping the application's identities for the central dispatcher.
// A split mounts each pane's identities, so the buttons are that pane's own.
func (state *State) paintSessionButtons(canvas *gfx.Canvas, session *Session) {
	s := state.paneBody(session)
	for index, action := range s.buttons {
		id := state.paneIdentity(session, action.id)
		button := components.BeginButton(canvas, state.Theme(), id, s.buttonBoxes[index].Rect()).Size(action.size)
		switch action.variant {
		case components.ButtonGhost:
			button.Ghost(action.label)
		case components.ButtonOutline:
			button.Outline(action.label)
		default:
			button.Default(action.label)
		}
	}
}

// keyHintWidth is how wide the keycaps of a hint are, side by side.
func (state *State) keyHintWidth(canvas *gfx.Canvas, keys []string, height float32) float32 {
	width := float32(0)
	for index, key := range keys {
		width += max(canvas.TextWidth(key, state.Theme().Metrics.FontSizeS)+8, height)
		if index > 0 {
			width += 4
		}
	}
	return width
}

// paintKeyHint draws keycaps in the box that was kept for them.
func (state *State) paintKeyHint(canvas *gfx.Canvas, box gfx.Rect, keys []string) {
	group := components.BeginKbdGroup(canvas, state.Theme(), box)
	for _, key := range keys {
		group.Add(key)
	}
}

// elapsedText is how long the connection has taken, kept between paints so a
// repaint within the same second builds nothing. The page asks for a frame
// each second while the clock runs.
func (state *State) elapsedText(canvas *gfx.Canvas, session *Session) string {
	end, running := session.finished, session.finished == 0
	if running {
		end = state.now()
	}
	elapsed := max(end-session.since, 0)
	if seconds := int64(elapsed / time.Second); session.elapsed == "" || seconds != session.elapsedSecond {
		session.elapsedSecond = seconds
		session.elapsed = fmt.Sprintf(state.messages.ConnElapsedFmt, clockText(seconds))
	}
	if running {
		canvas.RequestFrameIn(time.Second - elapsed%time.Second)
	}
	return session.elapsed
}

// clockText is seconds as m:ss, or h:mm:ss from an hour on.
func clockText(seconds int64) string {
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

// clickConnection handles the buttons of the connection page and the way back
// to it from the log.
func (state *State) clickConnection(id gfx.ID) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		return
	}
	switch id {
	case hitConnLog:
		session.showLog = true
	case hitConnProgress:
		session.showLog = false
	case hitConnClose:
		state.closeSession(session.ID)
	case hitConnRetry:
		if state.canReconnect(session) {
			state.reconnect(session)
		}
	case hitConnForget:
		state.forgetServerIdentity(session)
	case hitConnCancelReconnect:
		if session.reconnectPending {
			state.cancelReconnect(session)
			state.releaseWaitingForwards()
			state.recordConnectionEvent(session, connectionEventReconnectCanceled, "")
			session.term.say(sgrMuted, state.messages.ConnReconnectCanceled)
			session.showLog = true
		}
	case hitConnExport:
		state.exportConnectionLog(session)
	}
}

// paintSessionBadge is the machine's badge, the one its row in the host list has:
// its system, or the icon chosen for it. A local shell has the terminal.
func (state *State) paintSessionBadge(canvas *gfx.Canvas, session *Session, bounds gfx.Rect) {
	if session.isLocal() {
		canvas.Rounded(bounds, bounds.Width*0.24, state.pal.accentSoft)
		paintIcon(canvas, iconTerminal, insetRect(bounds, 0.24), state.pal.accent)
		return
	}
	state.paintHostCatalogIcon(canvas, session.host, bounds, state.pal.workspace)
}
