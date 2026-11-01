package sshconn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// A proxy command is a local program whose standard input and output carry the
// first connection of a session, as OpenSSH's ProxyCommand does: it is how a
// host behind `cloudflared access ssh`, `nc` through a gateway or a site's own
// connector is reached. The program runs only while a connection uses it.

// maxProxyCommand bounds a proxy command line.
const maxProxyCommand = 2048

// maxProxyCommandStderr bounds what is kept of the command's error output, to
// say why it failed.
const maxProxyCommandStderr = 4 << 10

// ExpandProxyCommand fills in the tokens of a proxy command for one connection,
// those of OpenSSH that Motty knows: %h the host, %p the port, %r the user and
// %% a percent sign. A value that a shell or the Windows command line would read
// as syntax is refused, so a host name cannot carry a command of its own.
func ExpandProxyCommand(command, host string, port int, user string) (string, error) {
	if err := checkProxyCommandLine(command); err != nil {
		return "", err
	}
	for _, value := range [...]string{host, user} {
		if !plainProxyValue(value) {
			return "", fmt.Errorf("the proxy command cannot be given %q", value)
		}
	}
	var line strings.Builder
	for index := 0; index < len(command); index++ {
		if command[index] != '%' {
			line.WriteByte(command[index])
			continue
		}
		index++
		if index == len(command) {
			return "", errors.New("the proxy command ends with a lone %")
		}
		switch command[index] {
		case 'h':
			line.WriteString(host)
		case 'p':
			line.WriteString(strconv.Itoa(port))
		case 'r':
			if user == "" {
				return "", errors.New("the proxy command uses %r, but this host has no user name")
			}
			line.WriteString(user)
		case '%':
			line.WriteByte('%')
		default:
			return "", fmt.Errorf("the proxy command uses %%%c; only %%h, %%p, %%r and %%%% are known", command[index])
		}
	}
	return line.String(), nil
}

// CheckProxyCommand reports whether command can be saved as a proxy command.
func CheckProxyCommand(command string) error {
	_, err := ExpandProxyCommand(command, "host", 22, "user")
	return err
}

func checkProxyCommandLine(command string) error {
	switch {
	case strings.TrimSpace(command) == "":
		return errors.New("the proxy command is empty")
	case len(command) > maxProxyCommand:
		return fmt.Errorf("the proxy command is longer than %d bytes", maxProxyCommand)
	case strings.ContainsAny(command, "\x00\r\n"):
		return errors.New("the proxy command must be one line")
	}
	return nil
}

// plainProxyValue reports whether a host or user name is made only of letters,
// digits and the punctuation of addresses and accounts, and cannot be read as an
// option by the program it is given to: OpenSSH refuses a leading dash too.
func plainProxyValue(value string) bool {
	if strings.HasPrefix(value, "-") {
		return false
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(".-_:@+", r) {
			return false
		}
	}
	return true
}

// dialCommand starts the proxy command for host:port and returns its input and
// output as the connection.
func (proxy *Proxy) dialCommand(ctx context.Context, host string, port int, user string) (net.Conn, error) {
	line, err := ExpandProxyCommand(proxy.Command, host, port, user)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	process, err := proxyCommandProcess(line)
	if err != nil {
		return nil, err
	}
	stdin, err := process.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("start the proxy command: %w", err)
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("start the proxy command: %w", err)
	}
	stderr := &tailWriter{}
	process.Stderr = stderr
	// A program the command left running must not hold the wait open forever
	// through the error output it inherited.
	process.WaitDelay = time.Second
	tree, err := startProxyCommand(process)
	if err != nil {
		return nil, fmt.Errorf("start the proxy command: %w", err)
	}
	// The process's pipes do not take deadlines on every platform; a pipe in
	// between does, and the copies below move the bytes across.
	near, far := net.Pipe()
	conn := &commandConn{
		Conn: near, remote: commandAddr(net.JoinHostPort(host, strconv.Itoa(port))),
		process: process, tree: tree, exited: make(chan struct{}),
	}
	go func() {
		_, _ = io.Copy(stdin, far)
		stdin.Close()
	}()
	go func() {
		_, _ = io.Copy(far, stdout)
		err := process.Wait()
		conn.failure = proxyCommandFailure(err, stderr.String())
		tree.release()
		close(conn.exited)
		far.Close()
	}()
	return conn, nil
}

// proxyCommandTree is the program a proxy command started and every process it
// started in turn: a pipeline, a wrapper script or a connector's helper would
// otherwise outlive the connection that asked for them. kill ends them all;
// release lets go of what tracks them once the program is gone, and may be
// called more than once.
type proxyCommandTree struct {
	kill, release func()
}

// commandConn is a running proxy command as a connection. Closing it ends the
// program.
type commandConn struct {
	net.Conn
	remote  commandAddr
	process *exec.Cmd
	// tree ends the program and whatever it started.
	tree proxyCommandTree
	// exited is closed once the program has ended; failure then says why, when
	// it ended badly.
	exited  chan struct{}
	failure error
	closing atomic.Bool
	once    sync.Once
}

// Read reports the program's own failure, instead of a bare end of input, when
// the connection ends because the program did.
func (conn *commandConn) Read(buffer []byte) (int, error) {
	n, err := conn.Conn.Read(buffer)
	if errors.Is(err, io.EOF) && !conn.closing.Load() {
		<-conn.exited
		if conn.failure != nil {
			return n, conn.failure
		}
	}
	return n, err
}

func (conn *commandConn) Close() error {
	conn.once.Do(func() {
		conn.closing.Store(true)
		conn.Conn.Close()
		conn.tree.kill()
		conn.tree.release()
	})
	return nil
}

// RemoteAddr is the destination the command reaches. Host key checks read the
// remote address as host:port, which a pipe's own address is not.
func (conn *commandConn) RemoteAddr() net.Addr { return conn.remote }

type commandAddr string

func (addr commandAddr) Network() string { return "proxy-command" }
func (addr commandAddr) String() string  { return string(addr) }

// proxyCommandFailure explains how the program ended, or is nil when it ended
// cleanly and said nothing.
func proxyCommandFailure(err error, stderr string) error {
	last := lastLine(stderr)
	switch {
	case err == nil && last == "":
		return nil
	case err == nil:
		return fmt.Errorf("the proxy command ended: %s", last)
	case last == "":
		return fmt.Errorf("the proxy command failed: %w", err)
	}
	return fmt.Errorf("the proxy command failed (%w): %s", err, last)
}

// lastLine is the last line of a program's output that says something, with
// control characters dropped and cut to a length an error message can carry.
func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	last := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(lines[len(lines)-1]))
	if len(last) > 300 {
		cut := 300
		for cut > 0 && !utf8.RuneStart(last[cut]) {
			cut--
		}
		last = last[:cut] + "…"
	}
	return last
}

// tailWriter keeps the end of what is written to it.
type tailWriter struct {
	mu   sync.Mutex
	data []byte
}

func (writer *tailWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.data = append(writer.data, data...)
	if extra := len(writer.data) - maxProxyCommandStderr; extra > 0 {
		writer.data = append(writer.data[:0], writer.data[extra:]...)
	}
	return len(data), nil
}

func (writer *tailWriter) String() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return string(writer.data)
}
