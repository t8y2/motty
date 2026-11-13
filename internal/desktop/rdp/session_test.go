package rdp

import (
	"context"
	"errors"
	"net"
	"testing"

	"motty/internal/desktop"
)

// Standard RDP security has no certificate step to ask the password at, so the
// question is asked before the connection is opened. A cancelled question must
// not dial.
func TestStandardSecurityAsksForThePasswordBeforeDialing(t *testing.T) {
	cancelled := errors.New("cancelled")
	var asked bool
	dial := func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dialed before the password question was answered")
		return nil, nil
	}
	_, err := Open(context.Background(), dial, "127.0.0.1:3389", desktop.Options{
		Username: "alice", StandardSecurity: true,
		AskPassword: func() (string, error) {
			asked = true
			return "", cancelled
		},
	})
	if !asked {
		t.Fatal("the password was not asked for")
	}
	if !errors.Is(err, cancelled) {
		t.Fatalf("open error = %v, want the cancelled question", err)
	}
}
