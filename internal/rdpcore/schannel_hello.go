package rdp

import (
	utls "github.com/refraction-networking/utls"
)

// tlsHello is the ClientHello an upgradeTLS sends.
type tlsHello uint8

const (
	// helloGo is crypto/tls's own ClientHello.
	helloGo tlsHello = iota
	// helloSchannel is the ClientHello of the Windows RDP client; see
	// schannelClientHello.
	helloSchannel
)

// schannelClientHello is the ClientHello Windows' own RDP client sends for
// CredSSP (mstsc on Windows 11 build 26200, captured on the wire), less its
// TLS 1.3 offer, because CredSSP over TLS 1.3 is what some Windows servers
// fail.
//
// Windows Server 2008 R2 era Schannel accepts the TLS handshake that
// crypto/tls offers but not CredSSP over it: the NTLM Negotiate is answered
// with an internal_error alert, at every TLS and CredSSP version, while mstsc
// connects to the same server. crypto/tls cannot be told which extensions,
// groups or signature algorithms to offer, so the connection probe offers the
// shape Windows servers were built against instead of guessing at which of
// the differences the server minds.
//
// The cipher suites are mstsc's in its order, without the ones utls does not
// implement (CBC with SHA-384 and RSA AES-256-CBC-SHA256): a server choosing
// one would end the handshake. The RSA key exchange suites, which mstsc offers
// last, are only offered with Options.LegacyTLS, as on every other path.
func schannelClientHello(host string, legacy bool) *utls.ClientHelloSpec {
	suites := []uint16{
		utls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		utls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		utls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		utls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		utls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
		utls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
		utls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
		utls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
		utls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		utls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
	}
	if legacy {
		suites = append(suites,
			utls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_RSA_WITH_AES_128_CBC_SHA256,
			utls.TLS_RSA_WITH_AES_256_CBC_SHA,
			utls.TLS_RSA_WITH_AES_128_CBC_SHA,
		)
	}
	return &utls.ClientHelloSpec{
		CipherSuites:       suites,
		CompressionMethods: []uint8{0},
		Extensions: []utls.TLSExtension{
			// Schannel names the dialed host even when it is an address,
			// which utls' SNIExtension leaves out (RFC 6066 §3).
			&utls.GenericExtension{Id: 0, Data: serverNameList(host)},
			&utls.StatusRequestExtension{},
			&utls.SupportedVersionsExtension{Versions: []uint16{
				utls.VersionTLS12, utls.VersionTLS11, utls.VersionTLS10,
			}},
			&utls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []utls.SignatureScheme{
				utls.PSSWithSHA256, utls.PSSWithSHA384, utls.PSSWithSHA512,
				utls.PKCS1WithSHA256, utls.PKCS1WithSHA384, utls.PKCS1WithSHA1,
				utls.ECDSAWithP256AndSHA256, utls.ECDSAWithP384AndSHA384, utls.ECDSAWithSHA1,
				0x0202, // dsa_sha1, which Schannel still lists
				utls.PKCS1WithSHA512, utls.ECDSAWithP521AndSHA512,
			}},
			&utls.SessionTicketExtension{},
			&utls.SupportedCurvesExtension{Curves: []utls.CurveID{
				utls.X25519, utls.CurveP256, utls.CurveP384,
			}},
			&utls.SupportedPointsExtension{SupportedPoints: []uint8{0}},
			&utls.ExtendedMasterSecretExtension{},
			&utls.RenegotiationInfoExtension{Renegotiation: utls.RenegotiateOnceAsClient},
		},
	}
}

// serverNameList is the extension_data of a server_name extension naming
// host (RFC 6066 §3).
func serverNameList(host string) []byte {
	n := len(host)
	data := make([]byte, 0, 5+n)
	data = append(data, byte((n+3)>>8), byte(n+3), 0, byte(n>>8), byte(n))
	return append(data, host...)
}
