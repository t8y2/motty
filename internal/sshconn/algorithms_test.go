package sshconn

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestCompatibleKeepsTheDefaultsFirstAndAddsTheWeakOnesAfter(t *testing.T) {
	compatible := CompatibleAlgorithms()
	defaults := ssh.SupportedAlgorithms()
	if compatible.Ciphers[0] != defaults.Ciphers[0] || compatible.KeyExchanges[0] != defaults.KeyExchanges[0] {
		t.Fatalf("the preferred algorithm changed: %q / %q", compatible.Ciphers[0], compatible.KeyExchanges[0])
	}
	for _, want := range []string{ssh.InsecureCipherTripleDESCBC, ssh.InsecureKeyExchangeDH1SHA1, ssh.InsecureHMACSHA196, ssh.KeyAlgoRSA} {
		found := slices.Contains(compatible.Ciphers, want) || slices.Contains(compatible.KeyExchanges, want) ||
			slices.Contains(compatible.MACs, want) || slices.Contains(compatible.HostKeys, want)
		if !found {
			t.Errorf("compatible mode lacks %s", want)
		}
	}
	if weak := compatible.Weak(); len(weak) < 4 {
		t.Fatalf("weak = %v", weak)
	}
	if (Algorithms{}).Weak() != nil || !(Algorithms{}).IsZero() {
		t.Fatal("the defaults offer nothing weak")
	}
}

func TestParseSortsNamesIntoTheirKindsInOrder(t *testing.T) {
	parsed, err := ParseAlgorithms("aes256-ctr, hmac-sha2-256  curve25519-sha256,aes128-ctr")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(parsed.Ciphers, []string{"aes256-ctr", "aes128-ctr"}) ||
		!slices.Equal(parsed.MACs, []string{"hmac-sha2-256"}) ||
		!slices.Equal(parsed.KeyExchanges, []string{"curve25519-sha256"}) || parsed.HostKeys != nil {
		t.Fatalf("parsed %+v", parsed)
	}
	again, _ := ParseAlgorithms("aes256-ctr,aes256-ctr")
	if len(again.Ciphers) != 1 {
		t.Fatalf("a repeated name was kept twice: %v", again.Ciphers)
	}
}

func TestParseRefusesWhatItDoesNotKnow(t *testing.T) {
	_, err := ParseAlgorithms("aes256-ctr, aes-999-xyz")
	var unknown *UnknownAlgorithmError
	if !errors.As(err, &unknown) || unknown.Name != "aes-999-xyz" {
		t.Fatalf("err = %v", err)
	}
	if empty, err := ParseAlgorithms("  "); err != nil || !empty.IsZero() {
		t.Fatalf("blank list: %+v, %v", empty, err)
	}
}

func TestHostKeyAlgorithmsKeepTheKnownKeyInCharge(t *testing.T) {
	known := algorithmsFor(ssh.KeyAlgoRSA)
	if got := hostKeyAlgorithms(nil, nil); got != nil {
		t.Fatalf("new host, defaults: %v", got)
	}
	if got := hostKeyAlgorithms(known, nil); !slices.Equal(got, known) {
		t.Fatalf("known host, defaults: %v", got)
	}
	wanted := []string{ssh.KeyAlgoED25519, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	if got := hostKeyAlgorithms(nil, wanted); !slices.Equal(got, wanted) {
		t.Fatalf("new host, wanted: %v", got)
	}
	// The trusted RSA key is checked, and an old server that signs with SHA-1 is
	// still allowed because ssh-rsa was asked for.
	if got := hostKeyAlgorithms(known, wanted); !slices.Equal(got, []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}) {
		t.Fatalf("known and wanted: %v", got)
	}
	if got := hostKeyAlgorithms(known, []string{ssh.KeyAlgoED25519}); !slices.Equal(got, known) {
		t.Fatalf("no overlap falls back to the trusted key: %v", got)
	}
	// A list that leaves ssh-rsa out keeps it out, whatever is on file.
	if got := hostKeyAlgorithms(known, []string{ssh.KeyAlgoRSASHA256}); slices.Contains(got, ssh.KeyAlgoRSA) {
		t.Fatalf("ssh-rsa offered although the list left it out: %v", got)
	}
}

// A router that can only sign its RSA key as ssh-rsa connects the first time
// because the library offers ssh-rsa last; once the key is saved, the second
// connection must still offer it or the handshake finds nothing in common.
func TestAKnownRSAKeyStillAllowsAServerThatOnlySignsWithSHA1(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := newHostKeys(filepath.Join(t.TempDir(), "known_hosts"))
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.remember("192.168.0.1:22", public); err != nil {
		t.Fatal(err)
	}
	offer := hostKeyAlgorithms(keys.algorithms("192.168.0.1:22"), nil)
	if !slices.Contains(offer, ssh.KeyAlgoRSA) {
		t.Fatalf("a trusted RSA key leaves out ssh-rsa: %v", offer)
	}
	if offer[0] != ssh.KeyAlgoRSASHA512 {
		t.Fatalf("the SHA-2 variants should stay preferred: %v", offer)
	}
}

// An old device offers only a weak cipher: the defaults cannot reach it, a
// compatible connection can, and says what it agreed on.
func TestACompatibleConnectionReachesAServerThatOnlyOffersAWeakCipher(t *testing.T) {
	legacy := &ssh.Config{Ciphers: []string{ssh.InsecureCipherAES128CBC}}
	server := startServer(t, func(s *testServer) { s.password = "pw"; s.algorithms = legacy })

	refused := dial(t, server, nil)
	failed := refused.until("the default connection to fail", func(s Status) bool { return s.Phase == PhaseFailed || s.Phase == PhaseHostKeyPrompt })
	if failed.Phase != PhaseFailed {
		t.Fatalf("the defaults negotiated with a cipher they exclude: %+v", failed)
	}

	host, port := server.host()
	allowed := dial(t, server, func(c *Config) {
		c.Algorithms = map[string]Algorithms{EndpointKey(host, port): CompatibleAlgorithms()}
	})
	allowed.phase(PhaseHostKeyPrompt)
	allowed.conn.TrustHostKey(true)
	allowed.phase(PhasePasswordPrompt)
	allowed.conn.SubmitPassword("pw")
	connected := allowed.phase(PhaseConnected)
	if connected.Negotiated.Cipher != ssh.InsecureCipherAES128CBC {
		t.Fatalf("negotiated %+v", connected.Negotiated)
	}
	if weak := connected.Negotiated.Weak(); !slices.Contains(weak, ssh.InsecureCipherAES128CBC) {
		t.Fatalf("weak = %v", weak)
	}
	if !strings.Contains(connected.Negotiated.String(), ssh.InsecureCipherAES128CBC) {
		t.Fatalf("description %q", connected.Negotiated)
	}
}

func TestACustomListLimitsWhatIsOffered(t *testing.T) {
	server := startServer(t, func(s *testServer) {
		s.password = "pw"
		s.algorithms = &ssh.Config{Ciphers: []string{"aes256-ctr", "aes128-ctr"}}
	})
	host, port := server.host()
	custom, err := ParseAlgorithms("aes128-ctr")
	if err != nil {
		t.Fatal(err)
	}
	h := dial(t, server, func(c *Config) { c.Algorithms = map[string]Algorithms{EndpointKey(host, port): custom} })
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(true)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("pw")
	connected := h.phase(PhaseConnected)
	if connected.Negotiated.Cipher != "aes128-ctr" || len(connected.Negotiated.Weak()) != 0 {
		t.Fatalf("negotiated %+v", connected.Negotiated)
	}
}
