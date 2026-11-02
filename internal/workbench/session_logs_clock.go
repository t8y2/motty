package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type sessionLogCalendar struct {
	location *time.Location
	source   string
	checkAt  time.Time
	timer    *time.Timer
	timerGen uint64
}

func (state *State) sessionLogLocation() *time.Location {
	if location := state.sessionLogs.calendar.location; location != nil {
		return location
	}
	return time.Local
}

// time.Local is initialized once by Go. Keep a page-local snapshot so a system
// zone change can be observed without mutating the process-wide location.
func (state *State) updateSessionLogCalendar() {
	clock := &state.sessionLogs.calendar
	now := state.wallNow()
	if clock.location == nil || !now.Before(clock.checkAt) {
		clock.source, clock.location = loadSessionLogLocation(clock.source, clock.location)
		clock.checkAt = now.Add(time.Minute)
	}
	if clock.timer != nil || state.closed || state.activeSection != sectionSessionLogs || state.options.Invalidate == nil {
		return
	}
	local := now.In(clock.location)
	midnight := sessionLogCalendarBoundary(sessionLogCalendarDate(local).AddDate(0, 0, 1), clock.location)
	delay := max(time.Millisecond, min(midnight.Sub(now), clock.checkAt.Sub(now)))
	clock.timerGen++
	generation, invalidate := clock.timerGen, state.options.Invalidate
	clock.timer = time.AfterFunc(delay, func() {
		state.aiPostWithInvalidator(func(state *State) {
			clock := &state.sessionLogs.calendar
			if state.closed || generation != clock.timerGen {
				return
			}
			clock.timer = nil
			clock.checkAt = time.Time{}
			state.syncSessionLogs()
		}, invalidate)
	})
}

func (state *State) stopSessionLogCalendar() {
	clock := &state.sessionLogs.calendar
	if clock.timer != nil {
		clock.timer.Stop()
		clock.timer = nil
		clock.timerGen++
	}
}

func loadSessionLogLocation(previous string, location *time.Location) (string, *time.Location) {
	var key string
	var data []byte
	var err error
	if zone, set := os.LookupEnv("TZ"); set {
		zone = strings.TrimPrefix(zone, ":")
		key = "TZ=" + zone
		if filepath.IsAbs(zone) {
			data, err = os.ReadFile(zone)
			key += string(data)
		} else if key == previous && location != nil {
			return previous, location
		} else {
			if zone == "" {
				zone = "UTC"
			}
			if loaded, err := time.LoadLocation(zone); err == nil {
				return key, loaded
			}
		}
	} else {
		data, err = readSessionLogSystemZone()
		key = string(data)
	}
	if err == nil && len(data) != 0 {
		if key == previous && location != nil {
			return previous, location
		}
		if loaded, err := time.LoadLocationFromTZData("Local", data); err == nil {
			return key, loaded
		}
	}
	if location == nil {
		location = time.Local
	}
	return previous, location
}
