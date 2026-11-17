package workbench

import (
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"motty/internal/sshconn"
)

func TestPrivateKeyAuthenticationContinuesWithMFA(t *testing.T) {
	for _, test := range []struct {
		name        string
		savedOTP    bool
		rejectKey   bool
		missingKey  bool
		noPublicKey bool
		keyOnly     bool
		retryOTP    bool
		cancel      bool
	}{
		{name: "manual OTP"},
		{name: "saved OTP", savedOTP: true},
		{name: "retry OTP", retryOTP: true},
		{name: "cancel OTP", cancel: true},
		{name: "rejected key", rejectKey: true},
		{name: "unavailable selected key", missingKey: true},
		{name: "server only offers keyboard-interactive", noPublicKey: true},
		{name: "key needs no second factor", keyOnly: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			selectedPath := writeWorkbenchIdentity(t, "id_selected", "")
			otherPath := writeWorkbenchIdentity(t, "id_other", "")
			_, options := newWorld(t)
			options.WallNow = func() time.Time { return time.Unix(59, 0) }
			options.Dial = func(config sshconn.Config) Connection { return realConn{sshconn.Dial(config)} }
			options.Invalidate = func() {}

			keys := make(chan string, 8)
			answers := make(chan string, 8)
			challenge := func(_ ssh.ConnMetadata, ask ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
				replies, err := ask("", "", []string{"Verification code: "}, []bool{false})
				if err != nil {
					return nil, err
				}
				if len(replies) != 1 {
					return nil, os.ErrPermission
				}
				answers <- replies[0]
				if replies[0] != testOTPCode {
					return nil, os.ErrPermission
				}
				return nil, nil
			}
			serverConfig := &ssh.ServerConfig{KeyboardInteractiveCallback: challenge}
			if !test.noPublicKey {
				serverConfig.PublicKeyCallback = func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
					keys <- ssh.FingerprintSHA256(key)
					if test.rejectKey {
						return nil, os.ErrPermission
					}
					if test.keyOnly {
						return nil, nil
					}
					return nil, &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{KeyboardInteractiveCallback: challenge}}
				}
			}
			port := startHostMFAServer(t, serverConfig)
			state := newTestState(t, options)
			state.restoreIdentities([]string{selectedPath, otherPath})
			fingerprint := state.identities[0].Fingerprint
			host := addStreamHost(state, Host{
				Label: "MFA", Address: "127.0.0.1", Port: port, User: "tester",
				Protocol: protocolSSH, Auth: authPrivateKey, Identity: fingerprint, InteractiveOnly: true,
			})
			if test.missingKey {
				state.identities = state.identities[1:]
			}
			if test.savedOTP {
				if err := state.secrets.vault.Prepare(); err != nil {
					t.Fatal(err)
				}
				if err := state.secrets.vault.Set(hostSecretName(host.CatalogID, hostOTPPart), testOTPSeed); err != nil {
					t.Fatal(err)
				}
			}

			openHost(state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			waitUntil(t, state, "host key confirmation", func() bool { return state.modal == modalHostKey || session.failed() })
			state.Click(hitHostKeyTrust)
			if test.rejectKey || test.missingKey || test.noPublicKey {
				waitUntil(t, state, "key rejection", func() bool { return session.failed() || state.modal == modalPassword })
				if state.modal == modalPassword || !errors.Is(session.conn.Status().Err, sshconn.ErrAuthFailed) {
					t.Fatalf("key failure fell back to interactive authentication: modal=%v status=%+v", state.modal, session.conn.Status())
				}
			} else {
				if !test.savedOTP && !test.keyOnly {
					waitUntil(t, state, "OTP prompt", func() bool { return state.modal == modalPassword || session.failed() })
					if state.modal != modalPassword || !slices.Equal(session.conn.Status().Questions, []sshconn.Question{{Text: "Verification code: ", Secret: true}}) {
						t.Fatalf("OTP prompt: modal=%v status=%+v", state.modal, session.conn.Status())
					}
					if test.cancel {
						state.Click(hitPasswordCancel)
						waitUntil(t, state, "canceled connection", func() bool { return session.failed() })
					} else {
						if test.retryOTP {
							typeInto(state, hitPasswordField, "000000")
							state.Click(hitPasswordSubmit)
							waitUntil(t, state, "OTP retry", func() bool { return state.modal == modalPassword && session.conn.Status().Attempt == 2 })
						}
						typeInto(state, hitPasswordField, testOTPCode)
						state.Click(hitPasswordSubmit)
					}
				}
				if !test.cancel {
					waitUntil(t, state, "authenticated shell", func() bool { return strings.Contains(screenText(state, 0), "MFA connected") || session.failed() })
					if session.failed() {
						t.Fatalf("connection failed: %s (%+v)", session.failure, session.conn.Status())
					}
				}
			}
			if test.missingKey || test.noPublicKey {
				if len(keys) != 0 {
					t.Fatal("an unavailable selected key fell back to another key")
				}
			} else if len(keys) != 1 || <-keys != fingerprint {
				t.Fatal("authentication did not use only the selected key")
			}
			wantAnswers := 1
			if test.retryOTP {
				wantAnswers = 2
			}
			if test.rejectKey || test.missingKey || test.noPublicKey || test.keyOnly || test.cancel {
				wantAnswers = 0
			}
			if len(answers) != wantAnswers {
				t.Fatalf("server received %d OTP answers, want %d", len(answers), wantAnswers)
			}
			if test.savedOTP && <-answers != testOTPCode {
				t.Fatal("the saved OTP was not used")
			}
		})
	}
}

// startHostMFAServer performs a real SSH handshake and opens a minimal shell,
// so the test follows the saved host's connection, prompt and OTP paths.
func startHostMFAServer(t *testing.T, config *ssh.ServerConfig) uint16 {
	t.Helper()
	_, private, err := newTestEd25519()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		raw, err := listener.Accept()
		if err != nil {
			return
		}
		defer raw.Close()
		_ = raw.SetDeadline(time.Now().Add(15 * time.Second))
		conn, channels, requests, err := ssh.NewServerConn(raw, config)
		if err != nil {
			return
		}
		defer conn.Close()
		go ssh.DiscardRequests(requests)
		for request := range channels {
			if request.ChannelType() != "session" {
				_ = request.Reject(ssh.UnknownChannelType, "unsupported")
				continue
			}
			channel, requests, err := request.Accept()
			if err != nil {
				continue
			}
			for request := range requests {
				_ = request.Reply(true, nil)
				if request.Type == "shell" {
					_, _ = io.WriteString(channel, "MFA connected\r\n")
				}
			}
			_ = channel.Close()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	return uint16(listener.Addr().(*net.TCPAddr).Port)
}
