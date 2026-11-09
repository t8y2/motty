package workbench

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

// Testing a connection from the host form dials the host as typed, without
// saving it, and says how far the attempt got. It asks nobody anything and
// changes nothing: an unknown host key is never trusted (nothing, least of all
// a password, is sent to a server the user has not vouched for), and a login
// that needs something the form cannot supply ends the test as unverified.

var hitDialogTest = tree.ID(hostFormHits, "test-connection")

// hostTestLimit bounds a test whose server accepts the connection and then says
// nothing; a refused or unreachable address fails sooner on its own.
const hostTestLimit = 30 * time.Second

type hostTestOutcome uint8

const (
	hostTestIdle hostTestOutcome = iota
	hostTestRunning
	// hostTestConnected: the server accepted the credentials.
	hostTestConnected
	// hostTestReachable: the server answered but the login could not be checked.
	hostTestReachable
	hostTestFailed
)

type hostTest struct {
	conn     Connection
	outcome  hostTestOutcome
	message  string
	version  uint64
	deadline time.Duration
	// skipped counts private keys the test could not unlock, so a refused login
	// is not blamed on the account when a key was never offered.
	skipped int
	// The form's own secrets, held only while the test runs.
	password, passphrase string
	// signature digests what the test was run against, so a result is shown only
	// while the form still says the same thing.
	signature [sha256.Size]byte
}

func (test *hostTest) clear() {
	if test.conn != nil {
		test.conn.Cancel()
	}
	*test = hostTest{}
}

// finish ends the running test with its result and lets go of the connection
// and of the secrets it used.
func (test *hostTest) finish(outcome hostTestOutcome, message string) {
	if test.conn != nil {
		test.conn.Cancel()
	}
	*test = hostTest{outcome: outcome, message: message, signature: test.signature}
}

// hostTestSignature digests the parts of the form that decide a test's result,
// the secrets included, without keeping any of them.
func (state *State) hostTestSignature() [sha256.Size]byte {
	editor := &state.editor
	proxy := ""
	if editor.proxy != nil {
		proxy = fmt.Sprint(editor.proxy.kind, editor.proxy.address, editor.proxy.user)
	}
	return sha256.Sum256([]byte(fmt.Sprint(
		editor.value(fieldAddress), "\x00", editor.value(fieldPort), "\x00", editor.value(fieldUser), "\x00",
		editor.auth, editor.identity, "\x00", editor.password.String(), "\x00", editor.passphrase.String(), "\x00",
		proxy, editor.proxyPassword)))
}

// draftSecrets answers the secrets typed into the form ahead of the vault's own,
// so a test sees what saving the form would have saved.
type draftSecrets struct {
	base        passwordReader
	name, value string
}

func (secrets draftSecrets) Get(name string) (string, bool, error) {
	if secrets.value != "" && name == secrets.name {
		return secrets.value, true, nil
	}
	if secrets.base == nil {
		return "", false, nil
	}
	return secrets.base.Get(name)
}

// hostTestAvailable reports whether the open form describes something a test can
// dial: an SSH host. Telnet, serial and desktop hosts have no login to check.
func (state *State) hostTestAvailable() bool {
	return state.modal == modalNewHost && state.editor.protocol == protocolSSH
}

// startHostTest dials the host in the form.
func (state *State) startHostTest() {
	test := &state.hostTest
	if !state.hostTestAvailable() || test.outcome == hostTestRunning {
		return
	}
	m := state.messages
	test.clear()
	test.signature = state.hostTestSignature()
	if state.editor.value(fieldAddress) == "" {
		test.finish(hostTestFailed, m.HostTestNoAddress)
		return
	}
	if _, ok := state.editor.portNumber(); !ok {
		test.finish(hostTestFailed, m.ErrPortInvalid)
		return
	}
	if message := state.hostProxyPrecheck(); message != "" {
		test.finish(hostTestFailed, message)
		return
	}
	var draftID uint32
	var catalogID string
	if state.hostDialogMode == hostDialogEdit {
		if index := state.hostIndex(state.hostDialogID); index >= 0 {
			draftID, catalogID = state.hosts[index].ID, state.hosts[index].CatalogID
		}
	}
	draft := state.editor.host(draftID)
	draft.CatalogID = catalogID

	env := state.connectionEnv(state.options.Invalidate)
	proxySecrets := draftSecrets{base: env.Passwords}
	if draft.Proxy != nil {
		proxySecrets.name, proxySecrets.value = hostProxyPasswordSecret(draft.CatalogID, draft.Proxy), state.editor.proxyPassword
	}
	env.Passwords = proxySecrets
	config, _, valid := state.hostCatalog.sshConfigFor(draft, draft, env)
	if !valid {
		test.finish(hostTestFailed, m.HostTestBrokenRoute)
		return
	}
	test.password, test.passphrase = state.editor.password.String(), state.editor.passphrase.String()
	test.conn = state.options.Dial(config)
	test.outcome = hostTestRunning
	test.message = m.HostTestRunning
	test.deadline = state.now() + hostTestLimit
}

// cancelHostTest drops a running test and its result, as the form closes.
func (state *State) cancelHostTest() { state.hostTest.clear() }

// pumpHostTest moves a running test along whenever the connection reports a
// change, and ends it when it has run out of time.
func (state *State) pumpHostTest() {
	test := &state.hostTest
	if test.conn == nil {
		return
	}
	m := state.messages
	if state.now() >= test.deadline {
		test.finish(hostTestFailed, m.HostTestTimeout)
		return
	}
	status := test.conn.Status()
	if status.Version == test.version {
		return
	}
	test.version = status.Version
	switch status.Phase {
	case sshconn.PhaseConnected:
		who := formatEndpoint(status.Endpoint)
		if status.ServerVersion != "" {
			who += " · " + status.ServerVersion
		}
		test.finish(hostTestConnected, fmt.Sprintf(m.HostTestConnectedFmt, who))
	case sshconn.PhaseFailed, sshconn.PhaseClosed:
		state.finishFailedHostTest(status)
	case sshconn.PhaseHostKeyPrompt:
		test.finish(hostTestReachable, m.HostTestHostKey)
	case sshconn.PhasePasswordPrompt:
		state.answerHostTestPassword(status)
	case sshconn.PhaseKeyPassphrasePrompt:
		state.answerHostTestPassphrase(status)
	}
}

func (state *State) finishFailedHostTest(status sshconn.Status) {
	test, m, err := &state.hostTest, state.messages, status.Err
	switch {
	case errors.Is(err, sshconn.ErrHostKeyChanged):
		test.finish(hostTestFailed, m.ConnChangedShort)
	case errors.Is(err, sshconn.ErrAuthFailed) && test.skipped > 0:
		test.finish(hostTestReachable, m.HostTestKeyLocked)
	case errors.Is(err, sshconn.ErrAuthFailed):
		test.finish(hostTestFailed, m.ConnAuthFailed)
	case err == nil:
		test.finish(hostTestFailed, m.ConnCanceled)
	default:
		test.finish(hostTestFailed, fmt.Sprintf(m.HostTestFailedFmt, err))
	}
}

func (state *State) answerHostTestPassword(status sshconn.Status) {
	test, m := &state.hostTest, state.messages
	if !isPasswordQuestion(status.Questions) {
		test.finish(hostTestReachable, m.HostTestNeedsInput)
		return
	}
	if status.Attempt > 1 {
		test.finish(hostTestFailed, m.ConnAuthFailed)
		return
	}
	password := ""
	if status.EndpointRole == sshconn.EndpointTarget {
		password = test.password
	} else if state.secrets.vault != nil {
		owner := state.secretOwner(Host{}, status.Endpoint, status.EndpointRole)
		if value, found, err := state.secrets.vault.Get(hostSecretName(owner, hostPasswordPart)); err == nil && found {
			password = value
		}
	}
	if password == "" {
		test.finish(hostTestReachable, m.HostTestNoPassword)
		return
	}
	test.conn.SubmitPassword(password)
}

func (state *State) answerHostTestPassphrase(status sshconn.Status) {
	test := &state.hostTest
	passphrase := ""
	if status.Attempt == 1 && status.IdentityPath != "" {
		if identity, ok := state.identityForFingerprint(state.editor.identity); ok && identity.Path == status.IdentityPath {
			passphrase = test.passphrase
		}
		if passphrase == "" && state.secrets.vault != nil {
			if value, found, err := state.secrets.vault.Get(passphraseSecretName(status.IdentityPath)); err == nil && found {
				passphrase = value
			}
		}
	}
	if passphrase == "" {
		test.skipped++
		test.conn.SkipIdentity()
		return
	}
	test.conn.SubmitKeyPassphrase(passphrase)
}

// requestHostTestFrame asks for the frame that lets a test run out of time.
func (state *State) requestHostTestFrame(canvas *gfx.Canvas) {
	if state.hostTest.outcome == hostTestRunning {
		canvas.RequestFrameIn(max(state.hostTest.deadline-state.now(), time.Millisecond))
	}
}

// paintHostTest paints the Test connection button at the footer's left edge and,
// beside it, what the last test found. The text keeps clear of the footer's own
// buttons, whose labels decide how much room is left.
func (state *State) paintHostTest(canvas *gfx.Canvas, theme gfx.Theme, footer gfx.Rect, rightLabels ...string) {
	if !state.hostTestAvailable() {
		return
	}
	m, test := state.messages, &state.hostTest
	button := components.BeginButton(canvas, theme, hitDialogTest, footer).Size(components.ButtonSizeDefault).SizeToFit(m.HostTest)
	buttonWidth := button.BoundsWidth()
	if test.outcome == hostTestRunning {
		button = button.Disabled()
	}
	button.Outline(m.HostTest)
	state.requestHostTestFrame(canvas)
	if test.message == "" || test.outcome != hostTestRunning && test.signature != state.hostTestSignature() {
		return
	}

	gap := theme.Metrics.GapItem
	right := footer.X + footer.Width
	for _, label := range rightLabels {
		right -= components.BeginButton(canvas, theme, gfx.ID{}, footer).Size(components.ButtonSizeDefault).SizeToFit(label).BoundsWidth() + gap
	}
	left := footer.X + buttonWidth + gap*1.5
	width := right - gap - left
	if width < 60 {
		return
	}
	color := state.pal.muted
	switch test.outcome {
	case hostTestConnected:
		color = state.pal.success
	case hostTestReachable:
		color = state.pal.warning
	case hostTestFailed:
		color = state.pal.danger
	}
	size := theme.Metrics.FontSizeS
	const lineHeight = 16
	var scratch [2]string
	lines := canvas.WrapLines(scratch[:0], strings.TrimSpace(test.message), size, gfx.WeightMedium, width, len(scratch))
	top := footer.Y + (footer.Height-float32(len(lines))*lineHeight)/2
	for index, line := range lines {
		canvas.Label(line, gfx.Rect{X: left, Y: top + float32(index)*lineHeight, Width: width, Height: lineHeight},
			gfx.TextStyle{Size: size, Weight: gfx.WeightMedium, Color: color})
	}
}
