package sshconn

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSudoCommandOnlySendsThePasswordWhenRequested(t *testing.T) {
	for _, prompt := range []bool{false, true} {
		t.Run(map[bool]string{false: "cached", true: "prompt"}[prompt], func(t *testing.T) {
			harness := connectedCommandHarness(t, func(command string, channel ssh.Channel) {
				if !strings.Contains(command, "sudo -S") || !strings.Contains(command, "collect") {
					t.Errorf("command = %q", command)
				}
				if prompt {
					_, _ = io.WriteString(channel.Stderr(), sudoPrompt)
					password, err := bufio.NewReader(channel).ReadString('\n')
					if err != nil || password != "secret\n" {
						t.Errorf("password = %q, %v", password, err)
						return
					}
				}
				_, _ = io.WriteString(channel.Stderr(), sudoReady)
				_, _ = io.WriteString(channel, "data\n")
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				_ = channel.Close()
			})
			output, err := harness.conn.RunSudoCommand(context.Background(), "collect", "secret", 32)
			if err != nil || string(output) != "data\n" {
				t.Fatalf("output = %q, error = %v", output, err)
			}
		})
	}
}

func TestSudoExecPreservesExitStatusAndOutputLimit(t *testing.T) {
	harness := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		_, _ = io.WriteString(channel.Stderr(), sudoReady)
		_, _ = io.WriteString(channel, "long output")
		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{7}))
		_ = channel.Close()
	})
	result, err := harness.conn.ExecSudo(context.Background(), "collect", "", 4)
	if err != nil || !result.Truncated || result.ExitCode != 7 || len(result.Output) != 4 {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestSudoStreamStopsWithoutClosingTheSSHConnection(t *testing.T) {
	harness := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		_, _ = io.WriteString(channel.Stderr(), sudoReady)
		_, _ = io.WriteString(channel, "sample\n")
		_, _ = io.Copy(io.Discard, channel)
	})
	var lines []string
	err := harness.conn.StreamSudoCommand(context.Background(), "monitor", "", 32, func(line []byte) bool {
		lines = append(lines, string(line))
		return false
	})
	if err != nil || len(lines) != 1 || lines[0] != "sample\n" {
		t.Fatalf("lines = %q, error = %v", lines, err)
	}
	if harness.conn.Status().Phase != PhaseConnected {
		t.Fatal("stream closed the authenticated connection")
	}
}

func TestSudoCommandCancellationEndsOnlyItsSession(t *testing.T) {
	started := make(chan struct{})
	harness := connectedCommandHarness(t, func(_ string, channel ssh.Channel) {
		_, _ = io.WriteString(channel.Stderr(), sudoReady)
		close(started)
		_, _ = io.Copy(io.Discard, channel)
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := harness.conn.RunSudoCommand(ctx, "wait", "", 32)
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
		t.Fatal("canceled sudo command did not stop")
	}
}
