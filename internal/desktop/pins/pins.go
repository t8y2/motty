// Package pins remembers remote desktop server identities by typed endpoint.
package pins

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Protocol string

const (
	RDP Protocol = "rdp"
	VNC Protocol = "vnc"
)

// Endpoint uses the address and port the user typed, including through a tunnel.
type Endpoint struct {
	Protocol Protocol
	Address  string
}

func (endpoint Endpoint) valid() bool {
	_, port, err := net.SplitHostPort(endpoint.Address)
	return err == nil && port != "" && (endpoint.Protocol == RDP || endpoint.Protocol == VNC) && !strings.ContainsAny(endpoint.Address, " \t\r\n")
}

func (endpoint Endpoint) String() string { return string(endpoint.Protocol) + ":" + endpoint.Address }

// Store writes one "protocol:host:port SHA256:base64" entry per server.
// Stores for the same path share a mutex, including connections in other windows.
type Store struct {
	path string
	mu   *sync.Mutex
}

var locks sync.Map

func New(path string) *Store {
	path = filepath.Clean(path)
	lock, _ := locks.LoadOrStore(path, new(sync.Mutex))
	return &Store{path: path, mu: lock.(*sync.Mutex)}
}

func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

var ErrChanged = errors.New("server identity changed while awaiting approval")

func (store *Store) Lookup(endpoint Endpoint) (string, error) {
	if !endpoint.valid() {
		return "", fmt.Errorf("pins: invalid endpoint %q", endpoint)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entries, err := store.read()
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.endpoint == endpoint {
			return entry.fingerprint, nil
		}
	}
	return "", nil
}

// Remember saves a fingerprint only while the pin still matches the one shown
// in the trust prompt. A concurrently saved different identity is never replaced.
func (store *Store) Remember(endpoint Endpoint, fingerprint, previous string) error {
	if !endpoint.valid() || !strings.HasPrefix(fingerprint, "SHA256:") || strings.ContainsAny(fingerprint, " \t\r\n") {
		return fmt.Errorf("pins: cannot pin %q for %q", fingerprint, endpoint)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entries, err := store.read()
	if err != nil {
		return err
	}
	found := false
	for index := range entries {
		if entries[index].endpoint != endpoint {
			continue
		}
		if entries[index].fingerprint != previous && entries[index].fingerprint != fingerprint {
			return ErrChanged
		}
		entries[index].fingerprint, found = fingerprint, true
	}
	if !found {
		if previous != "" {
			return ErrChanged
		}
		entries = append(entries, pin{endpoint, fingerprint})
	}
	return store.write(entries)
}

// Forget removes this server's identity without touching another endpoint.
func (store *Store) Forget(endpoint Endpoint) error {
	if !endpoint.valid() {
		return fmt.Errorf("pins: invalid endpoint %q", endpoint)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entries, err := store.read()
	if err != nil {
		return err
	}
	kept := entries[:0]
	for _, entry := range entries {
		if entry.endpoint != endpoint {
			kept = append(kept, entry)
		}
	}
	if len(kept) == len(entries) {
		return nil
	}
	return store.write(kept)
}

func (store *Store) write(entries []pin) error {
	var out strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&out, "%s %s\n", entry.endpoint, entry.fingerprint)
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return err
	}
	temporary := store.path + ".tmp"
	if err := os.WriteFile(temporary, []byte(out.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, store.path)
}

type pin struct {
	endpoint    Endpoint
	fingerprint string
}

// A missing file is empty. Legacy untyped RDP addresses are read as RDP pins.
func (store *Store) read() ([]pin, error) {
	file, err := os.Open(store.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var entries []pin
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 4096), 64<<10)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || !strings.HasPrefix(fields[1], "SHA256:") {
			continue
		}
		endpoint := Endpoint{Protocol: RDP, Address: fields[0]}
		if address, ok := strings.CutPrefix(fields[0], "vnc:"); ok && (Endpoint{Protocol: VNC, Address: address}).valid() {
			endpoint = Endpoint{Protocol: VNC, Address: address}
		} else if address, ok := strings.CutPrefix(fields[0], "rdp:"); ok && (Endpoint{Protocol: RDP, Address: address}).valid() {
			endpoint.Address = address
		}
		if endpoint.valid() {
			entries = append(entries, pin{endpoint, fields[1]})
		}
	}
	return entries, scanner.Err()
}
