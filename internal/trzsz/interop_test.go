//go:build !windows

package trzsz

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// These tests run the engine against the real trz and tsz programs, which sit
// at the other end of a transfer. They are skipped where those programs are not
// installed. MOTTY_TRZSZ_TSZ and MOTTY_TRZSZ_TRZ name a build that is not on
// PATH, so a local check can use one without changing the environment.

func referenceProgram(t *testing.T, name string) string {
	t.Helper()
	env := map[string]string{"tsz": "MOTTY_TRZSZ_TSZ", "trz": "MOTTY_TRZSZ_TRZ"}[name]
	if path := os.Getenv(env); path != "" {
		return path
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}
	return path
}

type peer struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	chunks chan []byte
	done   chan struct{}
	stderr bytes.Buffer
}

func startPeer(t *testing.T, dir, name string, args ...string) *peer {
	t.Helper()
	cmd := exec.Command(referenceProgram(t, name), args...)
	cmd.Dir = dir
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &peer{cmd: cmd, in: in, chunks: make(chan []byte, 64), done: make(chan struct{})}
	cmd.Stderr = &p.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(p.chunks)
		buffer := make([]byte, 4096)
		for {
			n, err := out.Read(buffer)
			if n > 0 {
				p.chunks <- append([]byte(nil), buffer[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
	})
	return p
}

// trigger waits for the announcement and returns it with the bytes that
// followed it.
func (p *peer) trigger(t *testing.T) (Trigger, []byte) {
	t.Helper()
	var buf []byte
	deadline := time.After(20 * time.Second)
	for {
		select {
		case chunk, ok := <-p.chunks:
			if !ok {
				t.Fatalf("the peer ended before announcing a transfer: %s", p.stderr.String())
			}
			buf = append(buf, chunk...)
			start, end, trigger, found, more := MatchTrigger(buf)
			if found {
				return trigger, buf[end:]
			}
			if more {
				buf = buf[start:]
			} else if len(buf) > TailSize {
				buf = buf[len(buf)-TailSize:]
			}
		case <-deadline:
			t.Fatalf("no announcement arrived: %s", p.stderr.String())
		}
	}
}

// pump moves bytes between the engine and the peer until the engine settles.
func (p *peer) pump(t *testing.T, engine *Transfer, initial []byte) {
	t.Helper()
	flush := func() {
		if out := engine.TakeOutput(nil); len(out) > 0 {
			if _, err := p.in.Write(out); err != nil {
				t.Logf("write to peer: %v", err)
			}
		}
	}
	engine.Feed(initial)
	flush()
	deadline := time.After(90 * time.Second)
	idle := time.NewTimer(10 * time.Second)
	defer idle.Stop()
	for !engine.Finished() || !engine.Settled() {
		select {
		case chunk, ok := <-p.chunks:
			if !ok {
				t.Fatalf("the peer ended early: %v (engine: %v, stderr: %s)", engine.Err(), engine.Err(), p.stderr.String())
			}
			engine.Feed(chunk)
			flush()
			idle.Reset(10 * time.Second)
		case <-deadline:
			t.Fatalf("transfer timed out: %v", engine.Err())
		case <-idle.C:
			t.Fatalf("transfer went quiet: %v", engine.Err())
		}
	}
	flush()
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the peer did not exit: %s", p.stderr.String())
	}
	if err := engine.Err(); err != nil {
		t.Fatalf("transfer failed: %v (stderr: %s)", err, p.stderr.String())
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func compareTree(t *testing.T, wantRoot, gotRoot string) {
	t.Helper()
	err := filepath.Walk(wantRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(wantRoot, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		got := filepath.Join(gotRoot, relative)
		if info.IsDir() {
			gotInfo, err := os.Stat(got)
			if err != nil {
				return fmt.Errorf("%s: %w", relative, err)
			}
			if !gotInfo.IsDir() {
				return fmt.Errorf("%s is not a directory", relative)
			}
			return nil
		}
		data, err := os.ReadFile(got)
		if err != nil {
			return fmt.Errorf("%s: %w", relative, err)
		}
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, want) {
			return fmt.Errorf("%s differs (%d bytes, want %d)", relative, len(data), len(want))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInteropReceivesFiles(t *testing.T) {
	remote := t.TempDir()
	one := bytes.Repeat([]byte("one "), 5000)
	two := []byte("two\n")
	writeFile(t, filepath.Join(remote, "one.txt"), one)
	writeFile(t, filepath.Join(remote, "two.txt"), two)
	writeFile(t, filepath.Join(remote, "empty.txt"), nil)

	local := t.TempDir()
	p := startPeer(t, remote, "tsz", "one.txt", "two.txt", "empty.txt")
	trigger, rest := p.trigger(t)
	engine, err := NewReceive(local, trigger)
	if err != nil {
		t.Fatal(err)
	}
	p.pump(t, engine, rest)
	compareTree(t, remote, local)
}

func TestInteropReceivesDirectory(t *testing.T) {
	remote := t.TempDir()
	writeFile(t, filepath.Join(remote, "tree", "a.txt"), []byte("a"))
	writeFile(t, filepath.Join(remote, "tree", "sub", "b.bin"), bytes.Repeat([]byte{7}, 3000))
	if err := os.MkdirAll(filepath.Join(remote, "tree", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	p := startPeer(t, remote, "tsz", "-d", "tree")
	trigger, rest := p.trigger(t)
	engine, err := NewReceive(local, trigger)
	if err != nil {
		t.Fatal(err)
	}
	p.pump(t, engine, rest)
	compareTree(t, filepath.Join(remote, "tree"), filepath.Join(local, "tree"))
}

func TestInteropSendsFiles(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "one.txt"), bytes.Repeat([]byte("x"), 200000))
	writeFile(t, filepath.Join(source, "two.bin"), []byte{0, 1, 2, 3})
	writeFile(t, filepath.Join(source, "empty.txt"), nil)
	remote := t.TempDir()
	var files []SendFile
	for _, name := range []string{"one.txt", "two.bin", "empty.txt"} {
		info, err := os.Stat(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, SendFile{
			Path: filepath.Join(source, name), Rel: []string{name}, Size: info.Size(),
			Mode: uint32(info.Mode().Perm()), ModTime: info.ModTime(),
		})
	}
	p := startPeer(t, remote, "trz")
	trigger, rest := p.trigger(t)
	engine, err := NewSend(files, trigger)
	if err != nil {
		t.Fatal(err)
	}
	p.pump(t, engine, rest)
	compareTree(t, source, remote)
}

func TestInteropSendsDirectory(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "tree", "a.txt"), []byte("alpha"))
	writeFile(t, filepath.Join(source, "tree", "sub", "b.bin"), bytes.Repeat([]byte{9}, 4000))
	if err := os.MkdirAll(filepath.Join(source, "tree", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := ListDirectory(filepath.Join(source, "tree"))
	if err != nil {
		t.Fatal(err)
	}
	remote := t.TempDir()
	p := startPeer(t, remote, "trz", "-d")
	trigger, rest := p.trigger(t)
	engine, err := NewSend(entries, trigger)
	if err != nil {
		t.Fatal(err)
	}
	p.pump(t, engine, rest)
	compareTree(t, filepath.Join(source, "tree"), filepath.Join(remote, "tree"))
}
