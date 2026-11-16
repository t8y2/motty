package cli

import (
	"motty/internal/ai/llm"
	"strings"
	"testing"
)

func TestClaudeMixedContentSnapshotsEmitEachTextBlockOnce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		want  string
	}{
		{"text then tool", []string{
			`{"type":"stream_event","event":{"type":"message_start"}}`,
			`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Before tool."}}}`,
			`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"mcp__motty__list_sessions","input":{}}}}`,
			`{"type":"assistant","message":{"content":[{"type":"text","text":"Before tool."},{"type":"tool_use","id":"call_1","name":"mcp__motty__list_sessions","input":{}}]}}`,
		}, "Before tool."},
		{"streamed and snapshot only blocks", []string{
			`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"First."}}}`,
			`{"type":"assistant","message":{"content":[{"type":"text","text":"First."},{"type":"text","text":"Second."}]}}`,
		}, "First.Second."},
		{"multiple text blocks", []string{
			`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"First."}}}`,
			`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}}`,
			`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Second."}}}`,
			`{"type":"assistant","message":{"content":[{"type":"text","text":"First."},{"type":"text","text":"Second."}]}}`,
			`{"type":"stream_event","event":{"type":"message_start"}}`,
			`{"type":"assistant","message":{"content":[{"type":"text","text":"Next message."}]}}`,
		}, "First.Second.Next message."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newParser("claude-code-cli")
			var text strings.Builder
			for _, line := range tc.lines {
				events, err := p.line([]byte(line))
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range events {
					if event.Kind == llm.EventText {
						text.WriteString(event.Text)
					}
				}
			}
			if text.String() != tc.want {
				t.Fatalf("rendered/history text = %q, want %q", text.String(), tc.want)
			}
		})
	}
}

func TestClaudeInitialTextStreamsBeforeSnapshot(t *testing.T) {
	p := newParser("claude-code-cli")
	events, err := p.line([]byte(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Initial text."}}}`))
	if err != nil || len(events) != 1 || events[0].Kind != llm.EventText || events[0].Text != "Initial text." {
		t.Fatalf("initial content did not stream: %+v, %v", events, err)
	}
	events, err = p.line([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Initial text."}]}}`))
	if err != nil || len(events) != 0 {
		t.Fatalf("initial content repeated in snapshot: %+v, %v", events, err)
	}
}
