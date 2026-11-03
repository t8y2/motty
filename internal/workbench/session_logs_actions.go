package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
	"motty/internal/sessionlog"
)

func (state *State) sessionLogHost(record sessionlog.Record) (Host, bool) {
	if record.TargetKind != sessionlog.TargetSavedHost || record.CatalogID == "" {
		return Host{}, false
	}
	for _, host := range state.hosts {
		if host.CatalogID == record.CatalogID && !host.tunnel {
			return host, true
		}
	}
	return Host{}, false
}

func (state *State) sessionLogTargetChanged(record sessionlog.Record, host Host) bool {
	return record.Protocol != sessionLogProtocol(host) || record.Address != host.Address || record.Account != state.login(host) || record.Port != sessionLogPort(host)
}

func (state *State) reopenSessionLogRecord(id string) {
	record, ok := state.sessionLogRecord(id)
	if !ok {
		return
	}
	if state.jumpToLogSession(id) {
		return
	}
	if record.Protocol == sessionlog.ProtocolLocal && record.TargetKind != sessionlog.TargetShared {
		state.openLocalTerminal()
		return
	}
	host, ok := state.sessionLogHost(record)
	if !ok {
		state.activeSection = sectionHosts
		state.save()
		return
	}
	if !state.sessionLogTargetChanged(record, host) {
		state.newHostSession(host)
		return
	}
	m := &state.messages.SessionLogs
	before := fmt.Sprintf("%s %s@%s:%d", sessionLogProtocolText(m, record.Protocol), record.Account, record.Address, record.Port)
	after := fmt.Sprintf("%s %s@%s:%d", sessionLogProtocolText(m, sessionLogProtocol(host)), state.login(host), host.Address, host.Port)
	account := state.login(host)
	state.openForm(formDialog{
		title: m.Reopen, description: fmt.Sprintf(m.TargetChangedFmt, before, after), submit: m.Reopen,
		run: func(state *State, _ *formDialog) string {
			current, ok := state.sessionLogHost(record)
			if !ok || !sameSessionLogTarget(current, host) || state.login(current) != account {
				return m.TargetChangedAgain
			}
			state.newHostSession(current)
			return ""
		},
	})
}

// Compare only the target shown in the confirmation, not credentials or other
// mutable host settings. Authentication always uses the current catalog entry.
func sameSessionLogTarget(a, b Host) bool {
	return a.CatalogID == b.CatalogID && a.Address == b.Address && a.User == b.User && a.Port == b.Port && a.Protocol == b.Protocol
}

func (state *State) locateSessionLogHost(id string) {
	record, ok := state.sessionLogRecord(id)
	if !ok {
		return
	}
	if host, ok := state.sessionLogHost(record); ok {
		state.selectedHostID = host.ID
		state.hostSelections = nil
		state.search = text.Editor{}
		state.syncVisible()
		for i := range state.hostGroups {
			if hostGroupWithin(strings.TrimSpace(host.Group), state.hostGroups[i].Name) {
				state.hostGroups[i].Expanded = true
			}
		}
		state.setHostFilter(false, "")
		state.hostRevealID = host.ID
	}
	state.activeSection = sectionHosts
	state.save()
}

func (state *State) sessionLogHostOffset(id uint32, width float32) float32 {
	var offset float32
	run := 0
	columns := hostGridColumns(width)
	for _, entry := range state.hostTreeRows {
		if entry.Kind == hostTreeGroup {
			if state.hostView == hostViewGrid {
				offset += float32((run+columns-1)/columns)*(hostCardHeight+hostCardGap) + hostGridHeader
				run = 0
			} else {
				offset += hostGroupRowHeight
			}
			continue
		}
		if state.hosts[entry.HostIndex].ID == id {
			if state.hostView == hostViewGrid {
				offset += float32(run/columns) * (hostCardHeight + hostCardGap)
			}
			return offset
		}
		if state.hostView == hostViewGrid {
			run++
		} else {
			offset += hostRowHeight
		}
	}
	return 0
}

func sessionLogMetadataSummary(record sessionlog.Record, events []sessionlog.Event) string {
	var text strings.Builder
	m := i18n.For(i18n.English).SessionLogs
	fmt.Fprintf(&text, "Motty session metadata\nTarget: %s\nAddress: %s\nAccount: %s\nPort: %d\nProtocol: %s\nStarted: %s\n", record.Host, record.Address, record.Account, record.Port, sessionLogProtocolText(&m, record.Protocol), record.StartedAt.UTC().Format("2006-01-02T15:04:05Z"))
	if !record.EndedAt.IsZero() {
		label := "Ended"
		if record.Reason == sessionlog.ReasonProcessRestart {
			label = "Recovery detected"
		} else if record.Status == sessionlog.StatusTrackingStopped {
			label = "Recording stopped"
		}
		fmt.Fprintf(&text, "%s: %s\n", label, record.EndedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	fmt.Fprintf(&text, "Status: %s\nReason: %s\nReconnects: %d\n", sessionLogStatusText(&m, record.Status), sessionLogReasonText(&m, record.Reason), record.ReconnectCount)
	for _, event := range events {
		fmt.Fprintf(&text, "%s %s; reason=%s; attempt=%d\n", event.At.UTC().Format("2006-01-02T15:04:05Z"), sessionLogEventLabel(&m, event), sessionLogReasonText(&m, event.Reason), event.Attempt)
	}
	return text.String()
}

func (state *State) copySessionLogMetadata(id string) {
	record, ok := state.sessionLogRecord(id)
	if !ok {
		return
	}
	view := &state.sessionLogs.view
	if view.eventsLoad || view.eventsFor != id || view.eventsErr != "" {
		view.statusText = state.messages.SessionLogs.Loading
		return
	}
	err := state.options.SetClipboardText(sessionLogMetadataSummary(record, view.events))
	view.statusDanger = err != nil
	view.statusText = state.messages.SessionLogs.MetadataCopied
	if err != nil {
		view.statusText = state.messages.SessionLogs.MetadataCopyFailed
	}
}
