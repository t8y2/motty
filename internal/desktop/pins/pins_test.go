package pins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPinsPersistTypedEndpointsAndForgetOnlyOneServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "rdp_certificates")
	store := New(path)
	first, second := Fingerprint([]byte("one")), Fingerprint([]byte("two"))
	endpoints := []Endpoint{{RDP, "host:3389"}, {VNC, "host:3389"}, {VNC, "host:5900"}, {VNC, "[::1]:5900"}}
	for _, endpoint := range endpoints {
		if got, err := store.Lookup(endpoint); got != "" || err != nil {
			t.Fatalf("empty store: %q, %v", got, err)
		}
		if err := store.Remember(endpoint, first, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Remember(endpoints[0], second, first); err != nil {
		t.Fatal(err)
	}
	if err := store.Forget(endpoints[1]); err != nil {
		t.Fatal(err)
	}
	for index, endpoint := range endpoints {
		want := first
		if index == 0 {
			want = second
		} else if index == 1 {
			want = ""
		}
		if got, err := New(path).Lookup(endpoint); err != nil || got != want {
			t.Fatalf("reopened %s: %q, %v; want %q", endpoint, got, err, want)
		}
	}
	if err := store.Forget(endpoints[1]); err != nil {
		t.Fatalf("forget absent endpoint: %v", err)
	}
	if err := store.Remember(Endpoint{VNC, "bad host:5900"}, first, ""); err == nil {
		t.Fatal("accepted an invalid endpoint")
	}
}

func TestLegacyRDPPinsRemainRDPIncludingProtocolNamedHosts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rdp_certificates")
	fingerprint := Fingerprint([]byte("legacy certificate"))
	addresses := []string{"host:3389", "rdp:3389", "vnc:3389", "[::1]:3389"}
	var data string
	for _, address := range addresses {
		data += fmt.Sprintf("%s %s\n", address, fingerprint)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(path)
	for _, address := range addresses {
		if got, err := store.Lookup(Endpoint{RDP, address}); err != nil || got != fingerprint {
			t.Fatalf("legacy RDP %s: %q, %v", address, got, err)
		}
		if got, err := store.Lookup(Endpoint{VNC, address}); err != nil || got != "" {
			t.Fatalf("legacy RDP reused by VNC %s: %q, %v", address, got, err)
		}
	}
	if err := store.Remember(Endpoint{VNC, "host:5900"}, fingerprint, ""); err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		if got, err := New(path).Lookup(Endpoint{RDP, address}); err != nil || got != fingerprint {
			t.Fatalf("rewriting lost legacy RDP %s: %q, %v", address, got, err)
		}
	}
}

func TestConcurrentStoresPreserveEndpointsAndRejectUnseenChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins")
	fingerprint := Fingerprint([]byte("server identity"))
	var wg sync.WaitGroup
	for index := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			endpoint := Endpoint{VNC, fmt.Sprintf("host-%d:5900", index)}
			if err := New(path).Remember(endpoint, fingerprint, ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for index := range 20 {
		endpoint := Endpoint{VNC, fmt.Sprintf("host-%d:5900", index)}
		if got, err := New(path).Lookup(endpoint); err != nil || got != fingerprint {
			t.Fatalf("concurrent write lost %s: %q, %v", endpoint, got, err)
		}
	}
	endpoint := Endpoint{VNC, "host-0:5900"}
	if err := New(path).Remember(endpoint, Fingerprint([]byte("another key")), ""); !errors.Is(err, ErrChanged) {
		t.Fatalf("a pending prompt replaced unseen trust: %v", err)
	}
	if got, err := New(path).Lookup(endpoint); err != nil || got != fingerprint {
		t.Fatalf("failed save changed pin: %q, %v", got, err)
	}
}

func TestPinWriteAndForgetFailuresPreserveSavedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins")
	store := New(path)
	endpoint := Endpoint{VNC, "host:5900"}
	fingerprint := Fingerprint([]byte("identity"))
	if err := store.Remember(endpoint, fingerprint, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.Forget(endpoint); err == nil {
		t.Fatal("forget succeeded despite unwritable temporary file")
	}
	if err := store.Remember(endpoint, Fingerprint([]byte("new identity")), fingerprint); err == nil {
		t.Fatal("save succeeded despite unwritable temporary file")
	}
	if got, err := store.Lookup(endpoint); err != nil || got != fingerprint {
		t.Fatalf("failed write discarded trusted identity: %q, %v", got, err)
	}
}
