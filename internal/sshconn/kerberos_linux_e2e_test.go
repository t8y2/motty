//go:build linux

package sshconn

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/crypto/ssh"
)

// The end-to-end tests need a Kerberos realm: a KDC, a keytab for host/localhost
// and a ticket for the user in the default credential cache. They run only when
// MOTTY_KERBEROS_E2E=1 and KRB5_CONFIG, KRB5_KTNAME and KRB5CCNAME point at that
// realm, so an ordinary test run skips them.
const (
	kerberosE2EPrincipal = "alice"
	kerberosE2EIdentity  = "alice@TEST.LOCAL"
)

func kerberosE2EEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("MOTTY_KERBEROS_E2E") != "1" {
		t.Skip("set MOTTY_KERBEROS_E2E=1 with a test KDC to run the Kerberos handshake")
	}
	for _, name := range []string{"KRB5_CONFIG", "KRB5_KTNAME", "KRB5CCNAME"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s must name the test realm", name)
		}
	}
}

// gssAcceptFuncs are the acceptor-side entry points, used by the test SSH
// server. The client side and the release calls are shared with kerberos_linux.go.
type gssAcceptFuncs struct {
	acquireCred func(minor *uint32, name unsafe.Pointer, timeReq uint32, mechs unsafe.Pointer, usage int32, cred *uintptr, actual unsafe.Pointer, timeRec unsafe.Pointer) uint32
	acceptCtx   func(minor *uint32, ctx *uintptr, cred uintptr, input *gssBuffer, bindings unsafe.Pointer, src *uintptr, mech unsafe.Pointer, output *gssBuffer, retFlags unsafe.Pointer, timeRec unsafe.Pointer, delegated unsafe.Pointer) uint32
	verifyMIC   func(minor *uint32, ctx uintptr, msg *gssBuffer, token *gssBuffer, qop unsafe.Pointer) uint32
	displayName func(minor *uint32, name uintptr, text *gssBuffer, nameType unsafe.Pointer) uint32
}

var gssAccept gssAcceptFuncs

func loadGSSAccept(t *testing.T) {
	t.Helper()
	handle, err := purego.Dlopen(gssLibrary, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatal(err)
	}
	bind := func(name string, fptr any) {
		sym, err := purego.Dlsym(handle, name)
		if err != nil {
			t.Fatal(err)
		}
		purego.RegisterFunc(fptr, sym)
	}
	bind("gss_acquire_cred", &gssAccept.acquireCred)
	bind("gss_accept_sec_context", &gssAccept.acceptCtx)
	bind("gss_verify_mic", &gssAccept.verifyMIC)
	bind("gss_display_name", &gssAccept.displayName)
}

// testGSSAcceptor implements ssh.GSSAPIServer for one connection, accepting
// tokens against the keytab named by KRB5_KTNAME.
type testGSSAcceptor struct {
	lib  *gssFuncs
	cred uintptr
	ctx  uintptr
}

func newTestGSSAcceptor(t *testing.T) *testGSSAcceptor {
	t.Helper()
	lib, err := loadGSS()
	if err != nil {
		t.Fatal(err)
	}
	var minor uint32
	var cred uintptr
	status := gssAccept.acquireCred(&minor, nil, 0, nil, gssAcceptUsage, &cred, nil, nil)
	if status != gssStatusComplete {
		t.Fatal(kerberosError(lib, "acquire acceptor credentials", status, minor))
	}
	return &testGSSAcceptor{lib: lib, cred: cred}
}

func (a *testGSSAcceptor) AcceptSecContext(token []byte) ([]byte, string, bool, error) {
	var minor uint32
	var src uintptr
	var output gssBuffer
	input := bufferOf(token)
	status := gssAccept.acceptCtx(&minor, &a.ctx, a.cred, &input, nil, &src, nil, &output, nil, nil, nil)
	defer a.lib.releaseBuffer(&minor, &output)
	if status != gssStatusComplete && status != gssStatusContinueNeeded {
		return nil, "", false, kerberosError(a.lib, "accept security context", status, minor)
	}
	name := ""
	if status == gssStatusComplete {
		var text gssBuffer
		if gssAccept.displayName(&minor, src, &text, nil) == gssStatusComplete {
			name = string(copyGSSBuffer(text))
			a.lib.releaseBuffer(&minor, &text)
		}
		a.lib.releaseName(&minor, &src)
	}
	return copyGSSBuffer(output), name, status == gssStatusContinueNeeded, nil
}

func (a *testGSSAcceptor) VerifyMIC(micField []byte, micToken []byte) error {
	var minor uint32
	message := bufferOf(micField)
	token := bufferOf(micToken)
	status := gssAccept.verifyMIC(&minor, a.ctx, &message, &token, nil)
	if status != gssStatusComplete {
		return kerberosError(a.lib, "verify SSH authentication", status, minor)
	}
	return nil
}

func (a *testGSSAcceptor) DeleteSecContext() error {
	if a.ctx == 0 {
		return nil
	}
	var minor uint32
	if status := a.lib.deleteSecContext(&minor, &a.ctx, nil); status != gssStatusComplete {
		return kerberosError(a.lib, "release acceptor context", status, minor)
	}
	return nil
}

// gssAcceptUsage is GSS_C_ACCEPT.
const gssAcceptUsage = 2

// startKerberosSSHServer runs a loopback SSH server that offers only
// gssapi-with-mic and returns its address.
func startKerberosSSHServer(t *testing.T) string {
	t.Helper()
	loadGSSAccept(t)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			socket, err := listener.Accept()
			if err != nil {
				return
			}
			go serveKerberosSSH(t, socket, signer)
		}
	}()
	return listener.Addr().String()
}

func serveKerberosSSH(t *testing.T, socket net.Conn, signer ssh.Signer) {
	config := &ssh.ServerConfig{
		GSSAPIWithMICConfig: &ssh.GSSAPIWithMICConfig{
			AllowLogin: func(_ ssh.ConnMetadata, srcName string) (*ssh.Permissions, error) {
				if srcName != kerberosE2EIdentity {
					return nil, fmt.Errorf("the server saw %q, want %q", srcName, kerberosE2EIdentity)
				}
				return &ssh.Permissions{}, nil
			},
			Server: newTestGSSAcceptor(t),
		},
	}
	config.AddHostKey(signer)
	conn, chans, requests, err := ssh.NewServerConn(socket, config)
	if err != nil {
		socket.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	for channel := range chans {
		channel.Reject(ssh.Prohibited, "the test server has no shell")
	}
	conn.Close()
}

// dialKerberos starts a connection to the test server as alice, trusting its
// host key, and returns once the connection succeeds or fails.
func dialKerberos(t *testing.T, address string) *Conn {
	t.Helper()
	host, port := splitTestAddress(t, address)
	endpoint := Endpoint{Host: host, Port: port, User: kerberosE2EPrincipal}
	conn := Dial(Config{
		Host:           host,
		Port:           port,
		User:           kerberosE2EPrincipal,
		KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts"),
		IdentityFiles:  []string{},
		Authentication: map[Endpoint]Authentication{endpoint: {Methods: AuthKerberos}},
	})
	t.Cleanup(conn.Close)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		status := conn.Status()
		switch status.Phase {
		case PhaseHostKeyPrompt:
			conn.TrustHostKey(false)
		case PhaseConnected, PhaseFailed, PhaseClosed:
			return conn
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the connection did not finish; last phase %v", conn.Status().Phase)
	return nil
}

func splitTestAddress(t *testing.T, address string) (string, uint16) {
	t.Helper()
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	// The principal is host/localhost, so the client names localhost while the
	// listener stays on the loopback address.
	return "localhost", uint16(port)
}

// TestKerberosLinuxHandshakeWithTicket authenticates alice with a ticket from
// the test KDC through the purego GSS-API client and a Go SSH server that
// accepts with the keytab.
func TestKerberosLinuxHandshakeWithTicket(t *testing.T) {
	kerberosE2EEnabled(t)
	address := startKerberosSSHServer(t)
	conn := dialKerberos(t, address)
	status := conn.Status()
	if status.Phase != PhaseConnected {
		t.Fatalf("phase = %v, err = %v", status.Phase, status.Err)
	}
	if status.Method != "gssapi-with-mic" {
		t.Fatalf("method = %q, want gssapi-with-mic", status.Method)
	}
}

// TestKerberosLinuxHandshakeWithoutTicket points the client at an empty
// credential cache. The connection must fail with a Kerberos error, not succeed
// and not hang.
func TestKerberosLinuxHandshakeWithoutTicket(t *testing.T) {
	kerberosE2EEnabled(t)
	t.Setenv("KRB5CCNAME", "FILE:"+filepath.Join(t.TempDir(), "empty"))
	address := startKerberosSSHServer(t)
	conn := dialKerberos(t, address)
	status := conn.Status()
	if status.Phase != PhaseFailed {
		t.Fatalf("phase = %v, want PhaseFailed", status.Phase)
	}
	if status.Err == nil || !strings.Contains(status.Err.Error(), "Kerberos") {
		t.Fatalf("err = %v, want a Kerberos error", status.Err)
	}
}
