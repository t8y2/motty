//go:build !windows

package workbench

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPMCPReadsMiddleOfCommandWithoutRerunningIt(t *testing.T) {
	state, _, address, token := hubFixture(t, "ask")
	initialize := map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "test", "version": "1"}}
	_, client, status := pagedRPC(t, state, address, token, "", "initialize", initialize)
	if status != http.StatusOK || client == "" {
		t.Fatal("initialization did not establish a client identity")
	}
	_, otherClient, status := pagedRPC(t, state, address, token, "", "initialize", initialize)
	if status != http.StatusOK || otherClient == client || otherClient == "" {
		t.Fatal("HTTP clients shared an identity")
	}
	line := "ERROR 中文日志 " + strings.Repeat("x", 64)
	command := "awk 'BEGIN { for (i=1;i<=1500;i++) printf \"MCPLOG:%04d:" + line + "\\n\",i }'"
	var expected strings.Builder
	for index := 1; index <= 1500; index++ {
		fmt.Fprintf(&expected, "MCPLOG:%04d:%s\n", index, line)
	}
	call, _, status := pagedRPC(t, state, address, token, client, "tools/call", map[string]any{"name": "terminal_exec", "arguments": map[string]any{"command": command}})
	text, isError := toolText(t, call)
	if status != http.StatusOK || isError || !strings.Contains(text, "[output truncated]") || strings.Contains(text, "MCPLOG:0750:") || !strings.Contains(text, "[exit status 0]") {
		t.Fatalf("unexpected bounded preview: %d, error %t, text %q", status, isError, text)
	}
	start := strings.Index(text, "output_id: ")
	if start < 0 {
		t.Fatal("no recoverable output id in the preview")
	}
	outputID := strings.SplitN(text[start+len("output_id: "):], ";", 2)[0]
	structured := call["result"].(map[string]any)["structuredContent"].(map[string]any)
	if structured["output_id"] != outputID || structured["source_output_lost"] != false || int(structured["total_bytes"].(float64)) != expected.Len()-1 {
		t.Fatal("structured result does not describe the recoverable output")
	}
	read := func(owner string, offset int) (map[string]any, bool) {
		t.Helper()
		response, _, status := pagedRPC(t, state, address, token, owner, "tools/call", map[string]any{"name": "tool_output_read", "arguments": map[string]any{"output_id": outputID, "offset": offset, "max_bytes": 1025}})
		_, failed := toolText(t, response)
		if status != http.StatusOK {
			t.Fatalf("unexpected HTTP status %d", status)
		}
		return response["result"].(map[string]any)["structuredContent"].(map[string]any), failed
	}
	private, _, privateStatus := pagedRPC(t, state, address, token, otherClient, "tools/call", map[string]any{"name": "tool_output_read", "arguments": map[string]any{"output_id": outputID, "offset": 0}})
	if privateStatus != http.StatusOK || private == nil {
		t.Fatal("unexpected response from other client")
	}
	if _, isError := toolText(t, private); !isError {
		t.Fatal("another client read the output")
	}
	if _, _, status := pagedRPC(t, state, address, token, "invalid-session", "tools/call", map[string]any{"name": "tool_output_read", "arguments": map[string]any{"output_id": outputID, "offset": 0}}); status != http.StatusNotFound {
		t.Fatal("unknown MCP session accessed output")
	}
	var recovered strings.Builder
	for offset := 0; ; {
		page, isError := read(client, offset)
		if isError {
			t.Fatal("failed reading retained command output")
		}
		recovered.WriteString(page["data"].(string))
		next := int(page["next_offset"].(float64))
		if page["eof"].(bool) {
			break
		}
		if next <= offset {
			t.Fatal("pagination stopped making progress")
		}
		offset = next
	}
	if recovered.String() != strings.TrimSuffix(expected.String(), "\n") {
		t.Fatalf("recovered %d bytes, expected %d", recovered.Len(), expected.Len()-1)
	}
}
