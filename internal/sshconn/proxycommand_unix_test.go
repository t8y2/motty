//go:build !windows

package sshconn

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A proxy command that starts programs of its own, as a pipeline or a wrapper
// script does, must not leave them running when the connection closes.
func TestClosingAProxyCommandEndsWhatItStarted(t *testing.T) {
	pidFile := t.TempDir() + "/child.pid"
	proxy := &Proxy{Kind: ProxyCommand, Command: `sh -c 'sleep 60 & echo $! > ` + pidFile + `; wait'`}
	conn, err := proxy.dialCommand(context.Background(), "host", 22, "user")
	if err != nil {
		t.Fatal(err)
	}
	var child int
	deadline := time.Now().Add(5 * time.Second)
	for child == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the proxy command never started its child")
		}
		if data, err := os.ReadFile(pidFile); err == nil {
			child, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		time.Sleep(10 * time.Millisecond)
	}
	conn.Close()
	for syscall.Kill(child, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("the child %d of the proxy command outlived the connection", child)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
