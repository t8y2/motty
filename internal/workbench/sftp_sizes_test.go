//go:build darwin || linux

package workbench

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

func execFindSizeCommand(command string) (sshconn.ExecResult, error) {
	output, err := exec.Command("/bin/sh", "-c", command).Output()
	if exit, ok := err.(*exec.ExitError); ok {
		return sshconn.ExecResult{Stdout: output, Stderr: exit.Stderr, ExitCode: exit.ExitCode()}, nil
	}
	return sshconn.ExecResult{Stdout: output}, err
}

func sftpTestSizeRecord(index, status int, report string) string {
	if report == "" {
		return fmt.Sprintf("\x1e%d %d -\n", index, status)
	}
	return fmt.Sprintf("\x1e%d %d %s\n", index, status, hex.EncodeToString([]byte(report)))
}

func newTestSizeTask(ctx context.Context, connection Connection) (*sftpSizeTask, map[string]sftpSizeResult) {
	results := make(map[string]sftpSizeResult)
	return &sftpSizeTask{
		ctx: ctx, connection: connection,
		post: func(fn func(*State)) bool {
			if ctx.Err() != nil {
				return false
			}
			fn(nil)
			return true
		},
		apply: func(found map[string]sftpSizeResult) {
			for name, result := range found {
				results[name] = result
			}
		},
	}, results
}

func localSizePath(dir, name string) string { return filepath.Join(dir, name) }

func TestSFTPSizeCommandPassesSpecialPathsAsArguments(t *testing.T) {
	root := t.TempDir()
	conn := &fakeConn{exec: func(_ context.Context, command string, _ int) (sshconn.ExecResult, error) {
		return execFindSizeCommand(command)
	}}
	task, results := newTestSizeTask(context.Background(), conn)
	task.walker = &sftpSizeWalker{local: true, join: localSizePath, incomplete: "%s (incomplete, %d)"}
	for _, name := range []string{"with space", "single'quote", "$(touch should-not-exist)", "new\nline", "-start"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, "two"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "two", "file"), []byte("payload"), 0o600); err != nil {
			t.Fatal(err)
		}
		task.names, task.roots = append(task.names, name), append(task.roots, dir)
	}
	task.run()
	if len(results) != len(task.roots) {
		t.Fatalf("got %d results for %d directories", len(results), len(task.roots))
	}
	for _, name := range task.names {
		result := results[name]
		if result.size != int64(len("payload")) || result.text != formatBytes(result.size) {
			t.Errorf("directory %q: result %+v", name, result)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "should-not-exist")); !os.IsNotExist(err) {
		t.Fatal("a directory name was intercepted as shell code")
	}
}

func TestSFTPSizeCommandBatchesAndFallsBack(t *testing.T) {
	for _, test := range []struct {
		name     string
		count    int
		rejected bool
	}{
		{name: "one command for a batch", count: 64},
		{name: "count limit splits the second batch", count: 65},
		{name: "no find uses walker", count: 2, rejected: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			conn := &fakeConn{exec: func(_ context.Context, command string, _ int) (sshconn.ExecResult, error) {
				if test.rejected {
					return sshconn.ExecResult{Stderr: []byte("not found"), ExitCode: 127}, nil
				}
				return execFindSizeCommand(command)
			}}
			task, results := newTestSizeTask(context.Background(), conn)
			task.walker = &sftpSizeWalker{local: true, join: localSizePath, incomplete: "%s (incomplete, %d)"}
			for i := range test.count {
				name := strconv.Itoa(i)
				dir := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Join(dir, "child"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "child", "first file"), []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "child", "second file"), []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
				task.names, task.roots = append(task.names, name), append(task.roots, dir)
			}
			task.run()
			if len(results) != len(task.roots) {
				t.Fatalf("got %d of %d root sizes", len(results), len(task.roots))
			}
			for _, name := range task.names {
				// Both the server helper and the SFTP fallback sum logical bytes,
				// never filesystem allocation blocks.
				if got := results[name]; got.size != int64(2*len(name)) {
					t.Errorf("%q result %+v", name, got)
				}
			}
			wantCommands := 1
			if !test.rejected {
				wantCommands = (test.count + sftpSizeBatchCount - 1) / sftpSizeBatchCount
			}
			if got := len(conn.commandCalls); got != wantCommands {
				t.Errorf("called Exec %d times, want %d", got, wantCommands)
			}
		})
	}
}

func TestSFTPSizeCommandMarksPartialResultsWithoutWalkingThem(t *testing.T) {
	conn := &fakeConn{exec: func(_ context.Context, command string, _ int) (sshconn.ExecResult, error) {
		return sshconn.ExecResult{Stdout: []byte(sftpTestSizeRecord(0, 0, "40\n") + sftpTestSizeRecord(1, 1, "7\n") + sftpTestSizeRecord(2, 1, "unreadable\n"))}, nil
	}}
	task, results := newTestSizeTask(context.Background(), conn)
	task.names, task.roots = []string{"fast", "partial", "fallback"}, []string{"/server/fast", "/server/partial", "/server/fallback"}
	reads := 0
	task.walker = &sftpSizeWalker{open: func() (sftpclient.Remote, error) {
		reads++
		return nil, errors.New("no readable tree")
	}, join: func(dir, name string) string { return path.Join(dir, name) }, incomplete: "%s (incomplete, %d)"}
	task.partial = "%s (some folders were unreadable)"
	task.run()
	if results["fast"].size != 40 || results["fast"].incomplete {
		t.Errorf("fast = %+v", results["fast"])
	}
	if results["partial"].size != 7 || !results["partial"].incomplete || !strings.Contains(results["partial"].text, "unreadable") {
		t.Errorf("partial = %+v", results["partial"])
	}
	if results["fallback"].incomplete || reads != 1 {
		t.Errorf("fallback = %+v, opened %d readers", results["fallback"], reads)
	}
}

func TestParseSFTPSizeResultsRejectsInvalidRecords(t *testing.T) {
	data := sftpTestSizeRecord(0, 0, "4\n") + sftpTestSizeRecord(1, 1, "7\n") + sftpTestSizeRecord(2, 1, "")
	valid, err := parseSFTPSizeResults(data, 3)
	if err != nil || valid[0].size != 4 || valid[1].size != 7 || !valid[1].incomplete || valid[2].size != -1 ||
		valid[2].text != "" {
		t.Fatalf("results = %+v, %v", valid, err)
	}
	for _, input := range []string{"\x1e0 4", "\x1e1 0 x\n", "\x1e0 0 0\nout\n", "\x1e0 x 0\n"} {
		if _, err := parseSFTPSizeResults(input, strings.Count(input, "\x1e")); err == nil {
			t.Errorf("invalid helper output was accepted: %q", input)
		}
	}
	if _, err := parseSFTPSizeResults("", 1); err == nil {
		t.Error("an empty count was supplied for a missing helper result")
	}
}

func TestSFTPDirectorySizePublishesLogicalBytesWithoutOpeningSFTP(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	session.host.System = sshconn.SystemUbuntu
	fake.exec = func(_ context.Context, command string, _ int) (sshconn.ExecResult, error) {
		return sshconn.ExecResult{Stdout: []byte(sftpTestSizeRecord(0, 0, "42\n"))}, nil
	}
	initialSFTPOpens := fake.sftpOpens
	state.calculateSFTPDirectorySize(session.files)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return view.rows[sftpRowIndex(t, view, "docs")].entry.Size == 42
	})
	if got := session.files.rows[sftpRowIndex(t, session.files, "docs")].cells[2]; got != formatBytes(42) {
		t.Fatalf("calculated cell = %q", got)
	}
	if got := len(fake.commandCalls); got != 1 || fake.sftpOpens != initialSFTPOpens {
		t.Fatalf("one remote batch opened %d SSH commands and %d SFTP channels", got, fake.sftpOpens)
	}
	view := session.files
	view.sudo = true
	if conn := sftpSizeExecutor(state, view, false); conn != nil {
		t.Fatal("sudo SFTP selected a command with the unprivileged SSH user")
	}
	if conn := sftpSizeExecutor(state, view, true); conn != nil {
		t.Fatal("local filesystem selected a server command")
	}
	session.host.System = sshconn.SystemUnknown
	view.sudo = false
	if conn := sftpSizeExecutor(state, view, false); conn != nil {
		t.Fatalf("unknown platform selected %T", conn)
	}
	session.host.System = sshconn.SystemWindows
	if conn := sftpSizeExecutor(state, view, false); conn != nil {
		t.Fatalf("Windows selected %T", conn)
	}
}

func FuzzParseSFTPSizeResults(f *testing.F) {
	for _, record := range []string{"", "\x1e0 0 -\n", "\x1e1 1 37320a\n", "\x1e1 0 " + strings.Repeat("f", 4*1024) + "\n"} {
		f.Add([]byte(record), byte(1))
	}
	f.Fuzz(func(t *testing.T, data []byte, count byte) {
		result, err := parseSFTPSizeResults(string(data), int(count))
		if err == nil && len(result) != int(count) {
			t.Fatalf("%d results for count %d", len(result), count)
		}
	})
}

func TestSFTPCancelledSizeDoesNotPublish(t *testing.T) {
	started := make(chan struct{})
	conn := &fakeConn{exec: func(ctx context.Context, _ string, _ int) (sshconn.ExecResult, error) {
		close(started)
		<-ctx.Done()
		return sshconn.ExecResult{}, ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	task, results := newTestSizeTask(ctx, conn)
	task.names, task.roots = []string{"slow"}, []string{"/slow"}
	done := make(chan struct{})
	go func() {
		task.run()
		close(done)
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not unblock the size command")
	}
	if len(results) != 0 {
		t.Fatalf("canceled task published %+v", results)
	}
}
