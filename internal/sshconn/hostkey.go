package sshconn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// knownHostsWriteMu serializes this process's writes to known-hosts files. A
// replacement reads, rewrites and renames the file, so an append that ran
// between its read and its rename would otherwise be lost.
var knownHostsWriteMu sync.Mutex

// hostKeys is the app-owned known-hosts file, in OpenSSH format so the user
// can read or edit it.
type hostKeys struct {
	path     string
	callback ssh.HostKeyCallback
}

func newHostKeys(path string) (*hostKeys, error) {
	if path == "" {
		return nil, errors.New("sshconn: no known-hosts file configured")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("sshconn: known-hosts directory: %w", err)
	}
	// knownhosts.New needs the file to exist.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("sshconn: known-hosts file: %w", err)
	}
	file.Close()
	callback, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("sshconn: read known hosts: %w", err)
	}
	return &hostKeys{path: path, callback: callback}, nil
}

// remember appends key as trusted for hostname.
func (keys *hostKeys) remember(hostname string, key ssh.PublicKey) error {
	knownHostsWriteMu.Lock()
	defer knownHostsWriteMu.Unlock()
	return keys.rememberLocked(hostname, key)
}

// rememberLocked appends key; the caller holds knownHostsWriteMu.
func (keys *hostKeys) rememberLocked(hostname string, key ssh.PublicKey) error {
	file, err := os.OpenFile(keys.path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	if _, err := fmt.Fprintln(file, line); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// rememberReplacing removes the plain records for hostname that hold a key of
// key's type, then appends key. Records for other hosts, other key types and
// the @cert-authority and @revoked markers stay. The file is replaced
// atomically, so a reader never sees half a rewrite.
func (keys *hostKeys) rememberReplacing(hostname string, key ssh.PublicKey) error {
	knownHostsWriteMu.Lock()
	defer knownHostsWriteMu.Unlock()

	data, err := os.ReadFile(keys.path)
	if err != nil {
		return err
	}
	host := knownhosts.Normalize(hostname)
	kept := make([]byte, 0, len(data)+128)
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if line == "" {
			continue
		}
		body := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if plainRecordFor(body, host, key.Type()) {
			continue
		}
		kept = append(kept, line...)
	}
	if len(kept) > 0 && kept[len(kept)-1] != '\n' {
		kept = append(kept, '\n')
	}
	kept = append(kept, knownhosts.Line([]string{host}, key)...)
	kept = append(kept, '\n')
	return writeFileAtomic(keys.path, kept)
}

// plainRecordFor is whether line is a trusted record, with no marker, for host
// and a key of keyType. A hashed host pattern is matched the way OpenSSH
// matches it, so an imported hashed record is replaced instead of duplicated.
func plainRecordFor(line, host, keyType string) bool {
	marker, hosts, known, _, _, err := ssh.ParseKnownHosts([]byte(line))
	if err != nil || marker != "" || known.Type() != keyType {
		return false
	}
	for _, pattern := range hosts {
		if pattern == host || hashedHostMatches(pattern, host) {
			return true
		}
	}
	return false
}

// hashedHostMatches is whether pattern is |1|salt|hash of host.
func hashedHostMatches(pattern, host string) bool {
	parts := strings.Split(pattern, "|")
	if len(parts) != 4 || parts[0] != "" || parts[1] != "1" {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(host))
	return hmac.Equal(mac.Sum(nil), want)
}

// writeFileAtomic writes data to a temporary file beside path and renames it
// over path, keeping the file's 0600 mode.
func writeFileAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".known_hosts-*")
	if err != nil {
		return err
	}
	name := file.Name()
	fail := func(err error) error {
		file.Close()
		os.Remove(name)
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := file.Write(data); err != nil {
		return fail(err)
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// algorithms lists the host-key algorithms to offer for hostname: the types
// already on file, so a server that also has other key types is checked
// against the one the user trusted instead of looking changed. Nil means the
// host is new and any algorithm is fine.
func (keys *hostKeys) algorithms(hostname string) []string {
	data, err := os.ReadFile(keys.path)
	if err != nil {
		return nil
	}
	want := knownhosts.Normalize(hostname)
	var algorithms []string
	for len(data) > 0 {
		marker, hosts, key, _, rest, err := ssh.ParseKnownHosts(data)
		if err != nil {
			break // end of file or a line we cannot read
		}
		data = rest
		if marker != "" || !slices.Contains(hosts, want) {
			continue
		}
		for _, algorithm := range algorithmsFor(key.Type()) {
			if !slices.Contains(algorithms, algorithm) {
				algorithms = append(algorithms, algorithm)
			}
		}
	}
	return algorithms
}

// algorithmsFor maps a stored key type to the signature algorithms a server
// uses for it. RSA keys sign with the SHA-2 variants nowadays, but a server that
// predates RFC 8332 can only sign the same key as ssh-rsa; the library offers
// that last for a new host, so the host stays reachable once its key is saved.
func algorithmsFor(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{keyType}
}

// knownHostsRemote returns remote in a form the knownhosts check can parse.
// The check always parses remote.String(), even when it already has the dial
// hostname, and net.SplitHostPort reads the first ']' as the end of the
// address. Windows reports an IPv6 link-local zone as the network adapter's
// friendly name, and names such as "Example VPN Adapter [0123456789abcdef]"
// contain brackets, so that parse fails and the handshake ends before the
// known-hosts lookup. The check discards the parsed remote when the hostname
// is present, so dropping the zone here does not change what is matched.
func knownHostsRemote(remote net.Addr) net.Addr {
	if _, _, err := net.SplitHostPort(remote.String()); err == nil {
		return remote
	}
	tcp, ok := remote.(*net.TCPAddr)
	if !ok || tcp.Zone == "" {
		return remote
	}
	withoutZone := *tcp
	withoutZone.Zone = ""
	return &withoutZone
}

// verifyHostKey is the ssh.HostKeyCallback: known keys pass, and a new or
// changed key waits for the user. A changed key is one whose address has a
// trusted key of the same type; a key of a type never stored is a first
// connection, as OpenSSH treats it, not a rotation alarm.
func (conn *Conn) verifyHostKey(keys *hostKeys, hostname string, remote net.Addr, key ssh.PublicKey) error {
	err := keys.callback(hostname, knownHostsRemote(remote), key)
	if err == nil {
		return nil
	}
	var mismatch *knownhosts.KeyError
	if !errors.As(err, &mismatch) {
		// A host certificate with no matching certificate authority on file is
		// an unknown key, as OpenSSH treats it: prompt with the certificate's
		// own fingerprint instead of failing the handshake. Trusting it saves
		// the certificate itself as the known key for the address.
		if !strings.HasSuffix(key.Type(), "-cert-v01@openssh.com") {
			return err
		}
		mismatch = &knownhosts.KeyError{}
	}
	info := HostKeyInfo{
		Address:     hostname,
		Type:        key.Type(),
		Fingerprint: ssh.FingerprintSHA256(key),
		File:        keys.path,
	}
	for _, known := range mismatch.Want {
		if known.Key.Type() != key.Type() {
			continue
		}
		info.Changed = true
		info.Known = append(info.Known, ssh.FingerprintSHA256(known.Key))
	}

	response, err := conn.ask(PhaseHostKeyPrompt, func(status *Status) { status.HostKey = info })
	if err != nil {
		return err
	}
	if !response.accept {
		return ErrHostKeyRejected
	}
	if response.save {
		if info.Changed {
			err = keys.rememberReplacing(hostname, key)
		} else {
			err = keys.remember(hostname, key)
		}
		if err != nil {
			return fmt.Errorf("sshconn: remember host key: %w", err)
		}
	}
	conn.update(func(status *Status) { status.Phase = PhaseAuthenticating })
	return nil
}
