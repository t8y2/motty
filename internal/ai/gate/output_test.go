package gate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"motty/internal/ai/tools"
)

func TestOutputStorePagesUTF8AndRejectsOtherOwners(t *testing.T) {
	var store OutputStore
	data := strings.Repeat("中文日志\n", 15000)
	id, err := store.save("client-a", "session", data)
	if err != nil {
		t.Fatal(err)
	}
	var combined strings.Builder
	for offset := 0; ; {
		_, page, next, err := store.read("client-a", tools.ToolOutputReadArgs{OutputID: id, Offset: offset, MaxBytes: 1024})
		if err != nil || !utf8.ValidString(page) || next <= offset {
			t.Fatalf("offset %d: next %d, err %v", offset, next, err)
		}
		combined.WriteString(page)
		offset = next
		if next == len(data) {
			break
		}
	}
	if combined.String() != data {
		t.Fatal("paged output differs from captured output")
	}
	for _, owner := range []string{"", "client-b"} {
		if _, _, _, err := store.read(owner, tools.ToolOutputReadArgs{OutputID: id, MaxBytes: 4}); err == nil {
			t.Fatal("another client read retained output")
		}
	}
	if _, _, _, err := store.read("client-a", tools.ToolOutputReadArgs{OutputID: id, Offset: 1, MaxBytes: 4}); err == nil {
		t.Fatal("offset split a UTF-8 character")
	}
	if _, _, _, err := store.read("client-a", tools.ToolOutputReadArgs{OutputID: id, Offset: len(data) + 1, MaxBytes: 4}); err == nil {
		t.Fatal("out of range offset was accepted")
	}
	store.mu.Lock()
	item := store.items[id]
	item.expires = time.Now().Add(-time.Second)
	store.items[id] = item
	store.mu.Unlock()
	if _, _, _, err := store.read("client-a", tools.ToolOutputReadArgs{OutputID: id, MaxBytes: 4}); err == nil {
		t.Fatal("expired output was accessible")
	}
	if store.bytes != 0 {
		t.Fatal("expired output still counts against the budget")
	}
}

func TestOutputStoreEvictsResultsAndClearsOnShutdown(t *testing.T) {
	var store OutputStore
	first, err := store.save("owner", "session", strings.Repeat("x", MaxRetainedOutput))
	if err != nil {
		t.Fatal(err)
	}
	for range outputBudget / MaxRetainedOutput {
		if _, err := store.save("owner", "session", strings.Repeat("y", MaxRetainedOutput)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := store.read("owner", tools.ToolOutputReadArgs{OutputID: first, MaxBytes: 4}); err == nil {
		t.Fatal("evicted result was accessible")
	}
	store.Clear()
	if store.bytes != 0 || len(store.items) != 0 {
		t.Fatal("shutdown left command output in memory")
	}
}

type lostBackend struct{ fakeBackend }

func (backend *lostBackend) Exec(context.Context, tools.TerminalExecArgs) (ExecResult, error) {
	return ExecResult{Output: "head\n[... bytes omitted ...]\ntail", ExitCode: 17, Truncated: true, RetainLost: true}, nil
}

func TestUnrecoverableOutputCannotExposeAReadbackHandle(t *testing.T) {
	var store OutputStore
	backend := &lostBackend{}
	result := (Gate{Backend: backend, Outputs: &store, Mode: ModeAllowAll}).Run(
		WithOutputOwner(context.Background(), "client"), tools.NameTerminalExec, []byte(`{"command":"uptime"}`))
	var fields struct {
		OutputID         string `json:"output_id"`
		SourceOutputLost bool   `json:"source_output_lost"`
	}
	if err := json.Unmarshal(result.Structured, &fields); err != nil {
		t.Fatal(err)
	}
	if result.IsError || fields.OutputID != "" || !fields.SourceOutputLost || !strings.Contains(result.Text, "full output unavailable") || !strings.Contains(result.Text, "exit status 17") || len(store.items) != 0 {
		t.Fatalf("irrecoverable output misreported: %+v, %+v", result, fields)
	}
}

func BenchmarkOutputPage(b *testing.B) {
	var store OutputStore
	id, err := store.save("owner", "session", strings.Repeat("日志\n", 20000))
	if err != nil {
		b.Fatal(err)
	}
	args := tools.ToolOutputReadArgs{OutputID: id, MaxBytes: 64 << 10}
	b.ReportAllocs()
	for range b.N {
		_, _, _, _ = store.read("owner", args)
	}
}
