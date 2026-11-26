package workbench

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"motty/internal/linkwatch"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
	"motty/internal/termsize"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// fakeConn stands in for a network connection: tests set its status the way
// the connection goroutine would and read back what the UI answered.
type fakeConn struct {
	mu                sync.Mutex
	status            sshconn.Status
	statusReads       int
	trusted           []bool
	rejected          int
	passwords         []string
	answers           [][]string
	keyPassphrases    []string
	skippedIdentities int
	canceled          int
	shells            []*fakeShell
	shellErr          error
	// shellHold, when set, keeps every shell request unanswered until it is
	// closed: a gateway that silently drops the channel request.
	shellHold      chan struct{}
	configs        []sshconn.ShellConfig
	sftp           sftpclient.Remote
	sftpErr        error
	sftpOpens      int
	scp            sftpclient.Remote
	scpOpens       int
	dial           func(context.Context, string, string) (net.Conn, error)
	dialCalls      []fakeDialCall
	shellDial      func(context.Context, string, string) (net.Conn, error)
	shellDialCalls []fakeShellDialCall
	listen         func(string, string) (net.Listener, error)
	listenCalls    []fakeListenCall
	runCommand     func(context.Context, string, int) ([]byte, error)
	exec           func(context.Context, string, int) (sshconn.ExecResult, error)
	streamCommand  func(context.Context, string, int, func([]byte) bool) error
	commandCalls   []string
	// verifies counts link checks, and verifyErr is what they answer. A link
	// change asks every established connection; the fake lets a test see that.
	verifies  int
	verifyErr error
}

type fakeDialCall struct {
	network string
	address string
}

type fakeShellDialCall struct {
	command string
	address string
}

type fakeListenCall struct {
	network string
	address string
}

// OpenShell hands out a scripted shell, as a connected server would.
// Verify answers a link check without touching a network, as a connection that
// survived the change would.
func (fake *fakeConn) Verify(context.Context) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.verifies++
	return fake.verifyErr
}

func (fake *fakeConn) verifyCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.verifies
}

func (fake *fakeConn) OpenShell(config sshconn.ShellConfig) (Shell, error) {
	fake.mu.Lock()
	hold := fake.shellHold
	shellErr := fake.shellErr
	fake.mu.Unlock()
	if hold != nil {
		<-hold // a gateway that leaves the request unanswered
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if shellErr != nil {
		return nil, shellErr
	}
	shell := &fakeShell{done: make(chan struct{})}
	fake.shells = append(fake.shells, shell)
	fake.configs = append(fake.configs, config)
	return shell, nil
}

func (fake *fakeConn) OpenSFTP() (sftpclient.Remote, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.sftpOpens++
	if fake.sftpErr != nil {
		return nil, fake.sftpErr
	}
	if fake.sftp == nil {
		return nil, errors.New("test SFTP client is not configured")
	}
	return fake.sftp, nil
}

func (fake *fakeConn) OpenSCP() (sftpclient.Remote, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.scpOpens++
	if fake.scp == nil {
		return nil, errors.New("test SCP client is not configured")
	}
	return fake.scp, nil
}

func (fake *fakeConn) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	fake.mu.Lock()
	fake.dialCalls = append(fake.dialCalls, fakeDialCall{network: network, address: address})
	dial := fake.dial
	fake.mu.Unlock()
	if dial == nil {
		return nil, errors.New("test remote dialer is not configured")
	}
	return dial(ctx, network, address)
}

func (fake *fakeConn) DialShellContext(ctx context.Context, command, address string) (net.Conn, error) {
	fake.mu.Lock()
	fake.shellDialCalls = append(fake.shellDialCalls, fakeShellDialCall{command: command, address: address})
	dial := fake.shellDial
	fake.mu.Unlock()
	if dial == nil {
		return nil, errors.New("test shell relay is not configured")
	}
	return dial(ctx, command, address)
}

func (fake *fakeConn) Listen(network, address string) (net.Listener, error) {
	fake.mu.Lock()
	fake.listenCalls = append(fake.listenCalls, fakeListenCall{network: network, address: address})
	listen := fake.listen
	fake.mu.Unlock()
	if listen == nil {
		return nil, errors.New("test remote listener is not configured")
	}
	return listen(network, address)
}

func (fake *fakeConn) ListenContext(ctx context.Context, network, address string, check func(net.Listener) error) (net.Listener, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	listener, err := fake.Listen(network, address)
	if err == nil && check != nil {
		if err = check(listener); err != nil {
			_ = listener.Close()
			return nil, err
		}
	}
	return listener, err
}

func (fake *fakeConn) RunCommand(ctx context.Context, command string, limit int) ([]byte, error) {
	fake.mu.Lock()
	fake.commandCalls = append(fake.commandCalls, command)
	run := fake.runCommand
	fake.mu.Unlock()
	if run == nil {
		return nil, errors.New("test command runner is not configured")
	}
	return run(ctx, command, limit)
}

func (fake *fakeConn) Exec(ctx context.Context, command string, limit int) (sshconn.ExecResult, error) {
	fake.mu.Lock()
	fake.commandCalls = append(fake.commandCalls, command)
	exec := fake.exec
	fake.mu.Unlock()
	if exec == nil {
		return sshconn.ExecResult{}, errors.New("test command executor is not configured")
	}
	return exec(ctx, command, limit)
}

func (fake *fakeConn) StreamCommand(ctx context.Context, command string, limit int, line func([]byte) bool) error {
	fake.mu.Lock()
	fake.commandCalls = append(fake.commandCalls, command)
	stream := fake.streamCommand
	fake.mu.Unlock()
	if stream == nil {
		return errors.New("test command streamer is not configured")
	}
	return stream(ctx, command, limit, line)
}

// shell returns the most recent shell, failing the test when there is none.
func (fake *fakeConn) shell(t testing.TB) *fakeShell {
	t.Helper()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.shells) == 0 {
		t.Fatal("no shell was opened")
	}
	return fake.shells[len(fake.shells)-1]
}

// fakeShell is a remote shell the test drives: it says things (output) and
// hears what the workbench types (input).
type fakeShell struct {
	mu      sync.Mutex
	output  []byte
	input   []byte
	resizes []termsize.Size
	done    chan struct{}
	err     error
	closed  bool
	refuse  bool // Write reports failure
	// onWrite, when set, is called with each write after it was accepted: a
	// test shell that answers what it is typed.
	onWrite func([]byte)
}

func (shell *fakeShell) say(text string) {
	shell.mu.Lock()
	shell.output = append(shell.output, text...)
	shell.mu.Unlock()
}

func (shell *fakeShell) Drain(dst []byte) []byte {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	dst = append(dst, shell.output...)
	shell.output = nil
	return dst
}

func (shell *fakeShell) Write(p []byte) bool {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	if shell.refuse {
		return false
	}
	shell.input = append(shell.input, p...)
	hook := shell.onWrite
	if hook != nil {
		shell.mu.Unlock()
		hook(p)
		shell.mu.Lock()
	}
	return true
}

func (shell *fakeShell) Resize(size termsize.Size) {
	shell.mu.Lock()
	shell.resizes = append(shell.resizes, size)
	shell.mu.Unlock()
}

func (shell *fakeShell) resizeCount() int {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	return len(shell.resizes)
}

func (shell *fakeShell) lastResize() [2]int {
	size := shell.lastSize()
	return [2]int{size.Cols, size.Rows}
}

// lastSize is the whole size the shell was told last, pixels included.
func (shell *fakeShell) lastSize() termsize.Size {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	if len(shell.resizes) == 0 {
		return termsize.Size{}
	}
	return shell.resizes[len(shell.resizes)-1]
}

func (shell *fakeShell) Done() <-chan struct{} { return shell.done }
func (shell *fakeShell) Err() error {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	return shell.err
}
func (shell *fakeShell) Close() {
	shell.mu.Lock()
	shell.closed = true
	shell.mu.Unlock()
}

// end finishes the session like a remote exit.
func (shell *fakeShell) end(err error) {
	shell.mu.Lock()
	shell.err = err
	shell.mu.Unlock()
	close(shell.done)
}

// typed is everything the workbench has written to the shell.
func (shell *fakeShell) typed() string {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	return string(shell.input)
}

func (fake *fakeConn) Status() sshconn.Status {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.statusReads++
	return fake.status
}

func (fake *fakeConn) TrustHostKey(save bool) {
	fake.mu.Lock()
	fake.trusted = append(fake.trusted, save)
	fake.mu.Unlock()
}

func (fake *fakeConn) RejectHostKey() {
	fake.mu.Lock()
	fake.rejected++
	fake.mu.Unlock()
}

func (fake *fakeConn) SubmitPassword(password string) {
	fake.SubmitAnswers([]string{password})
}

func (fake *fakeConn) SubmitAnswers(answers []string) {
	fake.mu.Lock()
	fake.answers = append(fake.answers, append([]string(nil), answers...))
	if len(answers) == 1 {
		fake.passwords = append(fake.passwords, answers[0])
	}
	fake.mu.Unlock()
}

func (fake *fakeConn) SubmitKeyPassphrase(passphrase string) {
	fake.mu.Lock()
	fake.keyPassphrases = append(fake.keyPassphrases, passphrase)
	fake.mu.Unlock()
}

func (fake *fakeConn) SkipIdentity() {
	fake.mu.Lock()
	fake.skippedIdentities++
	fake.mu.Unlock()
}

func (fake *fakeConn) Cancel() {
	fake.mu.Lock()
	fake.canceled++
	fake.mu.Unlock()
}

// set changes the status like a connection update: the version always moves.
func (fake *fakeConn) set(change func(*sshconn.Status)) {
	fake.mu.Lock()
	change(&fake.status)
	fake.status.Version++
	fake.mu.Unlock()
}

// world records every connection the workbench dials.
type world struct {
	configs []sshconn.Config
	conns   []*fakeConn
	onDial  func(call int)
}

// sampleHosts are the hosts most tests act on. The application starts empty,
// so tests save them into the state file the way an earlier run would have.
var sampleHosts = []savedHost{
	{Label: "Production", Group: "Servers", Address: "prod.example.com", User: "deploy", Port: 22},
	{Label: "Staging", Group: "Servers", Address: "staging.example.com", User: "deploy", Port: 22},
	{Label: "Router", Group: "Network", Address: "192.168.1.1", User: "admin", Port: 22},
}

func newWorld(t testing.TB) (*world, Options) {
	t.Helper()
	known := &world{}
	dir := t.TempDir()
	options := Options{
		KnownHostsPath: filepath.Join(dir, "known_hosts"),
		StatePath:      filepath.Join(dir, "state.json"),
		Keystore:       &memKeys{}, // never the real keychain
		// The tests read English; the machine running them may not speak it.
		PreferredLanguages:    func() []string { return nil },
		SystemAppearance:      func() app.Appearance { return app.AppearanceUnknown },
		WatchSystemAppearance: func(func()) func() { return func() {} },
		WatchNetworkChanges:   func(func(linkwatch.Change)) func() { return func() {} },
		SetNativeAppearance:   func(app.Appearance) {},
		Dial: func(config sshconn.Config) Connection {
			fake := &fakeConn{}
			if known.onDial != nil {
				known.onDial(len(known.configs) + 1)
			}
			known.configs = append(known.configs, config)
			known.conns = append(known.conns, fake)
			return fake
		},
	}
	if err := saveState(options.StatePath, savedState{Hosts: sampleHosts}); err != nil {
		t.Fatal(err)
	}
	return known, options
}

func testOptions(t testing.TB) Options {
	_, options := newWorld(t)
	return options
}

var testHostKey = sshconn.HostKeyInfo{
	Address: "prod.example.com:22", Type: "ssh-ed25519",
	Fingerprint: "SHA256:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG", File: "/tmp/known_hosts",
}

// openHost connects the selected saved host the way a user does: by
// double-clicking its row.
// openHost double-clicks the selected host, or the first one when nothing is
// selected, as nothing is until the user picks a host.
func openHost(state *State) {
	id := state.selectedHostID
	if id == 0 {
		id = state.hosts[0].ID
	}
	pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 2, Target: hitHostList.At(int(id))})
}

func connect(t testing.TB, state *State, world *world) *fakeConn {
	t.Helper()
	before := len(world.conns)
	openHost(state)
	if len(world.conns) != before+1 {
		t.Fatalf("opening the selected host dialed %d connections, want one more", len(world.conns)-before)
	}
	return world.conns[len(world.conns)-1]
}

func paintedTexts(state *State) (*gfx.Canvas, []string) {
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	return &canvas, texts
}

func contains(texts []string, part string) bool {
	for _, text := range texts {
		if strings.Contains(text, part) {
			return true
		}
	}
	return false
}

func TestConnectDialsTheSelectedHostAndShowsProgress(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)

	config := world.configs[0]
	if config.Host != "prod.example.com" || config.Port != 22 || config.User != "deploy" ||
		config.KnownHostsPath != options.KnownHostsPath || !config.Agent {
		t.Fatalf("dial config = %+v", config)
	}
	session := state.sessions[0]
	if !session.busy || session.tone != toneBusy || session.term.screen.Text() != "" {
		t.Fatalf("session = %+v, screen %q", session, session.term.screen.Text())
	}
	if !strings.Contains(session.details[stageConnect], "Waiting for the server") {
		t.Fatalf("connection page detail = %q", session.details[stageConnect])
	}
	if state.activeSection != sectionTerminal {
		t.Fatal("connecting did not show the terminal tab")
	}
}

// A derivative reports its parent in the SSH banner: fnOS serves Debian's
// OpenSSH, so Debian arrives a moment before the host says what it really is.
// The rail reads the host's system, so that guess must not badge it as Debian.
func TestABannerGuessNeverReplacesTheSystemTheHostNamed(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	report := func(system sshconn.RemoteSystem, provisional bool) {
		fake.set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseConnected
			status.System = system
			status.SystemProvisional = provisional
		})
		state.Wake()
	}

	report(sshconn.SystemFnOS, false)
	if got := state.hosts[0].System; got != sshconn.SystemFnOS {
		t.Fatalf("host system = %v, want the answer the host gave", got)
	}
	report(sshconn.SystemDebian, true)
	if got := state.hosts[0].System; got != sshconn.SystemFnOS {
		t.Fatalf("a banner guess replaced the known system: %v", got)
	}
	// The host's own answer still wins when it disagrees with what was known.
	report(sshconn.SystemDebian, false)
	if got := state.hosts[0].System; got != sshconn.SystemDebian {
		t.Fatalf("host system = %v, want the answer the host gave", got)
	}
}

// A server that ends the connection on the client's own probes is remembered as
// an interactive-only host, so the retry it promises can connect.
func TestAProbeRefusalKeepsTheHostToItsInteractiveSession(t *testing.T) {
	world, options := newWorld(t)
	now := 10 * time.Second
	options.Now = func() time.Duration { return now }
	state := newTestState(t, options)
	state.connectionReconnect = reconnectThree
	connection := connect(t, state, world)
	connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	if world.configs[0].InteractiveOnly {
		t.Fatal("a host was confined to its interactive session before a server refused a probe")
	}
	connection.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseClosed
		status.Err = fmt.Errorf("%w: ssh: disconnect, reason 11", sshconn.ErrDisconnected)
		status.ProbeRefused = true
	})
	state.Wake()
	if !state.hosts[0].InteractiveOnly {
		t.Fatal("the saved host was not confined to its interactive session")
	}
	if !state.sessions[0].host.InteractiveOnly {
		t.Fatal("the retry would still send probes: the session's host was not updated")
	}
	if screen := state.sessions[0].term.screen.Text(); !strings.Contains(screen, state.messages.ConnProbeRefused) {
		t.Fatalf("the user was not told why the host was confined: %q", screen)
	}
	now += time.Second
	state.Wake()
	if len(world.conns) != 2 {
		t.Fatalf("the retry dialed %d connections, want 2", len(world.conns))
	}
	if !world.configs[1].InteractiveOnly {
		t.Fatal("the retry was not dialed as an interactive session")
	}
}

func TestHostKeyDialogAnswersTrustOnceRejectAndDismiss(t *testing.T) {
	for _, test := range []struct {
		name    string
		hit     gfx.ID
		trusted []bool
		reject  int
	}{
		{"trust and save", hitHostKeyTrust, []bool{true}, 0},
		{"connect once", hitHostKeyOnce, []bool{false}, 0},
		{"reject", hitHostKeyReject, nil, 1},
		{"close button", hitHostKeyClose, nil, 1},
		{"escape or backdrop", hitHostKeyDismiss, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			world, options := newWorld(t)
			state := newTestState(t, options)
			fake := connect(t, state, world)
			fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseHostKeyPrompt; s.HostKey = testHostKey })
			state.Wake()
			if state.modal != modalHostKey {
				t.Fatal("an unknown host key did not open the dialog")
			}
			canvas, texts := paintedTexts(state)
			for _, want := range []string{"Trust this host?", testHostKey.Fingerprint, "ssh-ed25519", "prod.example.com:22", state.messages.FieldAddress, state.messages.HostKeyType} {
				if !contains(texts, want) {
					t.Fatalf("dialog does not show %q: %v", want, texts)
				}
			}
			if canvas.TrappedFocusScope() != dialogScope || canvas.InitialFocus() != hitHostKeyReject {
				t.Fatalf("scope=%d initial focus=%d; the safe choice must hold the focus", canvas.TrappedFocusScope(), canvas.InitialFocus())
			}
			if canvas.EscapeTarget() != hitHostKeyDismiss {
				t.Fatalf("escape target = %d, want the dismiss action", canvas.EscapeTarget())
			}

			state.Click(test.hit)
			if state.modal != modalNone {
				t.Fatal("answering did not close the dialog")
			}
			if !equalBools(fake.trusted, test.trusted) || fake.rejected != test.reject {
				t.Fatalf("trusted=%v rejected=%d, want %v/%d", fake.trusted, fake.rejected, test.trusted, test.reject)
			}
		})
	}
}

func equalBools(left, right []bool) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestPasswordDialogMasksSubmitsOnEnterAndAsksAgainAfterARefusal(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 1 })
	state.Wake()
	if state.modal != modalPassword || state.passwordError != "" {
		t.Fatalf("modal=%d error=%q", state.modal, state.passwordError)
	}

	editor := state.EditorFor(hitPasswordField)
	if editor == nil || state.EditorFor(hitDialogLabel) != nil {
		t.Fatal("only the password field may resolve to the password editor")
	}
	app.TextEvent{Kind: app.TextInsert, Text: "hunter2"}.Apply(editor)

	canvas, texts := paintedTexts(state)
	if contains(texts, "hunter2") {
		t.Fatalf("the password was painted: %v", texts)
	}
	var field gfx.Hit
	for _, hit := range canvas.Hits() {
		if hit.ID == hitPasswordField {
			field = hit
		}
	}
	if !field.Secure || field.Value != "" || field.Role != gfx.RoleTextField {
		t.Fatalf("password hit = %+v, want a secure field carrying no value", field)
	}
	if canvas.InitialFocus() != hitPasswordField {
		t.Fatal("the password field does not take focus when the dialog opens")
	}

	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitPasswordField}); !result.Handled {
		t.Fatal("Enter in the password field was not handled")
	}
	if len(fake.passwords) != 1 || fake.passwords[0] != "hunter2" {
		t.Fatalf("submitted passwords = %v", fake.passwords)
	}
	if state.modal != modalNone || len(state.passwordFields) != 0 {
		t.Fatal("the dialog stayed open or the password was kept after submitting")
	}

	// The server refused it and asks again.
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 2 })
	state.Wake()
	if state.modal != modalPassword || state.passwordError != state.messages.PasswordRetry || state.passwordFields[0].String() != "" {
		t.Fatalf("retry: modal=%d error=%q kept=%q", state.modal, state.passwordError, state.passwordFields[0].String())
	}
	if _, texts := paintedTexts(state); !contains(texts, state.messages.PasswordRetry) {
		t.Fatal("the retry message is not shown")
	}
}

func TestThePasswordDialogHintAndErrorLinesDoNotOverlap(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	askForPassword(state, fake, 2) // a refused password, with no vault to save into
	state.secrets.canSave = false
	if state.modal != modalPassword || state.passwordError == "" {
		t.Fatalf("modal %d, error %q", state.modal, state.passwordError)
	}

	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.PasswordRetry) || !contains(texts, state.messages.PasswordHintUnsaved) {
		t.Fatalf("the dialog does not show both lines: %v", texts)
	}
	var hint, problem gfx.Rect
	for _, text := range canvas.Texts() {
		switch text.Value {
		case state.messages.PasswordHintUnsaved:
			hint = text.Bounds
		case state.messages.PasswordRetry:
			problem = text.Bounds
		}
	}
	overlap := hint.Y < problem.Bottom() && problem.Y < hint.Bottom() &&
		hint.X < problem.Right() && problem.X < hint.Right()
	if overlap {
		t.Fatalf("the hint line %v overlaps the error line %v", hint, problem)
	}
	if height := state.passwordBodyHeight(); height < 66+18+18 {
		t.Fatalf("body height %v does not reserve the hint and error lines", height)
	}
}

func TestEnterOutsideThePasswordFieldDoesNotSubmit(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 1 })
	state.Wake()
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitPasswordCancel}); result.Handled {
		t.Fatal("Enter on a button was taken as a password submit")
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitPasswordField, Repeat: true}); result.Handled {
		t.Fatal("a held Enter submitted the password")
	}
	if len(fake.passwords) != 0 {
		t.Fatalf("passwords were submitted: %v", fake.passwords)
	}
}

func TestDismissingThePasswordDialogCancelsTheConnection(t *testing.T) {
	for _, hit := range []gfx.ID{hitPasswordCancel, hitPasswordClose, hitPasswordDismiss} {
		world, options := newWorld(t)
		state := newTestState(t, options)
		fake := connect(t, state, world)
		fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 1 })
		state.Wake()
		app.TextEvent{Kind: app.TextInsert, Text: "typed"}.Apply(state.EditorFor(hitPasswordField))
		state.Click(hit)
		if fake.canceled != 1 || len(fake.passwords) != 0 || state.modal != modalNone || len(state.passwordFields) != 0 {
			t.Fatalf("hit %d: canceled=%d passwords=%v modal=%d kept=%d", hit, fake.canceled, fake.passwords, state.modal, len(state.passwordFields))
		}
	}
}

// A changed key is a warning with the old and the presented fingerprint, and
// its primary action replaces the saved record rather than treating the key as
// a first connection.
func TestChangedHostKeyDialogShowsBothFingerprintsAndUpdates(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	changed := sshconn.HostKeyInfo{
		Address: "prod.example.com:22", Type: "ssh-ed25519", Fingerprint: "SHA256:NEW",
		Changed: true, Known: []string{"SHA256:OLD"}, File: "/tmp/known_hosts",
	}
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseHostKeyPrompt; s.HostKey = changed })
	state.Wake()
	if state.modal != modalHostKey {
		t.Fatal("a changed host key did not open the dialog")
	}
	_, texts := paintedTexts(state)
	for _, want := range []string{
		state.messages.HostKeyChangedTitle, state.messages.HostKeySavedFingerprint,
		state.messages.HostKeyUpdateAndContinue, "SHA256:OLD", "SHA256:NEW", "prod.example.com:22",
	} {
		if !contains(texts, want) {
			t.Fatalf("the changed-key dialog does not show %q: %q", want, texts)
		}
	}
	state.Click(hitHostKeyTrust)
	if len(fake.trusted) != 1 || !fake.trusted[0] {
		t.Fatalf("update and continue sent trusted=%v, want one save", fake.trusted)
	}
}

func TestAnsweredPromptIsNotShownAgainButANewAttemptIs(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseHostKeyPrompt; s.HostKey = testHostKey })
	state.Wake()
	state.Click(hitHostKeyTrust)

	// More progress arrives while the answer is still travelling.
	fake.set(func(s *sshconn.Status) { s.Banner = "hello" })
	state.Wake()
	if state.modal != modalNone {
		t.Fatal("an answered host-key prompt came back")
	}
}

func TestPromptsFromTwoSessionsAreShownOneAtATime(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	first := connect(t, state, world)
	state.Click(hitHosts)
	state.Click(hitHostList.At(2))
	second := connect(t, state, world)
	for _, fake := range []*fakeConn{first, second} {
		fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseHostKeyPrompt; s.HostKey = testHostKey })
	}
	state.Wake()
	if state.modal != modalHostKey || state.modalSession != state.sessions[0].ID {
		t.Fatalf("modal=%d for session %d, want the first session's prompt", state.modal, state.modalSession)
	}
	state.Click(hitHostKeyTrust)
	if state.modal != modalHostKey || state.modalSession != state.sessions[1].ID {
		t.Fatalf("after answering: modal=%d session=%d, want the second session's prompt", state.modal, state.modalSession)
	}
	state.Click(hitHostKeyReject)
	if state.modal != modalNone || len(first.trusted) != 1 || second.rejected != 1 {
		t.Fatalf("modal=%d first=%v second rejected=%d", state.modal, first.trusted, second.rejected)
	}
}

func TestPromptWaitsForTheNewHostDialog(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	state.Click(hitAddHost)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseHostKeyPrompt; s.HostKey = testHostKey })
	state.Wake()
	if state.modal != modalNewHost {
		t.Fatal("a prompt replaced the dialog the user was filling in")
	}
	state.Click(hitDialogCancel)
	if state.modal != modalHostKey {
		t.Fatal("the waiting prompt did not appear once the dialog closed")
	}
}

func TestEncryptedIdentitiesWithTheSameAttemptEachPrompt(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseKeyPassphrasePrompt
		status.Attempt = 1
		status.IdentityPath = "/keys/first"
	})
	state.Wake()
	if state.modal != modalKeyPassphrase {
		t.Fatal("first identity did not prompt")
	}
	state.Click(hitKeyPassphraseSkip)
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseKeyPassphrasePrompt
		status.Attempt = 1
		status.IdentityPath = "/keys/second"
	})
	state.Wake()
	if state.modal != modalKeyPassphrase || !strings.Contains(state.keyPassphraseDescription, "second") {
		t.Fatalf("second identity prompt = modal %d description %q", state.modal, state.keyPassphraseDescription)
	}
}

func TestStrayDismissDoesNotSkipTheKeyPrompt(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseKeyPassphrasePrompt
		status.Attempt = 1
		status.IdentityPath = "/keys/work"
	})
	state.Wake()
	state.Click(hitKeyPassphraseDismiss)
	if state.modal != modalKeyPassphrase || fake.skippedIdentities != 0 {
		t.Fatalf("a click beside the dialog dropped the key: modal %d, skipped %d", state.modal, fake.skippedIdentities)
	}
	state.Click(hitKeyPassphraseSkip)
	if fake.skippedIdentities != 1 {
		t.Fatalf("Skip did not skip the key: skipped %d", fake.skippedIdentities)
	}
}

func TestAPromptThatCanNoLongerBeAnsweredIsDismissed(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 1 })
	state.Wake()
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = errors.New("connection reset") })
	state.Wake()
	if state.modal != modalNone {
		t.Fatal("the password dialog outlived its connection")
	}
}

func TestConnectionOutcomesBecomeTerminalLines(t *testing.T) {
	changed := sshconn.HostKeyInfo{
		Address: "prod.example.com:22", Fingerprint: "SHA256:NEWKEY", Known: []string{"SHA256:OLDKEY"}, File: "/tmp/known_hosts",
	}
	for _, test := range []struct {
		name   string
		status sshconn.Status
		tone   tone
		want   []string
	}{
		{"connected", sshconn.Status{Phase: sshconn.PhaseConnected, ServerVersion: "SSH-2.0-OpenSSH_9.7", Method: "publickey", Banner: "Authorized use only\n"}, toneOK,
			[]string{"Authorized use only"}},
		{"host key changed", sshconn.Status{Phase: sshconn.PhaseFailed, Err: sshconn.ErrHostKeyChanged, HostKey: changed}, toneFail,
			[]string{"WARNING: THE HOST KEY HAS CHANGED", "SHA256:OLDKEY", "SHA256:NEWKEY", "/tmp/known_hosts"}},
		{"rejected", sshconn.Status{Phase: sshconn.PhaseFailed, Err: sshconn.ErrHostKeyRejected}, toneFail, []string{"Host key rejected. Nothing was sent to the server."}},
		{"canceled", sshconn.Status{Phase: sshconn.PhaseFailed, Err: sshconn.ErrCanceled}, toneFail, []string{"Connection canceled."}},
		{"refused credentials", sshconn.Status{Phase: sshconn.PhaseFailed, Err: sshconn.ErrAuthFailed}, toneFail, []string{"Authentication failed."}},
		{"network error", sshconn.Status{Phase: sshconn.PhaseFailed, Err: errors.New("connect to x:22: connection refused")}, toneFail, []string{"Connection failed: connect to x:22: connection refused"}},
		{"skipped key", sshconn.Status{Phase: sshconn.PhaseConnected, Skipped: []string{"/home/u/.ssh/id_rsa"}}, toneOK, []string{"Skipped /home/u/.ssh/id_rsa for this connection."}},
	} {
		t.Run(test.name, func(t *testing.T) {
			world, options := newWorld(t)
			state := newTestState(t, options)
			fake := connect(t, state, world)
			fake.set(func(s *sshconn.Status) { *s = test.status })
			state.Wake()
			session := state.sessions[0]
			if session.tone != test.tone || session.busy {
				t.Fatalf("tone=%d busy=%v", session.tone, session.busy)
			}
			for _, want := range test.want {
				if !strings.Contains(session.term.screen.Text(), want) {
					t.Fatalf("screen %q does not contain %q", session.term.screen.Text(), want)
				}
			}
			if test.name == "connected" {
				for _, hidden := range []string{"Waiting for the server", "Connected to", "Server:", "Authenticated with"} {
					if strings.Contains(session.term.screen.Text(), hidden) {
						t.Fatalf("screen %q still contains %q", session.term.screen.Text(), hidden)
					}
				}
			}
			// The log is the terminal's screen, one click from the connection page.
			state.Click(hitConnLog)
			if _, texts := paintedTexts(state); !contains(texts, test.want[0]) {
				t.Fatalf("the terminal does not paint %q", test.want[0])
			}
		})
	}
}

func TestOpeningTheSameHostAgainReusesItsWorkspace(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.startupReuse = true
	connect(t, state, world)
	firstID := state.activeSessionID
	firstWorkspace := state.deck.ActiveWorkspace()

	state.Click(hitHosts)
	state.selectedHostID = state.hosts[1].ID
	connect(t, state, world)

	state.Click(hitHosts)
	state.selectedHostID = state.hosts[0].ID
	openHost(state)
	if len(world.conns) != 2 || len(state.sessions) != 2 {
		t.Fatalf("conns=%d sessions=%d, want only the two already opened", len(world.conns), len(state.sessions))
	}
	if state.activeSessionID != firstID || state.deck.ActiveWorkspace() != firstWorkspace || state.activeSection != sectionTerminal {
		t.Fatalf("active session=%d workspace=%d section=%d, want session=%d workspace=%d on the terminal page", state.activeSessionID, state.deck.ActiveWorkspace(), state.activeSection, firstID, firstWorkspace)
	}
	world.conns[0].set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = sshconn.ErrAuthFailed })
	state.Wake()
	state.Click(hitHosts)
	openHost(state)
	if len(world.conns) != 3 || len(state.sessions) != 2 || state.activeSessionID != firstID {
		t.Fatalf("conns=%d sessions=%d active=%d, want one retry in the original session %d", len(world.conns), len(state.sessions), state.activeSessionID, firstID)
	}
}

func TestOpeningAHostReusesOnlyItsDialedRoute(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*State)
		change func(*State)
		reuse  bool
	}{
		{name: "unchanged jump", setup: func(state *State) {
			state.proxies = []proxyRecord{{ID: 1, TargetID: state.hosts[0].ID, JumpID: state.hosts[1].ID}}
		}, reuse: true},
		{name: "outbound proxy changes", change: func(state *State) {
			state.proxy = outboundProxy{kind: sshconn.ProxySOCKS5, address: "localhost:1080"}
		}},
		{name: "jump added", change: func(state *State) {
			state.proxies = []proxyRecord{{ID: 1, TargetID: state.hosts[0].ID, JumpID: state.hosts[1].ID}}
		}},
		{name: "jump authentication changes", setup: func(state *State) {
			state.proxies = []proxyRecord{{ID: 1, TargetID: state.hosts[0].ID, JumpID: state.hosts[1].ID}}
		}, change: func(state *State) {
			state.hosts[1].Auth = authPassword
		}},
		{name: "target algorithms change", change: func(state *State) {
			state.hosts[0].Algo = &hostAlgo{mode: algoCompatible}
		}},
		{name: "target credential changes", change: func(state *State) {
			state.hosts[0].Credential = "changed"
		}},
		{name: "jump algorithms change", setup: func(state *State) {
			state.proxies = []proxyRecord{{ID: 1, TargetID: state.hosts[0].ID, JumpID: state.hosts[1].ID}}
		}, change: func(state *State) {
			state.hosts[1].Algo = &hostAlgo{mode: algoCompatible}
		}},
		{name: "jump endpoint changes", setup: func(state *State) {
			state.proxies = []proxyRecord{{ID: 1, TargetID: state.hosts[0].ID, JumpID: state.hosts[1].ID}}
		}, change: func(state *State) {
			state.hosts[1].Address = "other-jump.example.com"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			world, options := newSharingWorld(t)
			state := newTestState(t, options)
			state.startupReuse = true
			if test.setup != nil {
				test.setup(state)
			}
			world.connectFirst(t, state)
			first := state.activeSessionID
			if test.change != nil {
				test.change(state)
			}
			opened := state.openHostSession(state.hosts[0])
			if (opened.ID == first) != test.reuse {
				t.Fatalf("opened session %d, original %d, reuse=%v", opened.ID, first, test.reuse)
			}
			if !test.reuse && (len(world.conns) != 2 || len(world.shared) != 0) {
				t.Fatalf("changed route: dials=%d shared=%d", len(world.conns), len(world.shared))
			}
		})
	}
}

func TestOpeningTheSameHostAgainPrefersTheActiveLiveSession(t *testing.T) {
	for _, ended := range []bool{false, true} {
		t.Run(map[bool]string{false: "active live session", true: "active failed session"}[ended], func(t *testing.T) {
			world, options := newWorld(t)
			state := newTestState(t, options)
			state.startupReuse = true
			connect(t, state, world)
			firstID := state.activeSessionID
			second := state.newHostSession(state.hosts[0])
			wantID := second.ID
			if ended {
				world.conns[1].set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = sshconn.ErrAuthFailed })
				state.Wake()
				wantID = firstID
			}
			state.Click(hitHosts)
			openHost(state)
			if state.activeSessionID != wantID || len(world.conns) != 2 || len(state.sessions) != 2 {
				t.Fatalf("active=%d conns=%d sessions=%d, want the existing live session %d", state.activeSessionID, len(world.conns), len(state.sessions), wantID)
			}
		})
	}
}

func TestOpeningAHostAfterItsAddressChangesRetriesTheExistingSession(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.startupReuse = true
	connect(t, state, world)
	firstID := state.activeSessionID
	previous := state.hosts[0]
	updated := previous
	updated.Address = "new-prod.example.com"
	state.replaceHost(0, previous, updated)
	world.conns[0].set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = sshconn.ErrDisconnected })
	state.Wake()
	state.Click(hitHosts)
	openHost(state)
	if len(world.conns) != 2 || len(state.sessions) != 1 || state.activeSessionID != firstID {
		t.Fatalf("conns=%d sessions=%d active=%d, want one retry in session %d", len(world.conns), len(state.sessions), state.activeSessionID, firstID)
	}
	if state.sessions[0].Address != state.sessionAddress(updated) {
		t.Fatalf("retried address=%q, want %q", state.sessions[0].Address, state.sessionAddress(updated))
	}
}

func TestOpeningANewAdHocAddressDoesNotReuseAFailedTarget(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	oldHost := Host{Label: "old target", Address: "old.example.com", Protocol: protocolSSH, Port: 22}
	old := state.createHostSession(oldHost, oldHost.Address)
	world.conns[0].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseFailed
		status.Err = sshconn.ErrDisconnected
	})
	state.Wake()

	newHost := Host{Label: "new target", Address: "new.example.com", Protocol: protocolSSH, Port: 22}
	opened := state.openHostSession(newHost)
	if opened == nil || opened.ID == old.ID || len(state.sessions) != 2 || len(world.conns) != 2 {
		t.Fatalf("opened=%+v sessions=%d conns=%d, want a new session for the new address", opened, len(state.sessions), len(world.conns))
	}
	if state.sessions[0].Address != state.sessionAddress(oldHost) || state.sessions[1].Address != state.sessionAddress(newHost) {
		t.Fatalf("session addresses = %q, %q; want separate old/new targets", state.sessions[0].Address, state.sessions[1].Address)
	}
}

func TestOpeningAHostAfterAddingAnUnrelatedIdentityReusesItsSession(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.startupReuse = true
	connect(t, state, world)
	firstID := state.activeSessionID
	state.identities = append(state.identities, identityRecord{ID: 99, Path: filepath.Join(t.TempDir(), "unrelated-key")})
	state.Click(hitHosts)
	openHost(state)
	if len(world.conns) != 1 || len(state.sessions) != 1 || state.activeSessionID != firstID {
		t.Fatalf("conns=%d sessions=%d active=%d, want the existing session %d", len(world.conns), len(state.sessions), state.activeSessionID, firstID)
	}
}

func TestOpeningAHostAfterProtocolEditUsesItsNewProtocol(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	previous := state.hosts[0]
	previous.Protocol = protocolVNC
	state.hosts[0] = previous
	// VNC and RDP can share the same address and explicit port.
	first := state.newHostSession(previous)
	firstID := first.ID
	updated := previous
	updated.Protocol = protocolRDP
	state.replaceHost(0, previous, updated)
	state.Click(hitHosts)
	openHost(state)
	if len(world.conns) != 0 || len(state.sessions) != 2 || state.activeSessionID == firstID || state.sessions[1].dialedProtocol != protocolRDP {
		t.Fatalf("sessions=%d active=%d, want a separate RDP session", len(state.sessions), state.activeSessionID)
	}
}

func TestOpeningTheSameHostAgainCreatesAnotherTabWithReuseOff(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitHosts)
	first := connect(t, state, world)
	firstSessionID := state.sessions[0].ID

	state.Click(hitHosts)
	openHost(state)
	if len(world.conns) != 2 || len(state.sessions) != 2 {
		t.Fatalf("conns=%d sessions=%d, want a second tab", len(world.conns), len(state.sessions))
	}
	if state.sessions[0].HostID != state.sessions[1].HostID || state.sessions[1].ID == firstSessionID || state.activeSessionID != state.sessions[1].ID {
		t.Fatalf("sessions=%+v active=%d", state.sessions, state.activeSessionID)
	}

	first.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = sshconn.ErrAuthFailed })
	state.Wake()
	state.Click(hitHosts)
	openHost(state)
	if len(world.conns) != 3 || len(state.sessions) != 3 {
		t.Fatalf("conns=%d sessions=%d, want a third tab", len(world.conns), len(state.sessions))
	}
}

func TestWakeAndConnectingPaintDoNotAllocateWhenNothingChanged(t *testing.T) {
	world, options := newWorld(t)
	options.Now = func() time.Duration { return 0 } // the elapsed text is rebuilt once a second
	state := newTestState(t, options)
	connect(t, state, world)
	state.Wake()
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)

	if allocations := testing.AllocsPerRun(100, state.Wake); allocations != 0 {
		t.Fatalf("Wake without a change allocated %v times", allocations)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("painting a connecting session allocated %v times", allocations)
	}
}

func TestWindowCloseCancelsOwnedConnectionsOnce(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	state.Close()
	state.Close()
	fake.mu.Lock()
	canceled := fake.canceled
	fake.mu.Unlock()
	if canceled != 1 || !state.closed {
		t.Fatalf("canceled=%d closed=%t", canceled, state.closed)
	}
}

func TestUnexpectedDisconnectIsReportedAsAFailure(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseClosed
		status.Err = fmt.Errorf("%w: %w", sshconn.ErrDisconnected, sshconn.ErrKeepAliveTimeout)
	})
	state.Wake()
	session := &state.sessions[0]
	if session.tone != toneFail || !errors.Is(session.err, sshconn.ErrKeepAliveTimeout) || state.status != state.messages.StatusConnectFailed+session.Title {
		t.Fatalf("tone=%d err=%v status=%q", session.tone, session.err, state.status)
	}
	if text := session.term.screen.Text(); !strings.Contains(text, "keepalive timed out") || !strings.Contains(text, state.messages.SessionReconnectHint) {
		t.Fatalf("terminal failure text = %q", text)
	}
}

func TestOneKeyboardInteractiveRoundShowsAFieldPerQuestion(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) {
		s.Phase, s.Attempt = sshconn.PhasePasswordPrompt, 1
		s.Endpoint = sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}
		s.Questions = []sshconn.Question{
			{Text: "Password: ", Secret: true},
			{Text: "Verification code: ", Secret: true},
			{Text: "Account: ", Secret: false},
		}
	})
	state.Wake()
	if state.modal != modalPassword || len(state.passwordFields) != 3 {
		t.Fatalf("modal=%d fields=%d", state.modal, len(state.passwordFields))
	}
	answers := []string{"hunter2", "123456", "deploy"}
	for index, answer := range answers {
		editor := state.EditorFor(hitPasswordFields.At(index + 1))
		if editor == nil {
			t.Fatalf("question %d has no editor", index)
		}
		app.TextEvent{Kind: app.TextInsert, Text: answer}.Apply(editor)
	}

	_, texts := paintedTexts(state)
	for _, want := range []string{"Password:", "Verification code:", "Account:"} {
		if !contains(texts, want) {
			t.Fatalf("the dialog does not ask %q: %v", want, texts)
		}
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	secure := map[gfx.ID]bool{}
	for _, hit := range canvas.Hits() {
		if index, ok := hit.ID.In(hitPasswordFields); ok {
			secure[hit.ID] = hit.Secure
			if index != 1 && index != 2 && index != 3 {
				t.Fatalf("password field %d is not one of the questions", index)
			}
		}
	}
	if len(secure) != 3 || !secure[hitPasswordFields.At(1)] || !secure[hitPasswordFields.At(2)] || secure[hitPasswordFields.At(3)] {
		t.Fatalf("secure flags = %+v", secure)
	}

	// The password is the only answer the save box may keep.
	state.secrets.save = true
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitPasswordFields.At(3)}); !result.Handled {
		t.Fatal("Enter in a question field was not handled")
	}
	if len(fake.answers) != 1 || len(fake.answers[0]) != 3 ||
		fake.answers[0][0] != "hunter2" || fake.answers[0][1] != "123456" || fake.answers[0][2] != "deploy" {
		t.Fatalf("submitted answers = %v", fake.answers)
	}
	session := &state.sessions[0]
	if session.pendingSecretName != prodPasswordName || session.pendingSecretValue != "hunter2" {
		t.Fatalf("saved secret = %q %q", session.pendingSecretName, session.pendingSecretValue)
	}
}

func TestMultiQuestionDialogFillsThePasswordAndTheCurrentCode(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	endpoint := sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}
	if state.secrets.vault == nil {
		t.Fatal("the test vault did not open")
	}
	if err := state.secrets.vault.Set(prodPasswordName, "hunter2"); err != nil {
		t.Fatal(err)
	}
	seed := "otpauth://totp/Prod:deploy?secret=" + testOTPSeed + "&issuer=Prod"
	if err := state.secrets.vault.Set(prodOTPName, seed); err != nil {
		t.Fatal(err)
	}
	fake.set(func(s *sshconn.Status) {
		s.Phase, s.Attempt, s.Endpoint = sshconn.PhasePasswordPrompt, 1, endpoint
		s.Questions = []sshconn.Question{
			{Text: "Password: ", Secret: true},
			{Text: "Verification code: ", Secret: true},
		}
	})
	state.Wake()
	if state.modal != modalPassword || len(state.passwordFields) != 2 {
		t.Fatalf("modal=%d fields=%d", state.modal, len(state.passwordFields))
	}
	if got := state.passwordFields[0].String(); got != "hunter2" {
		t.Fatalf("password field = %q, want the saved password", got)
	}
	if code := state.passwordFields[1].String(); len(code) != 6 {
		t.Fatalf("code field = %q, want a six-digit code", code)
	}
}

// A round that still asks for a one-time code must not claim the password was
// refused: the server may have rejected either factor, and the saved password
// has to survive.
func TestARepeatedSecondFactorRoundDoesNotBlameThePassword(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	session := &state.sessions[0]
	session.filledSecret = state.passwordName(session)
	fake.set(func(status *sshconn.Status) {
		status.Phase, status.Attempt = sshconn.PhasePasswordPrompt, 2
		status.Questions = []sshconn.Question{{Secret: true, Text: "Password: "}, {Secret: true, Text: "Verification code: "}}
		status.Endpoint = sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}
	})
	state.Wake()
	if state.modal != modalPassword || state.passwordError != state.messages.PasswordRetryFactor {
		t.Fatalf("modal %d error %q", state.modal, state.passwordError)
	}
	if session.filledSecret == "" {
		t.Fatal("a second-factor round forgot the saved password")
	}
}

// The user checks a host key's fingerprint against one they were given, often by
// pasting it: a drag across the dialog's fingerprint selects it as the server sent
// it, and the dialog's buttons still answer the prompt.
func TestHostKeyDialogFingerprintCanBeSelected(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseHostKeyPrompt; s.HostKey = testHostKey })
	state.Wake()
	canvas := &gfx.Canvas{}
	driver, err := app.NewDriver(state.WindowConfig(app.Config{
		Title: "host key", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
	}), canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	var line gfx.Text
	for _, text := range driver.Snapshot().Texts {
		if text.Value == testHostKey.Fingerprint {
			line = text
		}
	}
	if line.Value == "" {
		t.Fatal("the dialog does not show the fingerprint")
	}
	y := line.Bounds.Y + line.Bounds.Height/2
	start, end := line.Bounds.X+1, line.Bounds.X+canvas.TextWidthStyle(line.Value, line.Style)+8
	for _, step := range []func() error{
		func() error { return driver.Press(start, y, gfx.PointerButtonPrimary) },
		func() error { return driver.Move((start+end)/2, y) },
		func() error { return driver.Move(end, y) },
		func() error { return driver.Release(end, y, gfx.PointerButtonPrimary) },
		driver.Paint,
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if got := state.hostKeySelection.Text(); got != testHostKey.Fingerprint {
		t.Fatalf("the drag selected %q, want the fingerprint %q", got, testHostKey.Fingerprint)
	}
	state.Click(hitHostKeyTrust)
	if len(fake.trusted) != 1 {
		t.Fatalf("trust after selecting sent %v", fake.trusted)
	}
}
