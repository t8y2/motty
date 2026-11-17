package cli

import (
	"context"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"os"
	"testing"
)

// Opt-in check for an installed Qoder CLI. It uses an empty config root, so the
// run reaches sign-in rather than a real model, and proves the scoped arguments
// and plugin listing are accepted end to end.
func TestInstalledQoderScopedArgumentsReachAuthentication(t *testing.T) {
	exe := os.Getenv("MOTTY_TEST_QODER")
	if exe == "" {
		t.Skip("set MOTTY_TEST_QODER to a Qoder CLI executable")
	}
	t.Setenv("QODER_CONFIG_DIR", t.TempDir())
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "")
	c := command{program: exe}
	if err := probe(t.Context(), "qoder-cli", &c); err != nil {
		t.Fatalf("probe: %v", err)
	}
	state, err := CheckAuth(t.Context(), "qoder-cli", exe)
	if err != nil || state != AuthSignedOut {
		t.Fatalf("auth = %s, %v; want signed_out", state, err)
	}
	p := New("qoder-cli", exe).(*provider)
	err = p.RunSession(t.Context(), llm.Request{
		Model:    "default",
		System:   "Respond briefly.",
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "hello"}},
		Tools:    []llm.ToolSpec{{Name: "list_sessions"}},
	}, func(context.Context, llm.ToolCall) gate.Result { return gate.Result{} }, func(llm.Event) {})
	if err == nil || err.Error() != "cli: login_required" {
		t.Fatalf("unauthenticated scoped run: %v", err)
	}
}
