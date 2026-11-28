package workbench

import (
	"context"
	"encoding/json"
	"errors"

	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
)

func aiRunBoundSuggestion(ctx context.Context, config agent.Config, target, command, question string, emit func(agent.Event)) ([]llm.Message, error) {
	messages := []llm.Message{{Role: llm.RoleUser, Text: question, Images: config.UserImages}}
	if err := ctx.Err(); err != nil {
		return messages, err
	}
	args, _ := json.Marshal(tools.TerminalExecArgs{Session: target, Command: command})
	call := llm.ToolCall{ID: "suggestion_1", Name: tools.NameTerminalExec, Arguments: string(args)}
	emit(agent.Event{Kind: agent.EventToolStart, Call: call})
	toolContext, cancel := context.WithTimeout(ctx, config.StepTimeout)
	defer cancel()
	result := config.Gate.Run(toolContext, call.Name, args)
	if toolContext.Err() != nil {
		result.IsError = true
	}
	result.Text = aiSuggestionClip(agent.Redact(result.Text), 8192)
	emit(agent.Event{Kind: agent.EventToolDone, Call: call, Result: result})
	output := result.Text
	if result.IsError {
		output = "Error: " + output
	}
	messages = append(messages,
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
		llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Text: output},
	)
	if err := toolContext.Err(); err != nil {
		return messages, err
	}
	if result.Denied {
		return messages, gate.ErrDenied
	}
	if result.IsError {
		return messages, errors.New("bound command did not complete")
	}
	return messages, nil
}
