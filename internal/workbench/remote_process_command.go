package workbench

import (
	"context"
	"errors"

	"motty/internal/remotetools"
)

func (state *State) cancelRemoteProcessCommand() {
	details := &state.remote.processDetails
	details.commandRequest++
	details.commandLoading = false
	if details.commandCancel != nil {
		details.commandCancel()
		details.commandCancel = nil
	}
}

func (state *State) loadRemoteProcessCommand() {
	details := &state.remote.processDetails
	if !details.open || details.tab != hitRemoteProcessDetailsCommand ||
		details.commandLoading || details.commandLoaded && !details.commandFailed {
		return
	}
	session, ok := state.remoteSession()
	if !ok {
		details.commandLoading = false
		details.commandFailed = true
		details.commandLoaded = true
		details.commandStatus = state.messages.Tools.RemoteProcessCommandSummary
		return
	}
	if state.remote.stale {
		details.commandLoading = false
		details.commandFailed = true
		details.commandLoaded = true
		details.commandStatus = state.messages.Tools.RemoteProcessCommandSummary
		return
	}
	sourceID := state.remote.session
	process := details.process
	serial := details.serial
	details.commandRequest++
	request := details.commandRequest
	details.commandLoading, details.commandFailed = true, false
	details.commandStatus = state.messages.Tools.RemoteProcessCommandLoading
	runner := remotetools.ForSystem(commandRunnerFor(session), session.host.System.String())
	ctx, cancel := context.WithTimeout(state.remoteToolsContext(), remoteTimeout)
	details.commandCancel = cancel
	go func() {
		defer cancel()
		command, err := remotetools.ReadProcessCommand(ctx, runner, process)
		state.postRemoteToolsUpdate(func(state *State) {
			state.applyRemoteProcessCommand(serial, request, sourceID, process, command, err)
		})
	}()
}

func (state *State) applyRemoteProcessCommand(
	serial, request uint64,
	sourceID uint32,
	process remotetools.Process,
	command string,
	err error,
) {
	details := &state.remote.processDetails
	if !details.open || details.serial != serial || state.remote.session != sourceID ||
		details.commandRequest != request ||
		remotetools.ProcessKey(details.process) != remotetools.ProcessKey(process) {
		return
	}
	details.commandCancel = nil
	details.commandLoading = false
	details.commandLoaded = true
	if err == nil {
		details.commandValue = command
		details.commandFailed = false
		details.commandStatus = ""
		details.text.clearSelection()
		return
	}
	// macOS, Windows or legacy Linux listings cannot safely recover a complete
	// argv. Keep the already collected command as an honest fallback; the tab
	// label says "Command" rather than claiming the value is complete.
	if errors.Is(err, remotetools.ErrProcessCommandUnsupported) {
		details.commandValue = process.Command
		details.commandFailed = false
		details.commandStatus = state.messages.Tools.RemoteProcessCommandSummary
		return
	}
	details.commandFailed = true
	switch {
	case errors.Is(err, remotetools.ErrProcessCommandTooLarge):
		details.commandStatus = state.messages.Tools.RemoteProcessCommandTooLarge
	case errors.Is(err, remotetools.ErrProcessChanged),
		errors.Is(err, remotetools.ErrProcessUnverifiable),
		errors.Is(err, context.DeadlineExceeded):
		details.commandStatus = state.remoteErrorText(err)
	default:
		details.commandStatus = state.messages.Tools.RemoteProcessCommandSummary
	}
}
