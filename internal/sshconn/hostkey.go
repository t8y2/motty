package sshconn

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

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

// verifyHostKey is the ssh.HostKeyCallback: known keys pass, a changed key
// fails without asking, and an unknown key waits for the user.
func (conn *Conn) verifyHostKey(keys *hostKeys, hostname string, remote net.Addr, key ssh.PublicKey) error {
	err := keys.callback(hostname, remote, key)
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
	if len(mismatch.Want) > 0 {
		// Never offered for override: a changed key is either a rebuilt server
		// the user must re-trust deliberately in the file, or an attack.
		for _, known := range mismatch.Want {
			info.Known = append(info.Known, ssh.FingerprintSHA256(known.Key))
		}
		conn.update(func(status *Status) { status.HostKey = info })
		return fmt.Errorf("%w for %s", ErrHostKeyChanged, hostname)
	}

	response, err := conn.ask(PhaseHostKeyPrompt, func(status *Status) { status.HostKey = info })
	if err != nil {
		return err
	}
	if !response.accept {
		return ErrHostKeyRejected
	}
	if response.save {
		if err := keys.remember(hostname, key); err != nil {
			return fmt.Errorf("sshconn: remember host key: %w", err)
		}
	}
	conn.update(func(status *Status) { status.Phase = PhaseAuthenticating })
	return nil
}
