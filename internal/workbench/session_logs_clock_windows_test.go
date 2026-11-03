package workbench

import (
	"syscall"
	"testing"
	"time"
)

func TestSessionLogsWindowsCalendarRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		zone syscall.Timezoneinformation
		days map[string]time.Duration
	}{
		{"New York", syscall.Timezoneinformation{Bias: 300, DaylightBias: -60, StandardDate: syscall.Systemtime{Month: 11, Day: 1, Hour: 2}, DaylightDate: syscall.Systemtime{Month: 3, Day: 2, Hour: 2}}, map[string]time.Duration{"2026-03-08": 23, "2026-11-01": 25}},
		{"Sydney", syscall.Timezoneinformation{Bias: -600, DaylightBias: -60, StandardDate: syscall.Systemtime{Month: 4, Day: 1, Hour: 3}, DaylightDate: syscall.Systemtime{Month: 10, Day: 1, Hour: 2}}, map[string]time.Duration{"2026-04-05": 25, "2026-10-04": 23}},
		{"India", syscall.Timezoneinformation{Bias: -330}, map[string]time.Duration{"2026-11-01": 24}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy(tc.zone.StandardName[:], syscall.StringToUTF16("Standard"))
			copy(tc.zone.DaylightName[:], syscall.StringToUTF16("Daylight"))
			location, err := time.LoadLocationFromTZData(tc.name, sessionLogWindowsZoneData(tc.zone, 2026))
			if err != nil {
				t.Fatal(err)
			}
			for date, hours := range tc.days {
				from, until, err := sessionLogDateRange(date, date, location)
				if err != nil || until.Sub(from) != hours*time.Hour {
					t.Fatalf("%s [%v,%v), %v; want %v hours", date, from, until, err, hours)
				}
			}
		})
	}
	if data, err := readSessionLogSystemZone(); err != nil {
		t.Fatal(err)
	} else if _, err := time.LoadLocationFromTZData("Local", data); err != nil {
		t.Fatalf("current Win32 zone is not usable: %v", err)
	}
}
