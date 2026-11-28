package workbench

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"motty/internal/sshconn"
)

type deployTestVault map[string]string

func (v deployTestVault) Get(key string) (string, bool, error) {
	value, ok := v[key]
	return value, ok, nil
}

func testDeploySigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestHostKeyDeploymentUsesFreshExactKeyHandshake(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		trust, allowKey, switchAuth         bool
		imported, verified                  bool
		passwords, commands, keyConnections int32
	}{
		{"import only", true, true, false, true, false, 1, 1, 0},
		{"install and verify", true, true, true, true, true, 1, 1, 1},
		{"no password fallback after import", true, false, true, true, false, 1, 1, 0},
		{"unknown host receives neither password nor public key", false, true, true, false, false, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hostSigner := testDeploySigner(t)
			_, rawPrivate, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			signer, err := ssh.NewSignerFromKey(rawPrivate)
			if err != nil {
				t.Fatal(err)
			}
			block, err := ssh.MarshalPrivateKey(rawPrivate, "synthetic")
			if err != nil {
				t.Fatal(err)
			}
			private := pem.EncodeToMemory(block)
			line := ssh.MarshalAuthorizedKey(signer.PublicKey())
			var installed atomic.Bool
			var passwords, commands, keyConnections atomic.Int32
			auth := &ssh.ServerConfig{
				PasswordCallback: func(_ ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
					passwords.Add(1)
					if string(p) != "fixture" {
						return nil, errors.New("wrong password")
					}
					return &ssh.Permissions{}, nil
				},
				PublicKeyCallback: func(_ ssh.ConnMetadata, p ssh.PublicKey) (*ssh.Permissions, error) {
					if !tc.allowKey || !installed.Load() || !bytes.Equal(line, ssh.MarshalAuthorizedKey(p)) {
						return nil, errors.New("key refused")
					}
					return &ssh.Permissions{Extensions: map[string]string{"fixture-key": "yes"}}, nil
				},
			}
			auth.AddHostKey(hostSigner)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Skipf("loopback listener unavailable: %v", err)
			}
			defer listener.Close()
			go func() {
				for {
					socket, err := listener.Accept()
					if err != nil {
						return
					}
					go func() {
						defer socket.Close()
						conn, channels, requests, err := ssh.NewServerConn(socket, auth)
						if err != nil {
							return
						}
						defer conn.Close()
						// Check a completed public-key authentication, not a probe.
						if conn.Permissions.Extensions["fixture-key"] == "yes" {
							keyConnections.Add(1)
						}
						go ssh.DiscardRequests(requests)
						for pending := range channels {
							if pending.ChannelType() != "session" {
								_ = pending.Reject(ssh.UnknownChannelType, "")
								continue
							}
							ch, reqs, err := pending.Accept()
							if err != nil {
								continue
							}
							go func() {
								defer ch.Close()
								for r := range reqs {
									var request struct{ Command string }
									if r.Type != "exec" || ssh.Unmarshal(r.Payload, &request) != nil || request.Command != hostKeyInstallCommand {
										_ = r.Reply(false, nil)
										continue
									}
									_ = r.Reply(true, nil)
									data, _ := io.ReadAll(io.LimitReader(ch, 4097))
									commands.Add(1)
									if !bytes.Equal(data, line) {
										_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{1}))
										return
									}
									installed.Store(true)
									_, _ = io.WriteString(ch, "motty-key-installed\n")
									_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
									return
								}
							}()
						}
					}()
				}
			}()
			known := filepath.Join(t.TempDir(), "known_hosts")
			if tc.trust {
				if err := os.WriteFile(known, []byte(knownhosts.Line([]string{listener.Addr().String()}, hostSigner.PublicKey())+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			endpoint := sshconn.Endpoint{Host: "127.0.0.1", Port: uint16(listener.Addr().(*net.TCPAddr).Port), User: "fixture"}
			ref := sshconn.StoredKeyPrefix + "fixture"
			task := hostKeyDeployTask{
				host: Host{ID: 7},
				config: sshconn.Config{Host: endpoint.Host, Port: endpoint.Port, User: endpoint.User, KnownHostsPath: known, NoProbes: true,
					Authentication: map[sshconn.Endpoint]sshconn.Authentication{endpoint: {Methods: sshconn.AuthPassword}},
					StoredKey: func(path string) ([]byte, error) {
						if path != ref {
							return nil, errors.New("unexpected key")
						}
						return bytes.Clone(private), nil
					},
				},
				passwords: map[sshconn.Endpoint]string{endpoint: "test-pass"}, secrets: deployTestVault{"test-pass": "fixture"},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			got := task.deploy(ctx, line, ref, ssh.FingerprintSHA256(signer.PublicKey()), tc.switchAuth)
			// The server reports completed authentication on its own goroutine.
			if got.verified {
				for deadline := time.Now().Add(time.Second); keyConnections.Load() == 0 && time.Now().Before(deadline); {
					time.Sleep(time.Millisecond)
				}
			}
			if got.Imported != tc.imported || got.verified != tc.verified || passwords.Load() != tc.passwords || commands.Load() != tc.commands || keyConnections.Load() != tc.keyConnections {
				t.Fatalf("result=%+v password attempts=%d installs=%d key connections=%d", got, passwords.Load(), commands.Load(), keyConnections.Load())
			}
		})
	}
}

func TestHostKeyDeploymentOnlySwitchesVerifiedUnchangedProfiles(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := Host{ID: 44, CatalogID: "fixture-host", Label: "Synthetic", Address: "localhost", Port: 2244, User: "dev", Auth: authPassword}
	host.finish()
	state.hosts = []Host{host}
	const ref = "sshkey:fixture"
	const fingerprint = "SHA256:test-fingerprint"
	state.identities = []identityRecord{{ID: 1, Path: ref, Fingerprint: fingerprint, Stored: true}}
	_, route, _ := state.hostCatalog.sshConfigFor(host, host, state.connectionEnv(nil))
	finish := func(result hostKeyDeployResult, canceled bool) {
		ch := make(chan hostKeyDeployResult, 1)
		ch <- result
		close(ch)
		state.hostKeyDeploy = hostKeyDeployment{running: true, done: ch, ref: ref, fingerprint: fingerprint, canceled: canceled, cancel: func() {}}
		state.pollHostKeyDeployment()
		if state.hostKeyDeploy.running || len(state.hostKeyDeploy.results) != 1 {
			t.Fatal("batch did not finish")
		}
	}
	result := hostKeyDeployResult{HostID: host.ID, Imported: true, host: host, login: host.User, route: route}
	finish(result, false)
	if got := state.hosts[0]; got.Auth != authPassword || got.Identity != "" {
		t.Fatal("import-only or failed verification changed saved authentication")
	}
	result.verified = true
	finish(result, true)
	if got := state.hosts[0]; got.Auth != authPassword || got.Identity != "" {
		t.Fatal("canceled job changed saved authentication")
	}
	state.hosts[0].User = "changed"
	finish(result, false)
	if got := state.hosts[0]; got.Auth != authPassword || got.Identity != "" || got.User != "changed" {
		t.Fatal("stale verification overwrote a host edit")
	}
	state.hosts[0] = host
	finish(result, false)
	if got := state.hosts[0]; got.Auth != authPrivateKey || got.Identity != fingerprint || !state.hostKeyDeploy.results[0].Switched {
		t.Fatalf("verified profile did not switch: auth=%v key=%q error=%s", got.Auth, got.Identity, state.hostKeyDeploy.results[0].Error)
	}
}
