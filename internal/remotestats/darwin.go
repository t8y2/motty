package remotestats

import (
	"math"
	"strconv"
	"strings"
	"time"
)

const darwinPrefix = "gop-stats-darwin-v1 "

// darwinReading is one line of the macOS monitor. macOS has no per-CPU or
// system-wide running totals a shell can read, so the CPU figure is top's own
// percentage for its last interval; only the interface byte counts are totals.
type darwinReading struct {
	// warm marks the one reading taken only to give the next reading a
	// network baseline: top's first sample is not an interval's figure.
	warm                     bool
	cpuPercent               float64
	receive, send            uint64
	memTotalKiB, memAvailKiB uint64
	uptime                   uint64
	primary                  string
}

func parseDarwin(line []byte) (darwinReading, error) {
	fields := strings.Fields(string(line))
	if len(fields) != 9 || fields[0]+" " != darwinPrefix {
		return darwinReading{}, ErrMalformed
	}
	var reading darwinReading
	switch fields[1] {
	case "0":
	case "1":
		reading.warm = true
	default:
		return reading, ErrMalformed
	}
	cpu, err := strconv.ParseFloat(fields[2], 64)
	if err != nil || math.IsNaN(cpu) || math.IsInf(cpu, 0) || cpu < 0 || cpu > 100.5 {
		return reading, ErrMalformed
	}
	reading.cpuPercent = min(cpu, 100)
	var values [5]uint64
	for index := range values {
		value, err := strconv.ParseUint(fields[index+3], 10, 64)
		if err != nil {
			return reading, ErrMalformed
		}
		values[index] = value
	}
	reading.receive, reading.send = values[0], values[1]
	reading.memTotalKiB, reading.memAvailKiB, reading.uptime = values[2], values[3], values[4]
	if reading.memTotalKiB == 0 || reading.memAvailKiB > reading.memTotalKiB {
		return reading, ErrMalformed
	}
	if fields[8] != "-" {
		if len(fields[8]) > 64 {
			return reading, ErrMalformed
		}
		reading.primary = fields[8]
	}
	return reading, nil
}

// darwinSample is the use over the interval ending at after. The time between
// the two readings is the receiver's own clock, since the script can only
// read whole seconds.
func darwinSample(before, after darwinReading, elapsed time.Duration) Sample {
	seconds := elapsed.Seconds()
	reading := Sample{
		CPUPercent:       after.cpuPercent,
		MemoryUsedBytes:  (after.memTotalKiB - after.memAvailKiB) << 10,
		MemoryTotalBytes: after.memTotalKiB << 10,
		UptimeSeconds:    float64(after.uptime),
		NetworkInterface: after.primary,
	}
	if before.primary == after.primary && seconds > 0 {
		reading.ReceiveBytesPerSecond = rate(before.receive, after.receive, seconds)
		reading.SendBytesPerSecond = rate(before.send, after.send, seconds)
	}
	return reading
}

// StartDarwin samples a macOS host the way Start samples a Linux one.
func StartDarwin(runner Runner, notify func(), seconds int) *Worker {
	if !validInterval(seconds) {
		seconds = 2
	}
	return startCommand(runner, notify, firstReadingTimeout, time.Duration(seconds)*time.Second+readingTimeout, darwinScript(seconds), kindDarwin)
}

// StartDetailsDarwin is StartDetailsWithInterval for a macOS host.
func StartDetailsDarwin(runner DetailRunner, notify func(), seconds int) *DetailsWorker {
	return startDetails(runner, notify, seconds, detailsCommandDarwin)
}

// top's first sample is not an interval's figure: its sys share ran well above
// the second sample's on three out of three runs. So the command takes one
// short `top -l 2 -s 1` first, whose first sample only baselines the network
// counters, to give a real reading about two seconds after it starts instead
// of an interval later; the long stream then drops its own first sample too.
// After that top is the only resident process; each interval runs vm_stat,
// route and netstat once. Memory is Activity Monitor's: app memory
// (anonymous less purgeable) plus wired plus compressed. Interface counters
// are the default route's interface (or the en/ppp/pdp interfaces without
// one), read from the <Link#> rows of netstat, which lose their address column
// when the interface has none.
//
// Nothing here relies on word splitting of unquoted variables: the command may
// be run by zsh, bash or dash as the login shell.
const darwinCommand = `# motty-stats-darwin-v1
LC_ALL=C
export LC_ALL
if [ "$(uname -s 2>/dev/null)" != Darwin ]; then
	printf 'gop-stats-unsupported %s\n' "$(uname -s 2>/dev/null || printf unknown)"
	exit 0
fi
memsize=$(sysctl -n hw.memsize 2>/dev/null)
pagesize=$(sysctl -n hw.pagesize 2>/dev/null)
boot=$(sysctl -n kern.boottime 2>/dev/null | sed -n 's/.*{ sec = \([0-9]*\),.*/\1/p')
[ -n "$memsize" ] && [ -n "$pagesize" ] && [ -n "$boot" ] || exit 1
emit() {
	avail=$(vm_stat 2>/dev/null | awk -v ps="$pagesize" -v total="$memsize" '
		/^Pages wired down:/ { wired = $4 + 0 }
		/^Pages purgeable:/ { purgeable = $3 + 0 }
		/^Anonymous pages:/ { anonymous = $3 + 0 }
		/^Pages occupied by compressor:/ { compressed = $5 + 0 }
		END {
			used = (anonymous - purgeable + wired + compressed) * ps
			if (used < 0) used = 0
			if (used > total) used = total
			printf "%d", (total - used) / 1024
		}')
	primary=$(route -n get default 2>/dev/null | awk '/interface:/ { print $2; exit }')
	traffic=$(netstat -ibn 2>/dev/null | awk -v primary="$primary" '` + darwinTrafficAwk + `')
	now=$(date +%s)
	printf 'gop-stats-darwin-v1 %s %s %s %s %s %s %s\n' "$1" "$2" "$traffic" "$((memsize / 1024))" "${avail:-0}" "$((now - boot))" "${primary:--}" || exit 0
}
# loop reads top's output: $1 is "warm" for the first sample of a short run,
# "skip" for the first of the long one.
loop() {
	n=0
	while IFS= read -r line; do
		case $line in
		'CPU usage:'*)
			n=$((n + 1))
			cpu=$(printf '%s\n' "$line" | awk '$4 == "user," && $6 == "sys," { u = $3; s = $5; sub(/%/, "", u); sub(/%/, "", s); printf "%.2f", u + s; exit } { print "x" }')
			if [ "$n" -eq 1 ]; then
				[ "$1" = warm ] && emit 1 "$cpu"
			else
				emit 0 "$cpu"
			fi
			;;
		esac
	done
}
top -l 2 -s 1 -n 0 2>/dev/null | loop warm
top -l 0 -s __INTERVAL__ -n 0 2>/dev/null | loop skip`

// darwinTrafficAwk sums the received and sent bytes of the default route's
// interface (primary), or of the ordinary network interfaces without one.
const darwinTrafficAwk = darwinNetHeader + `
	$3 ~ /^<Link#/ {` + darwinNetRow + `
		if (name ~ /^lo/) next
		if (primary != "") {
			if (name == primary) { rx += r; tx += t }
			next
		}
		if (name ~ /^(en|pdp_ip|ppp)[0-9]/) { rx += r; tx += t }
	}
	END { printf "%.0f %.0f", rx, tx }`

// darwinNetHeader finds the byte columns from netstat's own header, and
// darwinNetRow reads one <Link#> row as name, r (received) and t (sent). A
// row without a hardware address has one field fewer, and its columns shift
// left by one.
const darwinNetHeader = `
		NR == 1 {
			for (i = 1; i <= NF; i++) { if ($i == "Ibytes") ib = i; if ($i == "Obytes") ob = i }
			header = NF
			next
		}`

const darwinNetRow = `
			name = $1
			sub(/\*$/, "", name)
			shift = header - NF
			if (!ib || !ob || shift < 0 || shift > 1) next
			r = $(ib - shift) + 0
			t = $(ob - shift) + 0`

// darwinDiskAwk reads `mount` output, a marker line, then `df -Pk -l`. APFS
// volumes of one container share its free space, so df's per-volume figures
// are meaningless on their own (the sealed system volume at / reads 20%
// while its container is 90% full). Each APFS container is one disk,
// labeled by its first user-visible mount: used is the container's size less
// the shared free space. The OS's internal volumes under /System/Volumes
// are the same container and add nothing. Other filesystems are read from df.
const darwinDiskAwk = `
	$0 == "__MOTTY_DF__" { in_df = 1; next }
	!in_df {
		on = index($0, " on ")
		if (on < 2) next
		device = substr($0, 1, on - 1)
		rest = substr($0, on + 4)
		if (match(rest, / \([^()]*\)$/)) {
			options = substr(rest, RSTART + 2)
			if (options ~ /^apfs[,)]/) apfs[device] = 1
		}
		next
	}
	$1 == "Filesystem" || index($1, "/dev/") != 1 || NF < 6 { next }
	{
		mount = $0
		for (i = 0; i < 5; i++) sub(/^[ \t]*[^ \t]+[ \t]+/, "", mount)
		sub(/[ \t]+$/, "", mount)
		if (mount == "" || index(mount, "/System/Volumes/") == 1) next
		if ($2 !~ /^[0-9]+$/ || $3 !~ /^[0-9]+$/ || $4 !~ /^[0-9]+$/ || $2 == 0) next
		total = $2
		available = $4
		if ($1 in apfs) {
			container = $1
			sub(/s[0-9]+(s[0-9]+)?$/, "", container)
			if (container in seen) next
			seen[container] = 1
			used = total - available
			if (used < 0) used = 0
			percent = int(used * 100 / total + 0.5)
		} else {
			used = $3
			percent = $5
			sub(/%$/, "", percent)
		}
		printf "disk\t%s\t%s\t%s\t%s\t0\t%s\n", total, used, available, percent, mount
		if (++count >= 64) exit
	}`

// darwinInterfaceAwk lists every interface that has carried traffic.
const darwinInterfaceAwk = darwinNetHeader + `
	$3 ~ /^<Link#/ {` + darwinNetRow + `
		if (name ~ /^lo/ || (r == 0 && t == 0) || (name in listed)) next
		listed[name] = 1
		printf "net\t%s\t%.0f\t%.0f\n", name, r, t
		if (++count >= 64) exit
	}`

const detailsCommandDarwin = `# motty-monitor-details-darwin-v1
LC_ALL=C
export LC_ALL
cores=$(sysctl -n hw.logicalcpu 2>/dev/null)
load=$(sysctl -n vm.loadavg 2>/dev/null | awk '{ printf "%s\t%s\t%s", $2, $3, $4 }')
boot=$(sysctl -n kern.boottime 2>/dev/null | sed -n 's/.*{ sec = \([0-9]*\),.*/\1/p')
[ -n "$cores" ] && [ -n "$boot" ] && [ -n "$load" ] || exit 1
now=$(date +%s)
printf 'info\t%s\t%s\t%s\t%s\n' "$cores" "$load" "$((now - boot))" "$(uname -r 2>/dev/null)"
{ mount 2>/dev/null; printf '__MOTTY_DF__\n'; df -Pk -l 2>/dev/null; } | awk '` + darwinDiskAwk + `'
netstat -ibn 2>/dev/null | awk '` + darwinInterfaceAwk + `'
`

func darwinScript(seconds int) string {
	return strings.ReplaceAll(darwinCommand, "__INTERVAL__", strconv.Itoa(seconds))
}
