package mcp

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"motty/internal/ai/gate"
)

func TestServeStdioAnswersRequestsWithoutAListener(t *testing.T) {
	g := okGate(&fakeBackend{})
	var out strings.Builder
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n" +
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_sessions","arguments":{}}}` + "\n"
	err := ServeStdio(context.Background(), strings.NewReader(input), &out, Config{
		Gate: func() gate.Gate { return g }, ServerName: "motty", Version: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	replies := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(replies) != 3 {
		t.Fatalf("replies = %q", replies)
	}
	joined := strings.Join(replies, "\n")
	for _, want := range []string{`"protocolVersion":"2025-06-18"`, `"name":"terminal_exec"`, "web:22"} {
		if !strings.Contains(joined, want) {
			t.Errorf("replies lack %s:\n%s", want, joined)
		}
	}
}

func TestServeStdioCancelsARunningCallWhenTheContextEnds(t *testing.T) {
	backend := &fakeBackend{block: true, execStarted: make(chan struct{}), execCtxDone: make(chan struct{})}
	g := okGate(backend)
	g.Mode = gate.ModeAuto
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	var out strings.Builder
	go func() {
		done <- ServeStdio(ctx, reader, &out, Config{Gate: func() gate.Gate { return g }})
	}()
	io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"terminal_exec","arguments":{"command":"ls"}}}`+"\n")
	select {
	case <-backend.execStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("the command never started")
	}
	cancel()
	select {
	case <-backend.execCtxDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the running command was not canceled")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ServeStdio returned nil after its context ended")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServeStdio did not return")
	}
}

func TestInstructionsAreTheCallersOrTheDefault(t *testing.T) {
	g := okGate(&fakeBackend{})
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n"
	for _, test := range []struct{ configured, want string }{
		{"Work on saved hosts.", "Work on saved hosts."},
		{"", "terminal sessions the user has open"},
	} {
		var out strings.Builder
		cfg := Config{Gate: func() gate.Gate { return g }, Instructions: test.configured}
		if err := ServeStdio(context.Background(), strings.NewReader(initialize), &out, cfg); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), test.want) {
			t.Errorf("instructions %q: reply %s lacks %q", test.configured, out.String(), test.want)
		}
	}
}
