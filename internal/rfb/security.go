package rfb

import (
	"strconv"
	"strings"
)

// UnsupportedSecurityError describes the modes actually offered by the server.
// Types are top-level RFB types unless VeNCrypt is true, when they are the
// subtypes read after selecting VeNCrypt. AuthenticationOnly means a supported
// RSA-AES authentication-only mode was refused because permission was absent.
type UnsupportedSecurityError struct {
	Types              []uint32
	VeNCrypt           bool
	AuthenticationOnly bool
}

func (err *UnsupportedSecurityError) Error() string {
	modes := formatSecurityTypes(err.Types, err.VeNCrypt)
	if err.AuthenticationOnly {
		return "rfb: authentication-only encryption requires permission for an unencrypted desktop; server offers " + modes
	}
	if err.VeNCrypt {
		return "rfb: no supported VeNCrypt subtype; server offers " + modes
	}
	return "rfb: no supported security type; server offers " + modes
}

func (err *UnsupportedSecurityError) Unwrap() error { return ErrUnsupported }

// VeNCryptSubtypeName names a wire subtype; unknown values retain their number.
func VeNCryptSubtypeName(value uint32) string {
	switch value {
	case vencryptPlain:
		return "Plain"
	case vencryptTLSNone:
		return "TLSNone"
	case vencryptTLSVnc:
		return "TLSVnc"
	case vencryptTLSPlain:
		return "TLSPlain"
	case vencryptX509None:
		return "X509None"
	case vencryptX509Vnc:
		return "X509Vnc"
	case vencryptX509Plain:
		return "X509Plain"
	}
	return "#" + strconv.FormatUint(uint64(value), 10)
}

// SecurityTypeName names a wire security type; unknown values retain their number.
func SecurityTypeName(value uint32) string {
	switch value {
	case 1:
		return "None"
	case 2:
		return "VNC Authentication"
	case 5:
		return "RA2"
	case 6:
		return "RA2ne"
	case 129:
		return "RA256"
	case 130:
		return "RA256ne"
	case 19:
		return "VeNCrypt"
	}
	return "#" + strconv.FormatUint(uint64(value), 10)
}

func formatSecurityTypes(types []uint32, vencrypt bool) string {
	names := make([]string, len(types))
	for i, value := range types {
		if vencrypt {
			names[i] = VeNCryptSubtypeName(value)
		} else {
			names[i] = SecurityTypeName(value)
		}
	}
	return strings.Join(names, ", ")
}
