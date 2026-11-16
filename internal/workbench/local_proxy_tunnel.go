package workbench

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"motty/internal/portforward"
)

var errLocalProxyBinding = errors.New("local proxy: remote listener is not verified as private")

// localProxyPrivateBindings requires every listener reported for the allocated
// port to be the requested IPv4 loopback address, including IPv6 listeners.
func localProxyPrivateBindings(output string, port uint16) bool {
	found := false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 4 || fields[0] != "LISTEN" {
			return false
		}
		host, rawPort, err := net.SplitHostPort(fields[3])
		if err != nil || rawPort != strconv.Itoa(int(port)) || host != "127.0.0.1" {
			return false
		}
		found = true
	}
	return found
}

// startLocalProxyTunnel validates the actual SSH listener before handing it to
// the forwarder; the SSH server may override the requested loopback binding.
func startLocalProxyTunnel(ctx context.Context, conn Connection, port uint16, notify func()) (*portforward.Forward, error) {
	if ctx == nil || conn == nil {
		return nil, errLocalProxyBinding
	}
	if err := validateLocalProxyPort(port); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	destination := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	probe, err := dialer.DialContext(ctx, "tcp4", destination)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errLocalProxyProbe, err)
	}
	_ = probe.Close()
	forward, err := portforward.Start(portforward.Config{
		ListenAddress: "127.0.0.1:0", Destination: destination,
		Mode: portforward.ModeRemote, MaxStreams: localProxyMaxStreams,
		Dial: dialer.DialContext, Notify: notify,
		Listen: func(network, address string) (net.Listener, error) {
			listenCtx, cancelListen := context.WithTimeout(ctx, 10*time.Second)
			defer cancelListen()
			return conn.ListenContext(listenCtx, network, address, func(listener net.Listener) error {
				host, rawPort, err := net.SplitHostPort(listener.Addr().String())
				allocated, parseErr := strconv.ParseUint(rawPort, 10, 16)
				if err != nil || parseErr != nil || host != "127.0.0.1" || allocated < 1024 {
					return errLocalProxyBinding
				}
				checkCtx, cancel := context.WithTimeout(listenCtx, 5*time.Second)
				defer cancel()
				result, err := conn.Exec(checkCtx, fmt.Sprintf("ss -H -ltn 'sport = :%d'", allocated), 16<<10)
				if err != nil || result.Truncated || result.ExitCode != 0 || !localProxyPrivateBindings(string(result.Output), uint16(allocated)) {
					return errLocalProxyBinding
				}
				return listenCtx.Err()
			})
		},
	})
	if err != nil {
		if errors.Is(err, errLocalProxyBinding) {
			return nil, errLocalProxyBinding
		}
		return nil, fmt.Errorf("%w: %v", errLocalProxyForwarding, err)
	}
	return forward, nil
}

func testLocalProxyTunnel(ctx context.Context, conn Connection, remotePort uint16) error {
	return testLocalProxyTunnelWithRoots(ctx, conn, remotePort, nil)
}

// testLocalProxyTunnelWithRoots enters through the allocated server listener,
// speaks CONNECT to the chosen local proxy, and validates the TLS response.
func testLocalProxyTunnelWithRoots(ctx context.Context, conn Connection, remotePort uint16, roots *x509.CertPool) error {
	if ctx == nil || conn == nil || remotePort == 0 {
		return errLocalProxyTunnel
	}
	testCtx, cancel := context.WithTimeout(ctx, localProxyTestTime)
	defer cancel()
	stream, err := conn.DialContext(testCtx, "tcp", localProxyAddress(remotePort))
	if err != nil {
		return fmt.Errorf("%w: %v", errLocalProxyTunnel, err)
	}
	defer stream.Close()
	stopDeadline := context.AfterFunc(testCtx, func() {
		_ = stream.Close()
	})
	defer stopDeadline()

	connect := "CONNECT " + localProxyTestTarget + " HTTP/1.1\r\nHost: " + localProxyTestTarget + "\r\nProxy-Connection: Keep-Alive\r\n\r\n"
	if _, err := io.WriteString(stream, connect); err != nil {
		return fmt.Errorf("%w: %v", errLocalProxyTunnel, err)
	}
	reader := bufio.NewReaderSize(io.LimitReader(stream, localProxyTestLimit), 4<<10)
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Scheme: "https", Host: localProxyTestTarget}}
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return fmt.Errorf("%w: %v", errLocalProxyTunnel, err)
	}
	if response.Body != nil {
		defer response.Body.Close()
	}
	if response.StatusCode == http.StatusProxyAuthRequired {
		return fmt.Errorf("%w: %v", errLocalProxyAuth, http.StatusText(response.StatusCode))
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: proxy returned %s", errLocalProxyTunnel, response.Status)
	}

	host, _, _ := net.SplitHostPort(localProxyTestTarget)
	// The header limit ends here; retain prefetched bytes before reading TLS.
	tlsConn := tls.Client(&bufferedConn{Conn: stream, reader: io.MultiReader(reader, stream)}, &tls.Config{
		MinVersion: tls.VersionTLS12, ServerName: host, RootCAs: roots,
	})
	if err := tlsConn.HandshakeContext(testCtx); err != nil {
		return fmt.Errorf("%w: TLS: %v", errLocalProxyTunnel, err)
	}
	if _, err := io.WriteString(tlsConn, "HEAD / HTTP/1.1\r\nHost: "+host+"\r\nConnection: close\r\n\r\n"); err != nil {
		return fmt.Errorf("%w: %v", errLocalProxyTunnel, err)
	}
	body, err := io.ReadAll(io.LimitReader(tlsConn, localProxyTestLimit+1))
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: %v", errLocalProxyTunnel, err)
	}
	if len(body) > localProxyTestLimit {
		return fmt.Errorf("%w: response too large", errLocalProxyTunnel)
	}
	_, parseErr := http.ReadResponse(bufio.NewReader(strings.NewReader(string(body))), &http.Request{Method: http.MethodHead})
	if parseErr != nil {
		return fmt.Errorf("%w: %v", errLocalProxyTunnel, parseErr)
	}
	head := strings.SplitN(string(body), "\r\n", 2)[0]
	if !strings.HasPrefix(head, "HTTP/1.1 200 ") && !strings.HasPrefix(head, "HTTP/1.0 200 ") {
		return fmt.Errorf("%w: target returned %q", errLocalProxyTunnel, head)
	}
	return nil
}

type bufferedConn struct {
	net.Conn
	reader io.Reader
}

func (conn *bufferedConn) Read(data []byte) (int, error) {
	return conn.reader.Read(data)
}
