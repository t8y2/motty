package rdstls

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"testing"
	"testing/iotest"
)

// duplex reads from the server's bytes and records what the client writes.
type duplex struct {
	io.Reader
	sent bytes.Buffer
}

func (d *duplex) Write(p []byte) (int, error) { return d.sent.Write(p) }

var capabilities = []byte{1, 0, 1, 0, 1, 0, 1, 0}

func response(code byte) []byte { return []byte{1, 0, 4, 0, 1, 0, code, 0, 0, 0} }

func authenticate(server []byte, byteWise bool) (*duplex, error) {
	var r io.Reader = bytes.NewReader(server)
	if byteWise {
		r = iotest.OneByteReader(r)
	}
	d := &duplex{Reader: r}
	err := Authenticate(d, slog.New(slog.DiscardHandler), Credentials{
		RedirectionGUID: []byte{0xAA, 0xBB},
		Username:        "u",
		Domain:          "",
		Password:        []byte{'p', 0, 0, 0},
	})
	return d, err
}

func TestAuthenticatePresentsThePasswordCredentials(t *testing.T) {
	for _, byteWise := range []bool{false, true} {
		d, err := authenticate(append(append([]byte(nil), capabilities...), response(0)...), byteWise)
		if err != nil {
			t.Fatalf("byteWise=%v: %v", byteWise, err)
		}
		want := []byte{
			1, 0, 2, 0, 1, 0, // Version, AUTHREQ, PASSWORD_CREDS
			2, 0, 0xAA, 0xBB, // RedirectionGuid
			4, 0, 'u', 0, 0, 0, // UserName, terminated
			2, 0, 0, 0, // Domain: just the terminator
			4, 0, 'p', 0, 0, 0, // Password as the server issued it
		}
		if !bytes.Equal(d.sent.Bytes(), want) {
			t.Errorf("sent % x, want % x", d.sent.Bytes(), want)
		}
	}
}

func TestAuthenticateReportsTheServersRefusal(t *testing.T) {
	_, err := authenticate(append(append([]byte(nil), capabilities...), response(0x05)...), false)
	var refusal *ResultError
	if !errors.Is(err, ErrRefused) || !errors.As(err, &refusal) || refusal.Code != ResultAccessDenied {
		t.Fatalf("err = %v, want an access denied refusal", err)
	}
}

func TestAuthenticateRefusesWhatIsNotRDSTLS(t *testing.T) {
	for name, server := range map[string][]byte{
		"empty":              nil,
		"truncated":          capabilities[:5],
		"wrong version":      {2, 0, 1, 0, 1, 0, 1, 0},
		"no common version":  {1, 0, 1, 0, 1, 0, 2, 0},
		"response in place":  response(0),
		"truncated response": append(append([]byte(nil), capabilities...), response(0)[:7]...),
	} {
		if _, err := authenticate(server, false); err == nil || errors.Is(err, ErrRefused) {
			t.Errorf("%s: err = %v, want a broken exchange", name, err)
		}
	}
}
