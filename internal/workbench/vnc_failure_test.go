package workbench

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"motty/internal/rfb"
)

func TestVNCFailureDescriptionPreservesOfferNumbersAndTransportErrors(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, errors.New("network lost"), rfb.ErrUnsupported} {
		if got := state.vncFailureDescription(err); got != "" {
			t.Fatalf("unrelated error was reclassified: %q", got)
		}
	}
	for _, test := range []struct {
		err    *rfb.UnsupportedSecurityError
		format string
		names  string
	}{
		{&rfb.UnsupportedSecurityError{Types: []uint32{30, 250}}, state.messages.Desktop.VNCUnsupportedSecurityFmt, "#30, #250"},
		{&rfb.UnsupportedSecurityError{Types: []uint32{257, 900}, VeNCrypt: true}, state.messages.Desktop.VNCUnsupportedVeNCryptFmt, "TLSNone, #900"},
		{&rfb.UnsupportedSecurityError{Types: []uint32{6, 130}, AuthenticationOnly: true}, state.messages.Desktop.VNCAuthOnlyPermissionFmt, "RA2ne, RA256ne"},
	} {
		got := state.vncFailureDescription(fmt.Errorf("handshake: %w", test.err))
		if got != fmt.Sprintf(test.format, test.names) || !strings.Contains(got, test.names) {
			t.Fatalf("wrong failure category or offers: %q", got)
		}
	}
}

func TestVNCFailureWithoutOfferedModesPreservesProtocolReason(t *testing.T) {
	state := newTestState(t, testOptions(t))
	err := fmt.Errorf("%w: %w: server offered no VeNCrypt subtypes", &rfb.UnsupportedSecurityError{VeNCrypt: true}, rfb.ErrProtocol)
	session := &Session{host: Host{Protocol: protocolVNC}, err: err}
	if got := state.failureLine(session); got != err.Error() {
		t.Fatalf("protocol failure was replaced with unsupported-mode advice: %q", got)
	}
}
