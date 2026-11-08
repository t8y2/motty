package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
)

// SessionProvider runs a native agent loop, but delegates every application
// tool to Motty. Tool events in its output are diagnostics, not instructions.
type SessionProvider interface {
	RunSession(context.Context, llm.Request, func(context.Context, llm.ToolCall) gate.Result, func(llm.Event)) error
}

func runSession(parent context.Context, cfg Config, provider SessionProvider, msgs []llm.Message, base int, specs []llm.ToolSpec, emit func(Event)) ([]llm.Message, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	req := llm.Request{
		Model:           cfg.Model,
		System:          cfg.System,
		Messages:        append([]llm.Message(nil), msgs...),
		Tools:           specs,
		MaxTokens:       cfg.MaxTokens,
		Temperature:     cfg.Temperature,
		ReasoningEffort: cfg.ReasoningEffort,
	}
	if cfg.Mode != ModeAgent {
		if cfg.NoTools {
			req.System += noToolsAnswerNote
		} else {
			req.System += askNote
		}
	}
	offered := make(map[string]bool)
	for _, spec := range specs {
		offered[spec.Name] = true
	}
	var mu, toolMu sync.Mutex
	var text strings.Builder
	var usage llm.Usage
	var done, escalated bool
	var protocolErr error
	steps, refused := 0, 0
	inflight := false
	callback := func(callCtx context.Context, call llm.ToolCall) gate.Result {
		toolMu.Lock()
		defer toolMu.Unlock()
		mu.Lock()
		deny := func(message string) gate.Result {
			refused++
			if refused >= stuckLimit {
				protocolErr = errors.New("agent: repeated unavailable tool calls")
				cancel()
			}
			mu.Unlock()
			return gate.Result{Text: message, IsError: true}
		}
		if ctx.Err() != nil || callCtx.Err() != nil {
			mu.Unlock()
			return gate.Result{Text: "request canceled", IsError: true}
		}
		if done || !offered[call.Name] {
			return deny("unknown tool " + call.Name)
		}
		if cfg.Mode != ModeAgent {
			if _, err := tools.DecodeRequestAgentMode([]byte(call.Arguments)); err != nil {
				return deny("invalid mode-switch arguments")
			}
			escalated = true
			emit(Event{Kind: EventAgentModeRequest, Call: call, Usage: usage})
			cancel()
			mu.Unlock()
			return gate.Result{Text: "mode switch requested"}
		}
		if steps >= cfg.MaxSteps {
			return deny("step limit reached; no more tools are available. Summarize the completed work.")
		}
		steps++
		call.ID = fmt.Sprintf("native_%d", steps)
		if call.Arguments == "" {
			call.Arguments = "{}"
		}
		prefix := text.String()
		text.Reset()
		inflight = true
		emit(Event{Kind: EventToolStart, Call: call, Step: steps})
		mu.Unlock()
		toolCtx, stop := context.WithCancel(callCtx)
		unregister := context.AfterFunc(ctx, stop)
		res := runTool(toolCtx, cfg, offered, call)
		unregister()
		stop()
		mu.Lock()
		defer mu.Unlock()
		inflight = false
		if cfg.RedactToolResults {
			res.Text = Redact(res.Text)
		}
		res.Text = truncate(res.Text, cfg.MaxToolResultBytes)
		// Keep complete audit pairs even when a later model event fails. A
		// canceled call is recorded as an error, never as successful work.
		if ctx.Err() != nil || callCtx.Err() != nil {
			res.IsError = true
		}
		emit(Event{Kind: EventToolDone, Call: call, Result: res, Step: steps})
		result := res.Text
		if res.IsError {
			result = "Error: " + result
		}
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Text: prefix, ToolCalls: []llm.ToolCall{call}}, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Text: result})
		return res
	}
	err := provider.RunSession(ctx, req, callback, func(event llm.Event) {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil || protocolErr != nil {
			return
		}
		if done && event.Kind != llm.EventToolCall {
			protocolErr = errors.New("agent: output after session completion")
			cancel()
			return
		}
		switch event.Kind {
		case llm.EventText:
			text.WriteString(event.Text)
			emit(Event{Kind: EventText, Text: event.Text, Step: steps + 1})
		case llm.EventReasoning:
			emit(Event{Kind: EventReasoning, Text: event.Text, Step: steps + 1})
		case llm.EventDone:
			if inflight {
				protocolErr = errors.New("agent: completion while tool is running")
			}
			done, usage = true, event.Usage
		case llm.EventError:
			protocolErr = event.Err
			if protocolErr == nil {
				protocolErr = errors.New("agent: native provider failed")
			}
			cancel()
		}
	})
	// A provider must finish its callbacks before returning.
	toolMu.Lock()
	defer toolMu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	newMsgs := func() []llm.Message { return append([]llm.Message(nil), msgs[base:]...) }
	if parent.Err() != nil {
		return newMsgs(), parent.Err()
	}
	if escalated {
		return newMsgs(), nil
	}
	if protocolErr != nil {
		err = protocolErr
	}
	if err == nil && !done {
		err = errors.New("agent: native stream ended without completion")
	}
	if err != nil {
		emit(Event{Kind: EventError, Text: err.Error(), Step: steps + 1, Usage: usage})
		return newMsgs(), err
	}
	msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Text: text.String()})
	emit(Event{Kind: EventDone, Step: steps + 1, Usage: usage})
	return newMsgs(), nil
}
