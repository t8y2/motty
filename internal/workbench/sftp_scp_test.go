package workbench

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/scpclient"
	"motty/internal/sftpclient"
)

// scpServer is a server that has no SFTP and answers the few commands the SCP
// backend sends, speaking the SCP protocol for the transfers.
type scpServer struct {
	mu    sync.Mutex
	files map[string]string // by absolute path, all in /home/test
}

type scpWire struct {
	toClient   *io.PipeWriter
	clientRead *io.PipeReader
	fromClient *io.PipeReader
	clientOut  *io.PipeWriter
}

func (w *scpWire) Read(p []byte) (int, error)  { return w.clientRead.Read(p) }
func (w *scpWire) Write(p []byte) (int, error) { return w.clientOut.Write(p) }
func (w *scpWire) CloseWrite() error           { return w.clientOut.Close() }
func (w *scpWire) Wait() error                 { return nil }
func (w *scpWire) Stderr() string              { return "" }
func (w *scpWire) Close() error {
	w.clientOut.CloseWithError(io.ErrClosedPipe)
	w.clientRead.CloseWithError(io.ErrClosedPipe)
	return nil
}

func (s *scpServer) start(command string) (scpclient.Channel, error) {
	toClientR, toClientW := io.Pipe()
	fromClientR, fromClientW := io.Pipe()
	wire := &scpWire{toClient: toClientW, clientRead: toClientR, fromClient: fromClientR, clientOut: fromClientW}
	go func() {
		defer toClientW.Close()
		in := bufio.NewReader(fromClientR)
		quoted := ""
		if start := strings.Index(command, "'"); start >= 0 {
			quoted = command[start+1 : start+1+strings.Index(command[start+1:], "'")]
		}
		switch {
		case command == "pwd":
			io.WriteString(toClientW, "/home/test\n")
		case strings.HasPrefix(command, "LC_ALL=C ls"):
			s.mu.Lock()
			defer s.mu.Unlock()
			if strings.Contains(command, "-lAd") { // one entry, as ls -d prints it
				if content, ok := s.files[quoted]; ok {
					fmt.Fprintf(toClientW, "-rw-r--r-- 1 1000 1000 %d 1700000000 %s\n", len(content), quoted)
				}
				return
			}
			io.WriteString(toClientW, "total 0\n")
			for file, content := range s.files {
				if path.Dir(file) == quoted {
					fmt.Fprintf(toClientW, "-rw-r--r-- 1 1000 1000 %d 1700000000 %s\n", len(content), path.Base(file))
				}
			}
		case strings.HasPrefix(command, "scp -f"):
			in.ReadByte()
			s.mu.Lock()
			content, ok := s.files[quoted]
			s.mu.Unlock()
			if !ok {
				io.WriteString(toClientW, "\x01scp: "+quoted+": No such file or directory\n")
				return
			}
			fmt.Fprintf(toClientW, "C0644 %d %s\n", len(content), path.Base(quoted))
			in.ReadByte()
			io.WriteString(toClientW, content+"\x00")
			in.ReadByte()
		case strings.HasPrefix(command, "scp -t"):
			toClientW.Write([]byte{0})
			for {
				for {
					line, err := in.ReadString('\n')
					if err != nil {
						return
					}
					if strings.HasPrefix(line, "T") {
						toClientW.Write([]byte{0})
						continue
					}
					var mode, size int
					var name string
					fmt.Sscanf(line, "C%o %d %s", &mode, &size, &name)
					toClientW.Write([]byte{0})
					data := make([]byte, size+1)
					if _, err := io.ReadFull(in, data); err != nil {
						return
					}
					s.mu.Lock()
					s.files[quoted+"/"+name] = string(data[:size])
					s.mu.Unlock()
					toClientW.Write([]byte{0})
					break
				}
			}
		}
	}()
	return wire, nil
}

func openScpFallback(t *testing.T, mode transferMode, sftpErr error) (*State, *Session, *fakeConn, *scpServer) {
	t.Helper()
	state, _, fake, _ := liveSession(t)
	server := &scpServer{files: map[string]string{"/home/test/notes.txt": "hello world"}}
	fake.mu.Lock()
	fake.sftpErr = sftpErr
	fake.scp = sftpclient.NewSCP(server.start)
	fake.mu.Unlock()
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	session.host.Transfer = mode
	state.Click(hitSFTPOpen)
	t.Cleanup(func() { state.closeSFTP(session) })
	return state, session, fake, server
}

// A server without an SFTP subsystem still gets a working file panel: the
// listing, a download and an upload go over SCP, and the panel says the listing
// is approximate.
func TestFilePanelFallsBackToSCPWhenSFTPIsRefused(t *testing.T) {
	state, session, fake, server := openScpFallback(t, transferAuto, errors.New("sftp: start subsystem: ssh: subsystem request failed"))
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.rows) == 1 })
	view := session.files
	if view.path != "/home/test" || view.rows[0].entry.Name != "notes.txt" || view.status != fmt.Sprintf(state.messages.SCPItemsFmt, 1) {
		t.Fatalf("listing = %q %+v status %q error %q", view.path, view.rows, view.status, view.errorText)
	}

	target := filepath.Join(t.TempDir(), "notes.txt")
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return target, nil }
	state.Click(hitSFTPEntryList.At(0))
	state.Click(hitSFTPDownload)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
	})
	if content, err := os.ReadFile(target); err != nil || string(content) != "hello world" {
		t.Fatalf("download = %q, %v", content, err)
	}

	local := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(local, []byte("uploaded payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{local}, nil }
	state.Click(hitSFTPUpload)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "upload.bin") >= 0
	})
	server.mu.Lock()
	uploaded := server.files["/home/test/upload.bin"]
	server.mu.Unlock()
	if uploaded != "uploaded payload" {
		t.Fatalf("uploaded = %q", uploaded)
	}
	fake.mu.Lock()
	sftpOpens, scpOpens := fake.sftpOpens, fake.scpOpens
	fake.mu.Unlock()
	if sftpOpens != 1 || scpOpens != 1 {
		t.Fatalf("opens: sftp %d scp %d", sftpOpens, scpOpens)
	}
}

func TestFilePanelFallbackFollowsTheHostChoice(t *testing.T) {
	refused := errors.New("sftp: start subsystem: ssh: subsystem request failed")
	t.Run("SFTP only never falls back", func(t *testing.T) {
		state, session, fake, _ := openScpFallback(t, transferSFTP, refused)
		waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.errorText != "" })
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if fake.scpOpens != 0 {
			t.Fatal("SCP was used although the host asks for SFTP only")
		}
	})
	t.Run("SCP skips SFTP", func(t *testing.T) {
		state, session, fake, _ := openScpFallback(t, transferSCP, nil)
		waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.rows) == 1 })
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if fake.sftpOpens != 0 || fake.scpOpens != 1 {
			t.Fatalf("opens: sftp %d scp %d", fake.sftpOpens, fake.scpOpens)
		}
	})
	t.Run("a dead connection is not a missing subsystem", func(t *testing.T) {
		state, session, fake, _ := openScpFallback(t, transferAuto, errors.New("sftp: start subsystem: connection reset by peer"))
		waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.errorText != "" })
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if fake.scpOpens != 0 {
			t.Fatal("SCP was tried after a connection failure")
		}
	})
}

func TestHostTransferChoiceIsSaved(t *testing.T) {
	host := Host{Protocol: protocolSSH, Transfer: transferSCP}
	var restored Host
	if !restored.applyTransport(host.transport()) || restored.Transfer != transferSCP {
		t.Fatalf("restored = %+v", restored)
	}
	if (Host{}).transport().Transfer != "" {
		t.Fatal("the automatic choice must not be written")
	}
}
