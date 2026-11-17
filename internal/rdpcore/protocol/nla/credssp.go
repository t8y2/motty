package nla

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"motty/internal/rdpcore/util"
)

// ErrCredentialsFatal is returned when the server rejects credentials with an
// NTSTATUS that will not succeed on retry (wrong password, locked account, etc.).
// Callers should stop probing and report the error immediately.
var ErrCredentialsFatal = errors.New("credentials rejected")

// ErrNegotiateToken marks a failure before an NTLM Authenticate message was
// sent. A caller may reconnect and try another token encoding without retrying
// credentials. Explicit credential refusals are never marked with this error.
var ErrNegotiateToken = errors.New("token negotiation failed")

// ErrCredentialsSent marks failures after sending an NTLM Authenticate message
// was attempted. Callers must not retry credentials or fall back to TLS-only.
var ErrCredentialsSent = errors.New("failed after credentials were sent")

// ErrClosedAfterCredentials marks a server that dropped the connection right
// after the NTLM Authenticate message, without an error code. A server that
// refuses the password and one that cannot handle this client's handshake
// (older CredSSP versions in particular) look the same here, so it is not a
// credential refusal: the saved password must not be discarded on its account.
// It is still a failed logon attempt, so callers must not retry.
var ErrClosedAfterCredentials = errors.New("the server closed the connection after the credentials were sent; the password may be wrong, or the server may not accept this client's NLA handshake")

// isFatalNTSTATUS returns true for error codes where retrying is pointless.
func isFatalNTSTATUS(code int) bool {
	switch uint32(code) {
	case 0xC000006D, // STATUS_LOGON_FAILURE
		0xC000006A, // STATUS_WRONG_PASSWORD
		0xC0000064, // STATUS_NO_SUCH_USER
		0xC000006E, // STATUS_ACCOUNT_RESTRICTION
		0xC000006F, // STATUS_INVALID_LOGON_HOURS
		0xC0000070, // STATUS_INVALID_WORKSTATION
		0xC0000071, // STATUS_PASSWORD_EXPIRED
		0xC0000072, // STATUS_ACCOUNT_DISABLED
		0xC000015B, // STATUS_LOGON_TYPE_NOT_GRANTED
		0xC0000193, // STATUS_ACCOUNT_EXPIRED
		0xC0000224, // STATUS_PASSWORD_MUST_CHANGE
		0xC0000234, // STATUS_ACCOUNT_LOCKED_OUT
		0x8009030C: // SEC_E_LOGON_DENIED (some servers return SSPI status directly)
		return true
	}
	return false
}

// serverError turns an NTSTATUS from a CredSSP ErrorCode field into an error,
// marking the codes that make a retry pointless.
func serverError(code int) error {
	err := fmt.Errorf("credssp: server error 0x%08X %s", uint32(code), ntstatusName(code))
	if isFatalNTSTATUS(code) {
		return fmt.Errorf("%w: %w", ErrCredentialsFatal, err)
	}
	return err
}

// MaxCredSSPVersion is the highest CredSSP version we support.
// v6 = nonce-based pubKeyAuth (SHA-256); v2 = legacy echo.
const MaxCredSSPVersion = 6

// TSRequest is the top-level CredSSP structure (MS-CSSP 2.2.1).
type tsRequest struct {
	Version     int         `asn1:"explicit,tag:0"`
	NegoTokens  []negoToken `asn1:"explicit,optional,tag:1"`
	AuthInfo    []byte      `asn1:"explicit,optional,tag:2"`
	PubKeyAuth  []byte      `asn1:"explicit,optional,tag:3"`
	ErrorCode   int         `asn1:"explicit,optional,tag:4"`
	ClientNonce []byte      `asn1:"explicit,optional,tag:5"`
}

type negoToken struct {
	Data []byte `asn1:"explicit,tag:0"`
}

// tsCredentials wraps the user's credentials for the final authInfo.
type tsCredentials struct {
	CredType    int    `asn1:"explicit,tag:0"`
	Credentials []byte `asn1:"explicit,tag:1"`
}

// tsPasswordCreds holds domain/user/password in the TSCredentials.
type tsPasswordCreds struct {
	DomainName []byte `asn1:"explicit,tag:0"`
	UserName   []byte `asn1:"explicit,tag:1"`
	Password   []byte `asn1:"explicit,tag:2"`
}

// Authenticate performs CredSSP/NLA authentication over a TLS connection.
// It runs the full CredSSP handshake using NTLMv2 wrapped in SPNEGO.
// advertiseVersion is the CredSSP version sent in the initial TSRequest;
// the actual version used is the minimum of this and the server's response.
// The hostname is used to construct the SPN for MsvAvTargetName in the NTLM TargetInfo.
func Authenticate(tlsConn *tls.Conn, log *slog.Logger, hostname, domain, username, password string, advertiseVersion int) error {
	return authenticate(tlsConn, log, hostname, domain, username, password, advertiseVersion, false)
}

// AuthenticateRawNTLM is Motty's compatibility path for unwrapped NTLM tokens
// inside CredSSP, for servers such as WinPR 2.x whose Negotiate package passes
// input directly to NTLM. It retains TLS, NTLMv2 MIC/channel binding and
// CredSSP public-key verification.
func AuthenticateRawNTLM(tlsConn *tls.Conn, log *slog.Logger, hostname, domain, username, password string, advertiseVersion int) error {
	return authenticate(tlsConn, log, hostname, domain, username, password, advertiseVersion, true)
}

func authenticate(tlsConn *tls.Conn, log *slog.Logger, hostname, domain, username, password string, advertiseVersion int, rawNTLM bool) (err error) {
	credentialsSent := false
	defer func() {
		if err == nil {
			return
		}
		if credentialsSent {
			err = fmt.Errorf("%w: %w", ErrCredentialsSent, err)
		} else if !errors.Is(err, ErrCredentialsFatal) {
			err = fmt.Errorf("%w: %w", ErrNegotiateToken, err)
		}
	}()
	ctx := context.Background()

	// Compute TLS channel bindings hash (RFC 5929 tls-server-end-point)
	// for MsvAvChannelBindings in NTLM TargetInfo (EPA support).
	cert := tlsConn.ConnectionState().PeerCertificates[0]
	cbHash := computeChannelBindingsHash(cert.Raw)
	log.LogAttrs(ctx, slog.LevelDebug, "TLS channel bindings computed",
		util.Bytes("cbHash", cbHash[:]),
		slog.String("certSigAlgo", cert.SignatureAlgorithm.String()))

	// SPN built inside ntlmClient.targetSPN

	ntlm := &ntlmClient{
		domain:              domain,
		username:            username,
		password:            password,
		log:                 log,
		channelBindingsHash: cbHash,
		targetSPN:           "TERMSRV/" + hostname,
	}

	// Step 1: Send an NTLM Negotiate token, optionally wrapped in SPNEGO.
	negotiateMsg := ntlm.negotiate()
	negotiateToken := negotiateMsg
	var mechTypeList []byte
	if !rawNTLM {
		negotiateToken, mechTypeList = encodeNegTokenInit(negotiateMsg)
	}

	req1 := &tsRequest{
		Version:    advertiseVersion,
		NegoTokens: []negoToken{{Data: negotiateToken}},
	}
	req1Bytes, _ := asn1.Marshal(*req1)
	log.LogAttrs(ctx, slog.LevelDebug, "sending NTLM Negotiate",
		slog.Int("advertiseVersion", advertiseVersion), slog.Int("negoBytes", len(negotiateMsg)),
		slog.Int("tsRequestBytes", len(req1Bytes)),
		slog.Bool("rawNTLM", rawNTLM),
		util.Bytes("negotiateToken", negotiateToken),
		util.Bytes("tsRequest", req1Bytes))
	if err := sendTSRequest(tlsConn, req1); err != nil {
		return fmt.Errorf("credssp: sending negotiate: %w", err)
	}

	// Step 2: Read NTLM Challenge from server TSRequest
	resp1, err := readTSRequest(tlsConn)
	if err != nil {
		return fmt.Errorf("credssp: reading challenge: %w", err)
	}
	log.LogAttrs(ctx, slog.LevelDebug, "received server challenge", slog.Int("serverVersion", resp1.Version), slog.Int("negoTokens", len(resp1.NegoTokens)), slog.Int("pubKeyAuthLen", len(resp1.PubKeyAuth)), util.Hex8("errorCode", uint32(resp1.ErrorCode)))
	if resp1.ErrorCode != 0 {
		return serverError(resp1.ErrorCode)
	}
	if len(resp1.NegoTokens) == 0 {
		return errors.New("credssp: no nego tokens in server response")
	}

	ntlmChallenge := resp1.NegoTokens[0].Data
	requestMIC := false
	if rawNTLM {
		if !bytes.HasPrefix(ntlmChallenge, ntlmSignature[:]) {
			return errors.New("credssp: expected a raw NTLM challenge")
		}
	} else {
		negResp, err := decodeNegTokenResp(ntlmChallenge)
		if err != nil {
			return fmt.Errorf("credssp: decoding SPNEGO response: %w", err)
		}
		log.LogAttrs(ctx, slog.LevelDebug, "SPNEGO challenge decoded", slog.Int("negState", negResp.negState), slog.Int("challengeBytes", len(negResp.responseToken)))
		if negResp.negState == spnegoReject {
			return errors.New("credssp: SPNEGO negotiation rejected by server")
		}
		// RFC 4178 §5: when the acceptor asks for a mechListMIC, the initiator
		// must sign the mechanism list it offered; otherwise the Authenticate
		// token closes the exchange.
		requestMIC = negResp.negState == spnegoRequestMIC
		ntlmChallenge = negResp.responseToken
	}

	// The version field of a TSRequest states the version its sender supports
	// (MS-CSSP 2.2.1), so every message keeps advertiseVersion. The server's
	// version only selects the public-key binding below: Windows peers reject a
	// client that lowers the version it advertised in the first TSRequest, and
	// answer with a bare TLS internal_error after the Authenticate message.
	// Patched Windows 7/Server 2008 R2 report 5 and unpatched ones 2, so echoing
	// the server's version here broke every pre-Windows 10 target.
	negotiatedVersion := advertiseVersion
	if resp1.Version > 0 && resp1.Version < negotiatedVersion {
		negotiatedVersion = resp1.Version
	}
	log.LogAttrs(ctx, slog.LevelDebug, "CredSSP version negotiated", slog.Int("negotiated", negotiatedVersion), slog.Int("client", advertiseVersion), slog.Int("server", resp1.Version))

	// Step 3: NTLM Authenticate, then the public-key binding.
	authMsg, err := ntlm.authenticate(ntlmChallenge)
	if err != nil {
		return fmt.Errorf("credssp: building authenticate: %w", err)
	}

	// Initialize NTLM SEAL for mechListMIC, pubKeyAuth, and authInfo.
	seal := newNTLMSeal(ntlm.exportedSessionKey, log)

	// Extract SubjectPublicKey (raw key DER) from SubjectPublicKeyInfo.
	// MS-CSSP 3.1.5 specifies "SubjectPublicKey" — the BIT STRING content
	// inside SubjectPublicKeyInfo, not the full SPKI with algorithm OID.
	spkiBytes := tlsConn.ConnectionState().PeerCertificates[0].RawSubjectPublicKeyInfo
	subjectPubKey, err := extractSubjectPublicKey(spkiBytes)
	if err != nil {
		return fmt.Errorf("credssp: extracting public key: %w", err)
	}
	log.LogAttrs(ctx, slog.LevelDebug, "TLS public key extracted", slog.Int("spkiBytes", len(spkiBytes)), slog.Int("pubKeyBytes", len(subjectPubKey)))

	var binding []byte
	var clientNonce []byte
	if negotiatedVersion >= 5 {
		// CredSSP v5+: nonce-based SHA-256 pubKeyAuth binding
		clientNonce = make([]byte, 32)
		if _, err := rand.Read(clientNonce); err != nil {
			return fmt.Errorf("credssp: generating nonce: %w", err)
		}

		hashInput := append([]byte("CredSSP Client-To-Server Binding Hash\x00"), clientNonce...)
		hashInput = append(hashInput, subjectPubKey...)
		hashValue := sha256.Sum256(hashInput)
		log.LogAttrs(ctx, slog.LevelDebug, "pubKeyAuth hash input", slog.Int("hashInputLen", len(hashInput)), util.Bytes("hashValue", hashValue[:]), util.Bytes("pubKey", subjectPubKey))
		binding = hashValue[:]
	} else {
		// CredSSP v2-v4: legacy pubKeyAuth — encrypt raw public key
		binding = make([]byte, len(subjectPubKey))
		copy(binding, subjectPubKey)
	}

	// A MIC exchange adds a round trip before the binding may be sent: the
	// acceptor answers the Authenticate token with its own NegTokenResp first
	// (WinPR, and therefore GNOME Remote Desktop on Ubuntu 24.04 and later,
	// always asks for the MIC because NTLM is never its preferred mechanism).
	spnegoRound := !rawNTLM && requestMIC

	authToken := authMsg
	if !rawNTLM {
		// The client signs the mechanism list it offered on every SPNEGO path,
		// even when the acceptor did not ask for it. Windows 11 24H2 and Server
		// 2025 verify the list against this MIC before they accept the
		// public-key binding and tear the TLS session down with internal_error
		// when it is missing; older servers accept it unrequested.
		sig := seal.mechListMIC(mechTypeList)
		log.LogAttrs(ctx, slog.LevelDebug, "mechListMIC computed", util.Bytes("mic", sig[:]), slog.Int("mechTypeListLen", len(mechTypeList)))
		// A mechListMIC exchange counts as a signed message on both peers:
		// WinPR's Negotiate layer adds one to every later
		// EncryptMessage/DecryptMessage sequence (negotiate.c, "if
		// (context->mic) MessageSeqNo++"), and Windows' SSPI advances its
		// own counters. The RC4 handles stay at their start (MS-SPNG
		// 3.2.5.1), which mechListMIC leaves untouched.
		seal.seqNumClient = 1
		seal.seqNumServer = 1
		authToken = encodeNegTokenResp(authMsg, sig[:])
	}
	log.LogAttrs(ctx, slog.LevelDebug, "NTLM Authenticate built", slog.Int("authBytes", len(authMsg)), slog.Int("tokenBytes", len(authToken)))

	req2 := &tsRequest{
		Version:    advertiseVersion,
		NegoTokens: []negoToken{{Data: authToken}},
	}
	if clientNonce != nil {
		// MS-CSSP 2.2.1.1.1: a version 5+ client includes its nonce in the
		// second and every later TSRequest, which now includes the Authenticate
		// message on the SPNEGO path.
		req2.ClientNonce = clientNonce
	}
	if !spnegoRound {
		// MS-CSSP 3.2.5: the TSRequest that carries the client's last SPNEGO
		// token or NTLM message MUST carry pubKeyAuth as well. A peer that
		// expects it there — Windows, raw NTLM servers — answers a bare
		// Authenticate by tearing down TLS with an internal_error, which is
		// reported to the user as a possibly wrong password.
		req2.PubKeyAuth = seal.seal(binding)
	}
	log.LogAttrs(ctx, slog.LevelDebug, "sending NTLM Authenticate",
		slog.Bool("withPubKeyAuth", !spnegoRound), slog.Bool("spnegoRound", spnegoRound))
	// A partial write can already count as a logon attempt at the server.
	credentialsSent = true
	if err := sendTSRequest(tlsConn, req2); err != nil {
		return fmt.Errorf("credssp: sending authenticate: %w", err)
	}

	if spnegoRound {
		// Step 3b: accept the acceptor's final SPNEGO token. The TSRequest
		// carrying it can repeat fields of the message it answers — FreeRDP 3.x
		// returns the client's own pubKeyAuth bytes in this reply — so only the
		// negoToken field is meaningful here.
		final, err := readTSRequest(tlsConn)
		if err != nil {
			log.LogAttrs(ctx, slog.LevelError, "failed reading final SPNEGO token", slog.String("error", err.Error()))
			// Same reasoning as below: the credentials have been sent, and a
			// close is not proof of a refusal.
			return fmt.Errorf("%w: credssp: %w", ErrClosedAfterCredentials, err)
		}
		log.LogAttrs(ctx, slog.LevelDebug, "received final SPNEGO token response", slog.Int("negoTokens", len(final.NegoTokens)), util.Hex8("errorCode", uint32(final.ErrorCode)))
		if final.ErrorCode != 0 {
			return serverError(final.ErrorCode)
		}
		if len(final.NegoTokens) > 0 {
			finalToken, err := decodeNegTokenResp(final.NegoTokens[0].Data)
			if err != nil {
				return fmt.Errorf("credssp: decoding final SPNEGO token: %w", err)
			}
			if finalToken.negState == spnegoReject {
				return errors.New("credssp: SPNEGO negotiation rejected by server")
			}
			if len(finalToken.mechListMIC) > 0 {
				// RFC 4178 §5: the acceptor's MIC covers the same mechanism
				// list; without it a peer could rewrite the list to force a
				// different mechanism.
				if !seal.verifyMechListMIC(mechTypeList, finalToken.mechListMIC) {
					return errors.New("credssp: server mechListMIC verification failed")
				}
			}
		}

		pubKeyAuth := seal.seal(binding)
		log.LogAttrs(ctx, slog.LevelDebug, "pubKeyAuth computed", slog.Int("sealedBytes", len(pubKeyAuth)))
		req3 := &tsRequest{Version: advertiseVersion, PubKeyAuth: pubKeyAuth}
		if clientNonce != nil {
			req3.ClientNonce = clientNonce
		}
		if err := sendTSRequest(tlsConn, req3); err != nil {
			return fmt.Errorf("credssp: sending pubKeyAuth: %w", err)
		}
	}

	// Step 4: Read + verify server pubKeyAuth
	resp2, err := readTSRequest(tlsConn)
	if err != nil {
		log.LogAttrs(ctx, slog.LevelError, "failed reading server pubKeyAuth", slog.String("error", err.Error()))
		// Motty divergence: the password has been sent, and trying again under
		// another TLS or CredSSP version would be another failed logon for the
		// account, enough to lock it out after a single mistyped password. The
		// close is not proof of a refusal, though (Windows Server 2012 closes on
		// handshake mismatches too), so it is not reported as one.
		return fmt.Errorf("%w: credssp: %w", ErrClosedAfterCredentials, err)
	}
	log.LogAttrs(ctx, slog.LevelDebug, "received server pubKeyAuth response", slog.Int("pubKeyAuthBytes", len(resp2.PubKeyAuth)), util.Hex8("errorCode", uint32(resp2.ErrorCode)))
	if resp2.ErrorCode != 0 {
		return serverError(resp2.ErrorCode)
	}
	if len(resp2.PubKeyAuth) == 0 {
		return errors.New("credssp: server did not send pubKeyAuth — authentication may have failed")
	}

	// Verify server's pubKeyAuth
	serverPubKeyPlain, err := seal.unseal(resp2.PubKeyAuth)
	if err != nil {
		return fmt.Errorf("credssp: verifying server pubKeyAuth: %w", err)
	}

	if negotiatedVersion >= 5 {
		// v5+: server computes SHA256("CredSSP Server-To-Client Binding Hash\0" || nonce || subjectPubKey)
		serverHashInput := append([]byte("CredSSP Server-To-Client Binding Hash\x00"), clientNonce...)
		serverHashInput = append(serverHashInput, subjectPubKey...)
		expectedHash := sha256.Sum256(serverHashInput)
		if !hmacEqual(serverPubKeyPlain, expectedHash[:]) {
			return errors.New("credssp: server pubKeyAuth verification failed — possible MITM")
		}
	} else {
		// v2-v4: server returns public key with first byte incremented by 1
		expectedPK := make([]byte, len(subjectPubKey))
		copy(expectedPK, subjectPubKey)
		expectedPK[0]++
		if !hmacEqual(serverPubKeyPlain, expectedPK) {
			return errors.New("credssp: server pubKeyAuth verification failed — possible MITM")
		}
	}
	log.LogAttrs(ctx, slog.LevelDebug, "server pubKeyAuth verified")

	// Step 5: Send sealed TSCredentials
	passCreds := tsPasswordCreds{
		DomainName: encodeUTF16LE(domain),
		UserName:   encodeUTF16LE(username),
		Password:   encodeUTF16LE(password),
	}
	passCredsBytes, err := asn1.Marshal(passCreds)
	if err != nil {
		return fmt.Errorf("credssp: encoding TSPasswordCreds: %w", err)
	}

	creds := tsCredentials{
		CredType:    1, // TSPasswordCreds
		Credentials: passCredsBytes,
	}
	credsBytes, err := asn1.Marshal(creds)
	if err != nil {
		return fmt.Errorf("credssp: encoding TSCredentials: %w", err)
	}

	authInfo := seal.seal(credsBytes)

	req3 := &tsRequest{
		Version:  advertiseVersion,
		AuthInfo: authInfo,
	}
	log.LogAttrs(ctx, slog.LevelDebug, "sending sealed credentials", slog.Int("authInfoBytes", len(authInfo)))
	if err := sendTSRequest(tlsConn, req3); err != nil {
		return fmt.Errorf("credssp: sending credentials: %w", err)
	}

	log.LogAttrs(ctx, slog.LevelDebug, "CredSSP handshake complete")
	return nil
}

// extractSubjectPublicKey parses a DER-encoded SubjectPublicKeyInfo and returns
// the raw SubjectPublicKey bytes (the BIT STRING content, i.e. the key-type-specific
// DER encoding without the algorithm OID wrapper).
func extractSubjectPublicKey(spki []byte) ([]byte, error) {
	var info struct {
		Algorithm asn1.RawValue
		PublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(spki, &info); err != nil {
		return nil, fmt.Errorf("parsing SubjectPublicKeyInfo: %w", err)
	}
	return info.PublicKey.Bytes, nil
}

// readTSRequest reads a DER-encoded TSRequest from the TLS stream.
// CredSSP messages are sent directly over TLS without TPKT framing.
func readTSRequest(r io.Reader) (*tsRequest, error) {
	// Read ASN.1 tag + length
	var tagBuf [1]byte
	if _, err := io.ReadFull(r, tagBuf[:]); err != nil {
		return nil, fmt.Errorf("reading TSRequest tag: %w", err)
	}

	length, err := readDERLength(r)
	if err != nil {
		return nil, fmt.Errorf("reading TSRequest length: %w", err)
	}

	// Read the full DER value
	value := make([]byte, length)
	if _, err := io.ReadFull(r, value); err != nil {
		return nil, fmt.Errorf("reading TSRequest body: %w", err)
	}

	// Reconstruct full DER encoding for asn1.Unmarshal
	lenBytes := encodeDERLength(length)
	full := make([]byte, 1+len(lenBytes)+length)
	full[0] = tagBuf[0]
	copy(full[1:], lenBytes)
	copy(full[1+len(lenBytes):], value)

	var req tsRequest
	if _, err := asn1.Unmarshal(full, &req); err != nil {
		return nil, fmt.Errorf("unmarshaling TSRequest: %w", err)
	}
	return &req, nil
}

// sendTSRequest marshals and writes a TSRequest to the TLS stream.
func sendTSRequest(w io.Writer, req *tsRequest) error {
	data, err := asn1.Marshal(*req)
	if err != nil {
		return fmt.Errorf("marshaling TSRequest: %w", err)
	}
	_, err = w.Write(data)
	return err
}

// readDERLength reads a DER length from the stream.
func readDERLength(r io.Reader) (int, error) {
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	if b[0] < 0x80 {
		return int(b[0]), nil
	}
	numBytes := int(b[0] & 0x7F)
	if numBytes > 4 {
		return 0, errors.New("DER length too large")
	}
	buf := make([]byte, numBytes)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, err
	}
	length := 0
	for _, v := range buf {
		length = (length << 8) | int(v)
	}
	return length, nil
}

// encodeDERLength encodes an integer as DER length bytes.
func encodeDERLength(length int) []byte {
	if length < 0x80 {
		return []byte{byte(length)}
	}
	if length < 0x100 {
		return []byte{0x81, byte(length)}
	}
	if length < 0x10000 {
		return []byte{0x82, byte(length >> 8), byte(length)}
	}
	if length < 0x1000000 {
		return []byte{0x83, byte(length >> 16), byte(length >> 8), byte(length)}
	}
	return []byte{0x84, byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)}
}

// computeChannelBindingsHash computes the MD5 hash of the gss_channel_bindings_struct
// containing the TLS server certificate hash per RFC 5929 (tls-server-end-point).
// This is used for MsvAvChannelBindings in the NTLM TargetInfo to support
// Extended Protection for Authentication (EPA).
//
// The hash input follows the gss_channel_bindings_struct layout (RFC 2744):
//
//	LE32(initiator_addrtype)       = 0
//	LE32(initiator_length)         = 0
//	LE32(acceptor_addrtype)        = 0
//	LE32(acceptor_length)          = 0
//	LE32(application_data_length)  = len("tls-server-end-point:" + certHash)
//	application_data               = "tls-server-end-point:" + SHA256(certDER)
//
// Note: only 5 LE32 fields are hashed (no offset fields), matching the
// gss_channel_bindings_struct serialization used by Windows SSPI (RFC 2743 §1.1.6).
func computeChannelBindingsHash(certDER []byte) [16]byte {
	// Hash the certificate DER with SHA-256 per RFC 5929 §4.1:
	// if the certificate's signature algorithm uses MD5 or SHA-1, use SHA-256;
	// otherwise use the certificate's hash algorithm.
	// SHA-256 is correct for the vast majority of certificates.
	certHash := sha256.Sum256(certDER)

	prefix := []byte("tls-server-end-point:")
	appDataLen := uint32(len(prefix) + len(certHash))

	// gss_channel_bindings_struct serialization: 5 x LE32 + application data
	h := md5.New()
	var buf [4]byte
	// dwInitiatorAddrType = 0
	binary.LittleEndian.PutUint32(buf[:], 0)
	h.Write(buf[:])
	// cbInitiatorLength = 0
	binary.LittleEndian.PutUint32(buf[:], 0)
	h.Write(buf[:])
	// dwAcceptorAddrType = 0
	binary.LittleEndian.PutUint32(buf[:], 0)
	h.Write(buf[:])
	// cbAcceptorLength = 0
	binary.LittleEndian.PutUint32(buf[:], 0)
	h.Write(buf[:])
	// cbApplicationDataLength
	binary.LittleEndian.PutUint32(buf[:], appDataLen)
	h.Write(buf[:])
	// application data
	h.Write(prefix)
	h.Write(certHash[:])

	var result [16]byte
	copy(result[:], h.Sum(nil))
	return result
}

// ntstatusName returns a human-readable description for common NTSTATUS codes.
func ntstatusName(code int) string {
	switch uint32(code) {
	case 0xC000006D:
		return "STATUS_LOGON_FAILURE (wrong username or password)"
	case 0xC000006E:
		return "STATUS_ACCOUNT_RESTRICTION"
	case 0xC000006F:
		return "STATUS_INVALID_LOGON_HOURS"
	case 0xC0000070:
		return "STATUS_INVALID_WORKSTATION"
	case 0xC0000071:
		return "STATUS_PASSWORD_EXPIRED"
	case 0xC0000072:
		return "STATUS_ACCOUNT_DISABLED"
	case 0xC000015B:
		return "STATUS_LOGON_TYPE_NOT_GRANTED"
	case 0xC0000193:
		return "STATUS_ACCOUNT_EXPIRED"
	case 0xC0000224:
		return "STATUS_PASSWORD_MUST_CHANGE"
	case 0xC0000234:
		return "STATUS_ACCOUNT_LOCKED_OUT"
	case 0x80090308:
		return "SEC_E_INVALID_TOKEN"
	case 0x8009030C:
		return "SEC_E_LOGON_DENIED"
	case 0x80090311:
		return "SEC_E_NO_AUTHENTICATING_AUTHORITY"
	case 0x80090331:
		return "SEC_E_ALGORITHM_MISMATCH"
	default:
		return ""
	}
}
