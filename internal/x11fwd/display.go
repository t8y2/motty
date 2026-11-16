// Package x11fwd implements the client side of SSH X11 forwarding (RFC 4254
// section 6.3): finding the local X display, reading the real cookie from the
// Xauthority file, swapping the session's fake cookie for it in each
// connection's setup packet, and piping the rest of the traffic.
//
// The cookie the server sees is random per session and worthless anywhere else:
// the real one never leaves this machine.
package x11fwd

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// ErrNoDisplay means there is no local X display to forward to.
var ErrNoDisplay = errors.New("no local X display (DISPLAY is not set)")

// UnreachableError means a display was named but nothing accepts connections
// there: the X server is not running, or DISPLAY is stale.
type UnreachableError struct {
	// Address is where the connection was tried.
	Address string
	Err     error
}

func (err *UnreachableError) Error() string {
	return "cannot reach the X server at " + err.Address + ": " + err.Err.Error()
}

func (err *UnreachableError) Unwrap() error { return err.Err }

// probeTimeout bounds Probe; a local X server answers at once.
const probeTimeout = 2 * time.Second

// Display is a parsed X display name.
type Display struct {
	// Network and Address say how to reach the server: ("unix", path) or
	// ("tcp", host:port).
	Network, Address string
	// Host is the host part of the display name for matching Xauthority
	// entries; empty for a local display.
	Host string
	// Number is the display number; Screen the screen on it.
	Number, Screen int
}

// ParseDisplay reads a DISPLAY value as goos would use it: ":0", ":0.1",
// "unix:0", "host:N[.S]", "[::1]:N", or the absolute socket path macOS's
// launchd gives XQuartz ("/private/tmp/com.apple.launchd.X/org.xquartz:0").
// Windows has no Unix sockets for X, so a local display there is TCP on
// localhost.
func ParseDisplay(value, goos string) (Display, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Display{}, ErrNoDisplay
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return Display{}, fmt.Errorf("invalid DISPLAY %q", value)
	}
	colon := strings.LastIndexByte(value, ':')
	if colon < 0 {
		return Display{}, fmt.Errorf("invalid DISPLAY %q", value)
	}
	host, rest := value[:colon], value[colon+1:]
	number, screen, err := parseNumbers(rest)
	if err != nil {
		return Display{}, fmt.Errorf("invalid DISPLAY %q: %w", value, err)
	}
	display := Display{Number: number, Screen: screen}
	switch {
	case strings.HasPrefix(host, "/"):
		// The socket's file name itself ends in ":N" (and never in ".S").
		display.Network, display.Address = "unix", value
		if dot := strings.LastIndexByte(value, '.'); dot > colon {
			display.Address = value[:dot]
		}
	case host == "" || host == "unix":
		if goos == "windows" {
			display.Network, display.Address = "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(6000+number))
		} else {
			display.Network, display.Address = "unix", "/tmp/.X11-unix/X"+strconv.Itoa(number)
		}
	default:
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		if host == "" || strings.ContainsAny(host, "/ ") {
			return Display{}, fmt.Errorf("invalid DISPLAY %q", value)
		}
		display.Host = host
		display.Network, display.Address = "tcp", net.JoinHostPort(host, strconv.Itoa(6000+number))
	}
	return display, nil
}

func parseNumbers(text string) (number, screen int, err error) {
	numberText, screenText, hasScreen := strings.Cut(text, ".")
	number, err = parseSmall(numberText, 59535) // 6000+number must be a port
	if err != nil {
		return 0, 0, err
	}
	if hasScreen {
		if screen, err = parseSmall(screenText, 9999); err != nil {
			return 0, 0, err
		}
	}
	return number, screen, nil
}

func parseSmall(text string, limit int) (int, error) {
	if text == "" || len(text) > 5 {
		return 0, errors.New("bad number")
	}
	value := 0
	for _, r := range text {
		if r < '0' || r > '9' {
			return 0, errors.New("bad number")
		}
		value = value*10 + int(r-'0')
	}
	if value > limit {
		return 0, errors.New("number out of range")
	}
	return value, nil
}

// Dial connects to the X server.
func (display Display) Dial() (net.Conn, error) {
	return net.DialTimeout(display.Network, display.Address, 5*time.Second)
}

// Probe checks that something accepts connections on the display, so a session
// can say so up front instead of every X client failing later with a bare
// "can't open display".
func (display Display) Probe() error {
	conn, err := net.DialTimeout(display.Network, display.Address, probeTimeout)
	if err != nil {
		return &UnreachableError{Address: display.Address, Err: err}
	}
	return conn.Close()
}
