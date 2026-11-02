//go:build !windows

package sshconn

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// bashGateway is a jump host that only offers a shell: a real bash on a real
// terminal, with no direct-tcpip forwarding, as a bastion that has the user
// switch accounts first.
func bashGateway(shell *fakeShell) {
	_, cols, rows, _, _ := shell.state()
	command := exec.Command("bash", "--noprofile", "--norc", "-i")
	command.Env = []string{"TERM=xterm", "PS1=gateway$ ", "PATH=" + os.Getenv("PATH"), "HOME=" + os.TempDir()}
	command.Dir = os.TempDir()
	master, err := pty.StartWithSize(command, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		shell.exit(1)
		return
	}
	go func() {
		io.Copy(master, shell.Channel)
		// A blocked read on the master does not end when it is closed on every
		// platform; ending the session's processes does. The shell leads its own
		// process group, which holds the relay program too.
		syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		master.Close()
	}()
	io.Copy(shell.Channel, master)
	command.Wait()
	shell.exit(0)
}

// bashRelay reaches host:port with bash alone, so the tests do not need nc. It
// insists on the environment the prelude sets, as a command that works only
// after the account was switched.
const bashRelay = `bash -c 'test "$SWITCHED" = 1 || { echo not switched; exit 7; }; exec 3<>/dev/tcp/%h/%p; cat <&3 & cat >&3'`

// switchAccount stands for `su` or `sudo -i`: a nested interactive shell with
// the account's environment, which takes a moment to start. The relay line is
// typed before it is up, and must reach it.
const switchAccount = `sleep 0.7; env SWITCHED=1 bash --noprofile --norc -i`

func needBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
}

func relayGateway(t *testing.T) *testServer {
	t.Helper()
	needBash(t)
	return startServer(t, func(server *testServer) {
		server.password = "secret"
		server.shell = bashGateway
	})
}

// sshServerBehindGateway serves SSH on an address only the gateway's shell
// reaches in these tests: nothing forwards to it.
func sshServerBehindGateway(t *testing.T) *testServer {
	t.Helper()
	return startServer(t, func(server *testServer) {
		server.password = "target-secret"
		server.shell = echoShell
	})
}

func dialThroughRelay(t *testing.T, gateway, target *testServer, relay Relay) *harness {
	t.Helper()
	gatewayHost, gatewayPort := gateway.host()
	targetHost, targetPort := target.host()
	return dial(t, target, func(config *Config) {
		config.ProxyJumps = []Endpoint{{Host: gatewayHost, Port: gatewayPort, User: "bastion"}}
		config.Relays = map[Endpoint]Relay{{Host: targetHost, Port: targetPort, User: "tester"}: relay}
	})
}

func authenticateBoth(h *harness) {
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("secret")
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("target-secret")
}

func TestARelayReachesAHostBehindAGatewayThatOnlyOffersAShell(t *testing.T) {
	gateway := relayGateway(t)
	target := sshServerBehindGateway(t)
	h := dialThroughRelay(t, gateway, target, Relay{Prelude: switchAccount, Command: bashRelay})
	authenticateBoth(h)
	connected := h.phase(PhaseConnected)
	if connected.EndpointRole != EndpointTarget || connected.Method != "password" {
		t.Fatalf("connected status = %+v", connected)
	}

	// The target is a full SSH host: its own shell works through the relay.
	w := openShell(t, h, 80, 24)
	w.until("the target's greeting", func(output string) bool { return strings.Contains(output, "welcome\r\n$ ") })
	if !w.shell.Write([]byte("through the relay\r")) {
		t.Fatal("writing to the shell failed")
	}
	w.until("the echo", func(output string) bool { return strings.Contains(output, "THROUGH THE RELAY") })
	if offered := target.offered(); len(offered) != 1 || offered[0] != "target-secret" {
		t.Fatalf("the target was offered %v", offered)
	}
	if offered := gateway.offered(); len(offered) != 1 || offered[0] != "secret" {
		t.Fatalf("the gateway was offered %v", offered)
	}
}

func TestTheRelayRunsAfterThePreludeAndNotBefore(t *testing.T) {
	gateway := relayGateway(t)
	target := sshServerBehindGateway(t)
	h := dialThroughRelay(t, gateway, target, Relay{Command: bashRelay})
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("secret")
	failed := h.phase(PhaseFailed)
	if !strings.Contains(failed.Err.Error(), "not switched") {
		t.Fatalf("error = %v", failed.Err)
	}
}

func TestARelayWithoutAPreludeStartsStraightAway(t *testing.T) {
	gateway := relayGateway(t)
	target := sshServerBehindGateway(t)
	command := strings.Replace(bashRelay, `test "$SWITCHED" = 1 || { echo not switched; exit 7; }; `, "", 1)
	h := dialThroughRelay(t, gateway, target, Relay{Command: command})
	authenticateBoth(h)
	h.phase(PhaseConnected)
}

// The relay moves SSH's binary stream through a terminal; no byte may be
// turned into another, or taken as a terminal control.
func TestARelayCarriesEveryByteValueUnchanged(t *testing.T) {
	gateway := relayGateway(t)
	h := connect(t, gateway)
	greeter, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { greeter.Close() })
	go func() {
		for {
			socket, err := greeter.Accept()
			if err != nil {
				return
			}
			go func() {
				defer socket.Close()
				io.WriteString(socket, "banner\r\nSSH-2.0-echo\r\n")
				io.Copy(socket, socket)
			}()
		}
	}()
	address := greeter.Addr().(*net.TCPAddr)
	next := Endpoint{Host: "127.0.0.1", Port: uint16(address.Port), User: "tester"}
	socket, err := h.conn.dialRelay(h.conn.Client(), Endpoint{Host: "gateway", Port: 22}, Relay{Prelude: switchAccount, Command: bashRelay}, next)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()

	greeting := make([]byte, len("SSH-2.0-echo\r\n"))
	if _, err := io.ReadFull(socket, greeting); err != nil || string(greeting) != "SSH-2.0-echo\r\n" {
		t.Fatalf("greeting = %q, %v", greeting, err)
	}
	payload := make([]byte, 0, 4*256)
	for range 4 {
		for value := range 256 {
			payload = append(payload, byte(value))
		}
	}
	go socket.Write(payload)
	got := make([]byte, len(payload))
	socket.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(socket, got); err != nil {
		t.Fatalf("read back %d bytes: %v", len(got), err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("the relay changed the bytes it carried")
	}
}

func TestARelayThatCannotStartSaysWhatTheShellSaid(t *testing.T) {
	gateway := relayGateway(t)
	target := sshServerBehindGateway(t)
	h := dialThroughRelay(t, gateway, target, Relay{Command: "no-such-relay-program %h %p"})
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("secret")
	failed := h.phase(PhaseFailed)
	message := failed.Err.Error()
	for _, want := range []string{"relay", "no-such-relay-program"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q does not mention %q", message, want)
		}
	}
	if len(target.offered()) != 0 {
		t.Fatal("credentials reached the target although the relay never started")
	}
}

func TestARelayThatNeverSpeaksSSHTimesOut(t *testing.T) {
	needBash(t)
	shortened := relayStartTimeout
	relayStartTimeout = 1500 * time.Millisecond
	t.Cleanup(func() { relayStartTimeout = shortened })
	gateway := relayGateway(t)
	target := sshServerBehindGateway(t)
	h := dialThroughRelay(t, gateway, target, Relay{Command: "sleep 30"})
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("secret")
	failed := h.phase(PhaseFailed)
	if !strings.Contains(failed.Err.Error(), "within") {
		t.Fatalf("error = %v", failed.Err)
	}
}

func TestCancelingWhileARelayStartsEndsTheConnection(t *testing.T) {
	gateway := relayGateway(t)
	target := sshServerBehindGateway(t)
	h := dialThroughRelay(t, gateway, target, Relay{Command: "sleep 30"})
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("secret")
	time.Sleep(200 * time.Millisecond)
	h.conn.Cancel()
	select {
	case <-h.conn.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the connection did not end")
	}
	if status := h.conn.Status(); !errors.Is(status.Err, ErrCanceled) {
		t.Fatalf("status = %+v", status)
	}
}

func TestClosingARelayedConnectionEndsTheRelayProgram(t *testing.T) {
	gateway := relayGateway(t)
	target := sshServerBehindGateway(t)
	h := dialThroughRelay(t, gateway, target, Relay{Prelude: switchAccount, Command: bashRelay})
	authenticateBoth(h)
	h.phase(PhaseConnected)
	h.conn.Close()
	deadline := time.Now().Add(10 * time.Second)
	for gateway.activeConnections() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the gateway connection stayed open")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
