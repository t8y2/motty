package sshconn

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"

	"motty/internal/keyidentity"
)

// defaultIdentities are the private keys OpenSSH tries without configuration.
var defaultIdentities = []string{"id_ed25519", "id_ecdsa", "id_rsa"}

// authMethods builds the client's authentication order: keys first, then the
// account password through keyboard-interactive and password. Prompts for the
// password block the connection goroutine and show up as Status.
func (conn *Conn) authMethods(authentication Authentication) []ssh.AuthMethod {
	methods := authentication.Methods
	if methods == 0 {
		methods = AuthAll
	}
	auth := make([]ssh.AuthMethod, 0, 3)
	if methods&AuthPublicKey != 0 {
		auth = append(auth, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			return conn.publicKeySigners(authentication)
		}))
	}
	if methods&AuthKeyboardInteractive != 0 {
		auth = append(auth, ssh.RetryableAuthMethod(ssh.KeyboardInteractive(conn.keyboardInteractive), maxPasswordAttempts))
	}
	if methods&AuthPassword != 0 {
		auth = append(auth, ssh.RetryableAuthMethod(ssh.PasswordCallback(conn.passwordPrompt), maxPasswordAttempts))
	}
	return auth
}

// publicKeySigners runs only after the server's host key is accepted. That
// ordering ensures the app never asks for a private-key passphrase on behalf
// of an untrusted endpoint.
func (conn *Conn) publicKeySigners(authentication Authentication) ([]ssh.Signer, error) {
	conn.method("publickey")
	var agents []ssh.Signer
	if authentication.Agent {
		agentSigners, err := conn.agentSigners()
		switch {
		case err == nil:
			agents = agentSigners
		case conn.config.AgentAddress != "":
			// The user pointed at this agent; saying nothing would leave a
			// failed login with no hint that the agent was never reached.
			conn.update(func(status *Status) { status.Skipped = append(status.Skipped, fmt.Sprintf("ssh-agent (%v)", err)) })
		}
	}
	signers, skipped, err := conn.loadIdentities(authentication, agents)
	if err != nil {
		return nil, err
	}
	if len(skipped) > 0 {
		conn.update(func(status *Status) { status.Skipped = append(status.Skipped, skipped...) })
	}
	return signers, nil
}

// method remembers the authentication method being tried; the one tried last
// is the one that succeeded, since methods stop at the first success.
func (conn *Conn) method(name string) {
	conn.mu.Lock()
	conn.lastMethod = name
	conn.mu.Unlock()
}

// passwordPrompt asks the user for the account password.
func (conn *Conn) passwordPrompt() (string, error) {
	conn.method("password")
	return conn.askPassword("")
}

// maxQuestions bounds one keyboard-interactive round. A server that asks more
// than this is asking for an SSO-style conversation the application cannot
// present, and a dialog with that many fields could not be used.
const maxQuestions = 8

// keyboardInteractive answers servers that take the password through
// keyboard-interactive, as PAM setups do. Every question of the round is asked
// at once, in the server's order, with its own wording and echo flag: a PAM
// stack may want the account password and a one-time code together, or show a
// visible question beside a hidden one.
func (conn *Conn) keyboardInteractive(name, instruction string, questions []string, echo []bool) ([]string, error) {
	if len(questions) == 0 {
		return nil, nil
	}
	if len(echo) != len(questions) {
		return nil, fmt.Errorf("sshconn: keyboard-interactive sent %d questions and %d echo flags", len(questions), len(echo))
	}
	if len(questions) > maxQuestions {
		return nil, fmt.Errorf("sshconn: keyboard-interactive asked %d questions, more than the %d supported", len(questions), maxQuestions)
	}
	conn.method("keyboard-interactive")
	prompts := make([]Question, len(questions))
	for index, text := range questions {
		prompts[index] = Question{Text: text, Secret: !echo[index]}
	}
	return conn.askQuestions(PhasePasswordPrompt, prompts)
}

// askQuestions publishes one prompt round and blocks until the UI answers it
// or the connection closes. One round is one attempt, however many questions
// it holds.
func (conn *Conn) askQuestions(phase Phase, questions []Question) ([]string, error) {
	conn.mu.Lock()
	if conn.attempts >= maxPasswordAttempts {
		conn.mu.Unlock()
		return nil, fmt.Errorf("%w: too many password attempts", ErrAuthFailed)
	}
	conn.attempts++
	attempt := conn.attempts
	conn.mu.Unlock()

	response, err := conn.ask(phase, func(status *Status) {
		status.Attempt = attempt
		status.Questions = questions
	})
	if err != nil {
		return nil, err
	}
	if !response.accept {
		return nil, ErrCanceled
	}
	if len(response.answers) != len(questions) {
		return nil, fmt.Errorf("sshconn: %d answers for %d questions", len(response.answers), len(questions))
	}
	conn.update(func(status *Status) { status.Phase = PhaseAuthenticating })
	return response.answers, nil
}

// askPassword asks the plain password method's one secret question.
func (conn *Conn) askPassword(question string) (string, error) {
	answers, err := conn.askQuestions(PhasePasswordPrompt, []Question{{Text: question, Secret: true}})
	if err != nil {
		return "", err
	}
	return answers[0], nil
}

// loadIdentities reads configured private keys and offers encrypted ones as
// identities that ask for their passphrase only when the server accepts the
// key. A key the user declines is skipped without canceling the connection, so
// agent and password authentication can still proceed. A key that is present
// but unusable is skipped too, with the reason in skipped.
func (conn *Conn) loadIdentities(authentication Authentication, agents []ssh.Signer) (signers []ssh.Signer, skipped []string, err error) {
	paths := authentication.IdentityFiles
	if paths == nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, nil
		}
		for _, name := range defaultIdentities {
			paths = append(paths, filepath.Join(home, ".ssh", name))
		}
	}
	paths = append(append([]string(nil), paths...), authentication.AdditionalIdentityFiles...)
	usedAgents := make([]bool, len(agents))
	var preferred []ssh.Signer
	for _, path := range paths {
		if path == "none" {
			continue
		}
		var public ssh.PublicKey
		publicRead := len(agents) > 0
		if publicRead {
			public = conn.identityPublicKey(path)
		}
		matched := false
		for index, signer := range agents {
			if public != nil && bytes.Equal(public.Marshal(), signer.PublicKey().Marshal()) {
				certified := signer
				var certErr error
				if !IsStoredKey(path) {
					certified, certErr = certificateSigner(path, signer)
				}
				if certErr != nil {
					skipped = append(skipped, certErr.Error())
				} else if !usedAgents[index] {
					preferred = append(preferred, certified)
				}
				usedAgents[index], matched = true, true
			}
		}
		if matched {
			continue
		}
		signer, err := conn.loadKey(path, nil)
		if err != nil {
			var passphrase *ssh.PassphraseMissingError
			if !errors.As(err, &passphrase) {
				// A default identity that is not there is the normal case. A
				// key that is there but cannot be used must say why, or the
				// login fails with no sign that the key was ever considered.
				if !errors.Is(err, os.ErrNotExist) {
					skipped = append(skipped, fmt.Sprintf("%s (%v)", path, err))
				}
				continue
			}
			if !publicRead {
				public = conn.identityPublicKey(path)
			}
			if public == nil {
				// Without the public half the key cannot be offered until it
				// is unlocked, so the passphrase is needed now.
				var accepted bool
				signer, accepted, err = conn.unlockIdentity(path)
				if err != nil {
					return nil, skipped, err
				}
				if !accepted {
					skipped = append(skipped, path)
					continue
				}
			} else {
				signer = &lockedIdentity{conn: conn, path: path, public: public}
			}
		}
		if IsStoredKey(path) {
			signers = append(signers, signer) // no file for a certificate to sit beside
			continue
		}
		// An accompanying certificate file upgrades the signer, as OpenSSH's
		// default identity pairing does.
		certified, err := certificateSigner(path, signer)
		if err != nil {
			skipped = append(skipped, err.Error())
			continue
		}
		signers = append(signers, certified)
	}
	if !authentication.IdentitiesOnly {
		unmatched := make([]ssh.Signer, 0, len(agents))
		for index, signer := range agents {
			if !usedAgents[index] {
				unmatched = append(unmatched, signer)
			}
		}
		signers = append(unmatched, signers...)
	}
	signers = append(preferred, signers...)
	return signers, skipped, nil
}

// identityPublicKey reads the public half of an identity without unlocking it,
// so the key can be offered to the server and matched against the agent. nil
// means the key cannot be named until its passphrase arrives, which is the case
// for an encrypted PEM block that carries no public key.
func (conn *Conn) identityPublicKey(path string) ssh.PublicKey {
	if !IsStoredKey(path) {
		public, _ := keyidentity.PublicKey(path)
		return public
	}
	if conn.config.StoredKey == nil {
		return nil
	}
	data, err := conn.config.StoredKey(path)
	if err != nil {
		return nil
	}
	defer clear(data)
	info, err := keyidentity.InspectData(path, data)
	if err != nil || info.PublicKey == "" {
		return nil
	}
	public, _, _, _, err := ssh.ParseAuthorizedKey([]byte(info.PublicKey))
	if err != nil {
		return nil
	}
	return public
}

// loadKey reads one entry of an identity list: a file, or a key the application
// keeps for itself. A stored key that is not there reports as an unusable key,
// not as the absent file that a default identity so often is: the user chose it.
func (conn *Conn) loadKey(ref string, passphrase []byte) (ssh.Signer, error) {
	if !IsStoredKey(ref) {
		return keyidentity.Load(ref, passphrase)
	}
	if conn.config.StoredKey == nil {
		return nil, errors.New("saved keys are not available here")
	}
	data, err := conn.config.StoredKey(ref)
	if err != nil {
		return nil, fmt.Errorf("saved key: %w", err)
	}
	defer clear(data)
	return keyidentity.Parse(data, passphrase)
}

func (conn *Conn) unlockIdentity(path string) (ssh.Signer, bool, error) {
	for attempt := 1; attempt <= maxPasswordAttempts; attempt++ {
		response, err := conn.ask(PhaseKeyPassphrasePrompt, func(status *Status) {
			status.Attempt = attempt
			status.IdentityPath = path
		})
		if err != nil {
			return nil, false, err
		}
		if !response.accept {
			conn.update(func(status *Status) { status.Phase = PhaseAuthenticating })
			return nil, false, nil
		}
		if len(response.answers) != 1 {
			return nil, false, fmt.Errorf("sshconn: %d answers for a key passphrase", len(response.answers))
		}
		passphrase := []byte(response.answers[0])
		signer, parseErr := conn.loadKey(path, passphrase)
		clear(passphrase)
		response.answers[0] = ""
		if parseErr == nil {
			conn.update(func(status *Status) { status.Phase = PhaseAuthenticating })
			return signer, true, nil
		}
	}
	conn.update(func(status *Status) { status.Phase = PhaseAuthenticating })
	return nil, false, nil
}

// errKeyLocked ends public-key authentication for a key the user declined to
// unlock. It is deliberately not ErrCanceled: the connection is not canceled,
// and password authentication may still follow.
var errKeyLocked = errors.New("the private key's passphrase was not supplied")

// lockedIdentity is an encrypted identity whose public half is known but whose
// private half needs a passphrase. The passphrase is requested from Sign, once
// the server has accepted the key and a signature is actually needed — the
// point at which OpenSSH asks too. Asking while the identity list is collected
// would demand a passphrase for a key the server never asks about, even when an
// ssh-agent key authenticates first.
type lockedIdentity struct {
	conn   *Conn
	path   string
	public ssh.PublicKey

	mu     sync.Mutex
	signer ssh.Signer
}

func (identity *lockedIdentity) PublicKey() ssh.PublicKey { return identity.public }

func (identity *lockedIdentity) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	signer, err := identity.unlock()
	if err != nil {
		return nil, err
	}
	return signer.Sign(rand, data)
}

func (identity *lockedIdentity) SignWithAlgorithm(rand io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	signer, err := identity.unlock()
	if err != nil {
		return nil, err
	}
	if algorithmSigner, ok := signer.(ssh.AlgorithmSigner); ok {
		return algorithmSigner.SignWithAlgorithm(rand, data, algorithm)
	}
	return signer.Sign(rand, data)
}

// Algorithms names what the key will sign with once unlocked. It cannot ask the
// key itself, so it reports what its format allows, in the order x/crypto uses
// (see algorithmsForKeyFormat): RSA keys offer the SHA-2 variants, every other
// format only its own name.
func (identity *lockedIdentity) Algorithms() []string {
	identity.mu.Lock()
	signer := identity.signer
	identity.mu.Unlock()
	if multi, ok := signer.(ssh.MultiAlgorithmSigner); ok {
		return multi.Algorithms()
	}
	switch identity.public.Type() {
	case ssh.KeyAlgoRSA:
		return []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSA}
	case ssh.CertAlgoRSAv01:
		return []string{ssh.CertAlgoRSASHA256v01, ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSAv01}
	default:
		return []string{identity.public.Type()}
	}
}

func (identity *lockedIdentity) unlock() (ssh.Signer, error) {
	identity.mu.Lock()
	defer identity.mu.Unlock()
	if identity.signer != nil {
		return identity.signer, nil
	}
	signer, accepted, err := identity.conn.unlockIdentity(identity.path)
	if err != nil {
		return nil, err
	}
	if !accepted {
		identity.conn.update(func(status *Status) { status.Skipped = append(status.Skipped, identity.path) })
		return nil, errKeyLocked
	}
	identity.signer = signer
	return signer, nil
}

// agentSigners offers the keys of the running ssh-agent. The link is shared
// with agent forwarding (see agentLink).
func (conn *Conn) agentSigners() ([]ssh.Signer, error) {
	client, err := conn.agentLink()
	if err != nil {
		return nil, err
	}
	return client.Signers()
}
