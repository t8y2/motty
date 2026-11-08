package workbench

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"motty/internal/ai/gate"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
	"motty/internal/ai/tools"
)

func TestAgentDirectoryTransfersAndShutdown(t *testing.T) {
	t.Run("recursive directory", func(t *testing.T) {
		world := newAgentWorld(t)
		backend, root := transferTestBackend(t, world)
		source := filepath.Join(root, "source")
		if err := os.MkdirAll(filepath.Join(source, "nested"), 0o700); err != nil {
			t.Fatal(err)
		}
		payload := []byte{0, 255, 3, 2, 1}
		if err := os.WriteFile(filepath.Join(source, "nested", "data.bin"), payload, 0o600); err != nil {
			t.Fatal(err)
		}
		job, err := backend.StartTransfer(context.Background(), tools.TransferArgs{LocalPath: source, RemotePath: "tree", Recursive: true, Existing: "fail"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if result := waitAgentTransfer(t, backend, job.ID); result.State != "completed" {
			t.Fatalf("directory upload: %+v", result)
		}
		target := filepath.Join(root, "target")
		job, err = backend.StartTransfer(context.Background(), tools.TransferArgs{LocalPath: target, RemotePath: "tree", Recursive: true, Existing: "fail"}, false)
		if err != nil {
			t.Fatal(err)
		}
		if result := waitAgentTransfer(t, backend, job.ID); result.State != "completed" {
			t.Fatalf("directory download: %+v", result)
		}
		actual, err := os.ReadFile(filepath.Join(target, "nested", "data.bin"))
		if err != nil || !bytes.Equal(actual, payload) {
			t.Fatalf("directory content: %v", err)
		}
	})
	t.Run("shutdown cancels establishment", func(t *testing.T) {
		world := newAgentWorld(t)
		world.server.stallSFTP = make(chan struct{}, 1)
		backend, root := transferTestBackend(t, world)
		job, err := backend.StartTransfer(context.Background(), tools.TransferArgs{LocalPath: filepath.Join(root, "file"), RemotePath: "file", Existing: "fail"}, false)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-world.server.stallSFTP:
		case <-time.After(5 * time.Second):
			t.Fatal("no subsystem request")
		}
		closed := make(chan struct{})
		go func() { backend.Close(); close(closed) }()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("shutdown left a stuck transfer")
		}
		if result := waitAgentTransfer(t, backend, job.ID); result.State != "canceled" {
			t.Fatalf("shutdown result: %+v", result)
		}
	})
}

func TestAgentMCPTransferSurvivesTheStartReplyAndRecordsCompletion(t *testing.T) {
	world := newAgentWorld(t)
	root := t.TempDir()
	world.options.TransferRoots = []string{root}
	saveAgentState(t, world, true)
	database, err := store.Open(world.options.AIDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetSetting(agentModeSetting, AgentModeAllowAll); err != nil {
		t.Fatal(err)
	}
	closeAgentTestResource(t, database)
	payload := bytes.Repeat([]byte{1, 255, 0, 8}, 100000)
	local := filepath.Join(root, "binary.bin")
	validated, validationErr := validateTransferRoots(world.options.TransferRoots)
	if validationErr != nil {
		t.Fatal(validationErr)
	}
	check := &agentBackend{transferRoots: validated}
	if _, err := check.transferPath(local, false, false); err != nil {
		t.Fatalf("normalized roots %q do not accept local path %q: %v", validated, local, err)
	}
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	input, requests := io.Pipe()
	responses, output := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		serveErr := ServeAgentMCP(ctx, input, output, world.options)
		done <- errors.Join(serveErr, output.Close())
	}()
	defer func() { cancel(); closeAgentTestResource(t, requests); closeAgentTestResource(t, responses) }()
	reader := bufio.NewReader(responses)
	sequence := 0
	call := func(tool string, arguments any) json.RawMessage {
		t.Helper()
		sequence++
		request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": sequence, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": arguments}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := requests.Write(append(request, '\n')); err != nil {
			t.Fatal(err)
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var reply struct {
			Result struct {
				IsError    bool            `json:"isError"`
				Structured json.RawMessage `json:"structuredContent"`
			}
			Error json.RawMessage
		}
		if err := json.Unmarshal(line, &reply); err != nil || reply.Result.IsError || len(reply.Error) > 0 {
			t.Fatalf("MCP reply %s: %v", line, err)
		}
		return reply.Result.Structured
	}
	var started gate.Transfer
	if err := json.Unmarshal(call(tools.NameSFTPUpload, tools.TransferArgs{LocalPath: local, RemotePath: "wire.bin", Existing: "fail"}), &started); err != nil {
		t.Fatal(err)
	}
	if started.ID == "" || started.State != "queued" {
		t.Fatalf("start falsely reports completion: %+v", started)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var status struct {
			Transfers []gate.Transfer `json:"transfers"`
		}
		if err := json.Unmarshal(call(tools.NameTransferStatus, tools.TransferQuery{TransferID: started.ID}), &status); err != nil {
			t.Fatal(err)
		}
		if len(status.Transfers) != 1 {
			t.Fatal("missing transfer")
		}
		if status.Transfers[0].State == "completed" {
			break
		}
		if status.Transfers[0].State == "failed" || time.Now().After(deadline) {
			t.Fatalf("transfer: %+v", status)
		}
		time.Sleep(5 * time.Millisecond)
	}
	closeAgentTestResource(t, requests)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("MCP did not stop at EOF")
	}
	actual, err := os.ReadFile(filepath.Join(world.server.root, "wire.bin"))
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatalf("protocol transfer bytes differ: %v", err)
	}
	database, err = store.Open(world.options.AIDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAgentTestResource(t, database)
	entries, err := database.Audit(100)
	if err != nil {
		t.Fatal(err)
	}
	queued, finished := false, false
	for _, entry := range entries {
		if entry.Tool == "transfer_queued" && strings.Contains(entry.Summary, started.ID) {
			queued = true
		}
		if entry.Tool == "transfer_finished" && strings.Contains(entry.Summary, started.ID) && entry.Outcome == store.AuditOK {
			finished = true
		}
	}
	if !queued || !finished {
		t.Fatalf("missing lifecycle audit: %+v", entries)
	}
}

func TestAgentTransferRefusesAnUnavailableAuditLog(t *testing.T) {
	world := newAgentWorld(t)
	backend, root := transferTestBackend(t, world)
	database, err := store.Open(world.options.AIDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	closeAgentTestResource(t, database)
	backend.auditLog = database
	_, err = backend.StartTransfer(context.Background(), tools.TransferArgs{LocalPath: filepath.Join(root, "file"), RemotePath: "file", Existing: "fail"}, false)
	if err == nil || !strings.Contains(err.Error(), "nothing was started") {
		t.Fatalf("unaudited transfer: %v", err)
	}
	if connections, _ := world.server.counts(); connections != 0 {
		t.Fatal("unaudited transfer reached host")
	}
}

func transferTestBackend(t *testing.T, world *agentWorld) (*agentBackend, string) {
	t.Helper()
	root := t.TempDir()
	world.options.TransferRoots = []string{root}
	backend := world.backend()
	backend.allowWrites = true
	return backend, root
}

func waitAgentTransfer(t *testing.T, backend *agentBackend, id string) gate.Transfer {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		values, err := backend.Transfers(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		value := values[0]
		if value.State == "completed" || value.State == "failed" || value.State == "canceled" {
			return value
		}
		select {
		case <-deadline.C:
			t.Fatalf("transfer did not settle: %+v", value)
		case <-ticker.C:
		}
	}
}

func TestAgentSettledTransferReleasesItsPaths(t *testing.T) {
	world := newAgentWorld(t)
	backend, root := transferTestBackend(t, world)
	payload := []byte("x")
	local := filepath.Join(root, "settled.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := backend.StartTransfer(ctx, tools.TransferArgs{LocalPath: local, RemotePath: "settled.bin", Existing: "replace"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result := waitAgentTransfer(t, backend, first.ID); result.State != "completed" {
		t.Fatalf("upload: %+v", result)
	}

	// A caller that has been told "completed" must not be told the same path is
	// still busy. The finishing goroutine publishes the terminal state before it
	// records the audit and marks the transfer done, so that gap is reachable:
	// hold the task in exactly that shape and try to reuse the path.
	backend.mu.Lock()
	var task *agentTransfer
	for _, candidate := range backend.transfers {
		if candidate.value.ID == first.ID {
			task = candidate
		}
	}
	backend.mu.Unlock()
	if task == nil {
		t.Fatalf("transfer %s is not in the history", first.ID)
	}
	task.mu.Lock()
	terminal := task.value.State
	task.done = false // the window the finishing goroutine can be in
	task.mu.Unlock()

	second, err := backend.StartTransfer(ctx, tools.TransferArgs{LocalPath: local, RemotePath: "settled.bin", Existing: "replace"}, true)
	if err != nil {
		t.Fatalf("a transfer that already reports %q still held its path: %v", terminal, err)
	}
	if result := waitAgentTransfer(t, backend, second.ID); result.State != "completed" {
		t.Fatalf("second upload: %+v", result)
	}
}

func TestAgentFileTransferBinaryRoundTripAndNoOverwrite(t *testing.T) {
	world := newAgentWorld(t)
	backend, root := transferTestBackend(t, world)
	payload := bytes.Repeat([]byte{0, 1, 255, 254, 128, 42, 10}, 180000)
	local := filepath.Join(root, "source.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	upload, err := backend.StartTransfer(ctx, tools.TransferArgs{LocalPath: local, RemotePath: "binary.bin", Existing: "fail"}, true)
	if err != nil {
		t.Fatal(err)
	}
	result := waitAgentTransfer(t, backend, upload.ID)
	if result.State != "completed" || result.BytesDone != int64(len(payload)) {
		t.Fatalf("upload: %+v", result)
	}
	remoteBytes, err := os.ReadFile(filepath.Join(world.server.root, "binary.bin"))
	if err != nil || !bytes.Equal(remoteBytes, payload) {
		t.Fatalf("remote binary mismatch: %v", err)
	}
	target := filepath.Join(root, "download.bin")
	download, err := backend.StartTransfer(ctx, tools.TransferArgs{LocalPath: target, RemotePath: "binary.bin", Existing: "fail"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result := waitAgentTransfer(t, backend, download.ID); result.State != "completed" {
		t.Fatalf("download: %+v", result)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("local binary mismatch: %v", err)
	}
	if err := os.WriteFile(local, []byte("must not overwrite"), 0o600); err != nil {
		t.Fatal(err)
	}
	conflict, err := backend.StartTransfer(ctx, tools.TransferArgs{LocalPath: local, RemotePath: "binary.bin", Existing: "fail"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result := waitAgentTransfer(t, backend, conflict.ID); result.State != "failed" {
		t.Fatalf("expected conflict: %+v", result)
	}
	got, err = os.ReadFile(filepath.Join(world.server.root, "binary.bin"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("conflict changed destination: %v", err)
	}
}

func TestAgentFileRangesPaginationAndChecksums(t *testing.T) {
	world := newAgentWorld(t)
	backend := world.backend()
	payload := append(bytes.Repeat([]byte("0123456789"), 40000), 0xff, 0xfe)
	if err := os.WriteFile(filepath.Join(world.server.root, "log.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, test := range []struct {
		name   string
		offset int64
		tail   bool
		count  int
	}{
		{"full allowance", 0, false, 262144}, {"offset", 100000, false, 20}, {"tail", 0, true, 20}, {"past EOF", int64(len(payload) + 100), false, 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := backend.ReadRange(ctx, tools.FileReadArgs{Path: "log.bin", MaxBytes: test.count, Offset: test.offset, Tail: test.tail, Encoding: "base64"})
			if err != nil {
				t.Fatal(err)
			}
			start := min(int(test.offset), len(payload))
			if test.tail {
				start = len(payload) - test.count
			}
			end := min(start+test.count, len(payload))
			data, err := base64.StdEncoding.DecodeString(result.Data)
			if err != nil || !bytes.Equal(data, payload[start:end]) || result.BytesRead != end-start || result.Truncated != (end < len(payload)) {
				t.Fatalf("range: %+v %v", result, err)
			}
		})
	}
	for _, algorithm := range []string{"sha256", "sha512"} {
		result, err := backend.ChecksumFile(ctx, tools.FileChecksumArgs{Path: "log.bin", Algorithm: algorithm})
		if err != nil {
			t.Fatal(err)
		}
		digest256, digest512 := sha256.Sum256(payload), sha512.Sum512(payload)
		want := hex.EncodeToString(digest256[:])
		if algorithm == "sha512" {
			want = hex.EncodeToString(digest512[:])
		}
		if result.Digest != want || result.Bytes != int64(len(payload)) {
			t.Fatalf("checksum: %+v", result)
		}
	}
	for index := 0; index < 505; index++ {
		if err := os.WriteFile(filepath.Join(world.server.root, fmt.Sprintf("entry-%03d", index)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	page, err := backend.ListFiles(ctx, tools.FileListArgs{Path: ".", Offset: 500, Limit: 3})
	if err != nil || page.Total != 506 || len(page.Entries) != 3 || page.Entries[0].Name != "entry-500" || page.NextOffset != 503 || !page.HasMore {
		t.Fatalf("page: %+v %v", page, err)
	}
	last, err := backend.ListFiles(ctx, tools.FileListArgs{Path: ".", Offset: page.NextOffset, Limit: 3})
	if err != nil || last.HasMore || last.Entries[len(last.Entries)-1].Name != "log.bin" {
		t.Fatalf("last page: %+v %v", last, err)
	}
	stat, err := backend.StatFile(ctx, tools.SFTPListArgs{Path: "log.bin"})
	if err != nil || stat.Size != int64(len(payload)) || stat.Directory || stat.Mode == "" || stat.ModifiedAt == "" {
		t.Fatalf("stat: %+v %v", stat, err)
	}
}

func TestAgentFileTransferCancellationEndsStalledSFTPButNotCommands(t *testing.T) {
	world := newAgentWorld(t)
	world.server.stallSFTP = make(chan struct{}, 1)
	backend, root := transferTestBackend(t, world)
	job, err := backend.StartTransfer(context.Background(), tools.TransferArgs{LocalPath: filepath.Join(root, "download"), RemotePath: "file", Existing: "fail"}, false)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-world.server.stallSFTP:
	case <-time.After(5 * time.Second):
		t.Fatal("SFTP was never requested")
	}
	if _, err := backend.StartTransfer(context.Background(), tools.TransferArgs{LocalPath: filepath.Join(root, "download"), RemotePath: "another-file", Existing: "fail"}, false); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("accepted concurrent writes to the same destination: %v", err)
	}
	canceled, err := backend.CancelTransfer(context.Background(), job.ID)
	if err != nil || !canceled.CancelRequested || canceled.State != "canceling" {
		t.Fatalf("cancel: %+v %v", canceled, err)
	}
	if result := waitAgentTransfer(t, backend, job.ID); result.State != "canceled" {
		t.Fatalf("settled: %+v", result)
	}
	result, err := backend.ExecCommand(context.Background(), tools.HostExecArgs{Command: "echo hello", TimeoutSeconds: 5})
	if err != nil || result.Stdout != "hello\n" {
		t.Fatalf("unrelated command: %+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, "download")); !os.IsNotExist(err) {
		t.Fatalf("canceled task created destination: %v", err)
	}
}

func TestAgentFileTransfersRefuseUnauthorizedPathsHostsAndModes(t *testing.T) {
	world := newAgentWorld(t)
	backend, root := transferTestBackend(t, world)
	ctx := context.Background()
	for _, test := range []struct{ name, path, host string }{
		{"outside", filepath.Join(t.TempDir(), "file"), "host_web"}, {"sibling prefix", root + "-other/file", "host_web"}, {"other host", filepath.Join(root, "file"), "host_hidden"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := backend.StartTransfer(ctx, tools.TransferArgs{LocalPath: test.path, RemotePath: "file", Session: test.host, Existing: "fail"}, false); err == nil {
				t.Fatal("unauthorized transfer accepted")
			}
		})
	}
	backend.allowWrites = false
	if _, err := backend.StartTransfer(ctx, tools.TransferArgs{LocalPath: filepath.Join(root, "file"), RemotePath: "file", Existing: "fail"}, false); err == nil {
		t.Fatal("read-only backend wrote a local file")
	}
	if connections, _ := world.server.counts(); connections != 0 {
		t.Fatal("refused transfer still connected")
	}
	backend.allowWrites = true
	backend.Close()
	if _, err := backend.StartTransfer(ctx, tools.TransferArgs{LocalPath: filepath.Join(root, "file"), RemotePath: "file", Existing: "fail"}, false); err == nil {
		t.Fatal("closed backend started transfer")
	}
}

func TestAgentLocalTransferRootsRejectSymlinks(t *testing.T) {
	world := newAgentWorld(t)
	backend, root := transferTestBackend(t, world)
	link := filepath.Join(root, "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := backend.transferPath(filepath.Join(link, "file"), false, false); err == nil {
		t.Fatal("followed symlink outside allowed root")
	}
}

func TestAgentMCPStructuredCommandAndFileResults(t *testing.T) {
	world := newAgentWorld(t)
	saveAgentState(t, world, true)
	database, err := store.Open(world.options.AIDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.SetSetting(agentModeSetting, AgentModeAllowAll); err != nil {
		t.Fatal(err)
	}
	closeAgentTestResource(t, database)
	replies := serveAgent(t, world.options, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"terminal_exec","arguments":{"command":"fail"}}}`)
	var response struct {
		Result struct {
			Structured gate.CommandResult `json:"structuredContent"`
			IsError    bool               `json:"isError"`
		}
	}
	if err := json.Unmarshal([]byte(replies[1]), &response); err != nil {
		t.Fatal(err)
	}
	result := response.Result.Structured
	if result.Stdout != "partial\n" || result.Stderr != "boom\n" || result.ExitCode == nil || *result.ExitCode != 3 || response.Result.IsError {
		t.Fatalf("structured command: %+v", response)
	}
	if err := os.WriteFile(filepath.Join(world.server.root, "range.bin"), []byte{0, 255, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	replies = serveAgent(t, world.options, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sftp_read","arguments":{"path":"range.bin","offset":1,"max_bytes":3,"encoding":"base64"}}}`)
	var fileReply struct {
		Result struct {
			Structured gate.FileRange `json:"structuredContent"`
			IsError    bool           `json:"isError"`
		}
	}
	if err := json.Unmarshal([]byte(replies[2]), &fileReply); err != nil {
		t.Fatal(err)
	}
	if fileReply.Result.IsError || fileReply.Result.Structured.Data != base64.StdEncoding.EncodeToString([]byte{255, 1, 2}) || fileReply.Result.Structured.NextOffset != 4 {
		t.Fatalf("file MCP reply: %+v", fileReply)
	}
	backend := world.backend()
	result, err = backend.ExecCommand(context.Background(), tools.HostExecArgs{Command: "hang", TimeoutSeconds: 1})
	if err == nil || !result.TimedOut || result.ExitCode != nil || result.Stdout != "started\n" {
		t.Fatalf("timeout lost output or invented exit status: %+v %v", result, err)
	}
	readOnly := gate.Gate{Backend: backend, Mode: gate.ModeObserve, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.Low}}
	if result := readOnly.Run(context.Background(), tools.NameSFTPDownload, []byte(`{}`)); !result.Denied {
		t.Fatalf("download not denied: %+v", result)
	}
}

func TestAgentCwdQuotesPathsAndNeverRunsAfterAFailedCd(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell unavailable")
	}
	dir := filepath.Join(t.TempDir(), "space and ' quote")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	probe := exec.Command(shell, "-c", "pwd -P")
	probe.Dir = dir
	output, err := probe.Output()
	if err != nil {
		t.Fatal(err)
	}
	remoteDir := strings.TrimSpace(string(output))
	command := exec.Command(shell, "-c", agentCommandAt("pwd -P; printf second", remoteDir))
	output, err = command.Output()
	if err != nil || string(output) != remoteDir+"\nsecond" {
		t.Fatalf("cwd: %q %v", output, err)
	}
	command = exec.Command(shell, "-c", agentCommandAt("printf must-not-run; printf nor-this", remoteDir+"/missing"))
	output, err = command.Output()
	if err == nil || len(output) != 0 {
		t.Fatalf("commands ran after cd failure: %q %v", output, err)
	}
}

func TestAgentSCPTransferUsesRealSSHAndSCPWireBytes(t *testing.T) {
	world := newAgentWorld(t)
	world.server.refuseSFTP = true
	files := &scpServer{files: map[string]string{}}
	world.server.commandHandler = func(channel ssh.Channel, command string, closed <-chan struct{}) {
		wire, err := files.start(command)
		if err != nil {
			t.Error(err)
			closeAgentTestResource(t, channel)
			return
		}
		defer closeAgentTestResource(t, wire)
		copied := make(chan error, 1)
		go func() {
			_, copyErr := io.Copy(wire, channel)
			copied <- errors.Join(copyErr, wire.CloseWrite())
		}()
		if _, err := io.Copy(channel, wire); err != nil {
			t.Errorf("SCP output copy: %v", err)
		}
		if _, err := channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0})); err != nil {
			t.Errorf("SCP exit status: %v", err)
		}
		closeAgentTestResource(t, channel)
		if err := <-copied; err != nil {
			t.Errorf("SCP input copy: %v", err)
		}
	}
	backend, root := transferTestBackend(t, world)
	payload := bytes.Repeat([]byte{0, 255, 128, 10, 3}, 70000)
	local := filepath.Join(root, "source.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	job, err := backend.StartTransfer(context.Background(), tools.TransferArgs{LocalPath: local, RemotePath: "/home/test/binary.bin", Existing: "fail"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result := waitAgentTransfer(t, backend, job.ID); result.State != "completed" || result.Protocol != "scp" {
		t.Fatalf("SCP upload: %+v", result)
	}
	files.mu.Lock()
	remoteBytes := []byte(files.files["/home/test/binary.bin"])
	files.mu.Unlock()
	if !bytes.Equal(remoteBytes, payload) {
		t.Fatal("SCP remote bytes differ")
	}
	target := filepath.Join(root, "copy.bin")
	job, err = backend.StartTransfer(context.Background(), tools.TransferArgs{LocalPath: target, RemotePath: "/home/test/binary.bin", Existing: "fail"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result := waitAgentTransfer(t, backend, job.ID); result.State != "completed" {
		t.Fatalf("SCP download: %+v", result)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("SCP local bytes differ: %v", err)
	}
	job, err = backend.StartTransfer(context.Background(), tools.TransferArgs{LocalPath: target, RemotePath: "/home/test/binary.bin", Existing: "resume"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result := waitAgentTransfer(t, backend, job.ID); result.State != "failed" || !strings.Contains(result.Error, "does not support resume") {
		t.Fatalf("SCP resume silently accepted: %+v", result)
	}
}

func closeAgentTestResource(t *testing.T, resource io.Closer) {
	t.Helper()
	if err := resource.Close(); err != nil && !errors.Is(err, io.EOF) {
		t.Errorf("close test resource: %v", err)
	}
}
