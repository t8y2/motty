package sshconn

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// A Windows IPv6 link-local zone is the network adapter's friendly name, and
// names may contain brackets. knownhosts' check parses the remote address even
// when the dial hostname is present, so a zone the parser cannot read used to
// fail the handshake before the known-hosts lookup.
func TestVerifyHostKeyAcceptsABracketedIPv6Zone(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{"host.example"}, key) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, err := newHostKeys(path)
	if err != nil {
		t.Fatal(err)
	}

	remote := &net.TCPAddr{
		IP:   net.ParseIP("fe80::1234:5678:9abc:def0"),
		Port: 22,
		Zone: "Example VPN Adapter [0123456789abcdef]",
	}
	if _, _, err := net.SplitHostPort(remote.String()); err == nil {
		t.Fatal("the test address parses; it does not cover a bracketed zone")
	}

	var conn Conn
	if err := conn.verifyHostKey(keys, "host.example:22", remote, key); err != nil {
		t.Fatalf("verifyHostKey = %v", err)
	}
}
