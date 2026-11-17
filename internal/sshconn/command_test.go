package sshconn

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func connectedCommandHarness(t *testing.T, command func(string, ssh.Channel)) *harness {
	t.Helper()
	server := startServer(t, func(server *testServer) {
		server.password = "pw"
		server.command = command
	})
	harness := dial(t, server, nil)
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	harness.phase(PhasePasswordPrompt)
	harness.conn.SubmitPassword("pw")
	harness.phase(PhaseConnected)
	return harness
}

func TestRunCommandReturnsBoundedStdout(t *testing.T) {
	harness := connectedCommandHarness(t, func(command string, channel ssh.Channel) {
		if command != "collect" {
			t.Errorf("command = %q", command)
		}
		_, _ = io.WriteString(channel, "0123456789")
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		_ = channel.Close()
	})

	output, err := harness.conn.RunCommand(context.Background(), "collect", 32)
	if err != nil || string(output) != "0123456789" {
		t.Fatalf("output = %q, err = %v", output, err)
	}
	output, err = harness.conn.RunCommand(context.Background(), "collect", 4)
	if !errors.Is(err, ErrCommandOutputTooLarge) || string(output) != "0123" {
		t.Fatalf("bounded output = %q, err = %v", output, err)
	}
}

func TestRunCommandReportsRemoteExitStatus(t *testing.T) {
	harness := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		_, _ = io.WriteString(channel, "partial")
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{7}))
		_ = channel.Close()
	})
	output, err := harness.conn.RunCommand(context.Background(), "fail", 32)
	var exit *ssh.ExitError
	if string(output) != "partial" || !errors.As(err, &exit) || exit.ExitStatus() != 7 {
		t.Fatalf("output = %q, err = %v", output, err)
	}
}

func TestRunCommandCancellationClosesOnlyItsSession(t *testing.T) {
	started := make(chan struct{})
	harness := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		close(started)
		_, _ = io.Copy(io.Discard, channel)
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := harness.conn.RunCommand(ctx, "wait", 32)
		result <- err
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled command did not return")
	}
	if harness.conn.Status().Phase != PhaseConnected {
		t.Fatalf("connection phase = %v", harness.conn.Status().Phase)
	}
}

func TestRunCommandWithCanceledContextNeverReachesTheServer(t *testing.T) {
	reached := make(chan struct{}, 1)
	harness := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		reached <- struct{}{}
		_ = channel.Close()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := harness.conn.RunCommand(ctx, "never", 32); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	select {
	case <-reached:
		t.Fatal("canceled command reached the server")
	case <-time.After(10 * time.Millisecond):
	}
}

func TestRunCommandNeedsAConnectedConnAndValidLimits(t *testing.T) {
	conn := &Conn{}
	for _, test := range []struct {
		command string
		limit   int
	}{
		{"", 1},
		{"true", 0},
		{"true", 1},
	} {
		if _, err := conn.RunCommand(context.Background(), test.command, test.limit); err == nil {
			t.Fatalf("RunCommand(%q, %d) succeeded", test.command, test.limit)
		}
	}
}

// A streamed command's lines reach the caller while the command is still
// running, and stopping from the caller's side ends the server's command.
func TestStreamCommandDeliversLinesAsTheyArriveAndEndsTheCommand(t *testing.T) {
	ended := make(chan struct{})
	harness := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		_, _ = io.WriteString(channel, "one\ntwo\n")
		_, _ = io.Copy(io.Discard, channel)
		close(ended)
	})
	var lines []string
	err := harness.conn.StreamCommand(context.Background(), "watch", 32, func(line []byte) bool {
		lines = append(lines, string(line))
		return len(lines) < 2
	})
	if err != nil || len(lines) != 2 || lines[0] != "one\n" || lines[1] != "two\n" {
		t.Fatalf("lines = %q, err = %v", lines, err)
	}
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("the server's command kept running after the caller stopped")
	}
}

func TestStreamCommandCancellationAndLineLimit(t *testing.T) {
	started := make(chan struct{})
	harness := connectedCommandHarness(t, func(command string, channel ssh.Channel) {
		if command == "long" {
			_, _ = io.WriteString(channel, "0123456789abcdefghijklmnopqrstuvwxyz0123456789\n")
		} else {
			close(started)
		}
		_, _ = io.Copy(io.Discard, channel)
	})
	err := harness.conn.StreamCommand(context.Background(), "long", 16, func([]byte) bool { return true })
	if !errors.Is(err, ErrCommandOutputTooLarge) {
		t.Fatalf("long line error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- harness.conn.StreamCommand(ctx, "wait", 16, func([]byte) bool { return true })
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled stream did not return")
	}
	if harness.conn.Status().Phase != PhaseConnected {
		t.Fatalf("connection phase = %v", harness.conn.Status().Phase)
	}
}

func TestExecReturnsBothStreamsAndTheExitStatusAsAResult(t *testing.T) {
	harness := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		_, _ = io.WriteString(channel, "out\n")
		_, _ = io.WriteString(channel.Stderr(), "err\n")
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{3}))
		_ = channel.Close()
	})
	result, err := harness.conn.Exec(context.Background(), "mixed", 64)
	if err != nil || result.ExitCode != 3 || result.Truncated {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if got := string(result.Output); got != "out\nerr\n" && got != "err\nout\n" {
		t.Fatalf("output = %q", got)
	}
	if string(result.Stdout) != "out\n" || string(result.Stderr) != "err\n" {
		t.Fatalf("separate streams: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestExecBoundsOutputAndKeepsWhatWasPrintedWhenCanceled(t *testing.T) {
	harness := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		_, _ = io.WriteString(channel, "0123456789")
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		_ = channel.Close()
	})
	result, err := harness.conn.Exec(context.Background(), "long", 4)
	if err != nil || string(result.Output) != "0123" || !result.Truncated {
		t.Fatalf("result = %+v, err = %v", result, err)
	}

	printed := make(chan struct{})
	slow := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		_, _ = io.WriteString(channel, "so far")
		close(printed)
		_, _ = io.Copy(io.Discard, channel)
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan ExecResult, 1)
	go func() {
		result, err := slow.conn.Exec(ctx, "wait", 64)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v", err)
		}
		done <- result
	}()
	<-printed
	time.Sleep(50 * time.Millisecond) // let the output arrive before canceling
	cancel()
	select {
	case result := <-done:
		if string(result.Output) != "so far" {
			t.Fatalf("output after cancel = %q", result.Output)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Exec did not return after cancel")
	}
}

func TestStreamCommandCancellationWhileExecAcknowledgmentIsWithheld(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.command = func(_ string, channel ssh.Channel) { _ = channel.Close() }
		server.beforeExecReply = func(command string) {
			if command == "follow" {
				close(started)
				<-release
			}
		}
	})
	defer close(release)
	h := connect(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- h.conn.StreamCommand(ctx, "follow", 4096, func([]byte) bool { return true })
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not reach the exec request")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled establishment returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation waited for the withheld acknowledgment")
	}
	if h.conn.Status().Phase != PhaseConnected {
		t.Fatal("canceling the stream disconnected the SSH client")
	}
}

func TestStreamCommandDeliversFinalPartialLine(t *testing.T) {
	h := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		defer channel.Close()
		io.WriteString(channel, "last line")
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
	})
	var got string
	err := h.conn.StreamCommand(context.Background(), "read", 32, func(line []byte) bool {
		got += string(line)
		return true
	})
	if err != nil || got != "last line" {
		t.Fatalf("got %q, error %v", got, err)
	}
}

func TestStreamCommandSkipsOversizedLinesAndKeepsReading(t *testing.T) {
	h := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		defer channel.Close()
		_, _ = io.WriteString(channel, "before\n"+strings.Repeat("x", 128)+"\nafter\n"+strings.Repeat("y", 70))
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
	})
	var got string
	err := h.conn.StreamCommandSkippingLongLines(context.Background(), "read", 32, func(line []byte) bool {
		got += string(line)
		return true
	})
	if err != nil || got != "before\nafter\n" {
		t.Fatalf("got %q, error %v", got, err)
	}
}

// streamRecorder keeps what ExecStream delivers and reports the first chunk.
type streamRecorder struct {
	mu     sync.Mutex
	text   strings.Builder
	seen   chan struct{}
	once   sync.Once
	needle string
}

func (r *streamRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	r.text.Write(p)
	reached := strings.Contains(r.text.String(), r.needle)
	r.mu.Unlock()
	if reached {
		r.once.Do(func() { close(r.seen) })
	}
	return len(p), nil
}

func (r *streamRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.text.String()
}

func TestExecStreamDeliversOutputWhileTheCommandRuns(t *testing.T) {
	release := make(chan struct{})
	harness := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		_, _ = io.WriteString(channel, "first\n")
		<-release
		_, _ = io.WriteString(channel, "second\n")
		channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{3}))
		_ = channel.Close()
	})
	recorder := &streamRecorder{seen: make(chan struct{}), needle: "first\n"}
	done := make(chan ExecResult, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := harness.conn.ExecStream(context.Background(), "stream", recorder)
		done <- result
		errs <- err
	}()
	select {
	case <-recorder.seen:
	case <-time.After(time.Second):
		t.Fatal("output did not arrive while the command was still running")
	}
	if got := recorder.String(); got != "first\n" {
		t.Fatalf("output before the command ended = %q", got)
	}
	close(release)
	select {
	case result := <-done:
		if err := <-errs; err != nil || result.ExitCode != 3 {
			t.Fatalf("result = %+v, err = %v", result, err)
		}
	case <-time.After(time.Second):
		t.Fatal("ExecStream did not return")
	}
	if got := recorder.String(); got != "first\nsecond\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestExecStreamCancellationClosesOnlyItsSession(t *testing.T) {
	started := make(chan struct{})
	harness := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		close(started)
		_, _ = io.Copy(io.Discard, channel)
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := harness.conn.ExecStream(ctx, "wait", io.Discard)
		result <- err
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled stream did not return")
	}
	if harness.conn.Status().Phase != PhaseConnected {
		t.Fatalf("connection phase = %v", harness.conn.Status().Phase)
	}
}
