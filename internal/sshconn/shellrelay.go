package sshconn

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// A jump host that only offers a shell, such as a bastion that has the user
// switch accounts first or reach the inner network from a menu, cannot forward
// a connection with a direct-tcpip channel. A Relay carries the next hop's
// connection through that shell instead: the shell is started on a terminal,
// the user's own command (the Prelude) is typed into it, and a relay program
// such as nc is then started in its place, so that the terminal's input and
// output are the bytes of the next hop's TCP connection. The SSH handshake with
// the next hop runs on them like on any other socket.

// maxRelayCommand bounds a relay command line.
const maxRelayCommand = 2048

// Timing of a relay's start. The shell is taken as ready once it has been quiet
// for relayQuiet after saying something, or after relayGrace if it says nothing.
const (
	relayQuiet = 400 * time.Millisecond
	relayGrace = 4 * time.Second
	// relayNoise bounds what is kept of the shell's output before the stream
	// starts, to say what went wrong when it does not.
	relayNoise = 64 << 10
)

// relayStartTimeout bounds the time from opening the shell to the first byte of
// the next hop's SSH stream. It is a variable so that tests can shorten it.
var relayStartTimeout = 30 * time.Second

// Relay says how the connection to an endpoint leaves the hop before it.
type Relay struct {
	// Prelude is typed into the hop's shell, as the user would, before the relay
	// program starts: the command that switches to the account which can reach
	// the endpoint. It is one line and may be empty.
	Prelude string
	// Command is the relay program, with the tokens of ExpandProxyCommand. It
	// must connect its standard input and output to host:port, as `nc %h %p`.
	Command string
}

// CheckRelay reports whether relay can be used for a connection.
func CheckRelay(relay Relay) error {
	if err := checkRelayPrelude(relay.Prelude); err != nil {
		return err
	}
	_, err := expandRelay(relay, "host", 22, "user")
	return err
}

func checkRelayPrelude(prelude string) error {
	if len(prelude) > maxRelayCommand {
		return fmt.Errorf("the command run before the relay is longer than %d bytes", maxRelayCommand)
	}
	if strings.ContainsFunc(prelude, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return errors.New("the command run before the relay must be one line")
	}
	return nil
}

func expandRelay(relay Relay, host string, port int, user string) (string, error) {
	line, err := ExpandProxyCommand(relay.Command, host, port, user)
	if err != nil {
		return "", errors.New(strings.Replace(err.Error(), "the proxy command", "the relay command", 1))
	}
	return line, nil
}

// relayLine is the line that turns the shell's terminal into a byte stream and
// starts the relay program. The terminal is made transparent first (no echo, no
// line editing, no translation of line ends, no signals from control bytes),
// then the marker is printed, then the program replaces the shell. The marker
// is written as two arguments, so the echo of this very line never contains it.
// If anything fails the shell ends, so the connection fails at once instead of
// waiting for a stream that will not come.
func relayLine(command, nonce string) string {
	return "stty raw -echo -iexten && printf '%s%s\\n' MOT TY" + nonce + " && exec " + command + "; exit"
}

const relayMarker = "MOTTY"

// dialRelay opens the connection to next through a relay run in a shell on
// previous, and returns it once the next hop's SSH stream has begun.
func (conn *Conn) dialRelay(previous *ssh.Client, previousEndpoint Endpoint, relay Relay, next Endpoint) (net.Conn, error) {
	command, err := expandRelay(relay, next.Host, int(next.Port), next.User)
	if err != nil {
		return nil, err
	}
	if err := checkRelayPrelude(relay.Prelude); err != nil {
		return nil, err
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	nonce := hex.EncodeToString(random[:])

	session, err := previous.NewSession()
	if err != nil {
		return nil, fmt.Errorf("open a shell on %s: %w", previousEndpoint.address(), err)
	}
	// Only an implementation known to take it is asked for a locale: a gateway
	// with a menu of its own may end the connection on a request it lacks.
	if standardImplementation(string(previous.ServerVersion())) {
		_ = session.Setenv("LANG", utf8Locale())
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}
	// The terminal is wide so that a long typed line is not wrapped by the
	// shell's own line editor.
	if err := session.RequestPty("xterm", 24, 240, modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("open a terminal on %s: %w", previousEndpoint.address(), err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	if err := session.Shell(); err != nil {
		session.Close()
		return nil, fmt.Errorf("start a shell on %s: %w", previousEndpoint.address(), err)
	}
	go session.Wait()

	start := &relayStart{
		session: session, stdin: stdin, stdout: stdout, hop: previousEndpoint,
		marker: relayMarker + nonce + "\n",
		chunks: make(chan []byte), stop: make(chan struct{}),
	}
	go start.read()
	socket, err := start.run(conn.ctx, relay.Prelude, relayLine(command, nonce), net.JoinHostPort(next.Host, strconv.Itoa(int(next.Port))))
	if err != nil {
		start.abandon()
		return nil, err
	}
	return socket, nil
}

// relayStart is a relay on its way up: the hop's shell, and what it has said.
type relayStart struct {
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	hop     Endpoint
	marker  string

	// The reader sends the shell's output in chunks, and closes chunks when it
	// ends; err then says why.
	chunks chan []byte
	err    error
	stop   chan struct{}
	once   sync.Once
	noise  []byte
}

func (start *relayStart) read() {
	defer close(start.chunks)
	buffer := make([]byte, 4096)
	for {
		n, err := start.stdout.Read(buffer)
		if n > 0 {
			select {
			case start.chunks <- append([]byte(nil), buffer[:n]...):
			case <-start.stop:
				return
			}
		}
		if err != nil {
			start.err = err
			return
		}
	}
}

// abandon ends a relay that did not come up.
func (start *relayStart) abandon() {
	start.once.Do(func() {
		close(start.stop)
		// Closing the session ends the channel, which is safe beside a write in
		// progress; closing only its input is not.
		start.session.Close()
	})
}

// run drives the shell to the point where the relay program's output begins,
// and wraps it as a connection. It waits for the shell to be ready, types the
// prelude and the relay line, skips everything up to the marker and then
// everything up to the first line of the SSH stream.
func (start *relayStart) run(parent context.Context, prelude, line, destination string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(parent, relayStartTimeout)
	defer cancel()

	if err := start.waitQuiet(ctx); err != nil {
		return nil, err
	}
	var typed strings.Builder
	if prelude != "" {
		typed.WriteString(prelude)
		typed.WriteByte('\r')
	}
	typed.WriteString(line)
	typed.WriteByte('\r')
	if _, err := io.WriteString(start.stdin, typed.String()); err != nil {
		return nil, start.fail(ctx, err)
	}
	// Both lines are typed ahead: the terminal holds the second until the
	// prelude's shell reads it, which is what it would do for a person typing
	// quickly.
	data, err := start.until(ctx, start.marker)
	if err != nil {
		return nil, err
	}
	// The relay program is running. Its first lines may be a banner or an error
	// message; the stream is what starts with the SSH version.
	data, err = start.untilVersion(ctx, data)
	if err != nil {
		return nil, err
	}
	return start.connection(data, destination), nil
}

// waitQuiet waits until the shell has said something and then fallen quiet, or
// has said nothing for relayGrace.
func (start *relayStart) waitQuiet(ctx context.Context) error {
	timer := time.NewTimer(relayGrace)
	defer timer.Stop()
	for {
		select {
		case chunk, ok := <-start.chunks:
			if !ok {
				return start.fail(ctx, start.err)
			}
			start.remember(chunk)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(relayQuiet)
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return start.fail(ctx, nil)
		}
	}
}

// until reads until pattern has been seen and returns what follows it.
func (start *relayStart) until(ctx context.Context, pattern string) ([]byte, error) {
	for {
		if index := bytes.Index(start.noise, []byte(pattern)); index >= 0 {
			rest := append([]byte(nil), start.noise[index+len(pattern):]...)
			start.noise = start.noise[:0]
			return rest, nil
		}
		select {
		case chunk, ok := <-start.chunks:
			if !ok {
				return nil, start.fail(ctx, start.err)
			}
			start.remember(chunk)
		case <-ctx.Done():
			return nil, start.fail(ctx, nil)
		}
	}
}

// untilVersion skips the relay program's output until a line that begins the
// SSH version exchange, and returns that line and what follows it. What it
// skipped is kept to explain a relay that never speaks SSH.
func (start *relayStart) untilVersion(ctx context.Context, data []byte) ([]byte, error) {
	start.noise = append(start.noise[:0], data...)
	for {
		if index := versionStart(start.noise); index >= 0 {
			rest := append([]byte(nil), start.noise[index:]...)
			start.noise = start.noise[:0]
			return rest, nil
		}
		select {
		case chunk, ok := <-start.chunks:
			if !ok {
				return nil, start.fail(ctx, start.err)
			}
			start.remember(chunk)
		case <-ctx.Done():
			return nil, start.fail(ctx, nil)
		}
	}
}

// versionStart is where the first line that begins with "SSH-" starts, or -1.
func versionStart(data []byte) int {
	if bytes.HasPrefix(data, []byte("SSH-")) {
		return 0
	}
	if index := bytes.Index(data, []byte("\nSSH-")); index >= 0 {
		return index + 1
	}
	return -1
}

func (start *relayStart) remember(chunk []byte) {
	start.noise = append(start.noise, chunk...)
	if extra := len(start.noise) - relayNoise; extra > 0 {
		start.noise = append(start.noise[:0], start.noise[extra:]...)
	}
}

// fail explains why the relay did not come up, with the last thing the hop's
// shell said.
func (start *relayStart) fail(ctx context.Context, cause error) error {
	said := lastVisibleLines(start.noise)
	where := start.hop.address()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		cause = fmt.Errorf("no SSH stream came from the relay on %s within %s", where, relayStartTimeout)
	case ctx.Err() != nil:
		return ctx.Err()
	case cause == nil || errors.Is(cause, io.EOF):
		cause = fmt.Errorf("the shell on %s ended before the relay started", where)
	default:
		cause = fmt.Errorf("the shell on %s failed: %w", where, cause)
	}
	if said != "" {
		return fmt.Errorf("%w; it last said: %s", cause, said)
	}
	return cause
}

// ansiSequence matches the escape sequences a shell and a terminal exchange.
var ansiSequence = regexp.MustCompile("\x1b(\\[[0-9;?<=>]*[ -/]*[@-~]|\\][^\x07\x1b]*(\x07|\x1b\\\\)|[()][0-2AB]|[=>78])")

// lastVisibleLines is the last few lines of terminal output that say something,
// with the terminal's escape sequences removed, as one line of text. The "exit"
// the relay line ends with is the shell's own, not news.
func lastVisibleLines(output []byte) string {
	text := ansiSequence.ReplaceAllString(string(output), "")
	text = strings.ReplaceAll(text, "\r", "\n")
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = lastLine(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 0 && lines[len(lines)-1] == "exit" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " / ")
}

// connection wraps the started relay as a net.Conn that begins with data.
func (start *relayStart) connection(data []byte, destination string) net.Conn {
	near, far := net.Pipe()
	conn := &relayConn{Conn: near, remote: commandAddr(destination), start: start, ended: make(chan struct{})}
	// The input ends when the connection is closed, which abandons the relay.
	go func() { _, _ = io.Copy(start.stdin, far) }()
	go func() {
		_, err := far.Write(data)
		for err == nil {
			chunk, ok := <-start.chunks
			if !ok {
				if !errors.Is(start.err, io.EOF) {
					conn.failure = fmt.Errorf("the relay on %s failed: %w", start.hop.address(), start.err)
				}
				break
			}
			_, err = far.Write(chunk)
		}
		close(conn.ended)
		far.Close()
		start.abandon()
	}()
	return conn
}

// relayConn is the next hop's connection, carried by a relay program in a shell
// on the hop before it. Closing it ends the shell, and with it the program.
type relayConn struct {
	net.Conn
	remote commandAddr
	start  *relayStart

	// ended is closed once the relay's output is over; failure then says why,
	// when it ended badly.
	ended   chan struct{}
	failure error
	closing atomic.Bool
	once    sync.Once
}

// Read reports the relay's own failure, instead of a bare end of input, when the
// stream ends because the shell did.
func (conn *relayConn) Read(buffer []byte) (int, error) {
	n, err := conn.Conn.Read(buffer)
	if errors.Is(err, io.EOF) && !conn.closing.Load() {
		<-conn.ended
		if conn.failure != nil {
			return n, conn.failure
		}
	}
	return n, err
}

func (conn *relayConn) Close() error {
	conn.once.Do(func() {
		conn.closing.Store(true)
		conn.Conn.Close()
		conn.start.abandon()
	})
	return nil
}

// RemoteAddr is the destination the relay reaches. Host key checks read the
// remote address as host:port, which a pipe's own address is not.
func (conn *relayConn) RemoteAddr() net.Addr { return conn.remote }
