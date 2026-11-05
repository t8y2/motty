package workbench

import (
	"context"
	"sort"
	"strings"
	"time"

	"motty/internal/completion"
	"motty/internal/localshell"
	"motty/internal/shellkind"
)

const completionProbeCache = 30 * time.Second

type completionProbe struct {
	ready      chan completionProbeResult
	cancel     context.CancelFunc
	generation uint64
	sessionID  uint32
	owner      *terminal
	conn       Connection
	pending    bool
	known      map[string]bool
	available  map[string]bool
	retryAt    time.Duration
	expiresAt  time.Duration
}

type completionProbeResult struct {
	generation uint64
	sessionID  uint32
	owner      *terminal
	conn       Connection
	names      []string
	available  map[string]bool
	err        error
}

func (state *State) cancelCompletionProbe() {
	probe := &state.completion.probe
	if probe.cancel != nil {
		probe.cancel()
	}
	*probe = completionProbe{generation: probe.generation + 1}
}

func (state *State) startCompletionProbe(session *Session) {
	if !state.completion.probeEnabled || state.history.suggestOff || !state.completionEligible(session) || !state.historyAvailable(session) || session.conn == nil {
		return
	}
	list := &state.completion.list
	if !list.active || list.sessionID != session.ID || strings.ContainsAny(list.line, " \t") {
		return
	}
	kind := session.term.shellKind()
	if kind != shellkind.POSIX && kind != shellkind.PowerShell {
		return
	}
	probe := &state.completion.probe
	if probe.sessionID != session.ID || probe.owner != session.term || probe.conn != session.conn {
		state.cancelCompletionProbe()
		probe.sessionID, probe.owner, probe.conn = session.ID, session.term, session.conn
		probe.known, probe.available = make(map[string]bool), make(map[string]bool)
	}
	now := state.options.Now()
	if probe.expiresAt != 0 && now >= probe.expiresAt {
		probe.known, probe.available = make(map[string]bool), make(map[string]bool)
		probe.expiresAt = 0
	}
	if probe.pending || now < probe.retryAt {
		return
	}
	var names []string
	for _, candidate := range list.candidates {
		if candidate.FromCatalog && !candidate.FromHost && candidate.Kind == completion.KindCommand && !probe.known[candidate.Text] {
			names = append(names, candidate.Text)
			if len(names) == 64 {
				break
			}
		}
	}
	if len(names) == 0 {
		return
	}
	var runner completion.CommandRunner = session.conn
	if local, ok := session.conn.(*localConn); ok {
		if _, wsl := localshell.WSLDistro(local.shell); wsl {
			return
		}
		runner = localshell.AvailabilityRunner{Program: local.shell, Dir: local.dir}
	}
	ctx, cancel := context.WithCancel(state.completionContext())
	probe.cancel, probe.pending = cancel, true
	probe.generation++
	ready := make(chan completionProbeResult, 1)
	probe.ready = ready
	result := completionProbeResult{generation: probe.generation, sessionID: session.ID, owner: session.term, conn: session.conn, names: names}
	invalidate := state.options.Invalidate
	state.completion.workers.Add(1)
	go func() {
		defer state.completion.workers.Done()
		result.available, result.err = completion.ProbeAvailable(ctx, runner, kind, names)
		ready <- result
		if invalidate != nil {
			invalidate()
		}
	}()
}

func (state *State) adoptCompletionProbe() {
	probe := &state.completion.probe
	if !state.completion.probeEnabled || state.history.suggestOff {
		if probe.conn != nil || probe.pending {
			state.cancelCompletionProbe()
		}
		return
	}
	if !probe.pending || probe.ready == nil {
		return
	}
	session, ok := state.sessionByIDPointer(probe.sessionID)
	if !ok || session.term != probe.owner || session.conn != probe.conn || session.host.NoHistory {
		state.cancelCompletionProbe()
		return
	}
	select {
	case result := <-probe.ready:
		probe.pending, probe.ready = false, nil
		if probe.cancel != nil {
			probe.cancel()
			probe.cancel = nil
		}
		session, ok := state.sessionByIDPointer(result.sessionID)
		if !ok || session.term != result.owner || session.conn != result.conn || session.host.NoHistory ||
			result.generation != probe.generation || result.err != nil {
			probe.retryAt = state.options.Now() + completionProbeCache
			return
		}
		for _, name := range result.names {
			probe.known[name] = true
		}
		for name := range result.available {
			probe.available[name] = true
		}
		probe.retryAt = 0
		probe.expiresAt = state.options.Now() + completionProbeCache
		if state.completion.list.active && state.completion.list.sessionID == session.ID && state.completion.preview.phase == previewIdle {
			state.completionRebuild(session)
		}
	default:
	}
}

func (state *State) rankCompletionAvailability(session *Session, line string, candidates []completion.Candidate, historyRows int) {
	sort.SliceStable(candidates[historyRows:], func(left, right int) bool {
		first, second := candidates[historyRows+left], candidates[historyRows+right]
		firstAvailable := state.completionAvailableOnLine(session, line, first)
		secondAvailable := state.completionAvailableOnLine(session, line, second)
		if firstAvailable != secondAvailable {
			return firstAvailable
		}
		return completion.CommonPriority(first.Text) < completion.CommonPriority(second.Text)
	})
}

func (state *State) completionAvailable(session *Session, candidate completion.Candidate) bool {
	return state.completionAvailableOnLine(session, state.completion.list.line, candidate)
}

func (state *State) completionAvailableOnLine(session *Session, line string, candidate completion.Candidate) bool {
	probe := &state.completion.probe
	if state.completionDiscoveredOnLine(session, line, candidate) {
		return true
	}
	return session != nil && candidate.FromCatalog && candidate.Kind == completion.KindCommand &&
		!strings.ContainsAny(line, " \t") && state.completion.probeEnabled &&
		probe.owner == session.term && probe.conn == session.conn && probe.sessionID == session.ID &&
		probe.expiresAt > state.options.Now() && probe.available[candidate.Text]
}
