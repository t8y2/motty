package sshconn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	signers, skipped, err := conn.loadIdentities(authentication)
	if err != nil {
		return nil, err
	}
	if len(skipped) > 0 {
		conn.update(func(status *Status) { status.Skipped = append(status.Skipped, skipped...) })
	}
	if authentication.Agent {
		if agentSigners, err := conn.agentSigners(); err == nil {
			signers = append(agentSigners, signers...)
		}
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

// loadIdentities reads configured private keys and asks for encrypted-key
// passphrases one at a time. A declined key is skipped without canceling the
// connection, so agent and password authentication can still proceed.
func (conn *Conn) loadIdentities(authentication Authentication) (signers []ssh.Signer, skipped []string, err error) {
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
	paths = append(paths, authentication.AdditionalIdentityFiles...)
	for _, path := range paths {
		signer, err := keyidentity.Load(path, nil)
		if err != nil {
			var passphrase *ssh.PassphraseMissingError
			if !errors.As(err, &passphrase) {
				continue // absent, unreadable, or invalid keys are not offered
			}
			var accepted bool
			signer, accepted, err = conn.unlockIdentity(path)
			if err != nil {
				return nil, skipped, err
			}
			if !accepted {
				skipped = append(skipped, path)
				continue
			}
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
	return signers, skipped, nil
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
		signer, parseErr := keyidentity.Load(path, passphrase)
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

// agentSigners offers the keys of the running ssh-agent. The link is shared
// with agent forwarding (see agentLink).
func (conn *Conn) agentSigners() ([]ssh.Signer, error) {
	client, err := conn.agentLink()
	if err != nil {
		return nil, err
	}
	return client.Signers()
}
