package workbench

import (
	"encoding/binary"
	"slices"
	"syscall"
	"time"
)

func readSessionLogSystemZone() ([]byte, error) {
	var zone syscall.Timezoneinformation
	if _, err := syscall.GetTimeZoneInformation(&zone); err != nil {
		return nil, err
	}
	return sessionLogWindowsZoneData(zone, time.Now().Year()), nil
}

// Match Go's Windows local-time semantics: the current OS rules are used for
// 100 years on either side of this year. TZif lets time's calendar engine own
// all date parsing and DST boundary resolution.
func sessionLogWindowsZoneData(zone syscall.Timezoneinformation, year int) []byte {
	offsets := []int32{-zone.Bias * 60}
	names := syscall.UTF16ToString(zone.StandardName[:]) + "\x00"
	type transition struct {
		at   int64
		zone byte
	}
	var transitions []transition
	daylightName := len(names)
	if zone.StandardDate.Month != 0 && zone.DaylightDate.Month != 0 {
		offsets[0] = -(zone.Bias + zone.StandardBias) * 60
		offsets = append(offsets, -(zone.Bias+zone.DaylightBias)*60)
		names += syscall.UTF16ToString(zone.DaylightName[:]) + "\x00"
		for y := year - 100; y < year+100; y++ {
			for i, date := range []syscall.Systemtime{zone.StandardDate, zone.DaylightDate} {
				first := time.Date(y, time.Month(date.Month), 1, int(date.Hour), int(date.Minute), int(date.Second), 0, time.UTC)
				day := 1 + (int(date.DayOfWeek)-int(first.Weekday())+7)%7 + (int(date.Day)-1)*7
				if date.Day == 5 && day > time.Date(y, time.Month(date.Month)+1, 0, 0, 0, 0, 0, time.UTC).Day() {
					day -= 7
				}
				at := first.AddDate(0, 0, day-1).Unix() - int64(offsets[1-i])
				transitions = append(transitions, transition{at, byte(i)})
			}
		}
		slices.SortFunc(transitions, func(a, b transition) int {
			if a.at < b.at {
				return -1
			}
			if a.at > b.at {
				return 1
			}
			return 0
		})
	}
	var data []byte
	for _, width := range []int{4, 8} {
		data = append(data, []byte("TZif2")...)
		data = append(data, make([]byte, 15)...)
		count := len(transitions)
		if width == 4 {
			count = 0
		}
		for _, value := range []int{0, 0, 0, count, len(offsets), len(names)} {
			data = binary.BigEndian.AppendUint32(data, uint32(value))
		}
		if width == 8 {
			for _, change := range transitions {
				data = binary.BigEndian.AppendUint64(data, uint64(change.at))
			}
			for _, change := range transitions {
				data = append(data, change.zone)
			}
		}
		for i, offset := range offsets {
			data = binary.BigEndian.AppendUint32(data, uint32(offset))
			data = append(data, byte(i), byte(i*daylightName))
		}
		data = append(data, names...)
	}
	return append(data, '\n', '\n')
}
