package workbench

import (
	"errors"
	"strings"
	"time"

	"motty/internal/sessionlog"
)

type sessionLogListItem struct {
	row     int
	heading string
}

type sessionLogClockStamp struct {
	year, day, offset int
	location          *time.Location
}

func (state *State) sessionLogClockStamp() sessionLogClockStamp {
	location := state.sessionLogLocation()
	now := state.wallNow().In(location)
	_, offset := now.Zone()
	return sessionLogClockStamp{year: now.Year(), day: now.YearDay(), offset: offset, location: location}
}

func sessionLogDateRange(startDate, endDate string, location *time.Location) (time.Time, time.Time, error) {
	if location == nil {
		location = time.Local
	}
	start, startErr := time.Parse("2006-01-02", startDate)
	end, endErr := time.Parse("2006-01-02", endDate)
	if startErr != nil || endErr != nil || start.Year() < 1 || end.Year() < 1 || start.After(end) || start.Format("2006-01-02") != startDate || end.Format("2006-01-02") != endDate {
		return time.Time{}, time.Time{}, errors.New("invalid calendar range")
	}
	since := sessionLogCalendarBoundary(start, location)
	last := sessionLogCalendarBoundary(end, location)
	until := sessionLogCalendarBoundary(end.AddDate(0, 0, 1), location)
	if since.IsZero() || until.IsZero() || !until.After(since) || since.In(location).Format("2006-01-02") != startDate || last.In(location).Format("2006-01-02") != endDate {
		return time.Time{}, time.Time{}, errors.New("invalid calendar range")
	}
	return since, until, nil
}

func sessionLogCalendarDate(local time.Time) time.Time {
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// Find the first instant whose local calendar is at or past date. Inspect zone
// intervals because midnight can be missing, repeated, or an entire day skipped.
// All IANA UTC offsets and date-line changes fit within this four-day window.
func sessionLogCalendarBoundary(date time.Time, location *time.Location) time.Time {
	limit := date.Add(48 * time.Hour)
	for cursor := date.Add(-48 * time.Hour); cursor.Before(limit); {
		local := cursor.In(location)
		_, offset := local.Zone()
		_, end := local.ZoneBounds()
		if end.IsZero() || end.After(limit) {
			end = limit
		}
		candidate := date.Add(-time.Duration(offset) * time.Second)
		if candidate.Before(cursor) {
			candidate = cursor
		}
		if candidate.Before(end) {
			return candidate.UTC()
		}
		cursor = end
	}
	return time.Time{}
}

const sessionLogGroupHeight = float32(26)

func (state *State) prepareSessionLogItems() {
	view := &state.sessionLogs.view
	view.items = view.items[:0]
	view.groupCount = 0
	location := state.sessionLogLocation()
	now := state.wallNow().In(location)
	today := sessionLogCalendarDate(now)
	yesterday := today.AddDate(0, 0, -1)
	previous := 0
	for i, record := range view.rows {
		if view.query.Sort <= sessionlog.SortStartedAsc {
			local := record.StartedAt.In(location)
			date := sessionLogCalendarDate(local)
			key, label := -1, state.messages.SessionLogs.DateEarlier
			switch {
			case date.Equal(today):
				key, label = -3, state.messages.SessionLogs.DateToday
			case date.Equal(yesterday):
				key, label = -2, state.messages.SessionLogs.DateYesterday
			case date.After(today):
				key = local.Year()*10000 + int(local.Month())*100 + local.Day()
				label = local.Format("2006-01-02")
			}
			if i == 0 || key != previous {
				view.items = append(view.items, sessionLogListItem{row: -1, heading: label})
				view.groupCount++
				previous = key
			}
		}
		view.items = append(view.items, sessionLogListItem{row: i})
	}
}

func (state *State) sessionLogListHeight(rowHeight float32) float32 {
	view := &state.sessionLogs.view
	return float32(len(view.page))*rowHeight + float32(view.groupCount)*sessionLogGroupHeight
}

func (state *State) sessionLogRowTop(index int, rowHeight float32) float32 {
	var top float32
	for _, item := range state.sessionLogs.view.items {
		if item.row < 0 {
			top += sessionLogGroupHeight
			continue
		}
		if item.row == index {
			return top
		}
		top += rowHeight
	}
	return 0
}

func (state *State) askSessionLogCustomRange() {
	view := &state.sessionLogs.view
	m := &state.messages.SessionLogs
	view.filterMenu = sessionLogFilterNone
	start, end := view.customStartDate, view.customEndDate
	if start == "" {
		start = state.wallNow().In(state.sessionLogLocation()).Format("2006-01-02")
	}
	if end == "" {
		end = start
	}
	form := state.openForm(formDialog{
		title: m.TimeCustom, submit: m.ApplyRange,
		cancel: func(state *State) { state.sessionLogs.view.focusPending = hitSessionLogsTime },
		run: func(state *State, form *formDialog) string {
			from, through := strings.TrimSpace(form.value(0)), strings.TrimSpace(form.value(1))
			if _, _, err := sessionLogDateRange(from, through, state.sessionLogLocation()); err != nil {
				return state.messages.SessionLogs.InvalidDateRange
			}
			view := &state.sessionLogs.view
			view.customStartDate, view.customEndDate, view.timeRange = from, through, sessionLogTimeCustom
			state.applySessionLogFilters()
			view.focusPending = hitSessionLogsTime
			return ""
		},
	}, formField{label: m.DateFrom, placeholder: "YYYY-MM-DD"}, formField{label: m.DateThrough, placeholder: "YYYY-MM-DD"})
	form.set(0, start)
	form.set(1, end)
}
