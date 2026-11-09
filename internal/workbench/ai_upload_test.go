package workbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

func TestSelectedUploadStreamsBinaryAndRefusesUnselectedOrChangedFiles(t *testing.T) {
	state, session, connection, _ := openTestSFTP(t)
	remote := &workbenchSFTPRemote{cwd: "/home/test", entries: map[string][]os.FileInfo{"/home/test": {}}, files: make(map[string][]byte)}
	connection.mu.Lock()
	connection.sftp = remote
	connection.mu.Unlock()
	backend, err := state.aiTargetBackend(aiSessionInfo(session))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.closeAll()
	content := bytes.Repeat([]byte{0, 255, 4, 1}, 256*1024)
	local := filepath.Join(t.TempDir(), "artifact.bin")
	if err := os.WriteFile(local, content, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(local)
	if err != nil {
		t.Fatal(err)
	}
	backend.uploads = map[string]aiUploadFile{"1": {path: local, name: info.Name(), size: info.Size(), modified: info.ModTime(), info: info}}
	args := tools.SelectedUploadArgs{FileID: "1", Session: aiSessionInfo(session).ID, RemotePath: "/home/test/artifact.bin", Existing: "fail"}
	if _, err := backend.UploadSelected(context.Background(), tools.SelectedUploadArgs{FileID: "not-selected", Session: args.Session, RemotePath: args.RemotePath}); err == nil {
		t.Fatal("unselected file was uploaded")
	}
	result := make(chan error, 1)
	go func() {
		transfer, err := backend.UploadSelected(context.Background(), args)
		if err == nil && (transfer.State != "completed" || transfer.BytesDone != int64(len(content))) {
			err = os.ErrInvalid
		}
		result <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
			goto uploaded
		default:
			if time.Now().After(deadline) {
				t.Fatal("upload timed out")
			}
			state.Wake()
			time.Sleep(time.Millisecond)
		}
	}
uploaded:
	remote.mu.Lock()
	got := bytes.Clone(remote.files[args.RemotePath])
	remote.mu.Unlock()
	if !bytes.Equal(got, content) {
		t.Fatal("remote binary bytes differ")
	}
	existing := &workbenchSFTPRemote{
		cwd: "/home/test", entries: map[string][]os.FileInfo{"/home/test": {workbenchFileInfo{name: "artifact.bin", size: 8, mode: 0o600}}},
		files: map[string][]byte{args.RemotePath: []byte("existing")},
	}
	connection.mu.Lock()
	connection.sftp = existing
	connection.mu.Unlock()
	failure := make(chan error, 1)
	go func() { _, err := backend.UploadSelected(context.Background(), args); failure <- err }()
	deadline = time.Now().Add(10 * time.Second)
	for {
		select {
		case err := <-failure:
			if !errors.Is(err, sftpclient.ErrDestinationExists) {
				t.Fatalf("existing remote destination was not protected: %v", err)
			}
			goto checkedConflict
		default:
			if time.Now().After(deadline) {
				t.Fatal("existing-destination check timed out")
			}
			state.Wake()
			time.Sleep(time.Millisecond)
		}
	}
checkedConflict:
	existing.mu.Lock()
	protected := bytes.Equal(existing.files[args.RemotePath], []byte("existing"))
	existing.mu.Unlock()
	if !protected {
		t.Fatal("existing remote file was overwritten")
	}
	replacement := filepath.Join(filepath.Dir(local), "replacement.bin")
	if err := os.WriteFile(replacement, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, local); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.UploadSelected(context.Background(), args); err == nil {
		t.Fatal("file replaced with matching size and timestamp reused a stale selection")
	}
	if err := os.WriteFile(local, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.UploadSelected(context.Background(), args); err == nil {
		t.Fatal("changed source reused a stale selection")
	}
}

type blockedUploadRemote struct {
	*workbenchSFTPRemote
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type blockedUploadFile struct {
	sftpclient.TransferFile
	remote *blockedUploadRemote
}

func (remote *blockedUploadRemote) OpenTransferFile(name string, flags int) (sftpclient.TransferFile, error) {
	file, err := remote.workbenchSFTPRemote.OpenTransferFile(name, flags)
	if err != nil {
		return nil, err
	}
	return &blockedUploadFile{TransferFile: file, remote: remote}, nil
}

func (file *blockedUploadFile) Write(data []byte) (int, error) {
	file.remote.once.Do(func() { close(file.remote.entered) })
	<-file.remote.release
	return 0, sftpclient.ErrCanceled
}

func (remote *blockedUploadRemote) Close() error {
	select {
	case <-remote.release:
	default:
		close(remote.release)
	}
	return remote.workbenchSFTPRemote.Close()
}

func TestSelectedUploadCancellationStopsTheWorker(t *testing.T) {
	state, session, connection, _ := openTestSFTP(t)
	remote := &blockedUploadRemote{
		workbenchSFTPRemote: &workbenchSFTPRemote{cwd: "/home/test", entries: map[string][]os.FileInfo{"/home/test": {}}, files: make(map[string][]byte)},
		entered:             make(chan struct{}), release: make(chan struct{}),
	}
	connection.mu.Lock()
	connection.sftp = remote
	connection.mu.Unlock()
	backend, err := state.aiTargetBackend(aiSessionInfo(session))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.closeAll()
	local := filepath.Join(t.TempDir(), "artifact.bin")
	if err := os.WriteFile(local, bytes.Repeat([]byte{0, 1}, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(local)
	if err != nil {
		t.Fatal(err)
	}
	backend.uploads = map[string]aiUploadFile{"1": {path: local, name: info.Name(), size: info.Size(), modified: info.ModTime(), info: info}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := &aiRun{cancel: func() {}}
	backend.uploadRun = run
	state.ai.run = run
	defer func() { state.ai.run = nil }()
	state.ai.items = append(state.ai.items, aiItem{kind: aiTool, call: llm.ToolCall{Name: tools.NameUploadSelected}, status: aiToolRunning})
	result := make(chan error, 1)
	go func() {
		_, err := backend.UploadSelected(ctx, tools.SelectedUploadArgs{FileID: "1", Session: aiSessionInfo(session).ID, RemotePath: "/home/test/artifact.bin", Existing: "fail"})
		result <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-remote.entered:
			progressDeadline := time.Now().Add(5 * time.Second)
			for state.ai.items[len(state.ai.items)-1].result == "" {
				if time.Now().After(progressDeadline) {
					t.Fatal("running upload has no visible progress")
				}
				state.Wake()
				time.Sleep(time.Millisecond)
			}
			if !strings.Contains(state.ai.items[len(state.ai.items)-1].result, " / ") {
				t.Fatalf("upload progress is not readable: %q", state.ai.items[len(state.ai.items)-1].result)
			}
			cancel()
			select {
			case err := <-result:
				if err != context.Canceled {
					t.Fatalf("cancellation: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("canceled upload did not finish")
			}
			return
		default:
			if time.Now().After(deadline) {
				t.Fatal("upload never started")
			}
			state.Wake()
			time.Sleep(time.Millisecond)
		}
	}
}

func TestAgentUploadsPickedBinaryOnlyAfterApproval(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn(tools.NameUploadSelected, `{"file_id":"1","remote_path":"/home/test/build.bin"}`)
		}
		for _, message := range request.Messages {
			if message.Role == llm.RoleTool && strings.Contains(message.Text, `"state":"completed"`) {
				return textTurn("Uploaded successfully")
			}
		}
		return textTurn("No confirmed transfer")
	}}
	state, _ := assistantTestSession(t, model)
	state.Paint(&gfx.Canvas{}, panelWide)
	state.Click(hitAIToggle)
	state.ai.mode = agent.ModeAgent
	remote := &workbenchSFTPRemote{cwd: "/home/test", entries: map[string][]os.FileInfo{"/home/test": {}}, files: make(map[string][]byte)}
	connection := state.sessions[0].conn.(*fakeConn)
	connection.mu.Lock()
	connection.sftp = remote
	connection.mu.Unlock()
	content := bytes.Repeat([]byte{0, 255, 6, 3}, 16*1024)
	selected := filepath.Join(t.TempDir(), "build.bin")
	if err := os.WriteFile(selected, content, 0o600); err != nil {
		t.Fatal(err)
	}
	state.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{selected}, nil }
	state.aiPickUploads()
	pumpUntil(t, state, "selected file", func() bool { return state.ai.attaching == 0 && len(state.ai.attachments) == 1 })
	ask(state, "Upload it to the SSH host", agent.ModeAgent)
	pumpUntil(t, state, "upload approval", func() bool { return len(state.ai.approvals) == 1 })
	if request := state.ai.approvals[0].request; request.Tool != tools.NameUploadSelected ||
		!strings.Contains(request.Summary, "build.bin -> /home/test/build.bin") ||
		request.Session != sessionHandle(state.sessions[0].ID) {
		t.Fatalf("upload approval hides its source or target: %+v", request)
	}
	remote.mu.Lock()
	_, uploadedEarly := remote.files["/home/test/build.bin"]
	remote.mu.Unlock()
	if uploadedEarly {
		t.Fatal("file uploaded before approval")
	}
	state.aiAnswerApproval(true)
	pumpUntil(t, state, "upload and answer", func() bool { return state.ai.run == nil })
	remote.mu.Lock()
	actual := bytes.Clone(remote.files["/home/test/build.bin"])
	remote.mu.Unlock()
	if !bytes.Equal(actual, content) || !strings.Contains(model.requests[1].Messages[len(model.requests[1].Messages)-1].Text, `"state":"completed"`) {
		t.Fatal("model reported upload without a completed binary transfer")
	}
}

func TestSelectedUploadRealSSH(t *testing.T) {
	endpoints := os.Getenv("MOTTY_TEST_SSH_ENDPOINTS")
	if endpoints == "" {
		t.Skip("set MOTTY_TEST_SSH_ENDPOINTS to test real SFTP connections")
	}
	for _, endpoint := range strings.Split(endpoints, ",") {
		t.Run(endpoint, func(t *testing.T) {
			host, portText, err := net.SplitHostPort(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.ParseUint(portText, 10, 16)
			if err != nil {
				t.Fatal(err)
			}
			conn := sshconn.Dial(sshconn.Config{Host: host, Port: uint16(port), User: os.Getenv("MOTTY_TEST_SSH_USER"), IdentityFiles: []string{os.Getenv("MOTTY_TEST_SSH_KEY")}, KnownHostsPath: os.Getenv("MOTTY_TEST_SSH_KNOWN_HOSTS"), DialTimeout: 5 * time.Second})
			defer conn.Close()
			deadline := time.Now().Add(10 * time.Second)
			for conn.Status().Phase != sshconn.PhaseConnected {
				if time.Now().After(deadline) {
					t.Fatalf("SSH connection did not open: %+v", conn.Status())
				}
				time.Sleep(time.Millisecond)
			}
			remote, err := openRemoteFiles(realConn{conn}, transferSFTP)
			if err != nil {
				t.Fatal(err)
			}
			cwd, err := remote.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			defer remote.Close()
			state, session, _, _ := openTestSFTP(t)
			backend, err := state.aiTargetBackend(aiSessionInfo(session))
			if err != nil {
				t.Fatal(err)
			}
			defer backend.closeAll()
			session.conn = realConn{conn}
			content := bytes.Repeat([]byte{0, 255, 4, 1}, 256*1024)
			local := filepath.Join(t.TempDir(), "artifact.bin")
			if err := os.WriteFile(local, content, 0o600); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(local)
			if err != nil {
				t.Fatal(err)
			}
			backend.uploads = map[string]aiUploadFile{"1": {path: local, name: info.Name(), size: info.Size(), modified: info.ModTime(), info: info}}
			destination := path.Join(cwd, fmt.Sprintf("motty-selected-upload-%d.bin", time.Now().UnixNano()))
			defer func() { _ = remote.(interface{ Remove(string) error }).Remove(destination) }()
			args := tools.SelectedUploadArgs{FileID: "1", Session: aiSessionInfo(session).ID, RemotePath: destination, Existing: "fail"}
			result := make(chan error, 1)
			go func() {
				transfer, err := backend.UploadSelected(context.Background(), args)
				if err == nil && (transfer.State != "completed" || transfer.BytesDone != int64(len(content))) {
					err = os.ErrInvalid
				}
				result <- err
			}()
			deadline = time.Now().Add(15 * time.Second)
			for {
				select {
				case err := <-result:
					if err != nil {
						t.Fatal(err)
					}
					goto uploaded
				default:
					if time.Now().After(deadline) {
						t.Fatal("SSH upload timed out")
					}
					state.Wake()
					time.Sleep(time.Millisecond)
				}
			}
		uploaded:
			reader, err := remote.Open(destination)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("remote artifact differs: %v", err)
			}
		})
	}
}
