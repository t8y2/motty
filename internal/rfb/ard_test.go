package rfb

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math/big"
	"testing"
	"testing/iotest"
)

// ardPrime is the 1024-bit MODP group a macOS server sends, as captured in
// public descriptions of the Apple Remote Desktop exchange.
func ardPrime(t *testing.T) *big.Int {
	t.Helper()
	prime, ok := new(big.Int).SetString(
		"ffffffffffffffffc90fdaa22168c234c4c6628b80dc1cd129024e088a67cc74"+
			"020bbea63b139b22514a08798e3404ddef9519b3cd3a431b302b0a6df25f14"+
			"374fe1356d6d51c245e485b576625e7ec6f44c42e9a637ed6b0bff5cb6f406"+
			"b7edee386bfb5a899fa5ae9f24117c4b1fe649286651ece65381ffffffffffffffff", 16)
	if !ok {
		t.Fatal("the test prime is not hexadecimal")
	}
	return prime
}

func ardParametersWire(prime, public *big.Int) []byte {
	size := (prime.BitLen() + 7) / 8
	wire := binary.BigEndian.AppendUint16(nil, 2)
	wire = binary.BigEndian.AppendUint16(wire, uint16(size))
	wire = append(wire, prime.FillBytes(make([]byte, size))...)
	return append(wire, public.FillBytes(make([]byte, size))...)
}

type ardAccount struct{ username, password string }

// ardChallenge answers one ARD challenge for a client that speaks RFB 3.8 and
// then completes the rest of the handshake. It returns the account and password
// it decrypted, so the test can compare them with what the client was given.
func (s *server) ardChallenge(t *testing.T, prime *big.Int, result uint32, reason string) ardAccount {
	s.write([]byte("RFB 003.889\n"))
	if banner := s.read(12); string(banner) != "RFB 003.008\n" {
		t.Errorf("client version = %q", banner)
		return ardAccount{}
	}
	s.write([]byte{1, 30})
	if chosen := s.read(1)[0]; chosen != 30 {
		t.Errorf("client chose security %d, want ARD", chosen)
		return ardAccount{}
	}
	private, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Error(err)
		return ardAccount{}
	}
	serverPublic := new(big.Int).Exp(big.NewInt(2), private, prime)
	size := (prime.BitLen() + 7) / 8
	wire := ardParametersWire(prime, serverPublic)
	s.write(wire)
	ciphertext := s.read(ardCredentials)
	clientPublic := new(big.Int).SetBytes(s.read(size))
	shared := new(big.Int).Exp(clientPublic, private, prime)
	key := md5.Sum(shared.FillBytes(make([]byte, size)))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Error(err)
		return ardAccount{}
	}
	plain := make([]byte, ardCredentials)
	for offset := 0; offset < ardCredentials; offset += aes.BlockSize {
		block.Decrypt(plain[offset:offset+aes.BlockSize], ciphertext[offset:offset+aes.BlockSize])
	}
	account := ardAccount{}
	user := plain[:ardFieldBytes]
	if end := bytes.IndexByte(user, 0); end >= 0 {
		account.username = string(user[:end])
	}
	pass := plain[ardFieldBytes:]
	if end := bytes.IndexByte(pass, 0); end >= 0 {
		account.password = string(pass[:end])
	}
	s.write(u32(int(result)))
	if result != 0 {
		if reason != "" {
			s.write(u32(len(reason)), []byte(reason))
		}
		return account
	}
	s.read(1) // ClientInit
	s.write(serverInit(640, 480, "mac desktop"))
	s.read(20)
	header := s.read(4)
	s.read(4 * int(binary.BigEndian.Uint16(header[2:])))
	return account
}

func TestARDAuthenticatesWithAccountCredentials(t *testing.T) {
	prime := ardPrime(t)
	accounts := make(chan ardAccount, 1)
	client, err := connect(t, Config{Username: "alice", Password: "s3cret"}, func(s *server) {
		accounts <- s.ardChallenge(t, prime, 0, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	account := <-accounts
	if account.username != "alice" || account.password != "s3cret" {
		t.Fatalf("server read account %q password %q", account.username, account.password)
	}
	if client.Security != 30 || client.Encrypted {
		t.Fatalf("security=%d encrypted=%v, want ARD with an unencrypted session", client.Security, client.Encrypted)
	}
	if width, height := client.Size(); width != 640 || height != 480 || client.Name != "mac desktop" {
		t.Fatalf("desktop = %dx%d %q", width, height, client.Name)
	}
}

func TestARDAsksForAccountAndPassword(t *testing.T) {
	prime := ardPrime(t)
	asked := 0
	accounts := make(chan ardAccount, 1)
	client, err := connect(t, Config{
		AskCredentials: func() (string, string, error) {
			asked++
			return "bob", "hunter2", nil
		},
	}, func(s *server) {
		accounts <- s.ardChallenge(t, prime, 0, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if asked != 1 || client.Security != 30 {
		t.Fatalf("asked=%d security=%d", asked, client.Security)
	}
	account := <-accounts
	if account.username != "bob" || account.password != "hunter2" {
		t.Fatalf("server read account %q password %q", account.username, account.password)
	}
}

func TestARDRefusalIsAnAuthenticationFailure(t *testing.T) {
	prime := ardPrime(t)
	_, err := connect(t, Config{Username: "alice", Password: "wrong"}, func(s *server) {
		s.ardChallenge(t, prime, 1, "authentication failed")
	})
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
}

func TestARDWithoutCredentialsAsksForThem(t *testing.T) {
	prime := ardPrime(t)
	_, err := connect(t, Config{}, func(s *server) {
		s.write([]byte("RFB 003.008\n"))
		s.read(12)
		s.write([]byte{1, 30})
		s.read(1)
		s.write(ardParametersWire(prime, new(big.Int).Exp(big.NewInt(2), big.NewInt(5), prime)))
	})
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("err = %v, want ErrAuthRequired", err)
	}
}

func TestARDRejectsCredentialsThatWouldNotFit(t *testing.T) {
	prime := ardPrime(t)
	_, err := connect(t, Config{Username: string(bytes.Repeat([]byte("u"), ardFieldBytes)), Password: "pw"}, func(s *server) {
		s.write([]byte("RFB 003.008\n"))
		s.read(12)
		s.write([]byte{1, 30})
		s.read(1)
		s.write(ardParametersWire(prime, new(big.Int).Exp(big.NewInt(2), big.NewInt(5), prime)))
	})
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v, want ErrProtocol", err)
	}
}

func TestARDParameterLimitsAndValidation(t *testing.T) {
	prime := ardPrime(t)
	valid := ardParametersWire(prime, new(big.Int).Exp(big.NewInt(2), big.NewInt(5), prime))
	even := new(big.Int).Set(prime)
	even.SetBit(even, 0, 0)
	for _, test := range []struct {
		name  string
		wire  []byte
		valid bool
	}{
		{"valid parameters", valid, true},
		{"empty", nil, false},
		{"truncated", valid[:len(valid)-1], false},
		{"zero key length", binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(nil, 2), 0), false},
		{"small key length", binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(nil, 2), ardMinKeyBytes-1), false},
		{"oversized key length", binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(nil, 2), ardMaxKeyBytes+1), false},
		{"generator one", append(binary.BigEndian.AppendUint16(nil, 1), valid[2:]...), false},
		{"even modulus", ardParametersWire(even, new(big.Int).Exp(big.NewInt(2), big.NewInt(5), even)), false},
		{"server key one", ardParametersWire(prime, big.NewInt(1)), false},
		{"server key at the modulus", ardParametersWire(prime, prime), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			// One byte at a time: the reader must not assume the whole message
			// arrives in one call.
			parameters, err := readARDParameters(iotest.OneByteReader(bytes.NewReader(test.wire)))
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
				if parameters.keyBytes != (prime.BitLen()+7)/8 {
					t.Fatalf("key bytes = %d", parameters.keyBytes)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted invalid parameters")
			}
		})
	}
}

func TestARDCredentialsNeedBothFields(t *testing.T) {
	for _, config := range []Config{
		{Username: "alice"},
		{Password: "s3cret"},
		{Username: "alice", AskPassword: func() (string, error) { return "", nil }},
		{AskCredentials: func() (string, string, error) { return "alice", "", nil }},
	} {
		client := &Client{conn: discardConn{}}
		if _, _, err := client.ardCredentials(config); !errors.Is(err, ErrAuthRequired) {
			t.Fatalf("config %+v: err = %v, want ErrAuthRequired", config, err)
		}
	}
}

func FuzzARDParameters(f *testing.F) {
	prime := new(big.Int).Lsh(big.NewInt(1), 1023)
	prime.Add(prime, big.NewInt(3))
	f.Add(ardParametersWire(prime, big.NewInt(5)))
	f.Add([]byte{})
	f.Add(binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(nil, 2), 65535))
	f.Fuzz(func(t *testing.T, input []byte) {
		parameters, err := readARDParameters(bytes.NewReader(input))
		if err != nil {
			return
		}
		if parameters.keyBytes < ardMinKeyBytes || parameters.keyBytes > ardMaxKeyBytes {
			t.Fatal("accepted a key length outside the bounds")
		}
		if parameters.generator < 2 || parameters.prime.Bit(0) == 0 ||
			parameters.public.Cmp(big.NewInt(1)) <= 0 ||
			parameters.public.Cmp(parameters.prime) >= 0 {
			t.Fatal("accepted invalid Diffie-Hellman parameters")
		}
		if parameters.prime.BitLen() > 8*ardMaxKeyBytes || parameters.public.BitLen() > 8*ardMaxKeyBytes {
			t.Fatal("accepted a value larger than the bounded message")
		}
	})
}
