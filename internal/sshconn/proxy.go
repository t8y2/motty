package sshconn

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// An outbound proxy carries the first TCP connection of a session: the one to the
// target, or to the first jump host. Everything after that already travels inside
// SSH. SOCKS5 (RFC 1928, with the user/password method of RFC 1929) and HTTP
// CONNECT are supported; the proxy resolves the destination's name, so a host
// that is only known inside the proxy's network works. A proxy command (see
// proxycommand.go) is a local program in the place of the proxy.

// ProxyKind says how a proxy is spoken to.
type ProxyKind uint8

const (
	ProxyNone ProxyKind = iota
	ProxySOCKS5
	ProxyHTTPConnect
	ProxyCommand
)

// Proxy is an outbound proxy.
type Proxy struct {
	Kind ProxyKind
	// Address is the proxy's host:port; a ProxyCommand has none.
	Address string
	// Command is a ProxyCommand's program line, with the tokens of
	// ExpandProxyCommand.
	Command string
	// Username and Password sign in to the proxy; both empty means no login.
	Username, Password string
	// Bypass lists destinations reached directly: a host name, a ".suffix" or
	// "*.suffix", an address, or a CIDR range.
	Bypass []string
	// Err, when set, is why the proxy cannot be used (its saved login is locked):
	// a connection that would go through it fails with this instead.
	Err error
}

// ErrProxyRefused is wrapped by the errors of a proxy that would not connect.
var ErrProxyRefused = errors.New("the proxy refused the connection")

// maxProxyHeader bounds what a proxy may send before the tunnel opens.
const maxProxyHeader = 8 << 10

// Uses reports whether a connection to address (host:port) goes through the
// proxy.
func (proxy *Proxy) Uses(address string) bool {
	if proxy == nil || !proxy.Configured() {
		return false
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	return !proxy.bypassed(host)
}

func (proxy *Proxy) bypassed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
	address, hostIsAddress := netip.ParseAddr(host)
	for _, entry := range proxy.Bypass {
		entry = strings.ToLower(strings.TrimSpace(entry))
		switch {
		case entry == "":
		case strings.Contains(entry, "/"):
			if prefix, err := netip.ParsePrefix(entry); err == nil && hostIsAddress == nil && prefix.Contains(address.Unmap()) {
				return true
			}
		case strings.HasPrefix(entry, "*."), strings.HasPrefix(entry, "."):
			suffix := strings.TrimPrefix(strings.TrimPrefix(entry, "*"), ".")
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				return true
			}
		case entry == host:
			return true
		default:
			if bypass, err := netip.ParseAddr(entry); err == nil && hostIsAddress == nil && bypass.Unmap() == address.Unmap() {
				return true
			}
		}
	}
	return false
}

// Configured reports whether the proxy names somewhere to go: an address, or a
// command for a ProxyCommand.
func (proxy *Proxy) Configured() bool {
	switch proxy.Kind {
	case ProxySOCKS5, ProxyHTTPConnect:
		return proxy.Address != ""
	case ProxyCommand:
		return proxy.Command != ""
	}
	return false
}

// Dial opens a TCP connection to address through the proxy.
func (proxy *Proxy) Dial(ctx context.Context, timeout time.Duration, address string) (net.Conn, error) {
	return proxy.DialAs(ctx, timeout, address, "")
}

// DialAs is Dial for a connection that will sign in as user, whom a proxy
// command can name (%r).
func (proxy *Proxy) DialAs(ctx context.Context, timeout time.Duration, address, user string) (net.Conn, error) {
	if proxy.Err != nil {
		return nil, proxy.Err
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port in %q", address)
	}
	if proxy.Kind == ProxyCommand {
		return proxy.dialCommand(ctx, host, port, user)
	}
	dialer := net.Dialer{Timeout: timeout}
	socket, err := dialer.DialContext(ctx, "tcp", proxy.Address)
	if err != nil {
		return nil, fmt.Errorf("connect to the proxy %s: %w", proxy.Address, err)
	}
	// The handshake gets the same bound as a plain connect, and ends with the
	// context.
	if timeout > 0 {
		_ = socket.SetDeadline(time.Now().Add(timeout))
	}
	stop := context.AfterFunc(ctx, func() { _ = socket.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	var result net.Conn
	switch proxy.Kind {
	case ProxySOCKS5:
		err = proxy.socks5(socket, host, port)
		result = socket
	case ProxyHTTPConnect:
		result, err = proxy.httpConnect(socket, host, port)
	default:
		err = errors.New("no proxy type")
	}
	if err != nil {
		socket.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("through the proxy %s: %w", proxy.Address, err)
	}
	_ = socket.SetDeadline(time.Time{})
	return result, nil
}

var socks5Replies = [...]string{
	0: "succeeded", 1: "general SOCKS server failure", 2: "connection not allowed by ruleset",
	3: "network unreachable", 4: "host unreachable", 5: "connection refused",
	6: "TTL expired", 7: "command not supported", 8: "address type not supported",
}

func (proxy *Proxy) socks5(socket net.Conn, host string, port int) error {
	login := proxy.Username != "" || proxy.Password != ""
	if len(proxy.Username) > 255 || len(proxy.Password) > 255 {
		return errors.New("the proxy login is too long for SOCKS5")
	}
	greeting := []byte{5, 1, 0}
	if login {
		greeting = []byte{5, 2, 0, 2}
	}
	if _, err := socket.Write(greeting); err != nil {
		return err
	}
	var choice [2]byte
	if _, err := io.ReadFull(socket, choice[:]); err != nil {
		return fmt.Errorf("no SOCKS5 answer: %w", err)
	}
	if choice[0] != 5 {
		return errors.New("the server is not a SOCKS5 proxy")
	}
	switch choice[1] {
	case 0:
	case 2:
		if !login {
			return fmt.Errorf("%w: it wants a login", ErrProxyRefused)
		}
		message := append([]byte{1, byte(len(proxy.Username))}, proxy.Username...)
		message = append(message, byte(len(proxy.Password)))
		message = append(message, proxy.Password...)
		if _, err := socket.Write(message); err != nil {
			return err
		}
		var status [2]byte
		if _, err := io.ReadFull(socket, status[:]); err != nil {
			return fmt.Errorf("no answer to the login: %w", err)
		}
		if status[1] != 0 {
			return fmt.Errorf("%w: wrong user name or password", ErrProxyRefused)
		}
	default:
		return fmt.Errorf("%w: no acceptable login method", ErrProxyRefused)
	}

	request := []byte{5, 1, 0}
	if address, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		address = address.Unmap()
		if address.Is4() {
			request = append(append(request, 1), address.AsSlice()...)
		} else {
			request = append(append(request, 4), address.AsSlice()...)
		}
	} else {
		if len(host) == 0 || len(host) > 255 {
			return fmt.Errorf("the host name %q cannot be sent to a SOCKS5 proxy", host)
		}
		request = append(append(request, 3, byte(len(host))), host...)
	}
	request = append(request, byte(port>>8), byte(port))
	if _, err := socket.Write(request); err != nil {
		return err
	}
	var head [4]byte
	if _, err := io.ReadFull(socket, head[:]); err != nil {
		return fmt.Errorf("no answer to the connect request: %w", err)
	}
	if head[0] != 5 {
		return errors.New("the SOCKS5 answer is malformed")
	}
	if head[1] != 0 {
		reason := "unknown error"
		if int(head[1]) < len(socks5Replies) {
			reason = socks5Replies[head[1]]
		}
		return fmt.Errorf("%w: %s", ErrProxyRefused, reason)
	}
	var skip int
	switch head[3] {
	case 1:
		skip = 4 + 2
	case 4:
		skip = 16 + 2
	case 3:
		var length [1]byte
		if _, err := io.ReadFull(socket, length[:]); err != nil {
			return err
		}
		skip = int(length[0]) + 2
	default:
		return errors.New("the SOCKS5 answer is malformed")
	}
	_, err := io.CopyN(io.Discard, socket, int64(skip))
	return err
}

func (proxy *Proxy) httpConnect(socket net.Conn, host string, port int) (net.Conn, error) {
	target := net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
	if strings.ContainsAny(target, " \r\n\t\x00") {
		return nil, fmt.Errorf("the destination %q cannot be sent to an HTTP proxy", target)
	}
	var request strings.Builder
	request.WriteString("CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n")
	if proxy.Username != "" || proxy.Password != "" {
		credentials := base64.StdEncoding.EncodeToString([]byte(proxy.Username + ":" + proxy.Password))
		request.WriteString("Proxy-Authorization: Basic " + credentials + "\r\n")
	}
	request.WriteString("\r\n")
	if _, err := io.WriteString(socket, request.String()); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(io.LimitReader(socket, maxProxyHeader), 1024)
	total := 0
	line, err := readProxyLine(reader, &total)
	if err != nil {
		return nil, fmt.Errorf("no answer to CONNECT: %w", err)
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return nil, errors.New("the answer is not HTTP")
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, errors.New("the HTTP status is malformed")
	}
	for {
		header, err := readProxyLine(reader, &total)
		if err != nil {
			return nil, fmt.Errorf("the answer ended inside its headers: %w", err)
		}
		if header == "" {
			break
		}
	}
	if status < 200 || status > 299 {
		reason := strings.TrimSpace(strings.Join(fields[2:], " "))
		if status == 407 {
			reason = "proxy authentication required"
		}
		return nil, fmt.Errorf("%w: HTTP %d %s", ErrProxyRefused, status, cleanProxyText(reason))
	}
	if reader.Buffered() > 0 {
		// Bytes after the headers belong to the tunnel; the limit was only for the headers.
		rest, _ := reader.Peek(reader.Buffered())
		return &prefixedConn{Conn: socket, pending: append([]byte(nil), rest...)}, nil
	}
	return socket, nil
}

// prefixedConn delivers bytes already read before the rest of the stream.
type prefixedConn struct {
	net.Conn
	pending []byte
}

func (conn *prefixedConn) Read(p []byte) (int, error) {
	if len(conn.pending) > 0 {
		n := copy(p, conn.pending)
		conn.pending = conn.pending[n:]
		return n, nil
	}
	return conn.Conn.Read(p)
}

func readProxyLine(reader *bufio.Reader, total *int) (string, error) {
	line, err := reader.ReadString('\n')
	*total += len(line)
	if err != nil {
		return "", err
	}
	if *total > maxProxyHeader {
		return "", errors.New("the proxy's headers are too long")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// cleanProxyText keeps a proxy's own wording printable before it is shown.
func cleanProxyText(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
}
