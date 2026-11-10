package remotestats

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type streamFunc func(ctx context.Context, command string, limit int, line func([]byte) bool) error

func (run streamFunc) StreamCommand(ctx context.Context, command string, limit int, line func([]byte) bool) error {
	return run(ctx, command, limit, line)
}

// reading is the line a server that is 37.5% busy, uses 3 of 8 GiB and moves
// 128 KiB/s in and 64 KiB/s out would print at the given second of uptime.
func reading(second int) string {
	return "gop-stats-v2 " + itoa(375*second) + " " + itoa(1000*second) + " " + itoa(131072*second) + " " + itoa(65536*second) +
		" 8388608 5242880 " + itoa(second) + ".00\n"
}

func itoa(value int) string { return strconv.Itoa(value) }

func TestSampleIsTheDifferenceBetweenTwoReadings(t *testing.T) {
	before, err := parse([]byte(reading(10)))
	if err != nil {
		t.Fatal(err)
	}
	after, err := parse([]byte(reading(11)))
	if err != nil {
		t.Fatal(err)
	}
	want := Sample{
		CPUPercent: 37.5, MemoryUsedBytes: 3 << 30, MemoryTotalBytes: 8 << 30,
		ReceiveBytesPerSecond: 128 << 10, SendBytesPerSecond: 64 << 10,
		UptimeSeconds: 11,
	}
	if got, ok := sample(before, after); !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("sample = %+v ok=%v", got, ok)
	}
	if _, ok := sample(after, before); ok {
		t.Fatal("readings in the wrong order produced a sample")
	}
	if _, ok := sample(after, after); ok {
		t.Fatal("two identical readings produced a sample")
	}
}

func TestParseRejectsUnsupportedAndMalformedReadings(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
		want error
	}{
		{"unsupported", "gop-stats-unsupported Darwin\n", ErrUnsupported},
		{"wrong version", "gop-stats-v1 1 2 3 4 5 6 7\n", ErrMalformed},
		{"short", "gop-stats-v2 1 2 3\n", ErrMalformed},
		{"not a number", "gop-stats-v2 1 2 nope 4 5 4 7.0\n", ErrMalformed},
		{"bad uptime", "gop-stats-v2 1 2 3 4 5 4 NaN\n", ErrMalformed},
		{"available exceeds total", "gop-stats-v2 1 2 3 4 5 6 7.0\n", ErrMalformed},
		{"zero memory", "gop-stats-v2 1 2 3 4 0 0 7.0\n", ErrMalformed},
		{"trailing data", "gop-stats-v2 1 2 3 4 5 4 7.0 surprise\n", ErrMalformed},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parse([]byte(test.data)); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func FuzzParseReading(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(reading(3)),
		[]byte("gop-stats-unsupported Darwin\n"),
		{},
		{0, 1, 2, 3},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := parse(data)
		if err == nil && (got.memTotalKiB == 0 || got.memAvailKiB > got.memTotalKiB || got.uptime < 0) {
			t.Fatalf("invalid accepted reading: %+v", got)
		}
	})
}

// feed returns a runner that hands the worker the given lines at once and then,
// like a live server, keeps the command open until it is stopped.
func feed(lines ...string) streamFunc {
	return func(ctx context.Context, _ string, _ int, line func([]byte) bool) error {
		for _, text := range lines {
			if !line([]byte(text)) {
				return nil
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}
}

// statsWait is how long a test waits for the worker goroutine to publish a
// result or stop. It reports a program the test cannot see, never a performance
// budget, so a slow machine must not turn it into a failure. Windows is the
// slow case: a hosted runner can take seconds to schedule the goroutine after
// the process starts.
func statsWait() time.Duration {
	if runtime.GOOS == "windows" {
		return 30 * time.Second
	}
	return 5 * time.Second
}

func nextResult(t *testing.T, worker *Worker) Result {
	t.Helper()
	select {
	case result := <-worker.Results():
		return result
	case <-time.After(statsWait()):
		t.Fatal("worker produced no result")
		return Result{}
	}
}

func TestWorkerRunsOneFixedCommandAndSamplesFromTheSecondReading(t *testing.T) {
	var commands atomic.Int32
	notified := make(chan struct{}, 4)
	worker := start(streamFunc(func(ctx context.Context, got string, limit int, line func([]byte) bool) error {
		commands.Add(1)
		if !strings.Contains(got, "# motty-stats-v2") || limit != maxLineBytes {
			t.Errorf("command marker=%v limit=%d", strings.Contains(got, "# motty-stats-v2"), limit)
		}
		return feed(reading(1), reading(2), reading(3))(ctx, got, limit, line)
	}), func() { notified <- struct{}{} }, statsWait(), statsWait())
	defer worker.Close()

	// Three readings make two samples: the first only sets the baseline.
	var last Result
	for range 2 {
		last = nextResult(t, worker)
		if last.Err != nil || last.Sample.CPUPercent != 37.5 {
			t.Fatalf("result = %+v", last)
		}
	}
	if commands.Load() != 1 {
		t.Fatalf("the monitor started %d commands", commands.Load())
	}
	worker.Close()
	select {
	case <-worker.Done():
	case <-time.After(statsWait()):
		t.Fatal("worker did not stop")
	}
}

func TestWorkerKeepsOnlyTheLatestUnreadSample(t *testing.T) {
	lines := []string{reading(1)}
	for second := 2; second <= 6; second++ {
		lines = append(lines, reading(second))
	}
	worker := start(feed(lines...), nil, time.Second, time.Second)
	defer worker.Close()
	time.Sleep(20 * time.Millisecond)
	if result := nextResult(t, worker); result.Err != nil || result.Sample.CPUPercent != 37.5 {
		t.Fatalf("result = %+v", result)
	}
	select {
	case result := <-worker.Results():
		t.Fatalf("a second unread result was kept: %+v", result)
	default:
	}
}

func TestWorkerStopsAfterAnError(t *testing.T) {
	for _, test := range []struct {
		name   string
		runner streamFunc
		want   error
	}{
		{"refused", func(context.Context, string, int, func([]byte) bool) error { return errors.New("refused") }, nil},
		{"unsupported", feed("gop-stats-unsupported Darwin\n"), ErrUnsupported},
		{"malformed", feed("hello\n"), ErrMalformed},
		{"ended", func(context.Context, string, int, func([]byte) bool) error { return nil }, ErrEnded},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			worker := start(streamFunc(func(ctx context.Context, command string, limit int, line func([]byte) bool) error {
				calls.Add(1)
				return test.runner(ctx, command, limit, line)
			}), nil, time.Second, time.Second)
			result := nextResult(t, worker)
			if result.Err == nil || test.want != nil && !errors.Is(result.Err, test.want) {
				t.Fatalf("error = %v, want %v", result.Err, test.want)
			}
			<-worker.Done()
			if calls.Load() != 1 {
				t.Fatalf("a failed monitor ran %d commands", calls.Load())
			}
		})
	}
}

func TestWorkerReportsAMonitorThatGoesSilent(t *testing.T) {
	worker := start(feed(reading(1), reading(2)), nil, time.Second, 20*time.Millisecond)
	defer worker.Close()
	if result := nextResult(t, worker); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := nextResult(t, worker); !errors.Is(result.Err, ErrStalled) {
		t.Fatalf("error = %v, want a stall", result.Err)
	}
	<-worker.Done()
}

func TestClosingWorkerCancelsTheRunningCommand(t *testing.T) {
	started := make(chan struct{})
	worker := start(streamFunc(func(ctx context.Context, _ string, _ int, _ func([]byte) bool) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}), nil, time.Hour, time.Hour)
	<-started
	worker.Close()
	select {
	case <-worker.Done():
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	if result, ok := <-worker.Results(); ok {
		t.Fatalf("a closed worker reported %+v", result)
	}
}

func BenchmarkParseReading(b *testing.B) {
	data := []byte(reading(7))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parse(data); err != nil {
			b.Fatal(err)
		}
	}
}

// The monitor is a raw Go string handed to a remote shell: a doubled backslash
// would reach printf as a literal "\n" and every reading would fail to parse.
func TestCommandEscapesReachTheShellOnce(t *testing.T) {
	if strings.Contains(command, `\\`) {
		t.Fatalf("monitor command contains a doubled backslash")
	}
}

// On a Linux host the real script runs under the system shell: it must print
// readings that parse and make sense, the first at once.
func TestCommandPrintsReadingsOnThisHost(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the monitor reads Linux procfs")
	}
	if _, err := os.Stat("/proc/net/dev"); err != nil {
		t.Skip("no procfs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	stdout, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = process.Wait() }()
	lines := bufio.NewScanner(stdout)
	var readings []counters
	started := time.Now()
	for len(readings) < 3 && lines.Scan() {
		if isGPULine(lines.Bytes()) || strings.HasPrefix(lines.Text(), "gop-net-primary ") || strings.HasPrefix(lines.Text(), "gop-core ") {
			continue
		}
		got, err := parse(append(lines.Bytes(), '\n'))
		if err != nil {
			t.Fatalf("line %q: %v", lines.Text(), err)
		}
		readings = append(readings, got)
		if len(readings) == 1 && time.Since(started) > time.Second {
			t.Fatalf("the first reading took %v", time.Since(started))
		}
	}
	if len(readings) != 3 {
		t.Fatalf("got %d readings: %v", len(readings), lines.Err())
	}
	for index := 1; index < len(readings); index++ {
		got, ok := sample(readings[index-1], readings[index])
		if !ok || got.MemoryTotalBytes == 0 || got.MemoryUsedBytes > got.MemoryTotalBytes {
			t.Fatalf("sample %d = %+v ok=%v", index, got, ok)
		}
	}
}
