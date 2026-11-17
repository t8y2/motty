package remotestats

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

const detailsFixture = "info\t8\t0.12\t1.23\t2.34\t123456.78\t6.8.0-test\n" +
	"disk\t102400\t90000\t12400\t88%\t1\t/run\n" +
	"disk\t204800\t180000\t24800\t88%\t0\t/data/config 备份\n" +
	"disk\t1024000\t512000\t512000\t50%\t0\t/\n" +
	"net\teth0\t102400\t204800\n"

func TestDetailsPreservesMountNamesAndPrioritizesRealDisks(t *testing.T) {
	d, err := parseDetails([]byte(detailsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if d.CPUCount != 8 || d.Load != [3]float64{.12, 1.23, 2.34} || d.Uptime != 123456.78 {
		t.Fatalf("system details=%+v", d)
	}
	if len(d.Disks) != 3 || d.Disks[0].Path != "/" || d.Disks[1].Path != "/data/config 备份" || !d.Disks[2].Virtual || d.Disks[0].Total != 1024000<<10 {
		t.Fatalf("disks=%+v", d.Disks)
	}

}

func TestDetailsRejectsMalformedSystemAndBoundsOptionalRecords(t *testing.T) {
	for _, data := range []string{"", "info\t0\t1\t1\t1\t1\tx", "info\t1\tNaN\t1\t1\t1\tx", strings.Repeat("x", (96<<10)+1)} {
		if _, err := parseDetails([]byte(data)); !errors.Is(err, ErrMalformed) {
			t.Fatalf("malformed details accepted, len=%d: %v", len(data), err)
		}
	}
	data := detailsFixture + "disk\t18446744073709551615\t1\t1\t5%\t0\t/overflow\n" +
		"net\tbad\t-1\t1\n"
	data += strings.Repeat("disk\t1024\t1\t1023\t1%\t0\t/extra\nnet\tx\t0\t0\n", 100)
	d, err := parseDetails([]byte(data))
	if err != nil || len(d.Disks) != 64 || len(d.Networks) != 64 {
		t.Fatalf("bounded details: disks=%d networks=%d err=%v", len(d.Disks), len(d.Networks), err)
	}
}

func TestDetailsLoginSessionsAreOptionalBoundedAndSanitized(t *testing.T) {
	rows := "logins-ready\nlogin\talice\tpts/1\t2026-11-09 10:40 (10.0.0.22)\n" +
		"login\tbob\tconsole\tNov 9 09:00\nlogin\tbad\t\tno-terminal\n"
	rows += strings.Repeat("login\tother\tpts/2\tNov 9 10:00\n", 40) + "logins-truncated\n"
	details, err := parseDetails([]byte(detailsFixture + rows))
	if err != nil || !details.LoginsAvailable || !details.LoginsTruncated || len(details.Logins) != 32 ||
		details.Logins[0] != (Login{User: "alice", Terminal: "pts/1", Since: "2026-11-09 10:40 (10.0.0.22)"}) {
		t.Fatalf("bounded login sessions=%+v, err=%v", details.Logins, err)
	}
	for _, input := range []string{"", "logins-unavailable\n", "logins-ready\n"} {
		details, err := parseDetails([]byte(detailsFixture + input))
		if err != nil || len(details.Logins) != 0 || details.LoginsAvailable != (input == "logins-ready\n") {
			t.Fatalf("login status %q: %+v, err=%v", input, details, err)
		}
	}
}

func TestNetworkRatesHandleCounterResetAndNewInterfaces(t *testing.T) {
	before := Details{Networks: []Network{{Name: "eth0", Received: 1000, Sent: 500}, {Name: "reset", Received: 999, Sent: 999}}}
	after := Details{Networks: []Network{{Name: "eth0", Received: 3000, Sent: 1500}, {Name: "reset", Received: 1, Sent: 1}, {Name: "new", Received: 5000, Sent: 5000}}}
	networkRates(&after, before, 2)
	if after.Networks[0].ReceivePerSecond != 1000 || after.Networks[0].SendPerSecond != 500 || !after.Networks[0].HasRate || after.Networks[1].ReceivePerSecond != 0 || after.Networks[2].HasRate {
		t.Fatalf("rates=%+v", after.Networks)
	}
}

type detailRunFunc func(context.Context, string, int) ([]byte, error)

func (f detailRunFunc) RunCommand(ctx context.Context, command string, limit int) ([]byte, error) {
	return f(ctx, command, limit)
}

func TestDetailsCloseCancelsAnInflightRequest(t *testing.T) {
	started := make(chan struct{})
	w := StartDetails(detailRunFunc(func(ctx context.Context, command string, limit int) ([]byte, error) {
		if command != detailsCommand || limit != 96<<10 {
			t.Error("unexpected command or output bound")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("request has no deadline")
		}
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}), nil)
	defer w.Close()
	<-started
	w.Close()
	select {
	case <-w.Done():
	case <-time.After(time.Second):
		t.Fatal("request did not stop")
	}
	if _, ok := <-w.Results(); ok {
		t.Fatal("cancelled request published a late result")
	}
}

func TestDetailsCommandOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /proc")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "sh", "-c", detailsCommand).Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = parseDetails(data); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerIntervalAndRouteChanges(t *testing.T) {
	for _, interval := range []int{1, 2, 5, 999} {
		seconds := interval
		if seconds == 999 {
			seconds = 2
		}
		w := StartWithInterval(streamFunc(func(ctx context.Context, script string, limit int, line func([]byte) bool) error {
			if !strings.HasSuffix(script, "sleep "+itoa(seconds)+"\ndone") {
				t.Error("wrong sampling interval")
			}
			return feed("gop-net-primary eth0", reading(1), "gop-net-primary eth1", reading(2))(ctx, script, limit, line)
		}), nil, interval)
		result := nextResult(t, w)
		w.Close()
		<-w.Done()
		if result.Err != nil || result.Sample.NetworkInterface != "eth1" || result.Sample.ReceiveBytesPerSecond != 0 || result.Sample.SendBytesPerSecond != 0 || result.Sample.CPUPercent != 37.5 {
			t.Fatalf("route changed between samples: %+v", result)
		}
	}
}
