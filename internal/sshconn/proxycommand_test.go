package sshconn

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// The test binary doubles as a proxy command: with relayEnv set it relays its
// standard input and output to that address, after checking that the tokens
// of its command line were filled in as wantEnv says.
const (
	relayEnv = "MOTTY_TEST_PROXY_COMMAND_RELAY"
	wantEnv  = "MOTTY_TEST_PROXY_COMMAND_WANT"
)

func TestMain(m *testing.M) {
	if target := os.Getenv(relayEnv); target != "" {
		os.Exit(relay(target))
	}
	os.Exit(m.Run())
}

func relay(target string) int {
	if got := strings.Join(os.Args[1:], " "); got != os.Getenv(wantEnv) {
		fmt.Fprintf(os.Stderr, "asked for %q\n", got)
		return 3
	}
	if target == "fail" {
		fmt.Fprintln(os.Stderr, "starting the tunnel")
		fmt.Fprintln(os.Stderr, "no route to the tunnel")
		return 1
	}
	conn, err := net.Dial("tcp", target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	go func() {
		io.Copy(conn, os.Stdin)
		conn.(*net.TCPConn).CloseWrite()
	}()
	io.Copy(os.Stdout, conn)
	return 0
}

func relayCommand(t *testing.T) string {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return `"` + program + `" %h %p %r`
}

func TestExpandingAProxyCommand(t *testing.T) {
	for _, test := range []struct {
		command, host, user, want, err string
	}{
		{command: "cloudflared access ssh --hostname %h", host: "ssh.example.com", want: "cloudflared access ssh --hostname ssh.example.com"},
		{command: "nc -X connect -x proxy:3128 %h %p", host: "2001:db8::1", want: "nc -X connect -x proxy:3128 2001:db8::1 22"},
		{command: "ssh -W %h:%p %r@gateway 100%%", host: "inner", user: "alice", want: "ssh -W inner:22 alice@gateway 100%"},
		{command: "connect %n", host: "h", err: "%n"},
		{command: "connect %", host: "h", err: "lone %"},
		{command: "connect %h", host: "h;reboot", err: "cannot be given"},
		{command: "connect %h", host: "$(id)", err: "cannot be given"},
		{command: "connect %r", host: "h", user: "a b", err: "cannot be given"},
		{command: "nc %h %p", host: "-oProxyCommand=x", err: "cannot be given"},
		{command: "connect %r", host: "h", user: "-lroot", err: "cannot be given"},
		{command: "connect %h", host: "[::1]", err: "cannot be given"},
		{command: "ssh -W %h:%p %r@gateway", host: "inner", err: "no user name"},
		{command: "  ", host: "h", err: "empty"},
		{command: "connect\n%h", host: "h", err: "one line"},
	} {
		got, err := ExpandProxyCommand(test.command, test.host, 22, test.user)
		if test.err != "" {
			if err == nil || !strings.Contains(err.Error(), test.err) {
				t.Errorf("%q with %q: err = %v, want one about %q", test.command, test.host, err, test.err)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Errorf("%q with %q = %q, %v; want %q", test.command, test.host, got, err, test.want)
		}
	}
}

func TestASessionConnectsThroughAProxyCommandToAHostOnlyTheCommandKnows(t *testing.T) {
	server := startServer(t, func(server *testServer) { server.password = "pw" })
	host, port := server.host()
	t.Setenv(relayEnv, net.JoinHostPort(host, itoa(port)))
	t.Setenv(wantEnv, "ssh.internal.example 2222 tester")
	harness := dial(t, server, func(config *Config) {
		config.Host, config.Port = "ssh.internal.example", 2222
		config.Proxy = &Proxy{Kind: ProxyCommand, Command: relayCommand(t)}
	})
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(true)
	harness.phase(PhasePasswordPrompt)
	harness.conn.SubmitPassword("pw")
	harness.phase(PhaseConnected)
	if known := harness.knownHosts(); !strings.HasPrefix(known, "[ssh.internal.example]:2222 ") {
		t.Fatalf("the host key was remembered as %q, not for the host the command reached", known)
	}
}

func TestAProxyCommandThatFailsFailsTheSessionWithWhatItSaid(t *testing.T) {
	server := startServer(t, func(server *testServer) { server.password = "pw" })
	host, port := server.host()
	t.Setenv(relayEnv, "fail")
	t.Setenv(wantEnv, host+" "+itoa(port)+" tester")
	harness := dial(t, server, func(config *Config) {
		config.Proxy = &Proxy{Kind: ProxyCommand, Command: relayCommand(t)}
	})
	status := harness.until("failure", func(status Status) bool { return status.Phase == PhaseFailed })
	if status.Err == nil || !strings.Contains(status.Err.Error(), "no route to the tunnel") {
		t.Fatalf("err = %v, want the command's last words", status.Err)
	}
}

func TestAProxyCommandIsUsedOnlyWhenItHasACommand(t *testing.T) {
	if (&Proxy{Kind: ProxyCommand}).Uses("host:22") {
		t.Fatal("a proxy command without a command was used")
	}
	proxy := &Proxy{Kind: ProxyCommand, Command: "nc %h %p", Bypass: []string{"*.lan"}}
	if !proxy.Uses("host.example:22") || proxy.Uses("nas.lan:22") {
		t.Fatal("the bypass list does not apply to a proxy command")
	}
}

func TestAProxyCommandFailureIsOneCleanLine(t *testing.T) {
	long := strings.Repeat("é", 200) // 400 bytes: a cut at 300 falls inside no letter
	got := lastLine("first\n\x1b[31mred\x1b[0m" + long)
	if strings.ContainsRune(got, '\x1b') || !utf8.ValidString(got) {
		t.Fatalf("lastLine = %q, want no control characters and valid UTF-8", got)
	}
}
