// Package rdstls speaks the client side of MS-RDSTLS: the authentication a
// reconnecting client runs inside TLS in place of CredSSP when a Server
// Redirection PDU hands it a password flagged LB_PASSWORD_IS_PK_ENCRYPTED
// (MS-RDPBCGR 2.2.13.1). The password field is then not the user's password
// but a cookie the redirecting server issued, which only this exchange
// presents back.
package rdstls

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"unicode/utf16"
)

// PDU fields (MS-RDSTLS 2.2).
const (
	version1 uint16 = 0x0001

	pduCapabilities uint16 = 0x0001
	pduAuthRequest  uint16 = 0x0002
	pduAuthResponse uint16 = 0x0004

	dataCapabilities  uint16 = 0x0001
	dataPasswordCreds uint16 = 0x0001
	dataResultCode    uint16 = 0x0001
)

// Result codes of the Authentication Response PDU (MS-RDSTLS 2.2.1.3).
const (
	ResultSuccess            uint32 = 0x00000000
	ResultAccessDenied       uint32 = 0x00000005
	ResultLogonFailure       uint32 = 0x0000052E
	ResultInvalidLogonHours  uint32 = 0x00000530
	ResultPasswordExpired    uint32 = 0x00000532
	ResultAccountDisabled    uint32 = 0x00000533
	ResultPasswordMustChange uint32 = 0x00000773
	ResultAccountLockedOut   uint32 = 0x00000775
)

// ErrRefused is wrapped by every ResultError: the server answered the
// authentication with a result other than success.
var ErrRefused = errors.New("credentials rejected")

// ResultError is the server's refusal, with the result code it gave.
type ResultError struct {
	Code uint32
}

func (e *ResultError) Error() string {
	return fmt.Sprintf("%s (0x%08X)", resultName(e.Code), e.Code)
}

func (e *ResultError) Unwrap() error { return ErrRefused }

func resultName(code uint32) string {
	switch code {
	case ResultAccessDenied:
		return "access denied"
	case ResultLogonFailure:
		return "logon failure"
	case ResultInvalidLogonHours:
		return "invalid logon hours"
	case ResultPasswordExpired:
		return "password expired"
	case ResultAccountDisabled:
		return "account disabled"
	case ResultPasswordMustChange:
		return "password must change"
	case ResultAccountLockedOut:
		return "account locked out"
	}
	return "unknown result"
}

// Credentials is the password credentials an Authentication Request carries.
// Everything is as the Server Redirection PDU gave it: the GUID and the
// password are opaque octets the client returns unchanged.
type Credentials struct {
	RedirectionGUID []byte
	Username        string
	Domain          string
	Password        []byte
}

// Authenticate runs the exchange on a connection that has finished its TLS
// handshake: it reads the server's Capabilities PDU, sends the password
// credentials and reads the verdict. A refusal is a *ResultError; any other
// error means the exchange itself broke.
func Authenticate(rw io.ReadWriter, log *slog.Logger, credentials Credentials) error {
	if err := readCapabilities(rw); err != nil {
		return err
	}
	log.LogAttrs(context.Background(), slog.LevelDebug, "sending RDSTLS password credentials")
	request, err := encodeAuthRequest(credentials)
	if err != nil {
		return err
	}
	if _, err := rw.Write(request); err != nil {
		return fmt.Errorf("sending the authentication request: %w", err)
	}
	return readAuthResponse(rw)
}

// readHeader reads the Version and PduType every RDSTLS PDU starts with.
func readHeader(r io.Reader, want uint16, name string) error {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return fmt.Errorf("reading the %s: %w", name, err)
	}
	if v := binary.LittleEndian.Uint16(head[0:2]); v != version1 {
		return fmt.Errorf("the %s has version 0x%04X, want 0x%04X", name, v, version1)
	}
	if t := binary.LittleEndian.Uint16(head[2:4]); t != want {
		return fmt.Errorf("expected the %s, got PDU type 0x%04X", name, t)
	}
	return nil
}

func readCapabilities(r io.Reader) error {
	const name = "Capabilities PDU"
	if err := readHeader(r, pduCapabilities, name); err != nil {
		return err
	}
	var body [4]byte
	if _, err := io.ReadFull(r, body[:]); err != nil {
		return fmt.Errorf("reading the %s: %w", name, err)
	}
	if d := binary.LittleEndian.Uint16(body[0:2]); d != dataCapabilities {
		return fmt.Errorf("the %s has data type 0x%04X, want 0x%04X", name, d, dataCapabilities)
	}
	if binary.LittleEndian.Uint16(body[2:4])&version1 == 0 {
		return fmt.Errorf("the %s offers no RDSTLS version this client speaks", name)
	}
	return nil
}

func readAuthResponse(r io.Reader) error {
	const name = "Authentication Response PDU"
	if err := readHeader(r, pduAuthResponse, name); err != nil {
		return err
	}
	var body [6]byte
	if _, err := io.ReadFull(r, body[:]); err != nil {
		return fmt.Errorf("reading the %s: %w", name, err)
	}
	if d := binary.LittleEndian.Uint16(body[0:2]); d != dataResultCode {
		return fmt.Errorf("the %s has data type 0x%04X, want 0x%04X", name, d, dataResultCode)
	}
	if code := binary.LittleEndian.Uint32(body[2:6]); code != ResultSuccess {
		return &ResultError{Code: code}
	}
	return nil
}

func encodeAuthRequest(c Credentials) ([]byte, error) {
	out := binary.LittleEndian.AppendUint16(nil, version1)
	out = binary.LittleEndian.AppendUint16(out, pduAuthRequest)
	out = binary.LittleEndian.AppendUint16(out, dataPasswordCreds)
	var err error
	for _, field := range [][]byte{c.RedirectionGUID, utf16z(c.Username), utf16z(c.Domain), c.Password} {
		if out, err = appendField(out, field); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// appendField appends a field behind its 16-bit length.
func appendField(out, field []byte) ([]byte, error) {
	if len(field) > 0xFFFF {
		return nil, fmt.Errorf("a field of %d bytes does not fit an RDSTLS PDU", len(field))
	}
	out = binary.LittleEndian.AppendUint16(out, uint16(len(field)))
	return append(out, field...), nil
}

// utf16z is s in UTF-16LE with its null terminator, which RDSTLS counts in
// the length.
func utf16z(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, 2*len(units)+2)
	for _, unit := range units {
		out = binary.LittleEndian.AppendUint16(out, unit)
	}
	return binary.LittleEndian.AppendUint16(out, 0)
}
