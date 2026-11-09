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
	hitSessionLogsMore           = tree.ID(sessionLogHits, "more-actions")
	hitSessionLogsMoreDismiss    = tree.ID(sessionLogHits, "more-dismiss")
	hitSessionLogsFavoriteFilter = tree.ID(sessionLogHits, "favorite-filter")
	hitSessionLogsFilterDismiss  = tree.ID(sessionLogHits, "filter-dismiss")
	hitSessionLogsFilterList     = tree.Sub(sessionLogHits, tree.Name("filter-option"))
	hitSessionLogsFavorite       = tree.ID(sessionLogHits, "favorite")
	hitSessionLogsDelete         = tree.ID(sessionLogHits, "delete")
	hitSessionLogsClearFavorite  = tree.ID(sessionLogHits, "clear-unfavorited")
	hitSessionLogsClearAll       = tree.ID(sessionLogHits, "clear-all")
	hitSessionLogsClearFilters   = tree.ID(sessionLogHits, "clear-filters")
	hitSessionLogsLoadMore       = tree.ID(sessionLogHits, "load-more")
	hitSessionLogsScroll         = tree.ID(sessionLogHits, "scroll")
	hitSessionLogsThumb          = tree.ID(sessionLogHits, "thumb")
	hitSessionLogsRowList        = tree.Sub(sessionLogHits, tree.Name("row"))
	hitSessionLogsRowFavorite    = tree.Sub(sessionLogHits, tree.Name("row-favorite"))
	hitSessionLogsDetailClose    = tree.ID(sessionLogHits, "detail-close")
	hitSessionLogsLive           = tree.ID(sessionLogHits, "open-session")
	hitSessionLogsCopy           = tree.ID(sessionLogHits, "copy-metadata")
	hitSessionLogsLocate         = tree.ID(sessionLogHits, "locate-host")
	hitSessionLogsEventsScroll   = tree.ID(sessionLogHits, "events-scroll")
	hitSessionLogsEventsThumb    = tree.ID(sessionLogHits, "events-thumb")
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

// sessionLogDragging is the scrollbar being dragged, if any.
type sessionLogDragging uint8

const (
	sessionLogDragNone sessionLogDragging = iota
	sessionLogDragRows
	sessionLogDragEvents
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
	// the session-jump action before it paints another field.
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

// paintSessionLogsWorkspace paints the page inside the section card.
func (state *State) paintSessionLogsWorkspace(canvas *gfx.Canvas, bounds gfx.Rect) {
	view, m, pal := &state.sessionLogs.view, &state.messages.SessionLogs, state.pal
	theme := state.Theme()
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
	var search, timeSlot, statusSlot, protocolSlot, outcomeSlot, sortSlot, favoriteFilterBox ui.Box
	var moreButton ui.Box
	var content, detailSlot, loadMore ui.Box
	var emptyIcon, emptyAction ui.Box

	f := state.sessionLogsLayout.Begin(canvas, theme, bounds)
	f.Column().Fill().PadSides(12, 18, 0, 18).Children(func() {
		f.Row().Height(36).Gap(8).Items(ui.Center).Children(func() {
			f.Text(m.Title).FontSize(sessionLogTitleSize).Weight(gfx.WeightSemibold).Color(pal.text).Grow(1).Ellipsis()
			f.Text(view.countText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Width(min(inner*0.62, 360)).TextEnd().Ellipsis()
			moreButton = f.Slot().Size(30, 30)
		})
		if !narrow {
			f.Text(m.Description).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18).Ellipsis().MarginSides(0, 0, 8, 0)
		}
		f.Row().Height(40).Gap(sessionLogToolbarGap).Items(ui.Center).Children(func() {
			search = f.Slot().Size(searchWidth, toolbarHeight)
			if !separateFilters {
				f.Spacer()
				timeSlot = f.Slot().Size(filterWidth, toolbarHeight)
				statusSlot = f.Slot().Size(filterWidth, toolbarHeight)
				protocolSlot = f.Slot().Size(filterWidth, toolbarHeight)
				outcomeSlot = f.Slot().Size(filterWidth, toolbarHeight)
				sortSlot = f.Slot().Size(filterWidth, toolbarHeight)
				favoriteFilterBox = f.Slot().Size(toolbarHeight, toolbarHeight)
			} else {
				f.Spacer()
				favoriteFilterBox = f.Slot().Size(toolbarHeight, toolbarHeight)
			}
		})
		if separateFilters {
			f.Row().Height(38).Gap(sessionLogToolbarGap).Items(ui.Center).Children(func() {
				timeSlot = f.Slot().Size(filterWidth, toolbarHeight)
				statusSlot = f.Slot().Size(filterWidth, toolbarHeight)
				protocolSlot = f.Slot().Size(filterWidth, toolbarHeight)
				if !stackedFilters {
					outcomeSlot = f.Slot().Size(filterWidth, toolbarHeight)
					sortSlot = f.Slot().Size(filterWidth, toolbarHeight)
				}
			})
			if stackedFilters {
				f.Row().Height(38).Gap(sessionLogToolbarGap).Items(ui.Center).Children(func() {
					outcomeSlot = f.Slot().Size(filterWidth, toolbarHeight)
					sortSlot = f.Slot().Size(filterWidth, toolbarHeight)
				})
			}
		}
		switch {
		case empty:
			descriptionColor := pal.muted
			if view.emptyDanger {
				descriptionColor = pal.danger
			}
			emptyIcon, emptyAction = state.declareEmptyPageWithColor(f, bounds, view.emptyTitle, view.emptyText, view.emptyAction, descriptionColor)
		case sideDetail:
			f.Row().Grow(1).Basis(0).Gap(14).Children(func() {
				content = f.Slot().Grow(1).Basis(0)
				detailSlot = f.Slot().Width(min(sessionLogDetailWidth, inner*0.46))
			})
		case detailOnly:
			detailSlot = f.Slot().Grow(1).Basis(0)
		default:
			content = f.Slot().Grow(1).Basis(0).MarginSides(4, 0, 0, 0)
		}
		f.Divider().Background(pal.line)
		f.Row().Height(46).Gap(12).Items(ui.Center).Children(func() {
			f.Text(view.statusText).FontSize(metrics.FontSizeS).Color(state.sessionLogStatusColor()).Grow(1).Ellipsis()
			if view.more {
				loadMoreWidth, _ := toolbarButtonLayout(canvas, m.LoadMore, iconChevronDown, iconChevronDown, condensed)
				loadMore = f.Slot().Size(loadMoreWidth, toolbarHeight)
			}
		})
	})
	f.End()

	value := view.search.String()
	components.PaintInputGroup(canvas, theme, search.Rect(), components.InputGroupSnapshot{
		ID: hitSessionLogsSearch, Label: m.Search, Value: value, Placeholder: m.SearchPlaceholder,
		NativeEditing: canvas.Focused() == hitSessionLogsSearch && value != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconSearch]},
	})
	// The triggers' places are kept: the open menu opens under its own trigger
	// in the frame's menu pass, outside the card's clip.
	view.filterBounds[0], view.filterBounds[1], view.filterBounds[2] = timeSlot.Rect(), statusSlot.Rect(), protocolSlot.Rect()
	view.filterBounds[3] = outcomeSlot.Rect()
	view.filterBounds[4] = sortSlot.Rect()
	state.paintSessionLogFilterTrigger(canvas, hitSessionLogsTime, timeSlot.Rect(), sessionLogFilterTimeMenu)
	state.paintSessionLogFilterTrigger(canvas, hitSessionLogsStatus, statusSlot.Rect(), sessionLogFilterStatusMenu)
	state.paintSessionLogFilterTrigger(canvas, hitSessionLogsProtocol, protocolSlot.Rect(), sessionLogFilterProtocolMenu)
	state.paintSessionLogFilterTrigger(canvas, hitSessionLogsOutcome, outcomeSlot.Rect(), sessionLogFilterOutcomeMenu)
	state.paintSessionLogFilterTrigger(canvas, hitSessionLogsSort, sortSlot.Rect(), sessionLogFilterSortMenu)
	state.paintSessionLogFavoriteFilter(canvas, favoriteFilterBox.Rect())

	view.moreBounds = moreButton.Rect()
	style := iconButtonQuiet
	if view.moreMenu {
		style = iconButtonActive
	}
	state.paintIconButton(canvas, hitSessionLogsMore, iconEllipsis, moreButton.Rect(), m.More, false, style, pal.surface)

	switch {
	case empty:
		state.paintEmptyPage(canvas, emptyIcon, emptyAction, view.emptyIcon, hitSessionLogsClearFilters, view.emptyAction, true)
	case sideDetail:
		state.paintSessionLogTable(canvas, content.Rect())
		state.paintSessionLogDetails(canvas, detailSlot.Rect(), false)
	case detailOnly:
		state.paintSessionLogDetails(canvas, detailSlot.Rect(), true)
	default:
		state.paintSessionLogTable(canvas, content.Rect())
	}
	if view.more {
		label := m.LoadMore
		if view.loadingMore {
			label = m.Loading
		}
		state.paintToolbarButtonIn(canvas, hitSessionLogsLoadMore, loadMore.Rect(), label, iconChevronDown, false, !view.loadingMore, condensed)
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

// paintSessionLogFilterTrigger paints one filter: a small control that shows the
// chosen value, names itself to the keyboard, and says whether its menu is open.
func (state *State) paintSessionLogFilterTrigger(canvas *gfx.Canvas, id gfx.ID, bounds gfx.Rect, filter sessionLogFilterMenu) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
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
	fill := gfx.Color{}
	border := pal.line
	switch {
	case open, active:
		foreground, border = pal.text, withAlpha(pal.accent, 0.55)
	case canvas.Pressed() == id:
		fill = pal.surfaceHot
	case canvas.Hovered() == id:
		foreground = pal.text
	}
	label := sessionLogFilterLabel(m, filter)
	hint := label
	if filter == sessionLogFilterTimeMenu && view.customRangeHint != "" {
		hint = view.customRangeHint
	}
	value := state.sessionLogFilterValue(filter)
	// The chevron is what a trigger past 96 pixels can afford; a narrower one
	// keeps the value legible and says it opens with its accessible name.
	chevron := bounds.Width >= 96
	f := state.chrome.widget.Begin(canvas, state.Theme(), bounds)
	f.Row().Fill().Center().Radius(radius.control).Border(1, border).Background(fill).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint, Selected: active, Expanded: open, Expandable: true}).
		Children(func() {
			f.Icon(lucideIcons[sessionLogFilterIcon(filter)]).Color(foreground).MarginSides(0, 6, 0, 8)
			f.Text(value).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground).Ellipsis()
			if chevron {
				f.Spacer()
				f.Icon(lucideIcons[iconChevronDown]).Color(pal.faint)
			}
		})
	f.End()
	if canvas.FocusVisible() == id {
		canvas.RoundedBorder(bounds, radius.control, 1.5, pal.accent, gfx.Color{})
	}
}

// paintSessionLogFavoriteFilter paints the favorites-only toggle: a star that
// fills while the filter is on. It is a filter, so it reports whether it is
// applied rather than whether it has the keyboard.
func (state *State) paintSessionLogFavoriteFilter(canvas *gfx.Canvas, bounds gfx.Rect) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	view, m, pal := &state.sessionLogs.view, &state.messages.SessionLogs, state.pal
	icon := iconStar
	foreground := pal.muted
	fill := gfx.Color{}
	border := pal.line
	if view.favoriteOnly {
		icon, foreground, border = iconStarSolid, pal.warning, withAlpha(pal.warning, 0.55)
	}
	switch {
	case canvas.Pressed() == hitSessionLogsFavoriteFilter:
		fill = pal.surfaceHot
	case canvas.Hovered() == hitSessionLogsFavoriteFilter:
		foreground = pal.text
	}
	f := state.chrome.widget.Begin(canvas, state.Theme(), bounds)
	f.Row().Fill().Center().Radius(radius.control).Border(1, border).Background(fill).
		HitWith(hitSessionLogsFavoriteFilter, m.FilterFavorite, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.FilterFavorite, Selected: view.favoriteOnly}).
		Children(func() {
			f.Icon(lucideIcons[icon]).Color(foreground)
		})
	f.End()
	if canvas.FocusVisible() == hitSessionLogsFavoriteFilter {
		canvas.RoundedBorder(bounds, radius.control, 1.5, pal.accent, gfx.Color{})
	}
}

// paintSessionLogFilters paints the open filter's popup in the frame's menu
// pass, outside the workspace clip, so it can leave the card. Its items are radio
// choices: the keyboard walks them with menuKey, as it does every popup menu of
// the workbench.
func (state *State) paintSessionLogFilters(canvas *gfx.Canvas) {
	view := &state.sessionLogs.view
	if state.activeSection != sectionSessionLogs || state.modal != modalNone {
		return
	}
	if view.moreMenu {
		m := &state.messages.SessionLogs
		var rows menuRows
		rows.add(components.ContextMenuItem{ID: hitSessionLogsClearFavorite, Label: m.ClearUnfavorited, Icon: lucideIcons[iconEraser], Disabled: !state.sessionLogsCanClear(), Variant: components.ContextMenuItemDestructive})
		rows.add(components.ContextMenuItem{ID: hitSessionLogsClearAll, Label: m.ClearAll, Icon: lucideIcons[iconTrash], Disabled: !state.sessionLogsCanClear(), Variant: components.ContextMenuItemDestructive})
		width, height := rows.size(canvas, state.contextMenuTheme())
		bounds := state.popupBelow(canvas, view.moreBounds, width, height, ui.End)
		menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{Layer: dialogLayer, Scope: dialogScope, DismissID: hitSessionLogsMoreDismiss, Viewport: state.windowRect(), Scroll: state.menuScrollFor(hitSessionLogsMoreDismiss), RowHeight: contextMenuRow})
		state.menuPlaced(menu.Bounds())
		rows.paint(&menu, nil)
		menu.End()
		return
	}
	if view.filterMenu == sessionLogFilterNone {
		return
	}
	anchor := view.filterBounds[view.filterMenu-1]
	if anchor.Width <= 0 || anchor.Height <= 0 {
		return
	}
	var labels [8]string
	var checked [8]bool
	count := state.sessionLogFilterChoices(view.filterMenu, &labels, &checked)
	if count == 0 {
		return
	}
	height := float32(count*32 + 8)
	width := max(float32(220), anchor.Width)
	bounds := state.popupBelow(canvas, anchor, width, height, ui.Start)
	menu := components.BeginDropdownMenu(canvas, state.Theme(), bounds, components.DropdownMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitSessionLogsFilterDismiss,
		Viewport: state.windowRect(), Scroll: state.selectScroll(true, hitSessionLogsFilterDismiss),
	})
	state.menuPlaced(menu.Bounds())
	for index := 0; index < count; index++ {
		menu.AddItem(components.DropdownMenuItem{
			ID: hitSessionLogsFilterList.At(index), Label: labels[index],
			Kind: components.DropdownMenuRadio, Checked: checked[index],
		})
	}
	menu.End()
}

// paintSessionLogTable paints the fixed header and the scrollable body of the
// record table.
func (state *State) paintSessionLogTable(canvas *gfx.Canvas, bounds gfx.Rect) {
	view, m := &state.sessionLogs.view, &state.messages.SessionLogs
	if bounds.Width < 80 || bounds.Height < 60 {
		return
	}
	theme := state.Theme()
	var columns [6]components.TableColumn
	var headings [6]string
	count := state.sessionLogColumns(bounds.Width, &columns, &headings)
	rowHeight := components.TableRowHeight(theme)
	headerHeight := components.TableHeaderHeight(theme)
	body := gfx.Rect{X: bounds.X, Y: bounds.Y + headerHeight, Width: bounds.Width, Height: max(bounds.Height-headerHeight, 0)}
	view.tableBody = body
	if body.Height <= 0 {
		return
	}
	components.FitTableHeadings(canvas, theme, columns[:count], headings[:count])
	head := components.BeginTable(canvas, theme, gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: headerHeight}, columns[:count])
	head.Header(headings[:count])

	content := gfx.Size{Width: bounds.Width, Height: state.sessionLogListHeight(rowHeight)}
	area := components.BeginScrollArea(canvas, theme, body, content).
		Offset(0, view.scrollRows).Viewport(hitSessionLogsScroll, m.ListScroll).
		VerticalScrollbar(hitSessionLogsThumb, m.ListScroll, "")
	geo := area.BeginContent()
	view.scrollRows = geo.OffsetY
	if len(view.page) > 0 {
		if cap(view.rowCells) < count {
			view.rowCells = make([]gfx.Rect, count)
		}
		y := geo.Content.Y
		for _, item := range view.items {
			if item.row < 0 {
				paintSessionLogText(canvas, item.heading, gfx.Rect{X: geo.Content.X + 8, Y: y, Width: max(geo.Content.Width-16, 0), Height: sessionLogGroupHeight}, metrics.FontSizeS, gfx.WeightSemibold, state.pal.muted)
				y += sessionLogGroupHeight
				continue
			}
			index := item.row
			row := &view.page[index]
			table := components.BeginTable(canvas, theme, gfx.Rect{X: geo.Content.X, Y: y, Width: geo.Content.Width, Height: rowHeight}, columns[:count])
			y += rowHeight
			var rowCells []string
			switch count {
			case 6:
				rowCells = row.cells[:]
			case 4:
				rowCells = row.compact[:]
			default:
				rowCells = row.minimal[:]
			}
			layout := table.RowLayout(hitSessionLogsRowList.At(index), rowCells, components.TableRowState{Selected: row.id == view.selected}, view.rowCells[:0])
			if layout.Bounds.Width <= 0 || count > len(layout.Cells) {
				continue
			}
			star := layout.Cells[count-1]
			if star.Width < 12 {
				continue
			}
			icon := iconStar
			if row.favorite {
				icon = iconStarSolid
			}
			style := iconButtonQuiet
			if row.favorite {
				style = iconButtonActive
			}
			state.paintIconButton(canvas, hitSessionLogsRowFavorite.At(index), icon, star, row.favoriteLabel, view.favoritePending != "", style, state.pal.surface)
		}
	}
	area.EndContent()
}

// sessionLogColumns lays out the table for a card width and returns how many of
// the six cells it paints. The target column takes what the others leave. The
// widths are what the longest localized value needs: a time (100), a status
// (140), a protocol (84) and a duration (80). A card too narrow for six columns
// drops protocol and duration, and the narrowest drops the status too; the detail
// panel still shows everything the row leaves out.
func (state *State) sessionLogColumns(width float32, columns *[6]components.TableColumn, headings *[6]string) int {
	m := &state.messages.SessionLogs
	favorite := float32(30)
	if width >= sessionLogFullColumns {
		when, protocol, status, duration := float32(104), float32(84), float32(200), float32(80)
		target := max(width-when-protocol-status-duration-favorite, 140)
		columns[0] = components.TableColumn{Width: target}
		columns[1] = components.TableColumn{Width: when}
		columns[2] = components.TableColumn{Width: protocol}
		columns[3] = components.TableColumn{Width: status}
		columns[4] = components.TableColumn{Width: duration}
		columns[5] = components.TableColumn{Width: favorite, Alignment: components.TableAlignCenter}
		headings[0], headings[1], headings[2] = m.ColumnTarget, m.ColumnWhen, m.ColumnProtocol
		headings[3], headings[4], headings[5] = m.ColumnStatus, m.ColumnDuration, ""
		return 6
	}
	if width >= sessionLogCompactColumns {
		when, status := float32(100), float32(190)
		target := max(width-when-status-favorite, 100)
		columns[0] = components.TableColumn{Width: target}
		columns[1] = components.TableColumn{Width: when}
		columns[2] = components.TableColumn{Width: status}
		columns[3] = components.TableColumn{Width: favorite, Alignment: components.TableAlignCenter}
		headings[0], headings[1], headings[2], headings[3] = m.ColumnTarget, m.ColumnWhen, m.ColumnStatus, ""
		if state.sessionLogs.view.query.Sort >= sessionlog.SortDurationDesc {
			headings[1] = m.ColumnDuration
		}
		return 4
	}
	when := float32(100)
	target := max(width-when-favorite, 80)
	columns[0] = components.TableColumn{Width: target}
	columns[1] = components.TableColumn{Width: when}
	columns[2] = components.TableColumn{Width: favorite, Alignment: components.TableAlignCenter}
	headings[0], headings[1], headings[2] = m.ColumnTarget, m.ColumnStatus, ""
	if state.sessionLogs.view.query.Sort >= sessionlog.SortDurationDesc {
		headings[1] = m.ColumnDuration
	}
	return 3
}

// paintSessionLogDetails paints the selected record: its fields, the lifecycle
// events that were recorded, and the way to the still-open session when there is
// one. Nothing here writes, and no replay is offered because no recording exists.
func (state *State) paintSessionLogDetails(canvas *gfx.Canvas, bounds gfx.Rect, stacked bool) {
	detail, m, pal := &state.sessionLogs.view.detail, &state.messages.SessionLogs, state.pal
	if bounds.Width < 160 || bounds.Height < 120 {
		return
	}
	view := &state.sessionLogs.view
	canvas.RoundedBorder(bounds, radius.surface, 1, pal.line, pal.surface)
	inner := gfx.Rect{X: bounds.X + 14, Y: bounds.Y + 12, Width: max(bounds.Width-28, 0), Height: max(bounds.Height-24, 0)}
	if inner.Width <= 0 || inner.Height <= 0 {
		return
	}
	y := inner.Y
	// The panel's own header: the record's host and the way back to the list.
	closeHint := m.CloseDetail
	if stacked {
		closeHint = m.BackToList
	}
	closeSize := float32(28)
	titleWidth := max(inner.Width-closeSize-8, 0)
	paintSessionLogText(canvas, detail.title, gfx.Rect{X: inner.X, Y: y, Width: titleWidth, Height: 22}, metrics.FontSizeM, gfx.WeightSemibold, pal.text)
	state.paintIconButton(canvas, hitSessionLogsDetailClose, iconClose, gfx.Rect{X: inner.X + inner.Width - closeSize, Y: y - 2, Width: closeSize, Height: closeSize}, closeHint, false, iconButtonQuiet, pal.surface)
	y += 26
	if detail.count > 0 {
		// Fields are two columns: a muted label and its value. A panel too short
		// for all of them keeps the first ones, which is why the prepared order
		// puts the outcome first and the snapshot details last.
		labelWidth := min(max(inner.Width*0.34, 64), 110)
		room := int((inner.Height - 26 - sessionLogEventsRoom) / sessionLogFieldHeight)
		fields := min(detail.count, max(room, 3))
		for index := 0; index < fields; index++ {
			field := &detail.fields[index]
			if y+sessionLogFieldHeight > inner.Y+inner.Height {
				break
			}
			valueColor := pal.text
			if index == detail.statusIndex {
				valueColor = state.sessionLogToneColor(detail.statusTone)
			}
			paintSessionLogText(canvas, field.label, gfx.Rect{X: inner.X, Y: y, Width: max(labelWidth-6, 0), Height: sessionLogFieldHeight}, metrics.FontSizeS, gfx.WeightMedium, pal.muted)
			paintSessionLogText(canvas, field.value, gfx.Rect{X: inner.X + labelWidth, Y: y, Width: max(inner.Width-labelWidth, 0), Height: sessionLogFieldHeight}, metrics.FontSizeS, gfx.WeightRegular, valueColor)
			y += sessionLogFieldHeight
		}
	}
	if detail.noInfo != "" {
		if y+18 <= inner.Y+inner.Height {
			paintSessionLogText(canvas, detail.noInfo, gfx.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 18}, metrics.FontSizeS, gfx.WeightRegular, pal.muted)
			y += 20
		}
	}
	// The session-jump action sits at the bottom; above it the recorded events
	// scroll on their own so a long timeline stays readable.
	action := gfx.Rect{X: inner.X, Y: inner.Y + inner.Height - 2*toolbarHeight - 6, Width: inner.Width, Height: toolbarHeight}
	eventsTop := y
	eventsBottom := action.Y - 10
	headingWidth := min(inner.Width*0.42, 96)
	view.eventsBody = gfx.Rect{}
	if eventsBottom-eventsTop > 34 {
		paintSessionLogText(canvas, m.Events, gfx.Rect{X: inner.X, Y: eventsTop, Width: headingWidth, Height: 18}, metrics.FontSizeS, gfx.WeightSemibold, pal.text)
		paintSessionLogText(canvas, detail.eventsText, gfx.Rect{X: inner.X + headingWidth, Y: eventsTop, Width: max(inner.Width-headingWidth, 0), Height: 18}, metrics.FontSizeS, gfx.WeightRegular, pal.muted)
		eventsTop += 24
		if detail.truncatedText != "" && eventsTop+16 <= eventsBottom {
			paintSessionLogText(canvas, detail.truncatedText, gfx.Rect{X: inner.X, Y: eventsTop, Width: inner.Width, Height: 16}, metrics.FontSizeS, gfx.WeightRegular, pal.warning)
			eventsTop += 18
		}
		events := gfx.Rect{X: inner.X, Y: eventsTop, Width: inner.Width, Height: max(eventsBottom-eventsTop, 0)}
		if events.Height > 0 {
			view.eventsBody = events
			content := gfx.Size{Width: events.Width, Height: float32(len(detail.events)) * sessionLogEventHeight}
			area := components.BeginScrollArea(canvas, state.Theme(), events, content).
				Offset(0, view.scrollEvents).Viewport(hitSessionLogsEventsScroll, m.Events).
				VerticalScrollbar(hitSessionLogsEventsThumb, m.Events, "")
			geo := area.BeginContent()
			view.scrollEvents = geo.OffsetY
			whenWidth := float32(64)
			for index := range detail.events {
				event := &detail.events[index]
				row := gfx.Rect{X: geo.Content.X, Y: geo.Content.Y + float32(index)*sessionLogEventHeight, Width: geo.Content.Width, Height: sessionLogEventHeight}
				paintSessionLogText(canvas, event.when, gfx.Rect{X: row.X, Y: row.Y, Width: whenWidth - 6, Height: row.Height}, metrics.FontSizeS, gfx.WeightRegular, pal.muted)
				paintSessionLogText(canvas, event.label, gfx.Rect{X: row.X + whenWidth, Y: row.Y, Width: max(row.Width-whenWidth, 0), Height: row.Height}, metrics.FontSizeS, gfx.WeightRegular, pal.text)
			}
			area.EndContent()
		}
	}
	state.paintToolbarButtonIn(canvas, hitSessionLogsLive, action, detail.actionLabel, iconTerminal, false, true, false)
	tools := gfx.Rect{X: action.X, Y: action.Y + toolbarHeight + 6, Width: (action.Width - 12) / 3, Height: toolbarHeight}
	state.paintToolbarButtonIn(canvas, hitSessionLogsCopy, tools, m.CopyMetadata, iconCopy, false, detail.copyEnabled, true)
	tools.X += tools.Width + 6
	state.paintToolbarButtonIn(canvas, hitSessionLogsLocate, tools, m.LocateHost, iconSearch, false, detail.hasHost, true)
	tools.X += tools.Width + 6
	state.paintToolbarButtonIn(canvas, hitSessionLogsDelete, tools, m.Delete, iconTrash, false, true, true)
}

// paintSessionLogText is one label with a one-line ellipsis that a warm paint
// does not allocate: FitLine remembers the cut line per width.
func paintSessionLogText(canvas *gfx.Canvas, text string, bounds gfx.Rect, size, weight float32, color gfx.Color) {
	if bounds.Width <= 0 || bounds.Height <= 0 || text == "" {
		return
	}
	canvas.Label(canvas.FitLine(text, size, weight, bounds.Width), bounds, gfx.TextStyle{Size: size, Weight: weight, Color: color})
}
