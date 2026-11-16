package remotestats

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestDashboardWindowsIntervalsReachTheRemoteScript(t *testing.T) {
	for _, seconds := range []int{1, 2, 5, 10, 30, 60} {
		command := WindowsCommand(seconds)
		encoded := command[strings.LastIndex(command, " ")+1:]
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		units := make([]uint16, len(data)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(data[i*2:])
		}
		script := string(utf16.Decode(units))
		if !strings.Contains(script, "Start-Sleep -Seconds "+strconv.Itoa(seconds)) {
			t.Fatalf("interval %d fell back to another frequency", seconds)
		}
	}
}

type commandCaptureRunner struct{ commands chan string }

func (r commandCaptureRunner) StreamCommand(ctx context.Context, command string, _ int, _ func([]byte) bool) error {
	r.commands <- command
	<-ctx.Done()
	return ctx.Err()
}
func TestDashboardLongIntervalUsesOneCancellableLinuxStream(t *testing.T) {
	r := commandCaptureRunner{commands: make(chan string, 1)}
	w := StartWithInterval(r, nil, 60)
	defer w.Close()
	select {
	case cmd := <-r.commands:
		if !strings.HasSuffix(cmd, "sleep 60\ndone") {
			t.Fatal("Linux sampler ignored requested interval")
		}
	case <-time.After(time.Second):
		t.Fatal("sampler did not start")
	}
	w.Close()
	select {
	case <-w.Done():
	case <-time.After(time.Second):
		t.Fatal("long sampling interval delayed cancellation")
	}
}
func TestDashboardWindowsRatesDoNotInventByteTotals(t *testing.T) {
	r, err := parseWindows([]byte(`gop-stats-win-v1 {"cpu":1,"total":1000,"free":500,"uptime":1,"networkAvailable":true,"net":[{"name":"Ethernet","rx":1234,"tx":5678}],"meta":{"count":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Details == nil || len(r.Details.Networks) != 1 || !r.Details.Networks[0].TotalsUnavailable || r.Sample.ReceiveBytesPerSecond != 1234 {
		t.Fatal("missing totals were exposed as real zeros or rates were lost")
	}
}
