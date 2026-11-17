//go:build windows

package sshconn

import "testing"

// TestKerberosWindowsWithoutTicket checks the SSPI binding without a usable KDC:
// the call fails with a Kerberos error, and the client does not report a
// context. It needs no realm, so it runs on any Windows machine.
func TestKerberosWindowsWithoutTicket(t *testing.T) {
	client := &kerberosClient{conn: &Conn{}}
	defer client.DeleteSecContext()
	token, _, err := client.InitSecContext("host@motty-kerberos-test.invalid", nil, false)
	if err == nil {
		t.Fatalf("InitSecContext without a realm returned a token of %d bytes", len(token))
	}
	if client.hasCtx {
		t.Fatal("a failed InitSecContext left a security context")
	}
}
