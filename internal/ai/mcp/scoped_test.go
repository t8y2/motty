package mcp

import (
	"context"
	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
	"strings"
	"testing"
)

func TestScopedMCPEnforcesListAndCallWithoutPublicGate(t *testing.T) {
	calls := 0
	_, srv := newServer(t, gate.Gate{}, func(c *Config) {
		c.Gate = nil
		c.Tools = []tools.Spec{tools.RequestAgentModeSpec()}
		c.Dispatch = func(ctx context.Context, name string, args []byte) gate.Result {
			calls++
			return gate.Result{Text: "switch requested"}
		}
	})
	r := rpc(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if !strings.Contains(string(r.Result), "request_agent_mode") || strings.Contains(string(r.Result), "terminal_exec") {
		t.Fatal(string(r.Result))
	}
	r = rpc(t, srv.URL, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"terminal_exec","arguments":{"command":"touch sentinel"}}}`)
	if r.Error == nil || calls != 0 {
		t.Fatal("hidden tool dispatched", r, calls)
	}
	r = rpc(t, srv.URL, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"request_agent_mode","arguments":{}}}`)
	if r.Error != nil || calls != 1 {
		t.Fatal(r, calls)
	}
}

func TestScopedMCPExplicitEmptyListDoesNotUseDefaultTools(t *testing.T) {
	_, srv := newServer(t, gate.Gate{}, func(c *Config) {
		c.Tools = []tools.Spec{}
		c.Dispatch = func(context.Context, string, []byte) gate.Result { t.Error("dispatch"); return gate.Result{} }
	})
	r := rpc(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if strings.Contains(string(r.Result), "terminal_") {
		t.Fatal(string(r.Result))
	}
	r = rpc(t, srv.URL, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_sessions"}}`)
	if r.Error == nil {
		t.Fatal("empty scope accepted a tool")
	}
}

func TestScopedMCPPublishesNoResourceReadChannel(t *testing.T) {
	_, srv := newServer(t, gate.Gate{}, func(c *Config) {
		c.Tools = []tools.Spec{}
		c.Dispatch = func(context.Context, string, []byte) gate.Result { return gate.Result{} }
	})
	for _, method := range []string{"resources/list", "resources/templates/list", "resources/read"} {
		r := rpc(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":{"uri":"file:///private"}}`)
		if r.Error == nil || r.Error.Code != codeMethodNotFound {
			t.Fatal("resource channel exposed", method, r)
		}
	}
}
