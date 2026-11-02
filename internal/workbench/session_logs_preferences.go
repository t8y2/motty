package workbench

import (
	"slices"
	"time"

	"motty/internal/sessionlog"
)

type savedSessionLogsView struct {
	TimeRange    string `json:"time_range,omitempty"`
	StartDate    string `json:"start_date,omitempty"`
	EndDate      string `json:"end_date,omitempty"`
	Status       string `json:"status,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	FavoriteOnly bool   `json:"favorite_only,omitempty"`
	Outcome      string `json:"outcome,omitempty"`
	Sort         string `json:"sort,omitempty"`
}

var sessionLogTimeKeys = [...]string{"all", "today", "week", "month", "custom"}
var sessionLogStatusKeys = [...]string{"all", "connecting", "active", "reconnecting", "ended", "failed", "interrupted", "tracking_stopped"}
var sessionLogProtocolKeys = [...]string{"all", "ssh", "local", "telnet", "serial", "vnc", "rdp"}
var sessionLogOutcomeKeys = [...]string{"all", "normal", "abnormal", "tracking_stopped"}
var sessionLogSortKeys = [...]string{"started_desc", "started_asc", "duration_desc", "duration_asc"}

func sessionLogPreferenceKey(keys []string, index int) string {
	if index < 0 || index >= len(keys) {
		return keys[0]
	}
	return keys[index]
}

func normalizeSessionLogsPreferences(saved *savedSessionLogsView) *savedSessionLogsView {
	if saved == nil {
		return nil
	}
	value := *saved
	value.TimeRange = sessionLogTimeKeys[max(0, slices.Index(sessionLogTimeKeys[:], value.TimeRange))]
	value.Status = sessionLogStatusKeys[max(0, slices.Index(sessionLogStatusKeys[:], value.Status))]
	value.Protocol = sessionLogProtocolKeys[max(0, slices.Index(sessionLogProtocolKeys[:], value.Protocol))]
	value.Outcome = sessionLogOutcomeKeys[max(0, slices.Index(sessionLogOutcomeKeys[:], value.Outcome))]
	value.Sort = sessionLogSortKeys[max(0, slices.Index(sessionLogSortKeys[:], value.Sort))]
	if _, _, err := sessionLogDateRange(value.StartDate, value.EndDate, time.Local); err != nil {
		value.StartDate, value.EndDate = "", ""
		if value.TimeRange == "custom" {
			value.TimeRange = "all"
		}
	}
	if value == (savedSessionLogsView{TimeRange: "all", Status: "all", Protocol: "all", Outcome: "all", Sort: "started_desc"}) {
		return nil
	}
	return &value
}

func cloneSessionLogsPreferences(saved *savedSessionLogsView) *savedSessionLogsView {
	if saved == nil {
		return nil
	}
	copy := *saved
	return &copy
}

func (state *State) restoreSessionLogsView(saved *savedSessionLogsView) {
	value := normalizeSessionLogsPreferences(saved)
	state.sessionLogs.preferences = value
	view := &state.sessionLogs.view
	if view.loaded {
		return
	}
	if value == nil {
		value = &savedSessionLogsView{}
	}
	view.timeRange = sessionLogTimeRange(max(0, slices.Index(sessionLogTimeKeys[:], value.TimeRange)))
	view.statusFilter = sessionlog.Status(max(0, slices.Index(sessionLogStatusKeys[:], value.Status)))
	view.protocolFilter = sessionlog.Protocol(max(0, slices.Index(sessionLogProtocolKeys[:], value.Protocol)))
	view.favoriteOnly = value.FavoriteOnly
	view.customStartDate, view.customEndDate = value.StartDate, value.EndDate
	view.query = sessionlog.Query{
		Sort:    sessionlog.SortOrder(max(0, slices.Index(sessionLogSortKeys[:], value.Sort))),
		Outcome: sessionlog.OutcomeFilter(max(0, slices.Index(sessionLogOutcomeKeys[:], value.Outcome))),
	}
	view.synced = false
}

func (state *State) currentSessionLogsView() *savedSessionLogsView {
	view := &state.sessionLogs.view
	return normalizeSessionLogsPreferences(&savedSessionLogsView{
		TimeRange: sessionLogPreferenceKey(sessionLogTimeKeys[:], int(view.timeRange)),
		StartDate: view.customStartDate, EndDate: view.customEndDate,
		Status:       sessionLogPreferenceKey(sessionLogStatusKeys[:], int(view.statusFilter)),
		Protocol:     sessionLogPreferenceKey(sessionLogProtocolKeys[:], int(view.protocolFilter)),
		FavoriteOnly: view.favoriteOnly,
		Outcome:      sessionLogPreferenceKey(sessionLogOutcomeKeys[:], int(view.query.Outcome)),
		Sort:         sessionLogPreferenceKey(sessionLogSortKeys[:], int(view.query.Sort)),
	})
}

func (state *State) snapshotSessionLogsView() *savedSessionLogsView {
	return cloneSessionLogsPreferences(state.sessionLogs.preferences)
}

// Share only remembered defaults. A peer keeps its active query, while its
// unrelated saves retain the most recently chosen defaults for future windows.
func (state *State) rememberSessionLogsView() {
	value := state.currentSessionLogsView()
	previous := state.sessionLogs.preferences
	if previous == nil && value == nil || previous != nil && value != nil && *previous == *value {
		return
	}
	state.sessionLogs.preferences = value
	if group := state.options.Group; group != nil {
		group.mu.Lock()
		members := append([]*State(nil), group.members...)
		group.mu.Unlock()
		for _, member := range members {
			if member != state {
				member.sessionLogs.preferences = cloneSessionLogsPreferences(value)
			}
		}
	}
	state.save()
}
