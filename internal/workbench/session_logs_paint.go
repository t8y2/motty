package workbench

import (
	"fmt"
	"strconv"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/sessionlog"
)

// The session-log page shows the local journal of connections the workbench
// opened: one row per logical session, newest first, its lifecycle events, and
// the actions that belong to the records. Two rules shape the painting here:
//
//   - Paint only reads prepared strings: applySessionLogsResult and the event
//     handlers format every cell, label and count off the paint path, so a warm
//     paint allocates nothing and touches no database.
//   - Every registered control does something. The one order the store offers is
//     newest first, so no heading paints a sort arrow.

// sessionLogHits is the place of the page's controls, dashboard-style: the
// sidebar row is declared here but painted by the shell.
var sessionLogHits = tree.Feature("session-logs")

var (
	hitSessionLogs               = tree.ID(sessionLogHits, "navigation")
	hitSessionLogsSearch         = tree.ID(sessionLogHits, "search")
	hitSessionLogsTime           = tree.ID(sessionLogHits, "time-filter")
	hitSessionLogsStatus         = tree.ID(sessionLogHits, "status-filter")
	hitSessionLogsProtocol       = tree.ID(sessionLogHits, "protocol-filter")
	hitSessionLogsOutcome        = tree.ID(sessionLogHits, "outcome-filter")
	hitSessionLogsSort           = tree.ID(sessionLogHits, "sort")
	hitSessionLogsSortHeader     = tree.Sub(sessionLogHits, tree.Name("sort-header"))
	hitSessionLogsMore           = tree.ID(sessionLogHits, "more-actions")
	hitSessionLogsFavoriteFilter = tree.ID(sessionLogHits, "favorite-filter")
	hitSessionLogsFilterList     = tree.Sub(sessionLogHits, tree.Name("filter-option"))
	hitSessionLogsFavorite       = tree.ID(sessionLogHits, "favorite")
	hitSessionLogsDelete         = tree.ID(sessionLogHits, "delete")
	hitSessionLogsClearFavorite  = tree.ID(sessionLogHits, "clear-unfavorited")
	hitSessionLogsClearAll       = tree.ID(sessionLogHits, "clear-all")
	hitSessionLogsClearFilters   = tree.ID(sessionLogHits, "clear-filters")
	hitSessionLogsLoadMore       = tree.ID(sessionLogHits, "load-more")
	hitSessionLogsRowFavorite    = tree.Sub(sessionLogHits, tree.Name("row-favorite"))
	hitSessionLogsDetailClose    = tree.ID(sessionLogHits, "detail-close")
	hitSessionLogsLive           = tree.ID(sessionLogHits, "open-session")
	hitSessionLogsCopy           = tree.ID(sessionLogHits, "copy-metadata")
	hitSessionLogsLocate         = tree.ID(sessionLogHits, "locate-host")
)

// sessionLogTimeRange is the time-range filter's choice.
type sessionLogTimeRange uint8

const (
	sessionLogTimeAll sessionLogTimeRange = iota
	sessionLogTimeToday
	sessionLogTimeWeek
	sessionLogTimeMonth
	sessionLogTimeCustom
)

// sessionLogFilterMenu is the filter whose choices are open, if any. The
// favorite filter has no menu: its icon toggles between any and favorites only,
// which is why it is not one of these.
type sessionLogFilterMenu uint8

const (
	sessionLogFilterNone sessionLogFilterMenu = iota
	sessionLogFilterTimeMenu
	sessionLogFilterStatusMenu
	sessionLogFilterProtocolMenu
	sessionLogFilterOutcomeMenu
	sessionLogFilterSortMenu
)

// sessionLogRow is one painted row: its stable record ID, its favorite mark
// and the cells of both table layouts. Everything is formatted when the page
// data changes, never in Paint.
type sessionLogRow struct {
	id            string
	favorite      bool
	favoriteLabel string
	// cells is the six-column layout: target, started, protocol, status,
	// duration. The favorite column is painted as an icon, so it stays empty.
	cells [6]string
	// compact is the four-column layout a narrow card paints: target, started,
	// status. Protocol and duration are still in the detail panel.
	compact [4]string
	// minimal is the three-column layout of the narrowest card: target, started
	// and the favorite mark, which leaves the target enough room to be read.
	minimal [3]string
}

// sessionLogDetailField is one label/value line of the detail panel.
type sessionLogDetailField struct{ label, value string }

// sessionLogEventRow is one lifecycle event of the detail panel.
type sessionLogEventRow struct{ when, label string }

// sessionLogDetail is the prepared detail panel: it never formats in Paint.
type sessionLogDetail struct {
	title  string
	fields [15]sessionLogDetailField
	count  int
	// statusIndex is the field that holds the status, which the panel paints
	// in the tone of that status rather than as plain text.
	statusIndex   int
	statusTone    sessionLogStatusTone
	events        []sessionLogEventRow
	eventsText    string
	truncatedText string
	noInfo        string
	live          bool
	actionLabel   string
	hasHost       bool
	copyEnabled   bool
}

// sessionLogStatusTone is how a status reads in the detail panel.
type sessionLogStatusTone uint8

const (
	sessionLogToneNeutral sessionLogStatusTone = iota
	sessionLogToneSuccess
	sessionLogToneWarning
	sessionLogToneDanger
)

const (
	// sessionLogPageSize is one page of records; the store clamps Query.Limit to
	// the same 50.
	sessionLogPageSize = 50
	// sessionLogTitleSize is the page title, matching the other data pages.
	sessionLogTitleSize = float32(18)
	// sessionLogSearchLeast and sessionLogSearchMost bound the search box.
	sessionLogSearchLeast = float32(120)
	sessionLogSearchMost  = float32(440)
	// sessionLogFilterWidth is the trigger width of one filter.
	sessionLogFilterWidth = float32(124)
	// sessionLogFullColumns and sessionLogCompactColumns are the widths at which
	// the table drops from six columns to four and then to three.
	sessionLogFullColumns    = float32(700)
	sessionLogCompactColumns = float32(460)
	// sessionLogSideDetail is the narrowest card that puts the detail panel
	// beside the table instead of in its place.
	sessionLogSideDetail = float32(940)
	// sessionLogDetailWidth is the side panel's width.
	sessionLogDetailWidth = float32(340)
	// sessionLogEventHeight is one event row.
	sessionLogEventHeight = float32(22)
	// sessionLogFieldHeight is one detail field row.
	sessionLogFieldHeight = float32(22)
	// sessionLogEventsRoom is what a detail panel keeps for the event list and
	// its heading, so a short panel drops fields instead of the events.
	sessionLogEventsRoom = float32(140)
	// sessionLogToolbarGap is the space between the toolbar's controls.
	sessionLogToolbarGap = float32(8)
)

// sessionLogStatusChoices and sessionLogProtocolChoices are the filter popups'
// fixed choices; the zero value means "any" in each.
var sessionLogStatusChoices = [...]sessionlog.Status{
	0, sessionlog.StatusConnecting, sessionlog.StatusActive, sessionlog.StatusReconnecting,
	sessionlog.StatusEnded, sessionlog.StatusFailed, sessionlog.StatusInterrupted, sessionlog.StatusTrackingStopped,
}

var sessionLogProtocolChoices = [...]sessionlog.Protocol{
	0, sessionlog.ProtocolSSH, sessionlog.ProtocolLocal, sessionlog.ProtocolTelnet,
	sessionlog.ProtocolSerial, sessionlog.ProtocolVNC, sessionlog.ProtocolRDP,
}

// sessionLogFiltered reports whether the view is narrowed by a search or a
// filter, which decides between the "nothing yet" and "no matches" states.
func (state *State) sessionLogFiltered() bool {
	view := &state.sessionLogs.view
	return view.appliedSearch != "" || view.timeRange != sessionLogTimeAll ||
		view.statusFilter != 0 || view.protocolFilter != 0 || view.favoriteOnly || view.query.Outcome != sessionlog.OutcomeAll
}

// prepareSessionLogsView rebuilds every string the page paints. It runs on the
// window thread from a query completion, a filter change or a language switch,
// never from Paint.
func (state *State) prepareSessionLogsView() {
	view := &state.sessionLogs.view
	view.synced = true
	view.syncedLanguage = state.language
	view.customRangeHint = ""
	if view.timeRange == sessionLogTimeCustom {
		view.customRangeHint = fmt.Sprintf(state.messages.SessionLogs.CustomRangeHintFmt, view.customStartDate, view.customEndDate)
	}
	state.prepareSessionLogRows()
	state.prepareSessionLogSummary()
	state.clampSessionLogSelection()
	state.prepareSessionLogDetail()
}

// prepareSessionLogRows projects the records into the rows the table paints.
func (state *State) prepareSessionLogRows() {
	view := &state.sessionLogs.view
	if cap(view.page) < len(view.rows) {
		view.page = make([]sessionLogRow, len(view.rows))
	} else {
		view.page = view.page[:len(view.rows)]
	}
	m := &state.messages.SessionLogs
	now := state.wallNow().In(state.sessionLogLocation())
	for index := range view.rows {
		record := &view.rows[index]
		row := &view.page[index]
		row.id = record.ID
		row.favorite = record.Favorite
		row.cells[0] = sessionLogTargetText(record)
		row.cells[1] = sessionLogStartedText(record.StartedAt, now)
		row.cells[2] = sessionLogProtocolText(m, record.Protocol)
		row.cells[3] = sessionLogResultText(m, record)
		row.cells[4] = sessionLogDurationText(m, record, now)
		row.cells[5] = ""
		row.favoriteLabel = m.Favorite + " " + row.cells[0]
		if record.Favorite {
			row.favoriteLabel = m.Unfavorite + " " + row.cells[0]
		}
		row.compact[0], row.compact[1] = row.cells[0], row.cells[1]
		row.compact[2], row.compact[3] = row.cells[3], ""
		row.minimal[0], row.minimal[1], row.minimal[2] = row.cells[0], row.cells[3], ""
		if view.query.Sort >= sessionlog.SortDurationDesc {
			row.compact[1], row.minimal[1] = row.cells[4], row.cells[4]
		}
	}
	state.prepareSessionLogItems()
}

// prepareSessionLogSummary prepares the counts and the state the page's content
// shows. Five states are kept apart: unavailable, unreadable, loading, nothing
// recorded yet, and nothing matching the filters.
func (state *State) prepareSessionLogSummary() {
	view := &state.sessionLogs.view
	m := &state.messages.SessionLogs
	view.countText = fmt.Sprintf(m.CountSummaryFmt, len(view.page), view.matched, view.total)
	if view.loading {
		view.countText = m.Loading
	}
	if view.unavailable || view.err != "" {
		view.countText = ""
	}
	switch {
	case view.unavailable:
		view.emptyTitle, view.emptyText, view.emptyIcon = m.Unavailable, m.UnavailableHint, iconAlert
		view.emptyAction, view.emptyDanger = "", true
	case view.err != "":
		view.emptyTitle, view.emptyText, view.emptyIcon = m.ListFailed, m.ListFailedHint, iconAlert
		view.emptyAction, view.emptyDanger = "", true
	case view.loading && len(view.page) == 0:
		view.emptyTitle, view.emptyText, view.emptyIcon = m.Loading, m.LoadingHint, iconRefresh
		view.emptyAction, view.emptyDanger = "", false
	case len(view.page) == 0 && state.sessionLogFiltered():
		view.emptyTitle, view.emptyText, view.emptyIcon = m.NoMatches, m.NoMatchesHint, iconSearch
		view.emptyAction, view.emptyDanger = m.ClearFilters, false
	default:
		view.emptyTitle, view.emptyText, view.emptyIcon = m.Empty, m.EmptyHint, iconFileClock
		view.emptyAction, view.emptyDanger = "", false
	}
}

// clampSessionLogSelection keeps the selection on a record that is still in the
// page. A page without the selected record closes the detail panel, so it never
// shows the events of a row that is gone.
func (state *State) clampSessionLogSelection() {
	view := &state.sessionLogs.view
	if view.selected == "" {
		view.detailOpen = false
		return
	}
	for index := range view.page {
		if view.page[index].id == view.selected {
			return
		}
	}
	view.selected = ""
	view.detailOpen = false
	view.events = nil
	view.eventsFor = ""
	view.eventsLoad = false
	view.detail = sessionLogDetail{}
}

// prepareSessionLogDetail formats the detail panel of the selected record: its
// fields, the events that were recorded, and the honest line a record without
// events shows instead of an invented reason.
func (state *State) prepareSessionLogDetail() {
	view := &state.sessionLogs.view
	detail := &view.detail
	detail.fields = [15]sessionLogDetailField{}
	detail.count = 0
	detail.statusIndex = -1
	detail.events = detail.events[:0]
	detail.noInfo = ""
	detail.truncatedText = ""
	m := &state.messages.SessionLogs
	record, ok := state.sessionLogRecord(view.selected)
	if view.selected == "" || !ok {
		detail.title = ""
		detail.eventsText = ""
		detail.live = false
		return
	}
	// The order is the order a short panel keeps: what happened, when, how long
	// and how it ended first, the snapshot details after them.
	detail.title = m.Details
	detail.add(m.FieldTarget, sessionLogTargetText(&record))
	now := state.wallNow()
	detail.add(m.FieldStarted, record.StartedAt.In(state.sessionLogLocation()).Format("2006-01-02 15:04:05"))
	if record.EndedAt.IsZero() {
		detail.add(m.FieldEnded, m.Pending)
	} else {
		label := m.FieldEnded
		if record.Status == sessionlog.StatusInterrupted && record.Reason == sessionlog.ReasonProcessRestart {
			label = m.FieldRecovered
		} else if record.Status == sessionlog.StatusTrackingStopped {
			label = m.FieldStopped
		}
		detail.add(label, record.EndedAt.In(state.sessionLogLocation()).Format("2006-01-02 15:04:05"))
	}
	detail.add(m.FieldDuration, sessionLogDurationText(m, &record, now))
	detail.statusIndex = detail.count
	detail.statusTone = sessionLogToneOf(record.Status)
	detail.add(m.FieldStatus, sessionLogStatusText(m, record.Status))
	if sessionLogTerminal(record.Status) || record.Reason != sessionlog.ReasonNone {
		detail.add(m.FieldResult, sessionLogReasonText(m, record.Reason))
	} else {
		detail.add(m.FieldResult, m.Pending)
	}
	if record.Address != "" {
		label := m.FieldAddress
		if record.Protocol == sessionlog.ProtocolLocal {
			label = m.FieldHostname
		}
		detail.add(label, record.Address)
	}
	if record.Port != 0 {
		detail.add(m.FieldPort, strconv.Itoa(int(record.Port)))
	}
	if record.ShellName != "" {
		detail.add(m.FieldShell, record.ShellName)
	}
	if record.Account != "" {
		detail.add(m.FieldAccount, record.Account)
	}
	detail.add(m.FieldProtocol, sessionLogProtocolText(m, record.Protocol))
	detail.add(m.FieldReconnects, strconv.Itoa(record.ReconnectCount))
	if record.Favorite {
		detail.add(m.FieldFavorite, m.Yes)
	} else {
		detail.add(m.FieldFavorite, m.No)
	}
	owner, _ := state.sessionLogOwner(record.ID)
	detail.live = owner != nil
	detail.actionLabel = m.ChooseHost
	_, detail.hasHost = state.sessionLogHost(record)
	detail.copyEnabled = !view.eventsLoad && view.eventsErr == "" && view.eventsFor == record.ID
	switch {
	case detail.live:
		detail.actionLabel = m.OpenSession
	case record.Protocol == sessionlog.ProtocolLocal && record.TargetKind != sessionlog.TargetShared:
		detail.actionLabel = m.NewLocal
	case detail.hasHost:
		detail.actionLabel = m.Reopen
	}
	switch {
	case view.eventsLoad:
		detail.eventsText = m.Loading
	case len(view.events) == 0:
		detail.eventsText = m.EventsEmpty
	default:
		detail.eventsText = fmt.Sprintf(m.EventsCountFmt, len(view.events))
	}
	if view.eventsMore {
		detail.truncatedText = fmt.Sprintf(m.EventsTruncatedFmt, len(view.events))
	}
	// The panel never invents a reason: without events it says so, and with a
	// result code it does not know it says that too.
	switch {
	case view.eventsLoad:
		detail.noInfo = ""
	case view.eventsErr != "":
		detail.noInfo = m.ListFailedHint
	case len(view.events) == 0:
		detail.noInfo = m.NoMoreInformation
	case sessionLogTerminal(record.Status) && (record.Reason == sessionlog.ReasonNone || record.Reason == sessionlog.ReasonUnknown):
		detail.noInfo = m.NoMoreInformation
	}
	if !view.eventsLoad && view.eventsErr == "" {
		detail.events = state.sessionLogTimeline(record, view.events)
	}
}

func (state *State) sessionLogTimeline(record sessionlog.Record, events []sessionlog.Event) []sessionLogEventRow {
	m := &state.messages.SessionLogs
	rows := make([]sessionLogEventRow, 0, len(events)+1)
	hasFinal := false
	for _, event := range events {
		label := sessionLogEventLabel(m, event)
		if event.Kind == sessionlog.EventFinished || event.Kind == sessionlog.EventRecovered {
			hasFinal = true
			label = sessionLogReasonLabel(m, label, event.Reason)
		}
		if event.Attempt > 0 && sessionLogReconnectEvent(event.Kind) {
			label += " · " + fmt.Sprintf(m.EventAttemptFmt, event.Attempt)
		}
		rows = append(rows, sessionLogEventRow{when: event.At.In(state.sessionLogLocation()).Format("15:04:05"), label: label})
	}
	if !hasFinal && sessionLogTerminal(record.Status) && !record.EndedAt.IsZero() {
		rows = append(rows, sessionLogEventRow{when: record.EndedAt.In(state.sessionLogLocation()).Format("15:04:05"), label: fmt.Sprintf(m.ResultSummaryFmt, sessionLogReasonText(m, record.Reason))})
	}
	return rows
}

// add appends one field in the order the panel paints, up to the panel's room.
func (detail *sessionLogDetail) add(label, value string) {
	if value == "" || detail.count >= len(detail.fields) {
		return
	}
	detail.fields[detail.count] = sessionLogDetailField{label: label, value: value}
	detail.count++
}

// sessionLogTargetText is the row's first cell: the saved host and, when it has
// one, the account it was opened as.
func sessionLogTargetText(record *sessionlog.Record) string {
	if record.Account == "" {
		return record.Host
	}
	return record.Host + " · " + record.Account
}

// sessionLogStartedText is the local start time; the year only appears when it
// is not the current one, which keeps the column readable at a glance.
func sessionLogStartedText(started time.Time, now time.Time) string {
	local := started.In(now.Location())
	if local.Year() == now.Year() {
		return local.Format("01-02 15:04")
	}
	return local.Format("2006-01-02")
}

// sessionLogDurationText is how long the session lasted, or that it is still in
// progress.
func sessionLogDurationText(m *i18n.SessionLogsMessages, record *sessionlog.Record, now time.Time) string {
	if record.Status == sessionlog.StatusInterrupted && record.Reason == sessionlog.ReasonProcessRestart {
		copy := *record
		copy.Status = sessionlog.StatusEnded
		return fmt.Sprintf(m.DurationRecoveredFmt, sessionLogDurationText(m, &copy, now))
	}
	if record.EndedAt.IsZero() {
		return m.DurationOngoing
	}
	span := record.EndedAt.Sub(record.StartedAt)
	if span < time.Second {
		span = time.Second
	}
	switch {
	case span < time.Minute:
		return strconv.Itoa(int(span.Seconds())) + "s"
	case span < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(span.Minutes()), int(span.Seconds())%60)
	case span < 24*time.Hour:
		return fmt.Sprintf("%dh %02dm", int(span.Hours()), int(span.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %02dh", int(span.Hours())/24, int(span.Hours())%24)
	}
}

// sessionLogStatusText localizes one stored status.
func sessionLogStatusText(m *i18n.SessionLogsMessages, status sessionlog.Status) string {
	switch status {
	case sessionlog.StatusConnecting:
		return m.StatusConnecting
	case sessionlog.StatusActive:
		return m.StatusActive
	case sessionlog.StatusReconnecting:
		return m.StatusReconnecting
	case sessionlog.StatusEnded:
		return m.StatusEnded
	case sessionlog.StatusFailed:
		return m.StatusFailed
	case sessionlog.StatusInterrupted:
		return m.StatusInterrupted
	case sessionlog.StatusTrackingStopped:
		return m.StatusTrackingStopped
	default:
		return m.ReasonUnknown
	}
}

func sessionLogResultText(m *i18n.SessionLogsMessages, record *sessionlog.Record) string {
	label := sessionLogStatusText(m, record.Status)
	if sessionLogTerminal(record.Status) {
		label = sessionLogReasonLabel(m, label, record.Reason)
	}
	return label
}

// sessionLogReasonLabel appends a reason only when it adds what the label does
// not already say. A normal end has no reason to add, and the tracking-stop and
// recovery labels already name their cause.
func sessionLogReasonLabel(m *i18n.SessionLogsMessages, label string, reason sessionlog.Reason) string {
	switch reason {
	case sessionlog.ReasonNone, sessionlog.ReasonTrackingOff, sessionlog.ReasonProcessRestart:
		return label
	}
	text := sessionLogReasonText(m, reason)
	if text == label {
		return label
	}
	return label + " · " + text
}

func sessionLogOutcomeChoices(m *i18n.SessionLogsMessages) [4]string {
	return [4]string{m.OutcomeAll, m.OutcomeNormal, m.OutcomeAbnormal, m.StatusTrackingStopped}
}

// sessionLogToneOf is how the detail panel colors a status.
func sessionLogToneOf(status sessionlog.Status) sessionLogStatusTone {
	switch status {
	case sessionlog.StatusActive:
		return sessionLogToneSuccess
	case sessionlog.StatusConnecting, sessionlog.StatusReconnecting, sessionlog.StatusInterrupted, sessionlog.StatusTrackingStopped:
		return sessionLogToneWarning
	case sessionlog.StatusFailed:
		return sessionLogToneDanger
	default:
		return sessionLogToneNeutral
	}
}

// sessionLogToneColor maps a status tone onto the palette.
func (state *State) sessionLogToneColor(tone sessionLogStatusTone) gfx.Color {
	switch tone {
	case sessionLogToneSuccess:
		return state.pal.success
	case sessionLogToneWarning:
		return state.pal.warning
	case sessionLogToneDanger:
		return state.pal.danger
	default:
		return state.pal.text
	}
}

// sessionLogTerminal reports whether a stored status is final. It mirrors the
// store's own rule; the store keeps the authority, the page only chooses words.
func sessionLogTerminal(status sessionlog.Status) bool {
	switch status {
	case sessionlog.StatusEnded, sessionlog.StatusFailed, sessionlog.StatusInterrupted, sessionlog.StatusTrackingStopped:
		return true
	default:
		return false
	}
}

// sessionLogProtocolText localizes one stored protocol.
func sessionLogProtocolText(m *i18n.SessionLogsMessages, protocol sessionlog.Protocol) string {
	switch protocol {
	case sessionlog.ProtocolSSH:
		return m.ProtocolSSH
	case sessionlog.ProtocolLocal:
		return m.ProtocolLocal
	case sessionlog.ProtocolTelnet:
		return m.ProtocolTelnet
	case sessionlog.ProtocolSerial:
		return m.ProtocolSerial
	case sessionlog.ProtocolVNC:
		return m.ProtocolVNC
	case sessionlog.ProtocolRDP:
		return m.ProtocolRDP
	default:
		return m.ProtocolSSH
	}
}

// sessionLogReasonText is the normalized result code: a known reason, or the
// honest statement that none was recorded. It is never an error string.
func sessionLogReasonText(m *i18n.SessionLogsMessages, reason sessionlog.Reason) string {
	switch reason {
	case sessionlog.ReasonUserClosed:
		return m.ReasonUserClosed
	case sessionlog.ReasonConnectFailed:
		return m.ReasonConnectFailed
	case sessionlog.ReasonAuthFailed:
		return m.ReasonAuthFailed
	case sessionlog.ReasonHostKeyRejected:
		return m.ReasonHostKeyRefused
	case sessionlog.ReasonCanceled:
		return m.ReasonCanceled
	case sessionlog.ReasonTimeout:
		return m.ReasonTimeout
	case sessionlog.ReasonNetworkLost:
		return m.ReasonNetworkLost
	case sessionlog.ReasonShellFailed:
		return m.ReasonShellFailed
	case sessionlog.ReasonTrackingOff:
		return m.ReasonTrackingOff
	case sessionlog.ReasonProcessRestart:
		return m.ReasonProcessRestart
	case sessionlog.ReasonUnknown:
		return m.ReasonUnknown
	default:
		return m.ReasonNone
	}
}

// sessionLogEventText localizes one lifecycle event.
func sessionLogEventText(m *i18n.SessionLogsMessages, kind sessionlog.EventKind) string {
	switch kind {
	case sessionlog.EventStarted:
		return m.EventStarted
	case sessionlog.EventHostKeyPrompt:
		return m.EventHostKeyPrompt
	case sessionlog.EventAuthenticating:
		return m.EventAuthenticating
	case sessionlog.EventFinished:
		return m.EventFinished
	case sessionlog.EventRecovered:
		return m.EventRecovered
	case sessionlog.EventConnected:
		return m.EventConnected
	case sessionlog.EventShellOpened:
		return m.EventShellOpened
	case sessionlog.EventShellFailed:
		return m.EventShellFailed
	case sessionlog.EventDisconnected:
		return m.EventDisconnected
	case sessionlog.EventFailed:
		return m.EventFailed
	case sessionlog.EventReconnectScheduled:
		return m.EventReconnectScheduled
	case sessionlog.EventReconnectStarted:
		return m.EventReconnectStarted
	case sessionlog.EventReconnectExhausted:
		return m.EventReconnectExhausted
	case sessionlog.EventReconnectCanceled:
		return m.EventReconnectCanceled
	case sessionlog.EventTrackingStopped:
		return m.EventTrackingStopped
	default:
		return m.ReasonUnknown
	}
}

func sessionLogEventLabel(m *i18n.SessionLogsMessages, event sessionlog.Event) string {
	if event.Kind == sessionlog.EventFinished {
		switch event.Reason {
		case sessionlog.ReasonTrackingOff:
			return m.EventTrackingStopped
		case sessionlog.ReasonProcessRestart:
			return m.EventRecovered
		}
	}
	return sessionLogEventText(m, event.Kind)
}

// sessionLogReconnectEvent reports whether an event counts a retry attempt.
func sessionLogReconnectEvent(kind sessionlog.EventKind) bool {
	switch kind {
	case sessionlog.EventReconnectScheduled, sessionlog.EventReconnectStarted,
		sessionlog.EventReconnectExhausted, sessionlog.EventReconnectCanceled:
		return true
	default:
		return false
	}
}

// sessionLogFilterLabel is the accessible name of a filter trigger: the filter's
// own name, which a trigger showing only its value cannot say.
func sessionLogFilterLabel(m *i18n.SessionLogsMessages, filter sessionLogFilterMenu) string {
	switch filter {
	case sessionLogFilterSortMenu:
		return m.FilterSort
	case sessionLogFilterOutcomeMenu:
		return m.FilterOutcome
	case sessionLogFilterTimeMenu:
		return m.FilterTime
	case sessionLogFilterStatusMenu:
		return m.FilterStatus
	default:
		return m.FilterProtocol
	}
}

// sessionLogFilterValue is the visible value of a filter trigger.
func (state *State) sessionLogFilterValue(filter sessionLogFilterMenu) string {
	view, m := &state.sessionLogs.view, &state.messages.SessionLogs
	switch filter {
	case sessionLogFilterSortMenu:
		choices := sessionLogSortChoices(m)
		if view.query.Sort <= sessionlog.SortDurationAsc {
			return choices[view.query.Sort]
		}
		return m.SortNewest
	case sessionLogFilterOutcomeMenu:
		choices := sessionLogOutcomeChoices(m)
		if view.query.Outcome <= sessionlog.OutcomeTrackingStopped {
			return choices[view.query.Outcome]
		}
		return m.OutcomeAll
	case sessionLogFilterTimeMenu:
		switch view.timeRange {
		case sessionLogTimeToday:
			return m.TimeToday
		case sessionLogTimeWeek:
			return m.Time7Days
		case sessionLogTimeMonth:
			return m.Time30Days
		case sessionLogTimeCustom:
			return m.TimeCustom
		default:
			return m.TimeAll
		}
	case sessionLogFilterStatusMenu:
		if view.statusFilter == 0 {
			return m.StatusAll
		}
		return sessionLogStatusText(m, view.statusFilter)
	default:
		if view.protocolFilter == 0 {
			return m.ProtocolAll
		}
		return sessionLogProtocolText(m, view.protocolFilter)
	}
}

// sessionLogFilterIcon is the glyph that names a filter to the eye.
func sessionLogFilterIcon(filter sessionLogFilterMenu) uint8 {
	switch filter {
	case sessionLogFilterSortMenu:
		return iconTransfer
	case sessionLogFilterTimeMenu:
		return iconClock
	case sessionLogFilterStatusMenu:
		return iconCircleCheck
	default:
		return iconGlobe
	}
}

func sessionLogSortChoices(m *i18n.SessionLogsMessages) [4]string {
	return [4]string{m.SortNewest, m.SortOldest, m.SortLongest, m.SortShortest}
}

// sessionLogFilterChoices fills the labels and checked states of the open
// filter's choices and returns how many there are. Painting and clicking share
// it, so a choice is the same item in both.
func (state *State) sessionLogFilterChoices(filter sessionLogFilterMenu, labels *[8]string, checked *[8]bool) int {
	view, m := &state.sessionLogs.view, &state.messages.SessionLogs
	switch filter {
	case sessionLogFilterSortMenu:
		for i, label := range sessionLogSortChoices(m) {
			labels[i], checked[i] = label, view.query.Sort == sessionlog.SortOrder(i)
		}
		return 4
	case sessionLogFilterOutcomeMenu:
		for i, label := range sessionLogOutcomeChoices(m) {
			labels[i], checked[i] = label, view.query.Outcome == sessionlog.OutcomeFilter(i)
		}
		return 4
	case sessionLogFilterTimeMenu:
		labels[0], labels[1], labels[2], labels[3] = m.TimeAll, m.TimeToday, m.Time7Days, m.Time30Days
		checked[0] = view.timeRange == sessionLogTimeAll
		checked[1] = view.timeRange == sessionLogTimeToday
		checked[2] = view.timeRange == sessionLogTimeWeek
		checked[3] = view.timeRange == sessionLogTimeMonth
		labels[4], checked[4] = m.TimeCustom, view.timeRange == sessionLogTimeCustom
		return 5
	case sessionLogFilterStatusMenu:
		labels[0] = m.StatusAll
		checked[0] = view.statusFilter == 0
		for index, status := range sessionLogStatusChoices[1:] {
			labels[index+1] = sessionLogStatusText(m, status)
			checked[index+1] = view.statusFilter == status
		}
		return len(sessionLogStatusChoices)
	default:
		labels[0] = m.ProtocolAll
		checked[0] = view.protocolFilter == 0
		for index, protocol := range sessionLogProtocolChoices[1:] {
			labels[index+1] = sessionLogProtocolText(m, protocol)
			checked[index+1] = view.protocolFilter == protocol
		}
		return len(sessionLogProtocolChoices)
	}
}

// sessionLogFilterFor is the filter menu a trigger identity opens.
func sessionLogFilterFor(id gfx.ID) sessionLogFilterMenu {
	switch id {
	case hitSessionLogsSort:
		return sessionLogFilterSortMenu
	case hitSessionLogsOutcome:
		return sessionLogFilterOutcomeMenu
	case hitSessionLogsTime:
		return sessionLogFilterTimeMenu
	case hitSessionLogsStatus:
		return sessionLogFilterStatusMenu
	case hitSessionLogsProtocol:
		return sessionLogFilterProtocolMenu
	default:
		return sessionLogFilterNone
	}
}

// sessionLogsSectionUI is what the session journal's declaration leaves for its
// paint: the empty state's icon, and the focus move an event asked for.
type sessionLogsSectionUI struct {
	empty      ui.Box
	emptyShown bool
	// draw is the page hook that paints the empty state's icon and delivers a
	// requested focus move; eventsDraw paints the detail panel's event rows.
	// Both are kept so a paint does not build a closure.
	draw, eventsDraw func(*ui.DrawContext)
}

// declareSessionLogsWorkspace declares the session journal in the workbench
// page: its header, search and filters, its table and detail panel, its empty
// state, and the menus its triggers open.
func (state *State) declareSessionLogsWorkspace(c ui.C) {
	canvas := c.Canvas()
	view, m, pal := &state.sessionLogs.view, &state.messages.SessionLogs, state.pal
	bounds := state.catalogSectionBounds()
	inner := max(bounds.Width-36, 0)
	narrow := inner < sessionLogFullColumns
	separateFilters := inner < 1260
	sideDetail := inner >= sessionLogSideDetail && view.detailOpen && view.selected != ""
	detailOnly := view.detailOpen && view.selected != "" && !sideDetail
	empty := len(view.page) == 0
	condensed := inner < 520
	filterWidth := sessionLogFilterWidth
	stackedFilters := separateFilters && inner < 680
	if separateFilters {
		columns := float32(5)
		if stackedFilters {
			columns = 3
		}
		filterWidth = min(sessionLogFilterWidth, max(76, (inner-(columns-1)*sessionLogToolbarGap)/columns))
	}
	searchWidth := min(sessionLogSearchMost, max(sessionLogSearchLeast, inner-5*filterWidth-toolbarHeight-7*sessionLogToolbarGap))
	if separateFilters {
		searchWidth = min(sessionLogSearchMost, max(sessionLogSearchLeast, inner-toolbarHeight-2*sessionLogToolbarGap))
	}
	layout := &state.sectionBodies.sessionLogs
	draw, eventsDraw := layout.draw, layout.eventsDraw
	*layout = sessionLogsSectionUI{}
	if draw == nil {
		draw = func(d *ui.DrawContext) { state.paintSessionLogsWorkspace(d.Canvas) }
	}
	layout.draw, layout.eventsDraw = draw, eventsDraw

	var timeTrigger, statusTrigger, protocolTrigger, outcomeTrigger, sortTrigger ui.Box
	body := ui.Column(c).Fill().PadSides(12, 18, 0, 18)
	body.Children(func() {
		ui.Row(c).Height(36).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, m.Title).FontSize(sessionLogTitleSize).Weight(gfx.WeightSemibold).Color(pal.text).Grow(1).Ellipsis()
			ui.Text(c, view.countText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Width(min(inner*0.62, 360)).TextEnd().Ellipsis()
			moreStyle := iconButtonQuiet
			if view.moreMenu {
				moreStyle = iconButtonActive
			}
			moreButton := state.declareIconButton(c, hitSessionLogsMore, iconEllipsis, m.More, false, moreStyle).Size(30, 30)
			state.declareSessionLogMoreMenu(c, moreButton)
		})
		if !narrow {
			ui.Text(c, m.Description).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18).Ellipsis().MarginSides(0, 0, 8, 0)
		}
		ui.Row(c).Height(40).Gap(sessionLogToolbarGap).Items(ui.Center).Children(func() {
			if !view.searchBound {
				view.searchField.Bind(&view.search)
				view.searchBound = true
			}
			ui.TextField(c, &view.searchField, m.Search, m.SearchPlaceholder, ui.WithID(hitSessionLogsSearch)).
				Icon(lucideIcons[iconSearch]).Size(searchWidth, toolbarHeight)
			if !separateFilters {
				ui.Spacer(c)
				timeTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsTime, sessionLogFilterTimeMenu, filterWidth).Size(filterWidth, toolbarHeight)
				statusTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsStatus, sessionLogFilterStatusMenu, filterWidth).Size(filterWidth, toolbarHeight)
				protocolTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsProtocol, sessionLogFilterProtocolMenu, filterWidth).Size(filterWidth, toolbarHeight)
				outcomeTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsOutcome, sessionLogFilterOutcomeMenu, filterWidth).Size(filterWidth, toolbarHeight)
				sortTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsSort, sessionLogFilterSortMenu, filterWidth).Size(filterWidth, toolbarHeight)
				state.declareSessionLogFavoriteFilter(c).Size(toolbarHeight, toolbarHeight)
			} else {
				ui.Spacer(c)
				state.declareSessionLogFavoriteFilter(c).Size(toolbarHeight, toolbarHeight)
			}
		})
		if separateFilters {
			ui.Row(c).Height(38).Gap(sessionLogToolbarGap).Items(ui.Center).Children(func() {
				timeTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsTime, sessionLogFilterTimeMenu, filterWidth).Size(filterWidth, toolbarHeight)
				statusTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsStatus, sessionLogFilterStatusMenu, filterWidth).Size(filterWidth, toolbarHeight)
				protocolTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsProtocol, sessionLogFilterProtocolMenu, filterWidth).Size(filterWidth, toolbarHeight)
				if !stackedFilters {
					outcomeTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsOutcome, sessionLogFilterOutcomeMenu, filterWidth).Size(filterWidth, toolbarHeight)
					sortTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsSort, sessionLogFilterSortMenu, filterWidth).Size(filterWidth, toolbarHeight)
				}
			})
			if stackedFilters {
				ui.Row(c).Height(38).Gap(sessionLogToolbarGap).Items(ui.Center).Children(func() {
					outcomeTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsOutcome, sessionLogFilterOutcomeMenu, filterWidth).Size(filterWidth, toolbarHeight)
					sortTrigger = state.declareSessionLogFilterTrigger(c, hitSessionLogsSort, sessionLogFilterSortMenu, filterWidth).Size(filterWidth, toolbarHeight)
				})
			}
		}
		state.declareSessionLogFilterMenu(c, timeTrigger, statusTrigger, protocolTrigger, outcomeTrigger, sortTrigger)
		// The detail panel takes what the fixed rows leave: its height decides
		// how many fields it can show and whether the events still fit.
		detailHeight := bounds.Height - 12 - 36 - 40 - 1 - 46
		if !narrow {
			detailHeight -= 26
		}
		if separateFilters {
			detailHeight -= 38
			if stackedFilters {
				detailHeight -= 38
			}
		}
		detailHeight = max(detailHeight, 0)
		switch {
		case empty:
			descriptionColor := pal.muted
			if view.emptyDanger {
				descriptionColor = pal.danger
			}
			layout.empty = state.reserveEmptyPageWithColor(c, bounds, view.emptyTitle, view.emptyText, view.emptyAction, descriptionColor, hitSessionLogsClearFilters, view.emptyIcon, true)
			layout.emptyShown = true
		case sideDetail:
			ui.Row(c).Grow(1).Basis(0).Gap(14).Children(func() {
				detailWidth := min(sessionLogDetailWidth, inner*0.46)
				ui.Column(c).Grow(1).Basis(0).Children(func() {
					view.tableWidth = max(inner-14-detailWidth, 0)
					state.declareSessionLogTable(c)
				})
				state.declareSessionLogDetails(c, false, detailWidth, detailHeight)
			})
		case detailOnly:
			state.declareSessionLogDetails(c, true, inner, detailHeight)
		default:
			view.tableWidth = inner
			ui.Column(c).Grow(1).Basis(0).MarginTop(4).Children(func() {
				state.declareSessionLogTable(c)
			})
		}
		ui.Divider(c).Background(pal.line)
		ui.Row(c).Height(46).Gap(12).Items(ui.Center).Children(func() {
			ui.Text(c, view.statusText).FontSize(metrics.FontSizeS).Color(state.sessionLogStatusColor()).Grow(1).Ellipsis()
			if view.more {
				loadMoreWidth, _ := toolbarButtonLayout(canvas, m.LoadMore, iconChevronDown, iconChevronDown, condensed)
				label := m.LoadMore
				if view.loadingMore {
					label = m.Loading
				}
				state.declareButtonSized(c, hitSessionLogsLoadMore, label, iconChevronDown, false, false, !view.loadingMore, toolbarFont, condensed).Size(loadMoreWidth, toolbarHeight)
			}
		})
	})
	body.Clip().DrawOver(layout.draw)
}

// declareSessionLogMoreMenu declares the journal actions under the More
// trigger while it is open. A choice runs as if its control had been clicked.
func (state *State) declareSessionLogMoreMenu(c ui.C, anchor ui.Box) {
	if !state.sessionLogs.view.moreMenu {
		return
	}
	m := &state.messages.SessionLogs
	var rows menuRows
	rows.add(components.ContextMenuItem{ID: hitSessionLogsClearFavorite, Label: m.ClearUnfavorited, Icon: lucideIcons[iconEraser], Disabled: !state.sessionLogsCanClear(), Variant: components.ContextMenuItemDestructive})
	rows.add(components.ContextMenuItem{ID: hitSessionLogsClearAll, Label: m.ClearAll, Icon: lucideIcons[iconTrash], Disabled: !state.sessionLogsCanClear(), Variant: components.ContextMenuItemDestructive})
	ui.Menu(c, anchor, &state.sessionLogs.view.moreMenu, func(menu *ui.MenuItems) {
		rows.declare(c, menu, func(id gfx.ID) { state.click(id, true) })
	})
}

// declareSessionLogFilterMenu declares the open filter's choices under its
// trigger. A choice runs the filter it belonged to, which the popup carries in
// its closure: the menu closes as the choice is delivered.
func (state *State) declareSessionLogFilterMenu(c ui.C, triggers ...ui.Box) {
	view := &state.sessionLogs.view
	filter := view.filterMenu
	if !view.filterOpen || filter == sessionLogFilterNone {
		return
	}
	index := int(filter) - 1
	if index < 0 || index >= len(triggers) || !triggers[index].Valid() {
		view.filterMenu, view.filterOpen = sessionLogFilterNone, false
		return
	}
	var labels [8]string
	var checked [8]bool
	count := state.sessionLogFilterChoices(filter, &labels, &checked)
	if count == 0 {
		view.filterMenu, view.filterOpen = sessionLogFilterNone, false
		return
	}
	rows := make([]components.ContextMenuItem, count)
	for index := 0; index < count; index++ {
		rows[index] = components.ContextMenuItem{
			ID: hitSessionLogsFilterList.At(index), Label: labels[index],
			Kind: components.ContextMenuRadio, Selected: checked[index],
		}
	}
	ui.Menu(c, triggers[index], &view.filterOpen, func(menu *ui.MenuItems) {
		menu.WidthRange(220, 0)
		for choice := range rows {
			row := &rows[choice]
			item := menu.Item(row.Label).Chosen(row.Selected)
			if item.Clicked() {
				chosen := choice
				c.After(func() { state.chooseSessionLogFilterIn(filter, chosen) })
			}
		}
	})
	if !view.filterOpen {
		// Escape, Tab or a press away closed the page menu.
		view.filterMenu = sessionLogFilterNone
	}
}

// paintSessionLogsWorkspace paints the empty state's icon and delivers a focus
// move an event handler asked for into the slots its declaration left.
func (state *State) paintSessionLogsWorkspace(canvas *gfx.Canvas) {
	view, layout := &state.sessionLogs.view, &state.sectionBodies.sessionLogs
	if layout.emptyShown {
		state.drawReservedEmptyPage(canvas, layout.empty, view.emptyIcon)
	}
	// A focus move an event handler asked for is honored by the next paint: a
	// click has no return value to carry it, so it waits here for the one place
	// a focus request belongs.
	if !view.focusPending.IsZero() {
		canvas.RequestFocus(view.focusPending)
		view.focusPending = gfx.ID{}
	}
}

// sessionLogSelectedRecord is the selected record while the page still holds it.
func (state *State) sessionLogSelectedRecord() *sessionlog.Record {
	view := &state.sessionLogs.view
	if view.selected == "" {
		return nil
	}
	for index := range view.rows {
		if view.rows[index].ID == view.selected {
			return &view.rows[index]
		}
	}
	return nil
}

// sessionLogFavoriteLabel is the favorite action's name: it says which way the
// selected record will change.
func (state *State) sessionLogFavoriteLabel() string {
	m := &state.messages.SessionLogs
	if record := state.sessionLogSelectedRecord(); record != nil && record.Favorite {
		return m.Unfavorite
	}
	return m.Favorite
}

// sessionLogFavoriteIcon is the filled star for a favorite record.
func (state *State) sessionLogFavoriteIcon() uint8 {
	if record := state.sessionLogSelectedRecord(); record != nil && record.Favorite {
		return iconStarSolid
	}
	return iconStar
}

// sessionLogsCanClear reports whether there is a journal to clear. Records other
// than the loaded page may exist, so the actions follow the page's state rather
// than its row count.
func (state *State) sessionLogsCanClear() bool {
	view := &state.sessionLogs.view
	return view.loaded && !view.unavailable
}

// sessionLogStatusColor is the footer text's color: an operation failure stays in
// the page's own status line instead of a toast that disappears.
func (state *State) sessionLogStatusColor() gfx.Color {
	if state.sessionLogs.view.statusDanger {
		return state.pal.danger
	}
	return state.pal.muted
}

// declareSessionLogFilterTrigger declares one filter in the page: a small
// control that shows the chosen value, names itself to the keyboard, and says
// whether its menu is open. The caller sizes it.
func (state *State) declareSessionLogFilterTrigger(c ui.C, id gfx.ID, filter sessionLogFilterMenu, width float32) ui.Box {
	view, m, pal := &state.sessionLogs.view, &state.messages.SessionLogs, state.pal
	open := view.filterMenu == filter
	active := false
	switch filter {
	case sessionLogFilterTimeMenu:
		active = view.timeRange != sessionLogTimeAll
	case sessionLogFilterStatusMenu:
		active = view.statusFilter != 0
	case sessionLogFilterProtocolMenu:
		active = view.protocolFilter != 0
	case sessionLogFilterOutcomeMenu:
		active = view.query.Outcome != sessionlog.OutcomeAll
	case sessionLogFilterSortMenu:
		active = view.query.Sort != sessionlog.SortStartedDesc
	}
	foreground := pal.muted
	border := pal.line
	if open || active {
		foreground, border = pal.text, withAlpha(pal.accent, 0.55)
	}
	label := sessionLogFilterLabel(m, filter)
	hint := label
	if filter == sessionLogFilterTimeMenu && view.customRangeHint != "" {
		hint = view.customRangeHint
	}
	value := state.sessionLogFilterValue(filter)
	// The chevron is what a trigger past 96 pixels can afford; a narrower one
	// keeps the value legible and says it opens with its accessible name.
	chevron := width >= 96
	trigger := ui.ButtonBase(c, label).Radius(radius.control).Border(1, border).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint, Selected: active, Expanded: open, Expandable: true}).
		Focus(ui.Look{}.Ring(1.5, pal.accent))
	if !open && !active {
		trigger = trigger.Hover(ui.Look{}.Color(pal.text)).Pressed(ui.Look{}.Background(pal.surfaceHot))
	}
	trigger.Children(func() {
		ui.Icon(c, lucideIcons[sessionLogFilterIcon(filter)]).Color(foreground).MarginSides(0, 6, 0, 8).
			StateFrom(trigger).Hover(ui.Look{}.Color(pal.text))
		ui.Text(c, value).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground).Ellipsis().
			StateFrom(trigger).Hover(ui.Look{}.Color(pal.text))
		if chevron {
			ui.Spacer(c)
			ui.Icon(c, lucideIcons[iconChevronDown]).Color(pal.faint)
		}
	})
	return trigger
}

// declareSessionLogFavoriteFilter declares the favorites-only toggle in the
// page: a star that fills while the filter is on. It is a filter, so it reports
// whether it is applied rather than whether it has the keyboard.
func (state *State) declareSessionLogFavoriteFilter(c ui.C) ui.Box {
	view, m, pal := &state.sessionLogs.view, &state.messages.SessionLogs, state.pal
	icon := iconStar
	foreground := pal.muted
	border := pal.line
	if view.favoriteOnly {
		icon, foreground, border = iconStarSolid, pal.warning, withAlpha(pal.warning, 0.55)
	}
	toggle := ui.ButtonBase(c, m.FilterFavorite).Radius(radius.control).Border(1, border).
		HitWith(hitSessionLogsFavoriteFilter, m.FilterFavorite, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.FilterFavorite, Selected: view.favoriteOnly}).
		Focus(ui.Look{}.Ring(1.5, pal.accent)).
		Hover(ui.Look{}.Color(pal.text)).
		Pressed(ui.Look{}.Background(pal.surfaceHot))
	toggle.Children(func() {
		ui.Icon(c, lucideIcons[icon]).Color(foreground).StateFrom(toggle).Hover(ui.Look{}.Color(pal.text))
	})
	return toggle
}

// declareSessionLogTable declares the journal's record table: its fixed
// heading row and the page List under it. The list's rows are the date headings
// and the records; a click, Enter or typing chooses a record, and the arrows,
// page keys and wheel are the page's, which owns the list's scroll offset.
func (state *State) declareSessionLogTable(c ui.C) {
	view, m, theme := &state.sessionLogs.view, &state.messages.SessionLogs, state.Theme()
	if view.tableWidth < 80 {
		return
	}
	if !view.rowsInit {
		view.rowsInit = true
		view.rowsList.Label = state.sessionLogItemLabel
		view.rowsList.Header = state.sessionLogItemHeader
		view.rowsList.NoTypeahead = true
		view.rowDecl = state.declareSessionLogItem
	}
	headerHeight := theme.Metrics.ControlHeight + 8
	view.rowClicked = false
	ui.Row(c).Height(headerHeight).Shrink(0).FillWidth().Children(func() {
		widths, headings, count := state.sessionLogColumns(view.tableWidth)
		for column := 0; column < count; column++ {
			ui.Row(c).Width(widths[column]).Height(headerHeight).Shrink(0).Items(ui.Center).Children(func() {
				ui.Text(c, headings[column]).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightMedium).
					Color(theme.Foreground).LineHeight(headerHeight).PadSides(0, theme.Metrics.GapItem, 0, theme.Metrics.GapItem).Ellipsis()
			})
		}
	})
	if len(view.items) == 0 {
		return
	}
	list := ui.List(c, &view.rowsList, len(view.items), view.rowDecl).
		Grow(1).Basis(0).FillWidth().AlwaysScrollbars().ScrollbarSize(catalogBarGutter).Label(m.Title)
	if scroll := list.ScrollState(); scroll != nil {
		view.scrollRows = scroll.Offset
	}
	state.takeSessionLogListChoice()
}

// takeSessionLogListChoice turns the list's choice into the page's selection.
// A click or Enter opens the record's detail panel; the arrows only move the
// list's current row, whose detail stays as it was until one of them opens it.
func (state *State) takeSessionLogListChoice() {
	view := &state.sessionLogs.view
	open := -1
	if index, ok := view.rowsList.Submitted(); ok {
		open = index
	}
	if view.rowsList.Changed() {
		if view.rowClicked {
			if index := view.rowsList.Selected(); index >= 0 {
				open = index
			}
		}
	} else if open < 0 {
		view.rowsList.Select(state.sessionLogSelectedItemIndex())
	}
	if open < 0 || open >= len(view.items) || view.items[open].row < 0 {
		return
	}
	state.selectSessionLogRecord(view.page[view.items[open].row].id)
}

// sessionLogHeaderSortable says whether clicking column's heading sorts: the
// when column and the duration column are the store's two orders, and a
// narrowest table shows the duration in the when column only while one of the
// duration orders is chosen.
func (state *State) sessionLogHeaderSortable(column, count int) bool {
	switch column {
	case 4:
		return count == 6
	case 1:
		return count != 3 || state.sessionLogs.view.query.Sort >= sessionlog.SortDurationDesc
	}
	return false
}

// sessionLogItemHeader says which rows of the record list are date headings.
func (state *State) sessionLogItemHeader(index int) bool {
	items := state.sessionLogs.view.items
	return index >= 0 && index < len(items) && items[index].row < 0
}

// sessionLogItemLabel is a row's text for typeahead and assistive technology:
// the heading, or the record's target and account.
func (state *State) sessionLogItemLabel(index int) string {
	view := &state.sessionLogs.view
	if index < 0 || index >= len(view.items) {
		return ""
	}
	item := view.items[index]
	if item.row < 0 {
		return item.heading
	}
	if item.row >= len(view.page) {
		return ""
	}
	return view.page[item.row].cells[0]
}

// declareSessionLogItem declares one row of the record list: a date heading, or
// a record's cells at the table's column widths with its favorite star at the
// end. The star is a page button of its own, so pressing it does not choose the
// record, as the table's separate cell did.
func (state *State) declareSessionLogItem(row ui.ListRow) {
	view, pal, theme := &state.sessionLogs.view, state.pal, state.Theme()
	if row.Index < 0 || row.Index >= len(view.items) {
		return
	}
	item := view.items[row.Index]
	if item.row < 0 {
		row.Element.Height(sessionLogGroupHeight).Shrink(0).FillWidth().PadX(8).Items(ui.Center).Children(func() {
			ui.Text(row.C, item.heading).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(pal.muted).Ellipsis()
		})
		return
	}
	if item.row >= len(view.page) {
		return
	}
	view.rowScope = row.ID.Scope()
	if row.C.Canvas().Clicked(row.ID) {
		view.rowClicked = true
	}
	record := &view.page[item.row]
	widths, _, count := state.sessionLogColumns(view.tableWidth)
	rowHeight := theme.Metrics.ControlHeight + 4
	row.Element.Height(rowHeight).Shrink(0).FillWidth().Items(ui.Center)
	if row.Selected() {
		row.Element = row.Element.Background(theme.Muted)
	} else {
		row.Element = row.Element.Hover(ui.Look{}.Background(theme.Hover))
	}
	row.Element.Children(func() {
		cells := record.cells[:]
		switch count {
		case 4:
			cells = record.compact[:]
		case 3:
			cells = record.minimal[:]
		}
		for column := 0; column < count-1; column++ {
			ui.Text(row.C, cells[column]).Width(widths[column]).Height(rowHeight).
				PadSides(0, theme.Metrics.GapItem, 0, theme.Metrics.GapItem).LineHeight(rowHeight).
				FontSize(theme.Metrics.FontSizeM).Color(theme.Foreground).Ellipsis()
		}
		icon, style := iconStar, iconButtonQuiet
		if record.favorite {
			icon, style = iconStarSolid, iconButtonActive
		}
		state.declareIconButton(row.C, hitSessionLogsRowFavorite.At(item.row), icon, record.favoriteLabel,
			view.favoritePending != "", style).Size(widths[count-1], rowHeight)
	})
}

// sessionLogColumns lays out the table for a card width. The target column
// takes what the others leave. The widths are what the longest localized value
// needs: a time (100), a status (140), a protocol (84) and a duration (80). A
// card too narrow for six columns drops protocol and duration, and the
// narrowest drops the status too; the detail panel still shows everything the
// row leaves out.
func (state *State) sessionLogColumns(width float32) (widths [6]float32, headings [6]string, count int) {
	m := &state.messages.SessionLogs
	favorite := float32(30)
	if width >= sessionLogFullColumns {
		when, protocol, status, duration := float32(104), float32(84), float32(200), float32(80)
		widths[0] = max(width-when-protocol-status-duration-favorite, 140)
		widths[1], widths[2], widths[3], widths[4], widths[5] = when, protocol, status, duration, favorite
		headings[0], headings[1], headings[2] = m.ColumnTarget, m.ColumnWhen, m.ColumnProtocol
		headings[3], headings[4] = m.ColumnStatus, m.ColumnDuration
		return widths, headings, 6
	}
	if width >= sessionLogCompactColumns {
		when, status := float32(100), float32(190)
		widths[0] = max(width-when-status-favorite, 100)
		widths[1], widths[2], widths[3] = when, status, favorite
		headings[0], headings[1], headings[2] = m.ColumnTarget, m.ColumnWhen, m.ColumnStatus
		if state.sessionLogs.view.query.Sort >= sessionlog.SortDurationDesc {
			headings[1] = m.ColumnDuration
		}
		return widths, headings, 4
	}
	when := float32(100)
	widths[0] = max(width-when-favorite, 80)
	widths[1], widths[2] = when, favorite
	headings[0], headings[1] = m.ColumnTarget, m.ColumnStatus
	if state.sessionLogs.view.query.Sort >= sessionlog.SortDurationDesc {
		headings[1] = m.ColumnDuration
	}
	return widths, headings, 3
}

// declareSessionLogDetails declares the selected record: its fields, the
// lifecycle events that were recorded on a page scroll area, and the way to the
// still-open session when there is one. Nothing here writes, and no replay is
// offered because no recording exists.
func (state *State) declareSessionLogDetails(c ui.C, stacked bool, width, height float32) {
	detail, m, pal := &state.sessionLogs.view.detail, &state.messages.SessionLogs, state.pal
	if width <= 0 {
		return
	}
	inner := max(width-28, 0)
	if inner <= 0 {
		return
	}
	// The panel is short enough that its fields must give way: the old panel
	// kept the first fields and the actions at the bottom, and showed the
	// events only while there was room for them.
	innerHeight := max(height-24, 0)
	fields := detail.count
	if fields > 0 {
		room := int((innerHeight - 26 - sessionLogEventsRoom) / sessionLogFieldHeight)
		fields = min(fields, max(room, 3))
	}
	y := float32(26)
	y += float32(fields) * sessionLogFieldHeight
	showNoInfo := detail.noInfo != "" && y+18 <= innerHeight
	if showNoInfo {
		y += 20
	}
	eventsBottom := innerHeight - 2*toolbarHeight - 6 - 10
	showEvents := innerHeight > 0 && eventsBottom-y > 34
	showTruncated := showEvents && detail.truncatedText != "" && y+24+16 <= eventsBottom

	panel := ui.Column(c).Fill().Width(width).Clip().Radius(radius.surface).Border(1, pal.line).Background(pal.surface).
		PadSides(12, 14, 12, 14).Gap(0)
	panel.Children(func() {
		closeHint := m.CloseDetail
		if stacked {
			closeHint = m.BackToList
		}
		// The old header row was 26 high and its close button, 28 high, sat two
		// pixels above it, so the button's top stayed flush with the panel.
		ui.Row(c).Height(26).Shrink(0).Items(ui.Start).Children(func() {
			ui.Text(c, detail.title).Height(22).LineHeight(22).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).
				Color(pal.text).Grow(1).Ellipsis()
			state.declareIconButton(c, hitSessionLogsDetailClose, iconClose, closeHint, false, iconButtonQuiet).
				Size(28, 28).MarginTop(-2)
		})
		ui.Column(c).Grow(1).Basis(0).MinHeight(0).Clip().Children(func() {
			if fields > 0 {
				// Fields are two columns: a muted label and its value. The
				// prepared order puts the outcome first and the snapshot details
				// last, so a panel that clips keeps what matters.
				labelWidth := min(max(inner*0.34, 64), 110)
				for index := 0; index < fields; index++ {
					field := &detail.fields[index]
					valueColor := pal.text
					if index == detail.statusIndex {
						valueColor = state.sessionLogToneColor(detail.statusTone)
					}
					ui.Row(c).Height(sessionLogFieldHeight).Shrink(0).FillWidth().Items(ui.Center).Children(func() {
						ui.Text(c, field.label).Width(labelWidth).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).
							Color(pal.muted).Ellipsis()
						ui.Text(c, field.value).Grow(1).FontSize(metrics.FontSizeS).Weight(gfx.WeightRegular).
							Color(valueColor).Ellipsis()
					})
				}
			}
			if showNoInfo {
				ui.Text(c, detail.noInfo).Height(18).LineHeight(18).Shrink(0).FillWidth().FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis()
				ui.Slot(c).Height(2).Shrink(0)
			}
			if showEvents {
				headingWidth := min(inner*0.42, 96)
				ui.Row(c).Height(24).Shrink(0).FillWidth().Items(ui.Center).Children(func() {
					ui.Text(c, m.Events).Width(headingWidth).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
					ui.Text(c, detail.eventsText).Grow(1).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis()
				})
				if showTruncated {
					ui.Text(c, detail.truncatedText).Height(16).LineHeight(16).Shrink(0).FillWidth().FontSize(metrics.FontSizeS).Color(pal.warning).Ellipsis()
					ui.Slot(c).Height(2).Shrink(0)
				}
				state.declareSessionLogEvents(c)
			}
		})
		// The actions stay at the bottom of the panel, whatever the room for
		// the fields above them.
		ui.Row(c).Height(toolbarHeight).Shrink(0).FillWidth().Children(func() {
			state.declareButtonSized(c, hitSessionLogsLive, detail.actionLabel, iconTerminal, false, false, true, toolbarFont, false).Grow(1).Basis(0).Shrink(1)
		})
		ui.Row(c).Height(toolbarHeight).Shrink(0).FillWidth().MarginTop(6).Gap(6).Children(func() {
			state.declareButtonSized(c, hitSessionLogsCopy, m.CopyMetadata, iconCopy, false, false, detail.copyEnabled, toolbarFont, true).Grow(1).Basis(0).Shrink(1)
			state.declareButtonSized(c, hitSessionLogsLocate, m.LocateHost, iconSearch, false, false, detail.hasHost, toolbarFont, true).Grow(1).Basis(0).Shrink(1)
			state.declareButtonSized(c, hitSessionLogsDelete, m.Delete, iconTrash, false, false, true, toolbarFont, true).Grow(1).Basis(0).Shrink(1)
		})
	})
}

// declareSessionLogEvents declares the recorded-events list of the detail
// panel: a page scroll area around a slot that paints the rows, so the wheel
// and the scrollbar are the page's while the prepared rows stay a no-allocation
// paint.
func (state *State) declareSessionLogEvents(c ui.C) {
	view := &state.sessionLogs.view
	detail := &view.detail
	layout := &state.sectionBodies.sessionLogs
	scroll := ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).FillWidth().MarginBottom(10).
		AlwaysScrollbars().ScrollbarSize(catalogBarGutter)
	if view.resetEvents {
		if area := scroll.ScrollState(); area != nil {
			area.ScrollTo(0)
		}
		view.resetEvents = false
	}
	content := float32(len(detail.events)) * sessionLogEventHeight
	scroll.Children(func() {
		if content <= 0 {
			return
		}
		if layout.eventsDraw == nil {
			layout.eventsDraw = func(d *ui.DrawContext) { state.paintSessionLogEventRows(d.Canvas, d.Rect) }
		}
		ui.Slot(c).FillWidth().Height(content).Draw(layout.eventsDraw)
	})
}

// paintSessionLogEventRows paints the prepared event rows into the slot the
// events scroll area moved.
func (state *State) paintSessionLogEventRows(canvas *gfx.Canvas, bounds gfx.Rect) {
	detail, pal := &state.sessionLogs.view.detail, state.pal
	whenWidth := float32(64)
	for index := range detail.events {
		event := &detail.events[index]
		y := bounds.Y + float32(index)*sessionLogEventHeight
		paintSessionLogText(canvas, event.when, gfx.Rect{X: bounds.X, Y: y, Width: whenWidth - 6, Height: sessionLogEventHeight}, metrics.FontSizeS, gfx.WeightRegular, pal.muted)
		paintSessionLogText(canvas, event.label, gfx.Rect{X: bounds.X + whenWidth, Y: y, Width: max(bounds.Width-whenWidth, 0), Height: sessionLogEventHeight}, metrics.FontSizeS, gfx.WeightRegular, pal.text)
	}
}

// paintSessionLogText is one label with a one-line ellipsis that a warm paint
// does not allocate: FitLine remembers the cut line per width.
func paintSessionLogText(canvas *gfx.Canvas, text string, bounds gfx.Rect, size, weight float32, color gfx.Color) {
	if bounds.Width <= 0 || bounds.Height <= 0 || text == "" {
		return
	}
	canvas.Label(canvas.FitLine(text, size, weight, bounds.Width), bounds, gfx.TextStyle{Size: size, Weight: weight, Color: color})
}
