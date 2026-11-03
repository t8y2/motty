package nla

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func credSSPTestCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "server.test"},
		DNSNames: []string{"server.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	encoded, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{encoded}, PrivateKey: key, Leaf: leaf}
}

func runCredSSPHandshake(t *testing.T, raw bool, version int, serve func(*tls.Conn, tls.Certificate) error) error {
	t.Helper()
	certificate := credSSPTestCertificate(t)
	serverPipe, clientPipe := net.Pipe()
	deadline := time.Now().Add(10 * time.Second)
	_ = serverPipe.SetDeadline(deadline)
	_ = clientPipe.SetDeadline(deadline)
	server := tls.Server(serverPipe, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	roots := x509.NewCertPool()
	roots.AddCert(certificate.Leaf)
	client := tls.Client(clientPipe, &tls.Config{RootCAs: roots, ServerName: "server.test", MinVersion: tls.VersionTLS12})
	serverDone := make(chan error, 1)
	go func() {
		defer serverPipe.Close()
		if err := server.Handshake(); err != nil {
			serverDone <- err
			return
		}
		serverDone <- serve(server, certificate)
	}()
	if err := client.Handshake(); err != nil {
		clientPipe.Close()
		t.Fatal(err)
	}
	authenticate := Authenticate
	if raw {
		authenticate = AuthenticateRawNTLM
	}
	err := authenticate(client, slog.New(slog.NewTextHandler(io.Discard, nil)), "server.test", "Domain", "User", "Password", version)
	clientPipe.Close()
	if serverErr := <-serverDone; serverErr != nil {
		t.Fatalf("test server: %v (client: %v)", serverErr, err)
	}
	return err
}

func credSSPTestChallenge() []byte {
	targetInfo := make([]byte, 16)
	binary.LittleEndian.PutUint16(targetInfo[0:2], avTimestamp)
	binary.LittleEndian.PutUint16(targetInfo[2:4], 8)
	binary.LittleEndian.PutUint64(targetInfo[4:12], 132271296000000000)
	message := make([]byte, 56)
	copy(message, ntlmSignature[:])
	binary.LittleEndian.PutUint32(message[8:12], ntlmChallenge)
	flags := ntlmFlagUnicode | ntlmFlagNTLM | ntlmFlagSeal | ntlmFlagSign | ntlmFlagAlwaysSign | ntlmFlagESS |
		ntlmFlagKeyExch | ntlmFlag128 | ntlmFlag56 | ntlmFlagTargetInfo | ntlmFlagVersion
	binary.LittleEndian.PutUint32(message[20:24], flags)
	copy(message[24:32], []byte{1, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef})
	binary.LittleEndian.PutUint16(message[40:42], uint16(len(targetInfo)))
	binary.LittleEndian.PutUint16(message[42:44], uint16(len(targetInfo)))
	binary.LittleEndian.PutUint32(message[44:48], 56)
	message[48], message[55] = 10, 15
	return append(message, targetInfo...)
}

func testNegotiateToken(token []byte, raw bool) ([]byte, []byte, error) {
	if raw {
		if !bytes.HasPrefix(token, ntlmSignature[:]) {
			return nil, nil, errors.New("first token is not raw NTLM")
		}
		return token, nil, nil
	}
	var outer, choice asn1.RawValue
	if _, err := asn1.Unmarshal(token, &outer); err != nil || outer.Class != asn1.ClassApplication || outer.Tag != 0 {
		return nil, nil, errors.New("first token is not a GSS-API initial token")
	}
	var mechanism asn1.ObjectIdentifier
	rest, err := asn1.Unmarshal(outer.Bytes, &mechanism)
	if err != nil || !mechanism.Equal(oidSPNEGO) {
		return nil, nil, errors.New("first token does not select SPNEGO")
	}
	if _, err := asn1.Unmarshal(rest, &choice); err != nil {
		return nil, nil, err
	}
	var initial negTokenInit
	if _, err := asn1.Unmarshal(choice.Bytes, &initial); err != nil {
		return nil, nil, err
	}
	mechanisms, err := asn1.Marshal(initial.MechTypes)
	return initial.MechToken, mechanisms, err
}

func testChallengeToken(challenge []byte, raw bool) []byte {
	if raw {
		return challenge
	}
	encoded, _ := asn1.Marshal(negTokenResp{NegState: spnegoAcceptIncomplete, SupportedMech: oidNTLMSSP, ResponseToken: challenge})
	return asn1WrapExplicit(0xa1, encoded)
}

func testAuthenticateToken(token []byte, raw bool) ([]byte, []byte, error) {
	if raw {
		return token, nil, nil
	}
	var choice asn1.RawValue
	if _, err := asn1.Unmarshal(token, &choice); err != nil {
		return nil, nil, err
	}
	var response struct {
		ResponseToken []byte `asn1:"explicit,tag:2"`
		MechListMIC   []byte `asn1:"explicit,optional,tag:3"`
	}
	_, err := asn1.Unmarshal(choice.Bytes, &response)
	return response.ResponseToken, response.MechListMIC, err
}

// Verify the proof, NTLM MIC and channel binding independently of ntlmClient.
// The response key is the published MS-NLMP User/Domain/Password test vector.
func verifyTestAuthenticate(auth, negotiate, challenge []byte, certificate tls.Certificate) ([16]byte, error) {
	var exported [16]byte
	if len(auth) < 88 || !bytes.Equal(auth[:8], ntlmSignature[:]) || binary.LittleEndian.Uint32(auth[8:12]) != ntlmAuthenticate {
		return exported, errors.New("invalid NTLM Authenticate message")
	}
	field := func(offset int) ([]byte, error) {
		length := int(binary.LittleEndian.Uint16(auth[offset:]))
		start := int(binary.LittleEndian.Uint32(auth[offset+4:]))
		if start > len(auth) || length > len(auth)-start {
			return nil, errors.New("NTLM field is out of bounds")
		}
		return auth[start : start+length], nil
	}
	response, err := field(20)
	if err != nil || len(response) < 44 {
		return exported, errors.New("missing NTLMv2 response")
	}
	key, _ := hex.DecodeString("0c868a403bfd7a93a3001ef22ef02e3f")
	proof := hmac.New(md5.New, key)
	proof.Write(challenge[24:32])
	proof.Write(response[16:])
	if !hmac.Equal(proof.Sum(nil), response[:16]) {
		return exported, errors.New("NTLMv2 proof does not match the test credentials")
	}
	baseKey := hmac.New(md5.New, key)
	baseKey.Write(response[:16])
	encryptedKey, err := field(52)
	if err != nil || len(encryptedKey) != 16 {
		return exported, errors.New("missing encrypted session key")
	}
	cipher, _ := rc4.NewCipher(baseKey.Sum(nil))
	cipher.XORKeyStream(exported[:], encryptedKey)
	unsignedAuth := bytes.Clone(auth)
	clear(unsignedAuth[72:88])
	mic := hmac.New(md5.New, exported[:])
	mic.Write(negotiate)
	mic.Write(challenge)
	mic.Write(unsignedAuth)
	if !hmac.Equal(mic.Sum(nil), auth[72:88]) {
		return exported, errors.New("NTLM Authenticate MIC is invalid")
	}
	cb := computeChannelBindingsHash(certificate.Certificate[0])
	pair := append([]byte{0x0a, 0, 16, 0}, cb[:]...)
	if !bytes.Contains(response[16:], pair) || !bytes.Contains(response[16:], []byte{6, 0, 4, 0, 2, 0, 0, 0}) {
		return exported, errors.New("channel binding or MIC-present flag was lost")
	}
	return exported, nil
}

func TestCredSSPHandshakeTokenFormats(t *testing.T) {
	for _, raw := range []bool{false, true} {
		for _, version := range []int{3, 6} {
			t.Run(fmt.Sprintf("raw=%t/version=%d", raw, version), func(t *testing.T) {
				err := runCredSSPHandshake(t, raw, version, func(conn *tls.Conn, certificate tls.Certificate) error {
					first, err := readTSRequest(conn)
					if err != nil || len(first.NegoTokens) != 1 || len(first.AuthInfo) != 0 {
						return errors.New("missing initial negotiate token or premature credentials")
					}
					negotiate, mechanisms, err := testNegotiateToken(first.NegoTokens[0].Data, raw)
					if err != nil {
						return err
					}
					challenge := credSSPTestChallenge()
					if err := sendTSRequest(conn, &tsRequest{Version: version, NegoTokens: []negoToken{{Data: testChallengeToken(challenge, raw)}}}); err != nil {
						return err
					}
					auth, err := readTSRequest(conn)
					if err != nil || len(auth.NegoTokens) != 1 {
						return errors.New("missing Authenticate token")
					}
					message, mechanismMIC, err := testAuthenticateToken(auth.NegoTokens[0].Data, raw)
					if err != nil {
						return err
					}
					key, err := verifyTestAuthenticate(message, negotiate, challenge, certificate)
					if err != nil {
						return err
					}
					seal := newNTLMSeal(key, slog.New(slog.NewTextHandler(io.Discard, nil)))
					if raw {
						if len(mechanismMIC) != 0 || len(auth.PubKeyAuth) < 16 || binary.LittleEndian.Uint32(auth.PubKeyAuth[12:16]) != 0 {
							return errors.New("raw NTLM consumed a SPNEGO MIC sequence number")
						}
					} else {
						expected := seal.makeSignature(mechanisms)
						if !hmac.Equal(expected[:], mechanismMIC) {
							return errors.New("SPNEGO mechanism-list MIC was lost")
						}
						seal.seqNumServer++
						seal.resetCipherState(key)
					}
					// Turn the client-side sealing helper into the server peer.
					seal.clientSignKey, seal.serverSignKey = seal.serverSignKey, seal.clientSignKey
					seal.clientSeal, seal.serverSeal = seal.serverSeal, seal.clientSeal
					publicKey, err := extractSubjectPublicKey(certificate.Leaf.RawSubjectPublicKeyInfo)
					if err != nil {
						return err
					}
					clientBinding := publicKey
					serverBinding := bytes.Clone(publicKey)
					serverBinding[0]++
					if version >= 5 {
						if len(auth.ClientNonce) != 32 {
							return errors.New("missing CredSSP nonce")
						}
						clientHash := sha256.Sum256(append(append([]byte("CredSSP Client-To-Server Binding Hash\x00"), auth.ClientNonce...), publicKey...))
						serverHash := sha256.Sum256(append(append([]byte("CredSSP Server-To-Client Binding Hash\x00"), auth.ClientNonce...), publicKey...))
						clientBinding, serverBinding = clientHash[:], serverHash[:]
					}
					plain, err := seal.unseal(auth.PubKeyAuth)
					if err != nil || !bytes.Equal(plain, clientBinding) {
						return fmt.Errorf("invalid client public-key binding: %v", err)
					}
					if err := sendTSRequest(conn, &tsRequest{Version: version, PubKeyAuth: seal.seal(serverBinding)}); err != nil {
						return err
					}
					last, err := readTSRequest(conn)
					if err != nil {
						return err
					}
					plain, err = seal.unseal(last.AuthInfo)
					if err != nil {
						return err
					}
					var credentials tsCredentials
					if _, err := asn1.Unmarshal(plain, &credentials); err != nil || credentials.CredType != 1 {
						return errors.New("invalid sealed credentials")
					}
					var password tsPasswordCreds
					if _, err := asn1.Unmarshal(credentials.Credentials, &password); err != nil || !bytes.Equal(password.Password, encodeUTF16LE("Password")) || !bytes.Equal(password.UserName, encodeUTF16LE("User")) || !bytes.Equal(password.DomainName, encodeUTF16LE("Domain")) {
						return errors.New("wrong sealed credential values")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestCredSSPErrorStages(t *testing.T) {
	tests := []struct {
		name      string
		afterAuth bool
		code      int
		want      error
	}{
		{"token rejected", false, 0x80090308, ErrNegotiateToken},
		{"explicit refusal before challenge", false, 0xC000006D, ErrCredentialsFatal},
		{"SSPI refusal before challenge", false, 0x8009030C, ErrCredentialsFatal},
		{"token rejected after Authenticate", true, 0x80090308, ErrCredentialsSent},
		{"password refused", true, 0xC000006D, ErrCredentialsFatal},
		{"SSPI password refused", true, 0x8009030C, ErrCredentialsFatal},
		{"connection closed after Authenticate", true, 0, ErrCredentialsSent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := runCredSSPHandshake(t, true, 6, func(conn *tls.Conn, _ tls.Certificate) error {
				if _, err := readTSRequest(conn); err != nil {
					return err
				}
				if test.afterAuth {
					if err := sendTSRequest(conn, &tsRequest{Version: 6, NegoTokens: []negoToken{{Data: credSSPTestChallenge()}}}); err != nil {
						return err
					}
					if _, err := readTSRequest(conn); err != nil {
						return err
					}
				}
				if test.code == 0 {
					return nil
				}
				return sendTSRequest(conn, &tsRequest{Version: 6, ErrorCode: test.code})
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if test.afterAuth && errors.Is(err, ErrNegotiateToken) {
				t.Fatalf("credentials could be retried after Authenticate: %v", err)
			}
			if test.code == 0x80090308 && errors.Is(err, ErrCredentialsFatal) {
				t.Fatalf("protocol error was classified as rejected credentials: %v", err)
			}
		})
	}
}

func TestRawNTLMRejectsInvalidServerBinding(t *testing.T) {
	err := runCredSSPHandshake(t, true, 6, func(conn *tls.Conn, certificate tls.Certificate) error {
		first, err := readTSRequest(conn)
		if err != nil || len(first.NegoTokens) != 1 {
			return errors.New("missing raw negotiate token")
		}
		challenge := credSSPTestChallenge()
		if err := sendTSRequest(conn, &tsRequest{Version: 6, NegoTokens: []negoToken{{Data: challenge}}}); err != nil {
			return err
		}
		auth, err := readTSRequest(conn)
		if err != nil || len(auth.NegoTokens) != 1 {
			return errors.New("missing raw Authenticate token")
		}
		key, err := verifyTestAuthenticate(auth.NegoTokens[0].Data, first.NegoTokens[0].Data, challenge, certificate)
		if err != nil {
			return err
		}
		seal := newNTLMSeal(key, slog.New(slog.NewTextHandler(io.Discard, nil)))
		seal.clientSignKey, seal.serverSignKey = seal.serverSignKey, seal.clientSignKey
		seal.clientSeal, seal.serverSeal = seal.serverSeal, seal.clientSeal
		// A valid NTLM seal around the wrong binding hash must not be enough.
		if err := sendTSRequest(conn, &tsRequest{Version: 6, PubKeyAuth: seal.seal(make([]byte, sha256.Size))}); err != nil {
			return err
		}
		if _, err := readTSRequest(conn); err == nil {
			return errors.New("client sent credentials after an invalid server binding")
		} else if !errors.Is(err, io.EOF) {
			return err
		}
		return nil
	})
	if !errors.Is(err, ErrCredentialsSent) || !strings.Contains(err.Error(), "server pubKeyAuth verification failed") {
		t.Fatalf("error = %v, want a public-key binding failure after Authenticate", err)
	}
	if errors.Is(err, ErrNegotiateToken) || errors.Is(err, ErrCredentialsFatal) {
		t.Fatalf("binding failure could trigger a retry or password removal: %v", err)
	}
}
