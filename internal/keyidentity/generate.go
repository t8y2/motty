package keyidentity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Generated is a new key pair: Private is the OpenSSH private-key file's text
// and Public the line to put in a server's authorized_keys.
type Generated struct {
	Private []byte
	Public  string
}

// Generate makes an Ed25519 key. A non-empty passphrase encrypts the private
// half the way ssh-keygen does; comment is the label the file carries ("" for
// none). The result is what ssh-keygen -t ed25519 would have written.
func Generate(comment string, passphrase []byte) (Generated, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Generated{}, fmt.Errorf("generate SSH key: %w", err)
	}
	return marshalEd25519(private, comment, passphrase)
}

// Normalize turns pasted key text into what the parsers read: line ends become
// \n, the indentation a chat message or a terminal adds is dropped, and the text
// ends with one newline. It does not decide whether the text is a key.
func Normalize(text string) ([]byte, error) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimSpace(line)
	}
	trimmed := strings.TrimSpace(strings.Join(lines, "\n"))
	if trimmed == "" {
		return nil, errors.New("no key text")
	}
	data := []byte(trimmed + "\n")
	if len(data) > maxPrivateKeySize {
		return nil, ErrTooLarge
	}
	if !bytes.Contains(data, []byte("PRIVATE KEY")) && !bytes.HasPrefix(data, []byte("PuTTY-User-Key-File-")) {
		return nil, errors.New("this is not a private key: expected a block that starts with -----BEGIN … PRIVATE KEY-----")
	}
	return data, nil
}

func marshalEd25519(private ed25519.PrivateKey, comment string, passphrase []byte) (Generated, error) {
	var block *pem.Block
	var err error
	if len(passphrase) == 0 {
		block, err = ssh.MarshalPrivateKey(private, comment)
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(private, comment, passphrase)
	}
	if err != nil {
		return Generated{}, fmt.Errorf("encode SSH key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		return Generated{}, fmt.Errorf("encode SSH key: %w", err)
	}
	var info Info
	info.describe(signer.PublicKey())
	public := info.PublicKey
	if comment != "" {
		public += " " + comment
	}
	return Generated{Private: pem.EncodeToMemory(block), Public: public}, nil
}
