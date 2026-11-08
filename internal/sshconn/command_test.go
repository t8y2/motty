package sshconn

import (
	"context"
	"errors"
	"io"
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
