package rfb

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// VeNCrypt 0.2 subtypes (security type 19). Only X.509 TLS subtypes are
// supported here; bare Plain (256) and anonymous DH/ECDH TLS subtypes
// (257..259) are refused before any credential callback runs.
//
// Protocol references: TigerVNC common/rfb/CSecurityVeNCrypt.cxx,
// SSecurityVeNCrypt.cxx, CSecurityTLS.cxx, CSecurityPlain.cxx, and
// NeatVNC src/auth/vencrypt.c.
const (
	vencryptPlain     uint32 = 256
	vencryptTLSNone   uint32 = 257
	vencryptTLSVnc    uint32 = 258
	vencryptTLSPlain  uint32 = 259
	vencryptX509None  uint32 = 260
	vencryptX509Vnc   uint32 = 261
	vencryptX509Plain uint32 = 262
)

func chooseVeNCryptSubtype(offered []uint32, config Config) uint32 {
	has := func(want uint32) bool {
		for _, value := range offered {
			if value == want {
				return true
			}
		}
		return false
	}
	// An available account callback does not mean the user chose account auth:
	// the desktop supplies both callbacks even for password-only VNC hosts.
	haveUser := config.Username != ""
	havePassword := config.Password != "" || config.AskPassword != nil
	switch {
	case haveUser && has(vencryptX509Plain):
		return vencryptX509Plain
	case havePassword && has(vencryptX509Vnc):
		return vencryptX509Vnc
	case config.AskCredentials != nil && has(vencryptX509Plain):
		return vencryptX509Plain
	case has(vencryptX509None):
		return vencryptX509None
	case has(vencryptX509Vnc):
		return vencryptX509Vnc
	case has(vencryptX509Plain):
		return vencryptX509Plain
	}
	return 0
}

func normalizeServerName(raw string) string {
	raw = strings.TrimSpace(raw)
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = raw[1 : len(raw)-1]
	}
	return raw
}

func (client *Client) vencryptTLSConfig(config Config) (*tls.Config, error) {
	var tlsConfig *tls.Config
	if config.TLSConfig != nil {
		tlsConfig = config.TLSConfig.Clone()
	} else {
		tlsConfig = &tls.Config{}
	}
	if tlsConfig.MinVersion < tls.VersionTLS12 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}
	if tlsConfig.MaxVersion != 0 && tlsConfig.MaxVersion < tls.VersionTLS12 {
		return nil, fmt.Errorf("%w: TLS maximum version %#x is below 1.2", ErrProtocol, tlsConfig.MaxVersion)
	}
	serverName := normalizeServerName(config.ServerName)
	if serverName == "" {
		serverName = normalizeServerName(tlsConfig.ServerName)
	}
	if serverName == "" && client.conn != nil && client.conn.RemoteAddr() != nil {
		serverName = normalizeServerName(client.conn.RemoteAddr().String())
	}
	tlsConfig.ServerName = serverName
	// Caller InsecureSkipVerify never unconditionally accepts a certificate:
	// without VerifyCertificate, standard chain and hostname verification is
	// enforced; with VerifyCertificate, the explicit callback decides below.
	tlsConfig.InsecureSkipVerify = config.VerifyCertificate != nil

	callerVerifyConnection := tlsConfig.VerifyConnection
	tlsConfig.VerifyConnection = func(cs tls.ConnectionState) error {
		if cs.Version < tls.VersionTLS12 {
			return fmt.Errorf("%w: TLS version %#x is below 1.2", ErrProtocol, cs.Version)
		}
		if len(cs.PeerCertificates) == 0 || len(cs.PeerCertificates[0].Raw) == 0 {
			return fmt.Errorf("%w: TLS server presented no certificate", ErrProtocol)
		}
		if callerVerifyConnection != nil {
			if err := callerVerifyConnection(cs); err != nil {
				return err
			}
		}
		if config.VerifyCertificate != nil {
			der := bytes.Clone(cs.PeerCertificates[0].Raw)
			if err := client.userWait(func() error {
				return config.VerifyCertificate(der)
			}); err != nil {
				_ = client.conn.SetDeadline(time.Now().Add(client.timeout))
				return err
			}
		}
		return nil
	}
	return tlsConfig, nil
}

func (client *Client) vencryptPlainCredentials(config Config) ([]byte, error) {
	packed, err := client.ra2Credentials(config, ra2UserPass)
	if err != nil {
		return nil, err
	}
	defer clear(packed)
	if len(packed) < 2 {
		return nil, fmt.Errorf("%w: invalid VeNCrypt credentials", ErrProtocol)
	}
	userLen := int(packed[0])
	if 1+userLen >= len(packed) {
		return nil, fmt.Errorf("%w: invalid VeNCrypt credentials", ErrProtocol)
	}
	user := packed[1 : 1+userLen]
	passLen := int(packed[1+userLen])
	if 2+userLen+passLen != len(packed) {
		return nil, fmt.Errorf("%w: invalid VeNCrypt credentials", ErrProtocol)
	}
	pass := packed[2+userLen:]
	out := make([]byte, 8+userLen+passLen)
	binary.BigEndian.PutUint32(out[0:4], uint32(userLen))
	binary.BigEndian.PutUint32(out[4:8], uint32(passLen))
	copy(out[8:8+userLen], user)
	copy(out[8+userLen:], pass)
	return out, nil
}

func (client *Client) authenticateVeNCrypt(config Config) error {
	if client.Security != 19 {
		return fmt.Errorf("%w: VeNCrypt security type %d", ErrUnsupported, client.Security)
	}
	var serverVersion [2]byte
	if _, err := io.ReadFull(client.r, serverVersion[:]); err != nil {
		return err
	}
	// The server advertises its highest version; answer with our supported
	// 0.2 and require its acknowledgement, as TigerVNC does.
	if binary.BigEndian.Uint16(serverVersion[:]) < 2 {
		return fmt.Errorf("%w: %w: VeNCrypt version %d.%d", ErrUnsupported, ErrProtocol, serverVersion[0], serverVersion[1])
	}
	if _, err := client.conn.Write([]byte{0, 2}); err != nil {
		return err
	}
	status, err := client.r.ReadByte()
	if err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("%w: server refused VeNCrypt 0.2 (status %d)", ErrProtocol, status)
	}
	count, err := client.r.ReadByte()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: %w: server offered no VeNCrypt subtypes", &UnsupportedSecurityError{VeNCrypt: true}, ErrProtocol)
	}
	rawSubtypes := make([]byte, 4*int(count))
	if _, err := io.ReadFull(client.r, rawSubtypes); err != nil {
		return err
	}
	subtypes := make([]uint32, int(count))
	for i := range subtypes {
		subtypes[i] = binary.BigEndian.Uint32(rawSubtypes[4*i:])
	}
	client.SecuritySubtypes = formatSecurityTypes(subtypes, true)
	selected := chooseVeNCryptSubtype(subtypes, config)
	if selected == 0 {
		return &UnsupportedSecurityError{Types: subtypes, VeNCrypt: true}
	}
	client.SecuritySubtype = int(selected)
	var selectedWire [4]byte
	binary.BigEndian.PutUint32(selectedWire[:], selected)
	if _, err := client.conn.Write(selectedWire[:]); err != nil {
		return err
	}
	accepted, err := client.r.ReadByte()
	if err != nil {
		return err
	}
	if accepted != 1 {
		return fmt.Errorf("%w: server refused VeNCrypt subtype %d (status %d)", ErrProtocol, selected, accepted)
	}

	tlsConfig, err := client.vencryptTLSConfig(config)
	if err != nil {
		return err
	}
	tlsConn := tls.Client(&readerConn{Conn: client.conn, reader: client.r}, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		return err
	}
	tlsState := tlsConn.ConnectionState()
	client.Cipher = tls.CipherSuiteName(tlsState.CipherSuite)
	client.TLSVersion = tls.VersionName(tlsState.Version)
	client.conn = tlsConn
	client.r = bufio.NewReaderSize(tlsConn, 64<<10)

	switch selected {
	case vencryptX509None:
		client.Authentication = "none"
	case vencryptX509Vnc:
		client.Authentication = "vnc-password"
		if config.Password == "" && config.AskPassword == nil {
			return ErrAuthRequired
		}
		var challenge [16]byte
		if _, err := io.ReadFull(client.r, challenge[:]); err != nil {
			return err
		}
		password := config.Password
		if password == "" {
			if err := client.userWait(func() (err error) {
				password, err = config.AskPassword()
				return err
			}); err != nil {
				return err
			}
			if password == "" {
				return ErrAuthRequired
			}
		}
		response := vncResponse(challenge, password)
		if _, err := client.conn.Write(response[:]); err != nil {
			return err
		}
	case vencryptX509Plain:
		credentials, err := client.vencryptPlainCredentials(config)
		if err != nil {
			return err
		}
		defer clear(credentials)
		if _, err := client.conn.Write(credentials); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: VeNCrypt subtype %d", ErrUnsupported, selected)
	}
	if err := client.securityResult(); err != nil {
		return err
	}
	client.Encrypted = true
	return nil
}
