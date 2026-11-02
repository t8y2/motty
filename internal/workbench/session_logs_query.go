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
	now := state.wallNow().In(time.Local)
	_, offset := now.Zone()
	return sessionLogClockStamp{year: now.Year(), day: now.YearDay(), offset: offset, location: time.Local}
}

func sessionLogDateRange(startDate, endDate string, location *time.Location) (time.Time, time.Time, error) {
	if location == nil {
		location = time.Local
	}
	start, startErr := time.ParseInLocation("2006-01-02", startDate, location)
	end, endErr := time.ParseInLocation("2006-01-02", endDate, location)
	if startErr != nil || endErr != nil || start.After(end) || start.Format("2006-01-02") != startDate || end.Format("2006-01-02") != endDate {
		return time.Time{}, time.Time{}, errors.New("invalid calendar range")
	}
	return start.UTC(), end.AddDate(0, 0, 1).UTC(), nil
}

const sessionLogGroupHeight = float32(26)

func (state *State) prepareSessionLogItems() {
	view := &state.sessionLogs.view
	view.items = view.items[:0]
	view.groupCount = 0
	now := state.wallNow().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	yesterday := today.AddDate(0, 0, -1)
	previous := 0
	for i, record := range view.rows {
		if view.query.Sort <= sessionlog.SortStartedAsc {
			local := record.StartedAt.In(time.Local)
			key, label := -1, state.messages.SessionLogs.DateEarlier
			switch {
			case !local.Before(today) && local.Before(today.AddDate(0, 0, 1)):
				key, label = -3, state.messages.SessionLogs.DateToday
			case !local.Before(yesterday) && local.Before(today):
				key, label = -2, state.messages.SessionLogs.DateYesterday
			case local.After(today):
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
		start = state.wallNow().In(time.Local).Format("2006-01-02")
	}
	if end == "" {
		end = start
	}
	form := state.openForm(formDialog{
		title: m.TimeCustom, submit: m.ApplyRange,
		cancel: func(state *State) { state.sessionLogs.view.focusPending = hitSessionLogsTime },
		run: func(state *State, form *formDialog) string {
			from, through := strings.TrimSpace(form.value(0)), strings.TrimSpace(form.value(1))
			if _, _, err := sessionLogDateRange(from, through, time.Local); err != nil {
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
