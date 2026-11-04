// Throwaway SSH server for end-to-end checks of motty's terminal, SFTP,
// forwarding, and jump-host paths. Password "secret" works for any user, the
// server listens on loopback only, and shells run on a real pty. The first
// argument is the port (default 2222); further arguments are exact loopback
// endpoints that direct-tcpip may reach for jump-host tests. With
// E2E_NO_DIRECT=1 every direct-tcpip request is refused, which makes the server a
// bastion that only offers a shell.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func main() {
	port := "2222"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(private)
	fixture, err := prepareFixture()
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(fixture.root)
	certs, err := prepareCerts(fixture.identityPath, fixture.publicKey)
	if err != nil {
		log.Fatal(err)
	}
	hostSigner, hostCALine, err := hostKey(signer)
	if err != nil {
		log.Fatal(err)
	}
	echo, err := startEchoServer()
	if err != nil {
		log.Fatal(err)
	}
	defer echo.Close()
	echoAddress := echo.Addr().String()
	_, echoPort, err := net.SplitHostPort(echoAddress)
	if err != nil {
		log.Fatal(err)
	}
	echoAlias := net.JoinHostPort("echo.gop.test", echoPort)
	allowedTargets := []directTarget{
		{requested: echoAddress, dial: echoAddress},
		{requested: echoAlias, dial: echoAddress},
	}
	for _, target := range os.Args[2:] {
		if err := validateLoopbackTarget(target); err != nil {
			log.Fatal(err)
		}
		allowedTargets = append(allowedTargets, directTarget{requested: target, dial: target})
	}
	config := &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-GopE2E",
		PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), fixture.publicKey.Marshal()) || certs.acceptKey(key) {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	keyOnly := certs.userCert != nil || len(certs.extraKeys) > 0
	if keyOnly {
		// The certificate and extra-key fixtures must be proven by the key
		// alone, so the password method is refused.
	} else if os.Getenv("E2ESSHD_KBI") == "password" {
		// A PAM-style password login: the account password is the only question
		// of a keyboard-interactive request, as on a default OpenSSH with PAM.
		// Password authentication stays refused.
		config.KeyboardInteractiveCallback = func(meta ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := challenge("", "", []string{"Password: "}, []bool{false})
			if err != nil {
				return nil, err
			}
			if len(answers) == 1 && answers[0] == "secret" {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		}
	} else if os.Getenv("E2ESSHD_KBI") == "1" {
		// A PAM-style round: the account password and a one-time code are two
		// questions of one keyboard-interactive request. Password authentication
		// stays refused, so a client that answers only one question cannot get in.
		config.KeyboardInteractiveCallback = func(meta ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := challenge("", "Two-factor authentication", []string{"Password: ", "Verification code: "}, []bool{false, false})
			if err != nil {
				return nil, err
			}
			if len(answers) == 2 && answers[0] == "secret" && answers[1] == "123456" {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		}
	} else {
		config.PasswordCallback = func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == "secret" {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		}
	}
	config.AddHostKey(hostSigner)
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("listening on 127.0.0.1:" + port)
	fmt.Println("identity " + fixture.identityPath)
	if certs.userCertPath != "" {
		fmt.Println("user-cert " + certs.userCertPath)
	}
	if hostCALine != "" {
		fmt.Println("host-ca " + hostCALine)
	}
	fmt.Println("identity passphrase key-secret")
	fmt.Println("sftp root " + fixture.sftpRoot)
	fmt.Println("echo " + echoAddress)
	fmt.Println("echo alias " + echoAlias)
	for _, target := range allowedTargets[2:] {
		fmt.Println("direct " + target.requested)
	}
	for {
		socket, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer socket.Close()
			conn, channels, requests, err := ssh.NewServerConn(socket, config)
			if err != nil {
				return
			}
			defer conn.Close()
			remoteForwards := newRemoteForwardSet(conn)
			defer remoteForwards.Close()
			go remoteForwards.Serve(requests)
			for newChannel := range channels {
				switch newChannel.ChannelType() {
				case "session":
					channel, reqs, err := newChannel.Accept()
					if err == nil {
						go serve(conn, fixture.sftpRoot, channel, reqs)
					}
				case "direct-tcpip":
					if os.Getenv("E2E_NO_DIRECT") == "1" {
						// A bastion that gives a shell and nothing else: forwarding is
						// refused whatever the target, as with AllowTcpForwarding no.
						newChannel.Reject(ssh.Prohibited, "administratively prohibited")
						continue
					}
					targets := append(remoteForwards.directTargets(), allowedTargets...)
					target, err := allowedDirectTarget(newChannel.ExtraData(), targets...)
					if err != nil {
						newChannel.Reject(ssh.Prohibited, err.Error())
						continue
					}
					log.Printf("direct-tcpip %s -> %s", target.requested, target.dial)
					remote, err := net.Dial("tcp", target.dial)
					if err != nil {
						newChannel.Reject(ssh.ConnectionFailed, "echo unavailable")
						continue
					}
					channel, reqs, err := newChannel.Accept()
					if err != nil {
						remote.Close()
						continue
					}
					go serveDirect(channel, remote, reqs)
				default:
					newChannel.Reject(ssh.UnknownChannelType, "session and restricted forwarding only")
				}
			}
		}()
	}
}

type directTCPIPRequest struct {
	DestinationHost string
	DestinationPort uint32
	OriginHost      string
	OriginPort      uint32
}

type directTarget struct {
	requested string
	dial      string
}

func allowedDirectTarget(payload []byte, allowed ...directTarget) (directTarget, error) {
	var request directTCPIPRequest
	if err := ssh.Unmarshal(payload, &request); err != nil {
		return directTarget{}, fmt.Errorf("invalid direct-tcpip request")
	}
	requested := net.JoinHostPort(request.DestinationHost, strconv.Itoa(int(request.DestinationPort)))
	for _, target := range allowed {
		if requested == target.requested {
			return target, nil
		}
	}
	return directTarget{}, fmt.Errorf("destination is outside the e2e fixtures")
}

func validateLoopbackTarget(target string) error {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return fmt.Errorf("invalid direct target %q: %w", target, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("direct target %q is not a loopback address", target)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid direct target %q: %w", target, err)
	}
	if number < 1 || number > 65535 {
		return fmt.Errorf("invalid direct target %q: port must be 1-65535", target)
	}
	return nil
}

func serveDirect(channel ssh.Channel, remote net.Conn, requests <-chan *ssh.Request) {
	defer channel.Close()
	defer remote.Close()
	go ssh.DiscardRequests(requests)
	uploaded := make(chan struct{})
	go func() {
		_, _ = io.Copy(remote, channel)
		if tcp, ok := remote.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		close(uploaded)
	}()
	_, _ = io.Copy(channel, remote)
	_ = channel.CloseWrite()
	<-uploaded
}

func startEchoServer() (net.Listener, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go serveEcho(connection)
		}
	}()
	return listener, nil
}

func serveEcho(connection net.Conn) {
	defer connection.Close()
	buffer := make([]byte, 4096)
	for {
		count, err := connection.Read(buffer)
		if count > 0 {
			_, _ = connection.Write(bytes.ToUpper(buffer[:count]))
		}
		if err != nil {
			return
		}
	}
}

func serve(sshConn ssh.Conn, sftpRoot string, channel ssh.Channel, requests <-chan *ssh.Request) {
	user := sshConn.User()
	defer channel.Close()
	x11 := newX11Relay(sshConn)
	defer x11.Close()
	var (
		term       = "xterm"
		cols, rows = 80, 24
		env        []string
		ptmx       *os.File
		once       sync.Once
		ptyAsked   bool
	)
	for request := range requests {
		switch request.Type {
		case "env":
			var payload struct{ Name, Value string }
			ssh.Unmarshal(request.Payload, &payload)
			env = append(env, payload.Name+"="+payload.Value)
			request.Reply(true, nil)
		case "pty-req":
			var payload struct {
				Term          string
				Columns, Rows uint32
				Width, Height uint32
				Modes         string
			}
			ssh.Unmarshal(request.Payload, &payload)
			term, cols, rows = payload.Term, int(payload.Columns), int(payload.Rows)
			ptyAsked = true
			request.Reply(true, nil)
		case "x11-req":
			request.Reply(x11.Request(request.Payload), nil)
		case "auth-agent-req@openssh.com":
			accepted := acceptAgentForward()
			request.Reply(accepted, nil)
			log.Printf("auth-agent-req accepted=%v", accepted)
		case "window-change":
			var payload struct{ Columns, Rows, Width, Height uint32 }
			ssh.Unmarshal(request.Payload, &payload)
			if ptmx != nil {
				pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(payload.Columns), Rows: uint16(payload.Rows)})
			}
		case "shell":
			request.Reply(true, nil)
			if os.Getenv("E2E_SFTP_ONLY") == "1" {
				// An account restricted to SFTP: the shell request is accepted, the
				// session says so and ends with a failure status.
				fmt.Fprint(channel, "This service allows sftp connections only.\r\n")
				channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{1}))
				channel.Close()
				return
			}
			once.Do(func() {
				command := exec.Command("/bin/bash", "--noprofile", "--norc", "-i")
				command.Env = append([]string{"TERM=" + term, "HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH"),
					"PS1=\\[\\e[01;32m\\]" + user + "@e2e\\[\\e[00m\\]:\\[\\e[01;34m\\]\\w\\[\\e[00m\\]\\$ ", "LANG=en_US.UTF-8", "CLICOLOR=1"}, append(x11.Env(), env...)...)
				command.Dir = os.TempDir()
				if login := os.Getenv("E2E_LOGIN_SHELL"); login != "" {
					// A server whose users have this login shell, which the client
					// learns from $SHELL and starts through its own startup shim.
					command = exec.Command(login, "-l")
					command.Env = append([]string{"TERM=" + term, "HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH"), "SHELL=" + login, "LANG=en_US.UTF-8"}, append(x11.Env(), env...)...)
					command.Dir = os.TempDir()
				}
				var err error
				ptmx, err = pty.StartWithSize(command, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
				if err != nil {
					fmt.Fprintf(channel, "cannot start shell: %v\r\n", err)
					channel.Close()
					return
				}
				go func() { io.Copy(ptmx, channel) }()
				go func() {
					io.Copy(channel, ptmx)
					status := uint32(0)
					if err := command.Wait(); err != nil {
						status = 1
						if exit, ok := err.(*exec.ExitError); ok {
							status = uint32(exit.ExitCode())
						}
					}
					channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
					channel.Close()
				}()
				queryForwardedAgent(sshConn, channel)
			})
		case "exec":
			var payload struct{ Command string }
			if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
				request.Reply(false, nil)
				continue
			}
			if scpOnlyEnabled() {
				// A server without SFTP: only the scp programs and their helper commands.
				if _, allowed := scpCommand(sftpRoot, payload.Command); !allowed {
					request.Reply(false, nil)
					continue
				}
				request.Reply(true, nil)
				log.Printf("scp-only exec %q", payload.Command)
				runScpOnly(sftpRoot, payload.Command, channel)
				return
			}
			ok := isStatsCommand(payload.Command)
			if !ok && os.Getenv("E2E_HANG_EXEC") == "1" {
				// A server whose commands are accepted and never answered, for
				// watching a client give up on a probe of its own: the session
				// must be left open rather than closed while the command runs.
				request.Reply(true, nil)
				log.Printf("hang-exec %q", payload.Command)
				select {}
			}
			if !ok && os.Getenv("E2E_ALLOW_EXEC") == "1" {
				// Opt-in, for checks of features that run a command on the server (such as
				// extracting an archive). The fixture already hands out a full shell on
				// loopback, so this adds no reach.
				request.Reply(true, nil)
				command := exec.Command("/bin/sh", "-c", payload.Command)
				if login := os.Getenv("E2E_LOGIN_SHELL"); login != "" {
					command.Env = append(os.Environ(), "SHELL="+login)
				}
				if ptyAsked {
					// Like sshd, a command asked for with a terminal runs on one: the
					// client's shell startup shim is such a command.
					command.Env = append(append(command.Env, "TERM="+term), env...)
					command.Env = append(command.Env, x11.Env()...)
					command.Dir = os.TempDir()
					var err error
					ptmx, err = pty.StartWithSize(command, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
					if err != nil {
						fmt.Fprintf(channel, "cannot start command: %v\r\n", err)
						channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{1}))
						channel.Close()
						return
					}
					go func() { io.Copy(ptmx, channel) }()
					io.Copy(channel, ptmx)
					status := uint32(0)
					if err := command.Wait(); err != nil {
						status = 1
						var exit *exec.ExitError
						if errors.As(err, &exit) {
							status = uint32(exit.ExitCode())
						}
					}
					channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
					channel.Close()
					return
				}
				command.Stdout, command.Stderr = channel, channel
				status := uint32(0)
				if err := command.Run(); err != nil {
					status = 1
					var exit *exec.ExitError
					if errors.As(err, &exit) {
						status = uint32(exit.ExitCode())
					}
				}
				channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				return
			}
			request.Reply(ok, nil)
			if !ok {
				continue
			}
			log.Printf("resource-stats")
			// Like the real monitor, it prints a reading and keeps printing until
			// the client closes the session.
			probe := gpuProbe(payload.Command)
			for second := 1; ; second++ {
				// With E2E_ALLOW_EXEC=1 the GPU section of the real monitor command
				// runs here, so a fake nvidia-smi on the fixture's PATH shows up.
				if probe != "" {
					out, _ := exec.Command("/bin/sh", "-c", probe).Output()
					if _, err := channel.Write(out); err != nil {
						return
					}
				}
				if _, err := io.WriteString(channel, statsReading(second)); err != nil {
					return
				}
				time.Sleep(200 * time.Millisecond)
			}
		case "subsystem":
			var payload struct{ Name string }
			ssh.Unmarshal(request.Payload, &payload)
			if payload.Name != "sftp" || scpOnlyEnabled() {
				request.Reply(false, nil)
				continue
			}
			// E2E_MAX_SFTP=n refuses an SFTP subsystem while n are open, as a server
			// whose MaxSessions is that small does.
			if !sftpSessionAllowed() {
				log.Printf("sftp subsystem refused")
				request.Reply(false, nil)
				continue
			}
			request.Reply(true, nil)
			log.Printf("sftp subsystem opened")
			server, err := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(sftpRoot))
			if err != nil {
				sftpSessionDone()
				return
			}
			_ = server.Serve()
			_ = server.Close()
			sftpSessionDone()
			log.Printf("sftp subsystem closed")
			return
		default:
			request.Reply(strings.HasPrefix(request.Type, "x-"), nil)
		}
	}
}

func isStatsCommand(command string) bool {
	return strings.Contains(command, "# motty-stats-v2")
}

// gpuProbe is the GPU section of the monitor command followed by a call to it,
// or "" unless E2E_ALLOW_EXEC=1. The rest of the command reads Linux procfs, so
// the fixture still prints its canned counters.
func gpuProbe(command string) string {
	if os.Getenv("E2E_ALLOW_EXEC") != "1" {
		return ""
	}
	begin, end := strings.Index(command, "# gpu-probe-begin"), strings.Index(command, "# gpu-probe-end")
	if begin < 0 || end < begin {
		return ""
	}
	return command[begin:end] + "\ngpu_probe\n"
}

// statsReading is the counters of a server that is 37.5% busy, uses 3 of 8 GiB
// and moves 128 KiB/s in and 64 KiB/s out, as they stand after the given second.
func statsReading(second int) string {
	return fmt.Sprintf("gop-stats-v2 %d %d %d %d 8388608 5242880 %d.00\n", 375*second, 1000*second, 131072*second, 65536*second, second)
}

type fixture struct {
	root         string
	sftpRoot     string
	identityPath string
	publicKey    ssh.PublicKey
}

func prepareFixture() (fixture, error) {
	root, err := os.MkdirTemp("", "motty-e2e-")
	if err != nil {
		return fixture{}, err
	}
	fail := func(err error) (fixture, error) {
		os.RemoveAll(root)
		return fixture{}, err
	}
	sftpRoot := filepath.Join(root, "sftp")
	if err := os.MkdirAll(filepath.Join(sftpRoot, "docs"), 0o755); err != nil {
		return fail(err)
	}
	files := map[string]string{
		"alpha.txt":                        "alpha from sftp\n",
		filepath.Join("docs", "guide.txt"): "guide from nested directory\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(sftpRoot, name), []byte(content), 0o644); err != nil {
			return fail(err)
		}
	}
	_, clientPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fail(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(clientPrivate, "motty e2e", []byte("key-secret"))
	if err != nil {
		return fail(err)
	}
	identityPath := filepath.Join(root, "id_ed25519")
	if err := os.WriteFile(identityPath, pem.EncodeToMemory(block), 0o600); err != nil {
		return fail(err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPrivate)
	if err != nil {
		return fail(err)
	}
	return fixture{root: root, sftpRoot: sftpRoot, identityPath: identityPath, publicKey: clientSigner.PublicKey()}, nil
}
