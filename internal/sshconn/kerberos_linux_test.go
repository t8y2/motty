package sshconn

import "testing"

// TestKerberosLinuxWithoutTicket checks the purego binding against the system
// library: with no ticket the call fails with a Kerberos error, and the client
// keeps no context. A machine without libgssapi_krb5 skips it.
func TestKerberosLinuxWithoutTicket(t *testing.T) {
	if _, err := loadGSS(); err != nil {
		t.Skip(err)
	}
	client := &kerberosClient{conn: &Conn{}}
	token, _, err := client.InitSecContext("host@example.com", nil, false)
	if err == nil {
		t.Fatalf("InitSecContext without a ticket returned a token of %d bytes", len(token))
	}
	if client.ctx != 0 {
		t.Fatal("a failed InitSecContext left a security context")
	}
}
