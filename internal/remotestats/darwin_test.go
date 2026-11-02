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

const darwinLine = "gop-stats-darwin-v1 "

func TestParseDarwinRejectsWhatItCannotTrust(t *testing.T) {
	good := darwinLine + "0 40.50 1000 2000 16777216 4194304 100 en0\n"
	got, err := parseDarwin([]byte(good))
	if err != nil || got.warm || got.cpuPercent != 40.5 || got.primary != "en0" || got.memTotalKiB != 16777216 {
		t.Fatalf("good line = %+v, %v", got, err)
	}
	for _, test := range []struct{ name, data string }{
		{"a line with no interface field", darwinLine + "0 40.50 1000 2000 16777216 4194304 100\n"},
		{"trailing data", darwinLine + "0 40.50 1000 2000 16777216 4194304 100 en0 more\n"},
		{"top's line did not parse", darwinLine + "0 x 1000 2000 16777216 4194304 100 en0\n"},
		{"more than all of the CPU", darwinLine + "0 101 1000 2000 16777216 4194304 100 en0\n"},
		{"not a warm flag", darwinLine + "2 40.50 1000 2000 16777216 4194304 100 en0\n"},
		{"available exceeds total", darwinLine + "0 40.50 1000 2000 16 17 100 en0\n"},
		{"no memory", darwinLine + "0 40.50 1000 2000 0 0 100 en0\n"},
		{"a counter that is not a number", darwinLine + "0 40.50 nope 2000 16777216 4194304 100 en0\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseDarwin([]byte(test.data)); !errors.Is(err, ErrMalformed) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

// The first line of the command only baselines the interface counters. The
// rates come from the receiver's clock, because the script can read whole
// seconds only.
func TestDarwinWorkerSamplesFromTheSecondReading(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := []time.Time{start, start.Add(2 * time.Second)}
	worker := newWorker(feed(
		darwinLine+"1 70.00 1000 2000 16777216 4194304 100 en0\n",
		darwinLine+"0 40.50 5000 2400 16777216 4194304 102 en0\n",
	), nil, "script", kindDarwin)
	worker.now = func() time.Time {
		at := clock[0]
		clock = clock[1:]
		return at
	}
	go worker.run(time.Second, time.Second)
	defer worker.Close()
	got := nextResult(t, worker)
	want := Sample{
		CPUPercent: 40.5, MemoryUsedBytes: (16777216 - 4194304) << 10, MemoryTotalBytes: 16777216 << 10,
		ReceiveBytesPerSecond: 2000, SendBytesPerSecond: 200, UptimeSeconds: 102, NetworkInterface: "en0",
	}
	if got.Err != nil || got.Sample.CPUPercent != want.CPUPercent || got.Sample.MemoryUsedBytes != want.MemoryUsedBytes ||
		got.Sample.ReceiveBytesPerSecond != want.ReceiveBytesPerSecond || got.Sample.SendBytesPerSecond != want.SendBytesPerSecond ||
		got.Sample.NetworkInterface != "en0" || got.Sample.Cores != nil {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
}

func TestDarwinWorkerPublishesNothingFromTheWarmReadingAlone(t *testing.T) {
	worker := newWorker(streamFunc(func(_ context.Context, _ string, _ int, line func([]byte) bool) error {
		line([]byte(darwinLine + "1 70.00 1000 2000 16777216 4194304 100 en0\n"))
		return nil
	}), nil, "script", kindDarwin)
	go worker.run(time.Second, time.Second)
	if result := nextResult(t, worker); !errors.Is(result.Err, ErrEnded) {
		t.Fatalf("a warm reading alone produced %+v", result)
	}
}

func TestDarwinSampleDoesNotInventRatesAcrossAChangeOfRoute(t *testing.T) {
	before := darwinReading{receive: 100, send: 100, memTotalKiB: 8, memAvailKiB: 4, primary: "en0"}
	after := darwinReading{receive: 9000, send: 9000, memTotalKiB: 8, memAvailKiB: 4, primary: "utun4"}
	if got := darwinSample(before, after, time.Second); got.ReceiveBytesPerSecond != 0 || got.SendBytesPerSecond != 0 || got.NetworkInterface != "utun4" {
		t.Fatalf("sample = %+v", got)
	}
}

func TestDarwinWorkerReportsUnsupportedAndMalformedOutput(t *testing.T) {
	for _, test := range []struct {
		line string
		want error
	}{
		{"gop-stats-unsupported Linux\n", ErrUnsupported},
		{darwinLine + "0 x 1 2 3 2 1 en0\n", ErrMalformed},
	} {
		worker := newWorker(feed(test.line), nil, "script", kindDarwin)
		go worker.run(time.Second, time.Second)
		if result := nextResult(t, worker); !errors.Is(result.Err, test.want) {
			t.Fatalf("%q: result = %+v", test.line, result)
		}
		worker.Close()
	}
}

func TestDarwinCommandHasTheSamplingIntervalAndNoDoubledBackslashes(t *testing.T) {
	script := darwinScript(5)
	if !strings.Contains(script, "top -l 0 -s 5 -n 0") || strings.Contains(script, "__INTERVAL__") {
		t.Fatal("the interval was not applied")
	}
	for _, text := range []string{darwinCommand, detailsCommandDarwin} {
		if strings.Contains(text, `\\`) {
			t.Fatal("a command contains a doubled backslash")
		}
	}
}

// runAwk runs one of the commands' awk programs over input with the system's
// awk, so that what ships is what is tested.
func runAwk(t *testing.T, program, input string, vars ...string) string {
	t.Helper()
	path, err := exec.LookPath("awk")
	if err != nil {
		t.Skip("no awk")
	}
	args := []string{}
	for _, variable := range vars {
		args = append(args, "-v", variable)
	}
	args = append(args, program)
	process := exec.Command(path, args...)
	process.Stdin = strings.NewReader(input)
	out, err := process.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// A Mac's volumes: the sealed system volume at /, its container's other
// volumes under /System/Volumes (one of them the data), an external APFS disk
// with two volumes, an exFAT stick and an automount. Every volume of a
// container reports the container's size and free space, so df's own
// percentages mean nothing.
const darwinDisks = `/dev/disk3s1s1 on / (apfs, sealed, local, read-only, journaled)
/dev/disk3s6 on /System/Volumes/VM (apfs, local, noexec, journaled, noatime, nobrowse)
/dev/disk3s5 on /System/Volumes/Data (apfs, local, journaled, nobrowse, protect, root data)
/dev/disk6s1 on /Volumes/Backup Disk (apfs, local, journaled)
/dev/disk6s2 on /Volumes/Backup Disk 2 (apfs, local, journaled)
/dev/disk7s1 on /Volumes/USB Stick (exfat, local, nodev, nosuid, noowners)
map -hosts on /net (autofs, nosuid, automounted, nobrowse)
__MOTTY_DF__
Filesystem 1024-blocks Used Available Capacity Mounted on
/dev/disk3s1s1 482797652 10989144 46501892 20% /
/dev/disk3s6 482797652 13632984 46501892 23% /System/Volumes/VM
/dev/disk3s5 482797652 401651956 46501892 90% /System/Volumes/Data
/dev/disk6s1 1000000 300000 600000 34% /Volumes/Backup Disk
/dev/disk6s2 1000000 100000 600000 15% /Volumes/Backup Disk 2
/dev/disk7s1 500000 100000 400000 20% /Volumes/USB Stick
`

func TestDarwinDisksAreContainersNotVolumes(t *testing.T) {
	got := runAwk(t, darwinDiskAwk, darwinDisks)
	want := "disk\t482797652\t436295760\t46501892\t90\t0\t/\n" +
		"disk\t1000000\t400000\t600000\t40\t0\t/Volumes/Backup Disk\n" +
		"disk\t500000\t100000\t400000\t20\t0\t/Volumes/USB Stick\n"
	if got != want {
		t.Fatalf("disks:\n%s\nwant:\n%s", got, want)
	}
	details, err := parseDetails([]byte("info\t8\t0.1\t0.1\t0.1\t5\t24.5.0\n" + got))
	if err != nil || len(details.Disks) != 3 || details.Disks[0].Path != "/" || details.Disks[0].Percent != 90 || details.Disks[0].Used != 436295760<<10 {
		t.Fatalf("details = %+v, %v", details, err)
	}
}

// netstat prints an interface without a hardware address (the loopback, the
// tunnels) with one field fewer, so its byte columns sit one to the left.
const darwinNetstat = `Name       Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0        16384 <Link#1>                       5317983     0 6559866339  5317983     0 6559866339     0
lo0        16384 127           127.0.0.1        5317983     - 6559866339  5317983     - 6559866339     -
gif0*      1280  <Link#2>                             0     0          0        0     0          0     0
en0        1500  <Link#14>   36:38:ae:c2:f9:80      100     0       5000       50     0       7000     0
en0        1500  172.31.90/24  172.31.90.5          100     -       5000       50     -       7000     -
en1*       1500  <Link#15>   36:38:ae:c2:f9:81       10     0        300        5     0        400     0
utun4      1380  <Link#20>                            9     0        900        8     0        800     0
`

func TestDarwinTrafficFollowsTheDefaultRoute(t *testing.T) {
	for _, test := range []struct{ primary, want string }{
		{"en0", "5000 7000"},
		{"utun4", "900 800"},
		{"", "5300 7400"},
	} {
		if got := runAwk(t, darwinTrafficAwk, darwinNetstat, "primary="+test.primary); got != test.want {
			t.Errorf("primary %q: traffic = %q, want %q", test.primary, got, test.want)
		}
	}
}

func TestDarwinInterfacesListOnlyThoseThatCarriedTraffic(t *testing.T) {
	got := runAwk(t, darwinInterfaceAwk, darwinNetstat)
	want := "net\ten0\t5000\t7000\nnet\ten1\t300\t400\nnet\tutun4\t900\t800\n"
	if got != want {
		t.Fatalf("interfaces:\n%s\nwant:\n%s", got, want)
	}
}

// On a Mac the real commands run under the system shell: the stream must warm
// up, then sample at the interval, and the slow details must parse. This is
// the same path a local terminal uses.
func TestDarwinCommandsOnThisMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the macOS monitor needs macOS")
	}
	if testing.Short() {
		t.Skip("takes a few seconds")
	}
	worker := StartDarwin(Local{}, nil, 1)
	defer func() {
		worker.Close()
		<-worker.Done()
	}()
	started := time.Now()
	for index := range 2 {
		select {
		case result := <-worker.Results():
			if result.Err != nil {
				t.Fatal(result.Err)
			}
			sample := result.Sample
			if sample.CPUPercent < 0 || sample.CPUPercent > 100 || sample.MemoryTotalBytes == 0 ||
				sample.MemoryUsedBytes == 0 || sample.MemoryUsedBytes > sample.MemoryTotalBytes || sample.UptimeSeconds <= 0 {
				t.Fatalf("sample %d = %+v", index, sample)
			}
			if index == 0 && time.Since(started) > 6*time.Second {
				t.Fatalf("the first reading took %v", time.Since(started))
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("no reading %d", index)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := Local{}.RunCommand(ctx, detailsCommandDarwin, 96<<10)
	if err != nil {
		t.Fatal(err)
	}
	details, err := parseDetails(data)
	if err != nil || details.CPUCount == 0 || len(details.Disks) == 0 || details.Disks[0].Path != "/" || details.Uptime <= 0 {
		t.Fatalf("details = %+v, %v", details, err)
	}
}
