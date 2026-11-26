package rdp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rc4"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"motty/internal/rdpcore/protocol/ber"
	"motty/internal/rdpcore/protocol/lic"
	"motty/internal/rdpcore/protocol/mcs"
	"motty/internal/rdpcore/protocol/pdu"
)

// The server here replays how GNOME Remote Desktop 46 receives a remote
// login. Its system daemon on port 3389 authenticates the user over NLA and
// takes the connection up to an active session, which stays blank. Once GDM
// has started the user's session, it sends an Enhanced Security Server
// Redirection PDU laid out by FreeRDP's
// rdp_write_enhanced_security_redirection_packet: a routing token, the
// credentials and the certificate of the handover daemon that serves the
// user's session. Nothing names another host, so the client is expected back
// at the same address with the token as its routingToken, where it
// authenticates with those credentials. A client that does not come back is
// dropped with ERRINFO_CB_CONNECTION_CANCELLED when the handover times out:
// that is the failure reported in issue #1659.

const (
	rigUserChannel uint16 = 1002
	rigIOChannel   uint16 = 1003
	rigShareID     uint32 = 0x000103EA

	// rigAbortDelay stands in for GNOME Remote Desktop's 30 s handover limit.
	rigAbortDelay = 1500 * time.Millisecond
)

var (
	rigUser             = "alice"
	rigSecret           = "alice-secret"
	rigToken            = "2147483647"
	rigHandoverUser     = "GrdRdpUser-73f1"
	rigHandoverPassword = "x7Qd2vLp9sR4tY6z"
)

// rigRedirection is the Server Redirection PDU the scripted server sends.
type rigRedirection struct {
	sessionID uint32
	target    string // LB_TARGET_NET_ADDRESS
	token     string // the routing token as GRD spells it, without "Cookie: msts="
	username  string
	password  string
	certDER   []byte
	// informational sets LB_NOREDIRECT: the server says where the session
	// lives and keeps it where it is.
	informational bool
}

// encode lays the Server Redirection Packet out as FreeRDP's server does.
func (r rigRedirection) encode() []byte {
	utf16z := func(s string) []byte { // NUL-terminated UTF-16LE
		units := utf16.Encode([]rune(s))
		out := make([]byte, 2*len(units)+2)
		for i, unit := range units {
			binary.LittleEndian.PutUint16(out[2*i:], unit)
		}
		return out
	}
	var body bytes.Buffer
	field := func(data []byte) {
		body.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(data))))
		body.Write(data)
	}

	// GNOME Remote Desktop flags every desktop client's password as
	// LB_PASSWORD_IS_PK_ENCRYPTED while sending it in the clear.
	flags := uint32(pdu.LBUsername | pdu.LBPassword | pdu.LBPasswordIsPKEncrypted)
	if r.target != "" {
		flags |= pdu.LBTargetNetAddress
		field(utf16z(r.target))
	}
	if r.token != "" {
		flags |= pdu.LBLoadBalanceInfo
		field([]byte("Cookie: msts=" + r.token + "\r\n"))
	}
	field(utf16z(r.username))
	field(utf16z(r.password))
	flags |= pdu.LBRedirectionGUID
	field(utf16z(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))))
	flags |= pdu.LBTargetCertificate
	container := binary.LittleEndian.AppendUint32(nil, 32)     // CERT_cert_file_element
	container = binary.LittleEndian.AppendUint32(container, 1) // ENCODING_TYPE_ASN1_DER
	container = binary.LittleEndian.AppendUint32(container, uint32(len(r.certDER)))
	field(utf16z(base64.StdEncoding.EncodeToString(append(container, r.certDER...))))
	if r.informational {
		flags |= pdu.LBNoRedirect
	}

	var packet bytes.Buffer
	packet.Write([]byte{0, 0}) // pad2Octets of the Enhanced Security Server Redirection PDU
	start := packet.Len()
	packet.Write(binary.LittleEndian.AppendUint16(nil, 0x0400))
	packet.Write(make([]byte, 2)) // length, patched below
	packet.Write(binary.LittleEndian.AppendUint32(nil, r.sessionID))
	packet.Write(binary.LittleEndian.AppendUint32(nil, flags))
	packet.Write(body.Bytes())
	packet.Write(make([]byte, 8)) // pad
	encoded := packet.Bytes()
	binary.LittleEndian.PutUint16(encoded[start+2:start+4], uint16(len(encoded)-start))
	return encoded
}

// rigStage is when a script sends its redirection. A redirection may end
// any phase of the connection sequence (MS-RDPBCGR 1.3.2).
type rigStage int

const (
	rigWhenActive     rigStage = iota // an active session: GNOME Remote Desktop's system daemon
	rigAfterLogin                     // instead of the licensing answer
	rigAfterLicensing                 // instead of the Demand Active, as a connection broker does
	rigAfterFontList                  // instead of the Font Map
)

// rigScript is what the scripted server does with one connection.
type rigScript struct {
	// logon is the account (domain, user, password) the connection must
	// present over CredSSP.
	logon [3]string
	// redirect is sent at stage, once the connection is active by default.
	redirect *rigRedirection
	stage    rigStage
	// abortUnfollowed ends the connection with GNOME Remote Desktop's
	// handover-timeout sequence unless the client came back with the token.
	abortUnfollowed bool
	// namedSession is the session the Client Cluster Data must then name.
	namedSession uint32
	// refuse drops the connection after its X.224 Connection Request.
	refuse bool
}

// rigConnection is what the server saw on one connection.
type rigConnection struct {
	cookie  string // the Connection Request's cookie or routingToken, CRLF included
	logon   [3]string
	cluster mcs.ClientClusterData
}

type rigServer struct {
	t         *testing.T
	cert      tls.Certificate
	publicKey []byte // SubjectPublicKey the CredSSP binding hashes
	scripts   []rigScript
	forever   bool // connections past the scripts repeat the last one

	listener   net.Listener
	accepted   atomic.Int32
	release    chan struct{}
	followed   chan struct{}
	followOnce sync.Once
	abortOnce  sync.Once

	mu         sync.Mutex
	seen       []rigConnection
	serverErrs []error
}

func newRigCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	certificateServer := httptest.NewTLSServer(nil)
	cert := certificateServer.TLS.Certificates[0]
	certificateServer.Close()
	return cert
}

func newRigServer(t *testing.T, cert tls.Certificate, scripts []rigScript, forever bool) *rigServer {
	t.Helper()
	s := &rigServer{t: t, cert: cert, scripts: scripts, forever: forever,
		release: make(chan struct{}), followed: make(chan struct{})}
	// The binding hashes the SubjectPublicKey: the BIT STRING content of the
	// SubjectPublicKeyInfo, as the client's extractSubjectPublicKey reads it.
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parsing the test certificate: %v", err)
	}
	var spki struct {
		Algorithm asn1.RawValue
		PublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(parsed.RawSubjectPublicKeyInfo, &spki); err != nil {
		t.Fatalf("parsing the test public key: %v", err)
	}
	s.publicKey = spki.PublicKey.Bytes

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.listener = listener
	go s.acceptLoop()
	t.Cleanup(func() {
		listener.Close()
		close(s.release)
		s.mu.Lock()
		errs := s.serverErrs
		s.serverErrs = nil
		s.mu.Unlock()
		for _, err := range errs {
			t.Errorf("test server: %v", err)
		}
	})
	return s
}

func (s *rigServer) addr() (string, int) {
	host, port, _ := net.SplitHostPort(s.listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return host, n
}

func (s *rigServer) script(index int) rigScript {
	if index <= len(s.scripts) {
		return s.scripts[index-1]
	}
	return s.scripts[len(s.scripts)-1]
}

func (s *rigServer) fail(err error) {
	s.mu.Lock()
	s.serverErrs = append(s.serverErrs, err)
	s.mu.Unlock()
}

func (s *rigServer) connections() []rigConnection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]rigConnection(nil), s.seen...)
}

func (s *rigServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		index := int(s.accepted.Add(1))
		if index > 1 {
			// The client is back: the redirecting server can let go of it.
			s.followOnce.Do(func() { close(s.followed) })
		}
		go func() {
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(15 * time.Second))
			_, err := s.play(conn, s.script(index))
			if err != nil {
				s.fail(fmt.Errorf("connection %d: %w", index, err))
			}
		}()
	}
}

// play runs one connection: X.224, TLS, CredSSP, MCS and the connection
// sequence up to the Font Map, then the script's ending.
func (s *rigServer) play(conn net.Conn, script rigScript) (rigConnection, error) {
	var seen rigConnection
	request, err := readTPKT(conn)
	if err != nil {
		return seen, err
	}
	if len(request) < 15 || request[1] != 0xE0 {
		return seen, fmt.Errorf("not an X.224 Connection Request: % x", request)
	}
	seen.cookie = string(request[7 : int(request[0])+1-8])
	if script.refuse {
		return seen, nil
	}
	// Connection Confirm selecting NLA (PROTOCOL_HYBRID).
	if _, err := conn.Write([]byte{3, 0, 0, 19, 14, 0xd0, 0, 0, 0, 0, 0, 2, 0, 8, 0, 2, 0, 0, 0}); err != nil {
		return seen, err
	}

	secured := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12})
	if err := secured.Handshake(); err != nil {
		return seen, err
	}
	seen.logon, err = credsspRigAuthenticate(secured, s.publicKey, script.logon)
	if err != nil {
		return seen, fmt.Errorf("CredSSP: %w", err)
	}

	// Channel connection: MCS Connect Initial and Response, Erect Domain,
	// Attach User, the joins of the user, I/O and two static channels.
	frame, err := readMCS(secured)
	if err != nil || frame.kind != 0x7F {
		return seen, fmt.Errorf("waiting for the MCS Connect Initial: %v", err)
	}
	seen.cluster = clusterDataOf(frame.data)
	s.mu.Lock()
	s.seen = append(s.seen, seen) // what the connection presented, before its ending
	s.mu.Unlock()
	if err := writeDataTPKT(secured, connectResponse()); err != nil {
		return seen, err
	}
	for _, want := range []byte{mcs.DomainMCSPDUErectDomainRequest, mcs.DomainMCSPDUAttachUserRequest} {
		if frame, err = readMCS(secured); err != nil || frame.kind != want {
			return seen, fmt.Errorf("waiting for MCS PDU %#x: %v", want, err)
		}
	}
	if err := writeDataTPKT(secured, []byte{0x2E, 0, 0, 1}); err != nil { // Attach User Confirm: user channel 1002
		return seen, err
	}
	for range 4 {
		if frame, err = readMCS(secured); err != nil || frame.kind != mcs.DomainMCSPDUChannelJoinRequest {
			return seen, fmt.Errorf("waiting for a Channel Join Request: %v", err)
		}
		confirm := append([]byte{0x3E, 0}, frame.data[1:]...)   // result, initiator, channel
		confirm = append(confirm, frame.data[3:5]...)           // and the channel confirmed
		if err := writeDataTPKT(secured, confirm); err != nil { // Channel Join Confirm
			return seen, err
		}
	}
	if frame, err = readMCS(secured); err != nil || frame.kind != mcs.DomainMCSPDUSendDataRequest {
		return seen, fmt.Errorf("waiting for the Client Info PDU: %v", err)
	}
	if script.namedSession != 0 && (seen.cluster.Flags&mcs.RedirectedSessionIDFieldValid == 0 || seen.cluster.RedirectedSessionID != script.namedSession) {
		return seen, fmt.Errorf("Client Cluster Data %#x names session %#x, want session %#x", seen.cluster.Flags, seen.cluster.RedirectedSessionID, script.namedSession)
	}

	// Security: no license needed.
	data := func(pduType2 uint8, payload []byte) error {
		return sendDataIndication(secured, rigIOChannel, pdu.EncodeShareDataPDU(rigShareID, pduType2, 0, payload))
	}
	redirects := script.redirect != nil && !script.redirect.informational
	redirectNow := func() error {
		if err := s.sendRedirection(secured, *script.redirect); err != nil {
			return err
		}
		if script.abortUnfollowed {
			// The redirecting server holds the client until the next one has
			// it, then lets go.
			s.mu.Lock()
			followed := s.followed
			s.mu.Unlock()
			select {
			case <-followed:
			case <-time.After(rigAbortDelay):
				s.abortOnce.Do(func() { s.abort(secured) })
			}
		}
		return nil
	}

	if !(redirects && script.stage == rigAfterLogin) {
		// Security: no license needed.
		licensing := append([]byte{0x80, 0, 0, 0}, lic.EncodePreamble(lic.ErrorAlert, byte(lic.PreambleVersion20), 16)...)
		licensing = binary.LittleEndian.AppendUint32(licensing, lic.StatusValidClient)
		licensing = binary.LittleEndian.AppendUint32(licensing, lic.STNoTransition)
		if err := sendDataIndication(secured, rigIOChannel, licensing); err != nil {
			return seen, err
		}
	}
	if redirects && (script.stage == rigAfterLogin || script.stage == rigAfterLicensing) {
		return seen, redirectNow()
	}

	// Capabilities exchange over a Demand Active without capability sets.
	demand := binary.LittleEndian.AppendUint32(nil, rigShareID)
	demand = append(demand, rigUint16s(0, 0, 0, 0)...)
	if err := sendDataIndication(secured, rigIOChannel, shareControl(pdu.TypeDemandActive, demand)); err != nil {
		return seen, err
	}
	if frame, err = readMCS(secured); err != nil || !isShare(frame.data, pdu.TypeConfirmActive) {
		return seen, fmt.Errorf("waiting for the Confirm Active: %v", err)
	}

	// Connection finalization. The client's four PDUs end with the Font List.
	if err := data(pdu.PDUType2Synchronize, pdu.EncodeSynchronize(rigUserChannel)); err != nil {
		return seen, err
	}
	if err := data(pdu.PDUType2Control, pdu.EncodeControl(pdu.ControlCooperate)); err != nil {
		return seen, err
	}
	for fontList := false; !fontList; {
		if frame, err = readMCS(secured); err != nil {
			return seen, fmt.Errorf("waiting for the client's finalization PDUs: %v", err)
		}
		_, rest, _ := pdu.DecodeShareControlHeader(discardLogger, frame.data)
		if header, _, err := pdu.DecodeShareDataHeader(discardLogger, rest); err == nil {
			fontList = header.PDUType2 == pdu.PDUType2FontList
		}
	}
	if redirects && script.stage == rigAfterFontList {
		return seen, redirectNow()
	}
	if err := data(pdu.PDUType2Control, pdu.EncodeControl(pdu.ControlGrantedControl)); err != nil {
		return seen, err
	}
	if err := data(pdu.PDUType2FontMap, []byte{0, 0, 0, 0, 3, 0, 4, 0}); err != nil {
		return seen, err
	}
	if script.redirect != nil {
		if redirects {
			return seen, redirectNow()
		}
		// Told where the session lives, it stays where it is.
		if err := s.sendRedirection(secured, *script.redirect); err != nil {
			return seen, err
		}
	}

	// The handover daemon's session: one pixel proves the desktop is live.
	// updateType bitmap, one rectangle at (0,0) 1×1, 32 bpp, uncompressed, 4 bytes
	rect := rigUint16s(pdu.UpdateBitmap, 1, 0, 0, 0, 0, 1, 1, 32, 0, 4)
	rect = append(rect, 0x20, 0xDB, 0x1A, 0xFF)
	if err := data(pdu.PDUType2Update, rect); err != nil {
		return seen, err
	}
	// The session stays open until the test is done, draining what the
	// client sends.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, err := readMCS(secured); err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-s.release:
	}
	return seen, nil
}

func (s *rigServer) sendRedirection(conn net.Conn, redirect rigRedirection) error {
	return sendDataIndication(conn, rigIOChannel, shareControl(pdu.TypeEnhancedRedirect, redirect.encode()))
}

// abort is grd_session_rdp_stop once the handover timed out: Deactivate All,
// ERRINFO_CB_CONNECTION_CANCELLED and an MCS Disconnect Provider Ultimatum.
func (s *rigServer) abort(conn net.Conn) {
	deactivate := binary.LittleEndian.AppendUint32(nil, rigShareID)
	deactivate = binary.LittleEndian.AppendUint16(deactivate, 1)
	deactivate = append(deactivate, 0)
	if err := sendDataIndication(conn, rigIOChannel, shareControl(pdu.TypeDeactivateAll, deactivate)); err != nil {
		s.fail(err)
		return
	}
	cancelled := pdu.EncodeShareDataPDU(rigShareID, pdu.PDUType2SetErrorInfo, 0, binary.LittleEndian.AppendUint32(nil, 0x00000409))
	if err := sendDataIndication(conn, rigIOChannel, cancelled); err != nil {
		s.fail(err)
		return
	}
	if err := writeDataTPKT(conn, []byte{0x21, 0x80}); err != nil { // reason 3: provider-initiated
		s.fail(err)
	}
}

func isShare(data []byte, pduType uint16) bool {
	header, _, err := pdu.DecodeShareControlHeader(discardLogger, data)
	return err == nil && header.PDUType&0x000F == pduType&0x000F
}

// clusterDataOf reads the Client Cluster Data out of an MCS Connect Initial.
func clusterDataOf(connectInitial []byte) mcs.ClientClusterData {
	var cluster mcs.ClientClusterData
	key := bytes.Index(connectInitial, []byte("Duca"))
	if key < 0 {
		return cluster
	}
	pos := key + 4
	if pos >= len(connectInitial) {
		return cluster
	}
	if connectInitial[pos]&0x80 != 0 {
		pos += 2
	} else {
		pos++
	}
	for pos+4 <= len(connectInitial) {
		blockType := binary.LittleEndian.Uint16(connectInitial[pos:])
		blockLen := int(binary.LittleEndian.Uint16(connectInitial[pos+2:]))
		if blockLen < 4 || pos+blockLen > len(connectInitial) {
			break
		}
		if blockType == mcs.ClientClusterDataType && blockLen >= 12 {
			cluster.Flags = binary.LittleEndian.Uint32(connectInitial[pos+4:])
			cluster.RedirectedSessionID = binary.LittleEndian.Uint32(connectInitial[pos+8:])
		}
		pos += blockLen
	}
	return cluster
}

func rigUint16s(values ...uint16) []byte {
	var out []byte
	for _, value := range values {
		out = binary.LittleEndian.AppendUint16(out, value)
	}
	return out
}

func readTPKT(conn io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	if header[0] != 3 {
		return nil, fmt.Errorf("TPKT version %d", header[0])
	}
	length := int(binary.BigEndian.Uint16(header[2:4])) - 4
	if length < 0 {
		return nil, fmt.Errorf("TPKT length %d", length+4)
	}
	payload := make([]byte, length)
	_, err := io.ReadFull(conn, payload)
	return payload, err
}

func writeDataTPKT(w io.Writer, mcsPDU []byte) error {
	out := make([]byte, 7+len(mcsPDU))
	out[0] = 3
	binary.BigEndian.PutUint16(out[2:4], uint16(len(out)))
	out[4], out[5], out[6] = 0x02, 0xF0, 0x80 // X.224 Data TPDU
	copy(out[7:], mcsPDU)
	_, err := w.Write(out)
	return err
}

func sendDataIndication(w io.Writer, channel uint16, data []byte) error {
	indication := []byte{mcs.DomainMCSPDUSendDataIndication, 0, 1, byte(channel >> 8), byte(channel), 0x70}
	indication = appendPERLength(indication, len(data))
	return writeDataTPKT(w, append(indication, data...))
}

func appendPERLength(dst []byte, length int) []byte {
	switch {
	case length < 0x80:
		return append(dst, byte(length))
	case length < 0x4000:
		return append(dst, byte(0x80|length>>8), byte(length))
	default:
		return append(dst, 0x80, byte(length>>8), byte(length))
	}
}

type rigFrame struct {
	kind      byte   // first byte of the MCS PDU
	data      []byte // MCS Connect Initial, or the user data of a Send Data Request
	initiator uint16
	channel   uint16
}

func readMCS(conn io.Reader) (rigFrame, error) {
	payload, err := readTPKT(conn)
	var frame rigFrame
	if err != nil {
		return frame, err
	}
	if len(payload) < 4 || payload[0] != 0x02 || payload[1] != 0xF0 {
		return frame, fmt.Errorf("not an X.224 Data TPDU: % x", payload)
	}
	p := payload[3:]
	frame.kind = p[0]
	switch p[0] {
	case 0x7F: // MCS Connect Initial: BER Application 101 in the long tag form
		frame.data = p
	case mcs.DomainMCSPDUErectDomainRequest, mcs.DomainMCSPDUAttachUserRequest:
	case mcs.DomainMCSPDUChannelJoinRequest, mcs.DomainMCSPDUSendDataRequest:
		if len(p) < 5 {
			return frame, errors.New("short MCS PDU")
		}
		frame.initiator = binary.BigEndian.Uint16(p[1:3]) + 1001
		frame.channel = binary.BigEndian.Uint16(p[3:5])
		frame.data = p
		if p[0] == mcs.DomainMCSPDUSendDataRequest {
			data := p[6:]
			var length int
			switch {
			case len(data) > 0 && data[0]&0x80 == 0:
				length, data = int(data[0]), data[1:]
			case len(data) > 2 && data[0] == 0x80:
				length, data = int(data[1])<<8|int(data[2]), data[3:]
			case len(data) > 1:
				length, data = int(data[0]&0x7F)<<8|int(data[1]), data[2:]
			default:
				return frame, errors.New("short PER length")
			}
			if length > len(data) {
				return frame, fmt.Errorf("Send Data Request wants %d bytes, has %d", length, len(data))
			}
			frame.data = data[:length]
		}
	default:
		return frame, fmt.Errorf("unexpected MCS PDU %#x", p[0])
	}
	return frame, nil
}

// connectResponse is an MCS Connect Response carrying the server's channel
// data: I/O channel 1003 and the static channels 1004 and 1005.
func connectResponse() []byte {
	var gcc bytes.Buffer
	gcc.Write([]byte{0x00, 0x05, 0x00, 0x14, 0x7C, 0x00, 0x01, 0xC0})
	gcc.Write([]byte("McDn"))
	blocks := []byte{
		0x01, 0x0C, 0x08, 0x00, 0x04, 0x00, 0x08, 0x00, // server core data: RDP 5.0
		0x03, 0x0C, 0x0C, 0x00, 0xEB, 0x03, 0x02, 0x00, 0xEC, 0x03, 0xED, 0x03, // network data
	}
	gcc.WriteByte(byte(len(blocks)))
	gcc.Write(blocks)

	var content bytes.Buffer
	ber.WriteEnumerated(&content, 0)
	ber.WriteInteger(&content, 1)
	ber.WriteDomainParameters(&content, ber.DomainParameters{MaxChannelIDs: 34, MaxUserIDs: 2, NumPriorities: 1, MaxHeight: 1, MaxMCSPDUSize: 65535, ProtocolVersion: 2})
	ber.WriteOctetString(&content, gcc.Bytes())

	var response bytes.Buffer
	ber.WriteTag(&response, ber.ClassApplication, ber.Constructed, mcs.TagConnectResponse)
	ber.WriteLength(&response, content.Len())
	response.Write(content.Bytes())
	return response.Bytes()
}

// ---- CredSSP and NTLM, the server side ----

// rigTokens is the negoTokens field of a TSRequest with one token.
func rigTokens(data []byte) []struct {
	Data []byte `asn1:"explicit,tag:0"`
} {
	return []struct {
		Data []byte `asn1:"explicit,tag:0"`
	}{{Data: data}}
}

// spnegoResponse is the NegTokenResp an acceptor sends: state 3 asks the
// initiator for a mechListMIC, which WinPR does, state 0 completes.
func spnegoResponse(negState int, token []byte) []byte {
	body, _ := asn1.Marshal(struct {
		State asn1.Enumerated `asn1:"explicit,tag:0"`
		Token []byte          `asn1:"explicit,optional,tag:2"`
	}{State: asn1.Enumerated(negState), Token: token})
	wrapped, _ := asn1.Marshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 1, IsCompound: true, Bytes: body})
	return wrapped
}

// credsspRigAuthenticate plays the server of the CredSSP exchange as WinPR
// plays it for GNOME Remote Desktop: SPNEGO-wrapped NTLM with the mechListMIC
// round, the public-key binding, then the sealed credentials. want is the
// account the client must prove; the credentials it presents are returned.
func credsspRigAuthenticate(conn *tls.Conn, publicKey []byte, want [3]string) ([3]string, error) {
	none := [3]string{}
	req, err := readNLATestRequest(conn)
	if err != nil {
		return none, err
	}
	if len(req.NegoTokens) != 1 || !bytes.Contains(req.NegoTokens[0].Data, []byte("NTLMSSP\x00")) {
		return none, errors.New("the first TSRequest does not offer NTLM over SPNEGO")
	}
	challenge := clientTestNTLMChallenge()
	if err := writeNLATestRequest(conn, nlaTestRequest{Version: 6, NegoTokens: rigTokens(spnegoResponse(3, challenge))}); err != nil {
		return none, err
	}
	req, err = readNLATestRequest(conn)
	if err != nil || len(req.NegoTokens) != 1 || len(req.ClientNonce) == 0 {
		return none, fmt.Errorf("waiting for the NTLM Authenticate and nonce: %v", err)
	}
	keys, err := newRigNTLMKeys(req.NegoTokens[0].Data, challenge, want)
	if err != nil {
		return none, err
	}
	if err := writeNLATestRequest(conn, nlaTestRequest{Version: 6, NegoTokens: rigTokens(spnegoResponse(0, nil))}); err != nil {
		return none, err
	}
	req, err = readNLATestRequest(conn)
	if err != nil || len(req.PubKeyAuth) == 0 {
		return none, fmt.Errorf("waiting for the client's public-key binding: %v", err)
	}
	keys.nonce = req.ClientNonce
	clientBinding, err := keys.unsealClient(req.PubKeyAuth, 1)
	if err != nil {
		return none, err
	}
	if want := keys.binding("CredSSP Client-To-Server Binding Hash", publicKey); !hmac.Equal(clientBinding, want) {
		return none, errors.New("the client's public-key binding does not match")
	}
	serverBinding := keys.binding("CredSSP Server-To-Client Binding Hash", publicKey)
	if err := writeNLATestRequest(conn, nlaTestRequest{Version: 6, PubKeyAuth: keys.sealServer(serverBinding, 1)}); err != nil {
		return none, err
	}
	req, err = readNLATestRequest(conn)
	if err != nil || len(req.AuthInfo) == 0 {
		return none, fmt.Errorf("waiting for the credentials: %v", err)
	}
	credentials, err := keys.unsealClient(req.AuthInfo, 2)
	if err != nil {
		return none, err
	}
	var envelope struct {
		CredType    int    `asn1:"explicit,tag:0"`
		Credentials []byte `asn1:"explicit,tag:1"`
	}
	if _, err := asn1.Unmarshal(credentials, &envelope); err != nil {
		return none, fmt.Errorf("TSCredentials: %w", err)
	}
	var password struct {
		Domain []byte `asn1:"explicit,tag:0"`
		User   []byte `asn1:"explicit,tag:1"`
		Secret []byte `asn1:"explicit,tag:2"`
	}
	if _, err := asn1.Unmarshal(envelope.Credentials, &password); err != nil {
		return none, fmt.Errorf("TSPasswordCreds: %w", err)
	}
	return [3]string{decodeRigUTF16(password.Domain), decodeRigUTF16(password.User), decodeRigUTF16(password.Secret)}, nil
}

// rigNTLMKeys are the NTLM sealing keys of an exchange (MS-NLMP 3.4.4), as
// the client's protocol/nla seal uses them.
type rigNTLMKeys struct {
	nonce                        []byte
	clientSign, serverSign       [16]byte
	clientSealRC4, serverSealRC4 *rc4.Cipher
}

const rigNTLMKeyExch = 0x40000000

// newRigNTLMKeys checks that the NTLM Authenticate message proves want and
// derives the keys of the session it establishes.
func newRigNTLMKeys(token, challenge []byte, want [3]string) (*rigNTLMKeys, error) {
	start := bytes.Index(token, []byte("NTLMSSP\x00"))
	if start < 0 || len(token)-start < 88 {
		return nil, errors.New("no NTLM Authenticate message in the SPNEGO token")
	}
	auth := token[start:]
	if binary.LittleEndian.Uint32(auth[8:12]) != 3 {
		return nil, errors.New("SPNEGO token does not carry the NTLM Authenticate")
	}
	field := func(lengthAt, offsetAt int) ([]byte, error) {
		length, at := int(binary.LittleEndian.Uint16(auth[lengthAt:])), int(binary.LittleEndian.Uint32(auth[offsetAt:]))
		if at+length > len(auth) {
			return nil, errors.New("NTLM Authenticate message is truncated")
		}
		return auth[at : at+length], nil
	}
	response, err := field(20, 24)
	if err != nil {
		return nil, err
	}
	domainBytes, _ := field(28, 32)
	userBytes, _ := field(36, 40)
	serverChallenge := challenge[24:32]

	// NTLMv2: NTProofStr = HMAC_MD5(ResponseKeyNT, ServerChallenge || temp),
	// ResponseKeyNT = HMAC_MD5(NT hash, UTF16(UPPER(user) + domain)).
	user, domain := decodeRigUTF16(userBytes), decodeRigUTF16(domainBytes)
	ntHash := rigMD4(encodedRigUTF16(want[2]))
	responseKey := hmacMD5(ntHash, encodedRigUTF16(strings.ToUpper(user)+domain))
	if user != want[1] || domain != want[0] || !hmac.Equal(response[:16], hmacMD5(responseKey, append(serverChallenge, response[16:]...))) {
		return nil, fmt.Errorf("the logon does not prove %q in domain %q with its password", want[1], want[0])
	}
	sessionBaseKey := hmacMD5(responseKey, response[:16])
	exported := sessionBaseKey
	if binary.LittleEndian.Uint32(auth[60:64])&rigNTLMKeyExch != 0 {
		encryptedKey, err := field(52, 56)
		if err != nil || len(encryptedKey) != 16 {
			return nil, errors.New("no key exchange in the NTLM Authenticate message")
		}
		cipher, _ := rc4.NewCipher(sessionBaseKey)
		exported = make([]byte, 16)
		cipher.XORKeyStream(exported, encryptedKey)
	}
	key := func(magic string) []byte {
		sum := md5.Sum(append(append([]byte{}, exported...), magic...))
		return sum[:]
	}
	keys := &rigNTLMKeys{}
	copy(keys.clientSign[:], key("session key to client-to-server signing key magic constant\x00"))
	copy(keys.serverSign[:], key("session key to server-to-client signing key magic constant\x00"))
	keys.clientSealRC4, _ = rc4.NewCipher(key("session key to client-to-server sealing key magic constant\x00"))
	keys.serverSealRC4, _ = rc4.NewCipher(key("session key to server-to-client sealing key magic constant\x00"))
	return keys, nil
}

// binding is the CredSSP public-key binding of a direction (MS-CSSP 3.2.5).
func (k *rigNTLMKeys) binding(direction string, publicKey []byte) []byte {
	sum := sha256.Sum256(bytes.Join([][]byte{[]byte(direction + "\x00"), k.nonce, publicKey}, nil))
	return sum[:]
}

// sealServer seals a server message: signature, then ciphertext, with the
// persistent RC4 handle (MS-NLMP 3.4.4).
func (k *rigNTLMKeys) sealServer(plaintext []byte, seq uint32) []byte {
	ciphertext := make([]byte, len(plaintext))
	k.serverSealRC4.XORKeyStream(ciphertext, plaintext)
	return append(k.mac(k.serverSign[:], k.serverSealRC4, seq, plaintext), ciphertext...)
}

// unsealClient reads a sealed client message and verifies its signature.
func (k *rigNTLMKeys) unsealClient(data []byte, seq uint32) ([]byte, error) {
	if len(data) < 16 {
		return nil, errors.New("sealed message too short")
	}
	plaintext := make([]byte, len(data)-16)
	k.clientSealRC4.XORKeyStream(plaintext, data[16:])
	if !hmac.Equal(data[:16], k.mac(k.clientSign[:], k.clientSealRC4, seq, plaintext)) {
		return nil, errors.New("the client's signature does not verify")
	}
	return plaintext, nil
}

func (k *rigNTLMKeys) mac(signKey []byte, seal *rc4.Cipher, seq uint32, message []byte) []byte {
	mac := hmac.New(md5.New, signKey)
	mac.Write(binary.LittleEndian.AppendUint32(nil, seq))
	mac.Write(message)
	signature := make([]byte, 16)
	binary.LittleEndian.PutUint32(signature, 1)
	seal.XORKeyStream(signature[4:12], mac.Sum(nil)[:8])
	binary.LittleEndian.PutUint32(signature[12:], seq)
	return signature
}

func hmacMD5(key, data []byte) []byte {
	mac := hmac.New(md5.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func encodedRigUTF16(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 2*len(units))
	for i, unit := range units {
		binary.LittleEndian.PutUint16(out[2*i:], unit)
	}
	return out
}

func decodeRigUTF16(b []byte) string {
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(units))
}

// rigMD4 is MD4 (RFC 1320), which the standard library lacks: it gives the
// NT hash of a password.
func rigMD4(data []byte) []byte {
	var a, b, c, d uint32 = 0x67452301, 0xefcdab89, 0x98badcfe, 0x10325476
	msg := append(append(make([]byte, 0, len(data)+64), data...), 0x80)
	for len(msg)%64 != 56 {
		msg = append(msg, 0)
	}
	msg = binary.LittleEndian.AppendUint64(msg, uint64(len(data))*8)
	for off := 0; off < len(msg); off += 64 {
		var x [16]uint32
		for i := range x {
			x[i] = binary.LittleEndian.Uint32(msg[off+4*i:])
		}
		aa, bb, cc, dd := a, b, c, d
		round := func(shifts []uint, pick func(i int) int, f func(b, c, d uint32) uint32, add uint32) {
			for i := 0; i < 16; i++ {
				a, b, c, d = d, bits.RotateLeft32(a+f(b, c, d)+x[pick(i)]+add, int(shifts[i%4])), b, c
			}
		}
		identity := func(i int) int { return i }
		round([]uint{3, 7, 11, 19}, identity, func(b, c, d uint32) uint32 { return b&c | ^b&d }, 0)
		round([]uint{3, 5, 9, 13}, func(i int) int { return (i*4 + i/4) & 15 }, func(b, c, d uint32) uint32 { return b&c | b&d | c&d }, 0x5A827999)
		round([]uint{3, 9, 11, 15}, func(i int) int { return ([16]int{0, 8, 4, 12, 2, 10, 6, 14, 1, 9, 5, 13, 3, 11, 7, 15})[i] }, func(b, c, d uint32) uint32 { return b ^ c ^ d }, 0x6ED9EBA1)
		a, b, c, d = a+aa, b+bb, c+cc, d+dd
	}
	out := make([]byte, 16)
	binary.LittleEndian.PutUint32(out, a)
	binary.LittleEndian.PutUint32(out[4:], b)
	binary.LittleEndian.PutUint32(out[8:], c)
	binary.LittleEndian.PutUint32(out[12:], d)
	return out
}

// ---- tests ----

func TestRigMD4IsTheRFC1320Hash(t *testing.T) {
	for data, want := range map[string]string{"": "31d6cfe0d16ae931b73c59d7e0c089c0", "abc": "a448017aaf21d8525fc10ae87aa6729d"} {
		if got := hex.EncodeToString(rigMD4([]byte(data))); got != want {
			t.Errorf("MD4(%q) = %s, want %s", data, got, want)
		}
	}
}

// The sequence of issue #1659. Until the client followed the redirection it
// read the token-less PDU as LB_NOREDIRIRECT and stayed on the first
// connection, and the server's handover timeout then ended the session with
// "capabilities exchange failed: the server reported error 0x00004009".
func TestServerRedirectionFollowsGnomeRemoteDesktopHandover(t *testing.T) {
	cert := newRigCertificate(t)
	handover := rigRedirection{
		token:    rigToken,
		username: rigHandoverUser,
		password: rigHandoverPassword,
		certDER:  cert.Certificate[0],
	}
	server := newRigServer(t, cert, []rigScript{
		{logon: [3]string{"", rigUser, rigSecret}, redirect: &handover, abortUnfollowed: true},
		{logon: [3]string{"", handover.username, handover.password}},
	}, false)

	options := rigOptions(t, server, cert)
	var pictures atomic.Int32
	client, err := NewClient(options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.OnBitmap(func(*BitmapUpdate) { pictures.Add(1) })
	disconnected := rigDisconnected(client)
	reconnected := make(chan struct{}, 1)
	client.OnReconnected(func() {
		select {
		case reconnected <- struct{}{}:
		default:
		}
	})

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	rigAwait(t, "the handover", reconnected, disconnected)

	// The system daemon only lets go once the handover daemon has the client.
	if err := rigWaitForPictures(&pictures); err != nil {
		t.Fatal(err)
	}
	connections := server.connections()
	if len(connections) != 2 {
		t.Fatalf("the server saw %d connections, want the login and the redirected session", len(connections))
	}
	if want := "Cookie: mstshash=" + rigUser + "\r\n"; connections[0].cookie != want {
		t.Errorf("first cookie = %q, want %q", connections[0].cookie, want)
	}
	if want := "Cookie: msts=" + rigToken + "\r\n"; connections[1].cookie != want {
		t.Errorf("reconnected with routingToken %q, want %q", connections[1].cookie, want)
	}
	// Both connections present their own account over NLA, which the server
	// checks (the redirected one with the handover daemon's credentials).
	if connections[0].cluster.Flags&mcs.RedirectedSessionIDFieldValid != 0 || connections[1].cluster.Flags&mcs.RedirectedSessionIDFieldValid != 0 {
		t.Errorf("a redirection without a session ID named session %#x / %#x", connections[0].cluster.RedirectedSessionID, connections[1].cluster.RedirectedSessionID)
	}
	if server.accepted.Load() != 2 {
		t.Errorf("accepted %d connections, want 2", server.accepted.Load())
	}
}

// A connection broker hands the session over the same way before the
// capabilities exchange, names another host and a session to reconnect to.
func TestServerRedirectionBeforeActivationToAnotherHost(t *testing.T) {
	cert := newRigCertificate(t)
	broker := rigRedirection{sessionID: 0x5A17C0DE, target: "session-host.example", username: rigUser, password: "broker-cookie", certDER: cert.Certificate[0]}
	server := newRigServer(t, cert, []rigScript{
		{logon: [3]string{"", rigUser, rigSecret}, redirect: &broker, stage: rigAfterLicensing},
		{logon: [3]string{"", rigUser, "broker-cookie"}, namedSession: broker.sessionID},
	}, false)

	host, port := server.addr()
	listener := net.JoinHostPort(host, strconv.Itoa(port))
	options := DefaultOptions()
	options.Host, options.Port = host, port
	options.Username, options.Password = rigUser, rigSecret
	var certificateChecks atomic.Int32
	options.VerifyCertificate = rigVerifyOnce(t, cert, &certificateChecks)
	var mu sync.Mutex
	var dialed []string
	options.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, addr)
		mu.Unlock()
		// The client decides where the bytes go; the test listens on one address.
		return new(net.Dialer).DialContext(ctx, network, listener)
	}
	var pictures atomic.Int32
	client, err := NewClient(options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.OnBitmap(func(*BitmapUpdate) { pictures.Add(1) })
	disconnected := rigDisconnected(client)

	// The redirection arrives during the handshake: Connect follows it
	// before returning.
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rigWaitForPictures(&pictures); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-disconnected:
		t.Fatalf("the redirected session ended: %v", err)
	default:
	}
	mu.Lock()
	want := []string{listener, "session-host.example:" + strconv.Itoa(port)}
	if len(dialed) != 2 || dialed[0] != want[0] || dialed[1] != want[1] {
		t.Errorf("dialed %q, want %q (the redirection names a host, the port stays)", dialed, want)
	}
	mu.Unlock()
	if certificateChecks.Load() != 1 {
		t.Errorf("the caller was asked about the certificate %d times, want the one of the first connection", certificateChecks.Load())
	}
}

// A server that keeps redirecting must not keep the client reconnecting.
func TestServerRedirectionIsBounded(t *testing.T) {
	cert := newRigCertificate(t)
	loop := rigRedirection{token: rigToken, username: rigUser, password: rigSecret, certDER: cert.Certificate[0]}
	server := newRigServer(t, cert, []rigScript{
		{logon: [3]string{"", rigUser, rigSecret}, redirect: &loop, stage: rigAfterLicensing},
	}, true)

	client, err := NewClient(rigOptions(t, server, cert))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	err = client.Connect()
	if !errors.Is(err, ErrServerRedirect) || !strings.Contains(err.Error(), "9 times") {
		t.Fatalf("Connect error = %v, want the redirection chain to be refused after %d redirections", err, maxRedirections)
	}
	if got := server.accepted.Load(); got != maxRedirections+1 {
		t.Errorf("the server saw %d connections, want %d", got, maxRedirections+1)
	}
}

// LB_NOREDIRIRECT makes the PDU a statement about the session's placement:
// the session stays where it is.
func TestInformationalRedirectionKeepsTheSession(t *testing.T) {
	cert := newRigCertificate(t)
	informational := rigRedirection{token: rigToken, username: rigUser, password: rigSecret, certDER: cert.Certificate[0], informational: true}
	server := newRigServer(t, cert, []rigScript{
		{logon: [3]string{"", rigUser, rigSecret}, redirect: &informational},
	}, false)

	var pictures atomic.Int32
	client, err := NewClient(rigOptions(t, server, cert))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.OnBitmap(func(*BitmapUpdate) { pictures.Add(1) })
	disconnected := rigDisconnected(client)
	reconnected := make(chan struct{}, 1)
	client.OnReconnected(func() { reconnected <- struct{}{} })

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rigWaitForPictures(&pictures); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reconnected:
		t.Fatal("an informational redirection was followed")
	default:
	}
	rigAwaitNothing(t, reconnected, disconnected)
	if got := server.accepted.Load(); got != 1 {
		t.Errorf("the server saw %d connections, want 1", got)
	}
}

func rigOptions(t *testing.T, server *rigServer, cert tls.Certificate) *Options {
	t.Helper()
	options := DefaultOptions()
	host, port := server.addr()
	options.Host, options.Port = host, port
	options.Username, options.Password = rigUser, rigSecret
	options.VerifyCertificate = rigVerifyOnce(t, cert, new(atomic.Int32))
	return options
}

// rigVerifyOnce accepts the server's certificate and counts the times the
// client asks about it.
func rigVerifyOnce(t *testing.T, cert tls.Certificate, checks *atomic.Int32) func([]byte) error {
	t.Helper()
	return func(der []byte) error {
		checks.Add(1)
		if !bytes.Equal(der, cert.Certificate[0]) {
			return errors.New("not the test server's certificate")
		}
		return nil
	}
}

func rigDisconnected(client *Client) <-chan error {
	disconnected := make(chan error, 1)
	client.OnDisconnect(func(err error) {
		select {
		case disconnected <- err:
		default:
		}
	})
	return disconnected
}

func rigAwait(t *testing.T, what string, done <-chan struct{}, disconnected <-chan error) {
	t.Helper()
	select {
	case <-done:
	case err := <-disconnected:
		t.Fatalf("%s ended the session: %v", what, err)
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not happen", what)
	}
}

func rigAwaitNothing(t *testing.T, reconnected <-chan struct{}, disconnected <-chan error) {
	t.Helper()
	select {
	case <-reconnected:
		t.Fatal("reconnected unexpectedly")
	case err := <-disconnected:
		t.Fatalf("the session ended: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
}

func rigWaitForPictures(pictures *atomic.Int32) error {
	deadline := time.Now().Add(5 * time.Second)
	for pictures.Load() == 0 {
		if time.Now().After(deadline) {
			return errors.New("the redirected session did not show its desktop")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// A redirection password flagged LB_PASSWORD_IS_PK_ENCRYPTED is an RDSTLS
// cookie unless it reads as a terminated Unicode string, which is the
// cleartext GNOME Remote Desktop sends.
func TestRedirectPasswordTakesCleartextOnly(t *testing.T) {
	packet := func(password []byte) *pdu.RedirectInfo {
		return &pdu.RedirectInfo{Flags: pdu.LBPassword | pdu.LBPasswordIsPKEncrypted, Password: password}
	}
	if got := redirectPassword(packet(encodedRigUTF16(rigHandoverPassword + "\x00"))); got != rigHandoverPassword {
		t.Errorf("cleartext redirection password read as %q, want %q", got, rigHandoverPassword)
	}
	for name, blob := range map[string][]byte{
		"not Unicode":         {0x02, 0x00, 0x00, 0x80},
		"unterminated":        encodedRigUTF16("open-ended"),
		"empty":               nil,
		"embedded terminator": encodedRigUTF16("two\x00parts\x00"),
	} {
		if got := redirectPassword(packet(blob)); got != "" {
			t.Errorf("%s password read as %q, want it refused", name, got)
		}
	}
}

// Under Standard RDP Security a Server Redirection PDU has no Share Control
// Header and no padding octets (MS-RDPBCGR 2.2.13.2.1). Wrapped as the share
// PDU the handlers dispatch on, its packet must read like the enhanced one's.
func TestStandardSecurityRedirectionIsReadWithoutItsPadding(t *testing.T) {
	client := capabilitiesClient(t)
	packet := rigRedirection{sessionID: 7, token: rigToken, username: rigUser, password: rigSecret, certDER: []byte{0x30, 0x03, 0x02, 0x01, 0x00}}

	enhanced := shareControl(pdu.TypeEnhancedRedirect, packet.encode())
	_, fromEnhanced := client.capabilitiesPDU(client.ioChannelID, enhanced)
	standard := shareControl(pdu.TypeRedirect, packet.encode()[2:])
	_, fromStandard := client.capabilitiesPDU(client.ioChannelID, standard)
	var want, got *RedirectError
	if !errors.As(fromEnhanced, &want) || !errors.As(fromStandard, &got) {
		t.Fatalf("enhanced: %v; standard: %v; want both to be redirections", fromEnhanced, fromStandard)
	}
	if got.Info.SessionID != 7 || got.Info.Username != rigUser || !bytes.Equal(got.Info.Password, want.Info.Password) ||
		!bytes.Equal(got.Info.TargetCertificate, packet.certDER) || string(got.Info.LoadBalanceInfo) != "Cookie: msts="+rigToken+"\r\n" {
		t.Fatalf("standard redirection read as %+v, want the packet of %+v", got.Info, want.Info)
	}
	if hasRedirectionSecurityHeader([]byte{0x80, 0x00}) || !hasRedirectionSecurityHeader([]byte{0x00, 0x04, 0x02, 0x00}) {
		t.Fatal("SEC_REDIRECTION_PKT in the Basic Security Header marks the redirection")
	}
	if !bytes.Equal(wrapStandardRedirection([]byte{1, 2}), []byte{8, 0, 0x15, 0, 0, 0, 1, 2}) {
		t.Fatalf("wrapped as % x", wrapStandardRedirection([]byte{1, 2}))
	}
}

// A redirection may end any phase of the connection sequence (MS-RDPBCGR
// 1.3.2): wherever it comes, the session continues on the redirected
// connection, which Connect returns once it is up when the redirection came
// during the handshake.
func TestServerRedirectionIsFollowedWhereverItArrives(t *testing.T) {
	for name, stage := range map[string]rigStage{
		"before the licensing answer": rigAfterLogin,
		"before the Demand Active":    rigAfterLicensing,
		"during finalization":         rigAfterFontList,
	} {
		t.Run(name, func(t *testing.T) {
			cert := newRigCertificate(t)
			redirect := rigRedirection{token: rigToken, username: rigHandoverUser, password: rigHandoverPassword, certDER: cert.Certificate[0]}
			server := newRigServer(t, cert, []rigScript{
				{logon: [3]string{"", rigUser, rigSecret}, redirect: &redirect, stage: stage, abortUnfollowed: true},
				{logon: [3]string{"", rigHandoverUser, rigHandoverPassword}},
			}, false)

			var pictures atomic.Int32
			client, err := NewClient(rigOptions(t, server, cert))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			client.OnBitmap(func(*BitmapUpdate) { pictures.Add(1) })
			disconnected := rigDisconnected(client)

			if err := client.Connect(); err != nil {
				t.Fatalf("Connect: %v", err)
			}
			if err := rigWaitForPictures(&pictures); err != nil {
				t.Fatal(err)
			}
			connections := server.connections()
			if len(connections) != 2 || connections[1].cookie != "Cookie: msts="+rigToken+"\r\n" {
				t.Fatalf("connections %d, reconnected with %q", len(connections), connections[len(connections)-1].cookie)
			}
			select {
			case err := <-disconnected:
				t.Fatalf("the redirected session ended: %v", err)
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
}

// When the redirected connection cannot be made, the session ends with an
// error that says where the redirection was going and why the connection
// stopped there; one that fails after the first session was up ends it the
// same way.
func TestFailedRedirectionEndsTheSessionWithItsCause(t *testing.T) {
	for name, stage := range map[string]rigStage{"before the Demand Active": rigAfterLicensing, "active": rigWhenActive} {
		t.Run(name, func(t *testing.T) {
			cert := newRigCertificate(t)
			handover := rigRedirection{token: rigToken, username: rigHandoverUser, password: rigHandoverPassword, certDER: cert.Certificate[0]}
			server := newRigServer(t, cert, []rigScript{
				{logon: [3]string{"", rigUser, rigSecret}, redirect: &handover, stage: stage},
				{refuse: true},
			}, false)
			client, err := NewClient(rigOptions(t, server, cert))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			disconnected := rigDisconnected(client)

			err = client.Connect()
			if stage == rigWhenActive {
				// The first session comes up; the redirection then fails.
				if err != nil {
					t.Fatalf("Connect: %v", err)
				}
				select {
				case err = <-disconnected:
				case <-time.After(5 * time.Second):
					t.Fatal("the failed redirection did not end the session")
				}
			} else if err == nil {
				t.Fatal("Connect returned nil")
			}
			for _, want := range []string{"server redirection to the same server", "EOF"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not say %q", err, want)
				}
			}
			if got := server.accepted.Load(); got != 2 {
				t.Errorf("the server saw %d connections, want the session and the refused one", got)
			}
		})
	}
}
