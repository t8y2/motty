// Package remotestats samples explicitly enabled server resource usage.
package remotestats

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// maxLineBytes leaves room for rocm-smi's JSON, which is one line.
const maxLineBytes = 16384

var (
	// ErrUnsupported reports a host whose system the monitor has no command
	// for: neither Linux procfs, macOS nor Windows.
	ErrUnsupported = errors.New("remote stats: unsupported server")
	// ErrMalformed reports output that does not match the bounded stats protocol.
	ErrMalformed = errors.New("remote stats: malformed response")
	// ErrStalled reports a monitor command that stopped producing readings.
	ErrStalled = errors.New("remote stats: no reading arrived in time")
	// ErrEnded reports a monitor command that exited on its own.
	ErrEnded = errors.New("remote stats: the monitor command ended")
)

// Runner starts the fixed monitor command over an authenticated connection and
// hands it each line of output until line reports false.
type Runner interface {
	StreamCommand(ctx context.Context, command string, maxLineBytes int, line func([]byte) bool) error
}

// Sample is one view of aggregate server resource use over the last interval.
type Sample struct {
	CPUUnavailable        bool
	NetworkUnavailable    bool
	Cores                 []Core
	CPUPercent            float64
	MemoryUsedBytes       uint64
	MemoryTotalBytes      uint64
	ReceiveBytesPerSecond uint64
	SendBytesPerSecond    uint64
	UptimeSeconds         float64
	NetworkInterface      string
	// GPUs lists the accelerators the server reported at this reading, and is
	// empty on a server without any (or without their tools).
	GPUs []GPU
}

// Core is a logical CPU, identified by the server (including processor groups
// on Windows). Missing counters are omitted rather than represented as 0%.
type Core struct {
	ID      string
	Percent float64
}

// counters is one line of the monitor's output: the kernel's running totals, so
// that the rates are worked out here and the server does no arithmetic beyond
// adding up interfaces.
type counters struct {
	cpuBusy, cpuTotal uint64
	receive, send     uint64
	memTotalKiB       uint64
	memAvailKiB       uint64
	uptime            float64
}

// The command has a version marker so the e2e server and diagnostics can
// distinguish it from arbitrary command sessions. It is one shell process that
// stays up and prints one line of counters a second (preceded, on a server with a
// GPU whose tool is installed, by that GPU's lines; see gpu.go): no new session and no
// helper process per reading beyond sleep. The first reading comes at once and
// the second after a quarter second where sleep takes fractions, so the first
// figures appear almost immediately. It reads only Linux procfs and sums the
// non-loopback interfaces. The loop ends when its output is closed.
const command = `# motty-stats-v2
LC_ALL=C
export LC_ALL
gpu_timeout=
if command -v timeout >/dev/null 2>&1; then gpu_timeout='timeout 3'; fi
# gpu-probe-begin
gpu_read() {
	v=-
	[ -r "$1" ] && read -r v <"$1" 2>/dev/null
	case $v in '' | *[!0-9]*) v=- ;; esac
}
gpu_probe() {
	if command -v nvidia-smi >/dev/null 2>&1; then
		$gpu_timeout nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,fan.speed --format=csv,noheader,nounits 2>&1 | head -n 16 | while IFS= read -r row; do
			printf 'gop-gpu-nvidia %s\n' "$row"
		done
	fi
	amd=
	if command -v rocm-smi >/dev/null 2>&1; then
		amd=$($gpu_timeout rocm-smi --showuse --showmemuse --showtemp --showmeminfo vram --showproductname --json 2>/dev/null | tr -d '\n\r' | cut -c1-15000)
		case $amd in *'{'*) printf 'gop-gpu-rocm %s\n' "$amd" ;; *) amd= ;; esac
	fi
	if [ -z "$amd" ]; then
		for f in /sys/class/drm/card[0-9]*/device/gpu_busy_percent; do
			[ -r "$f" ] || continue
			d=${f%/gpu_busy_percent}
			c=${d%/device}
			c=${c##*/}
			case $c in *[!0-9a-z]*) continue ;; esac
			gpu_read "$f"
			b=$v
			gpu_read "$d/mem_info_vram_used"
			u=$v
			gpu_read "$d/mem_info_vram_total"
			m=$v
			t=-
			for h in "$d"/hwmon/hwmon*/temp1_input; do
				gpu_read "$h"
				t=$v
				break
			done
			printf 'gop-gpu-sysfs %s %s %s %s %s\n' "$c" "$b" "$u" "$m" "$t"
		done
	fi
}
# gpu-probe-end
if [ ! -r /proc/stat ] || [ ! -r /proc/meminfo ] || [ ! -r /proc/net/dev ] || [ ! -r /proc/uptime ]; then
	printf 'gop-stats-unsupported %s\n' "$(uname -s 2>/dev/null || printf unknown)"
	exit 0
fi
sample() {
	gpu_probe
	core_count=0
	while read -r name cu cn cs ci cw cq csq cst _; do
		case $name in cpu[0-9]*)
			cb=$((cu + cn + cs + ${cq:-0} + ${csq:-0} + ${cst:-0}))
			ct=$((cb + ci + ${cw:-0}))
			printf 'gop-core %s %s %s\n' "${name#cpu}" "$cb" "$ct"
			core_count=$((core_count + 1))
			[ "$core_count" -ge 256 ] && break
		;; esac
	done </proc/stat
	read -r _ u n s i w q sq st _ </proc/stat
	busy=$((u + n + s + ${q:-0} + ${sq:-0} + ${st:-0}))
	total=$((busy + i + ${w:-0}))
	rx=0
	tx=0
	primary=
	if [ -r /proc/net/route ]; then
		while read -r device destination _; do
			if [ "$destination" = 00000000 ]; then primary=$device; break; fi
		done </proc/net/route
	fi
	while IFS=': ' read -r name r _ _ _ _ _ _ _ t _; do
		case $name in lo | '') continue ;; esac
		if [ -n "$primary" ] && [ "$name" != "$primary" ]; then continue; fi
		case $r in '' | *[!0-9]*) continue ;; esac
		rx=$((rx + r))
		tx=$((tx + ${t:-0}))
	done </proc/net/dev
	mt=
	ma=
	mf=0; mb=0; mc=0; mr=0; ms=0
	while read -r key value _; do
		case $key in
		MemTotal:) mt=$value ;;
		MemAvailable:) ma=$value ;;
		MemFree:) mf=$value ;;
		Buffers:) mb=$value ;;
		Cached:) mc=$value ;;
		SReclaimable:) mr=$value ;;
		Shmem:) ms=$value ;;
		esac
		if [ -n "$mt" ] && [ -n "$ma" ]; then break; fi
	done </proc/meminfo
	if [ -z "$ma" ] && [ -n "$mt" ]; then
		ma=$((mf + mb + mc + mr - ms))
		[ "$ma" -lt 0 ] && ma=0
		[ "$ma" -gt "$mt" ] && ma=$mt
	fi
	[ -n "$ma" ] && [ -n "$mt" ] || return 1
	read -r up _ </proc/uptime
	printf 'gop-net-primary %s\n' "${primary:--}"
	printf 'gop-stats-v2 %s %s %s %s %s %s %s\n' "$busy" "$total" "$rx" "$tx" "$mt" "$ma" "$up" || exit 0
}
sample || exit 1
sleep 0.25 2>/dev/null || sleep 1
while :; do
	sample || exit 1
	sleep 1
done`

// parse reads one line of the monitor's output.
func parse(line []byte) (counters, error) {
	var fields [8][]byte
	count := splitFields(line, &fields)
	if count >= 1 && string(fields[0]) == "gop-stats-unsupported" {
		name := "unknown"
		if count == 2 {
			name = string(fields[1])
		} else if count != 1 {
			return counters{}, ErrMalformed
		}
		return counters{}, fmt.Errorf("%w: %s", ErrUnsupported, name)
	}
	if count != 8 || string(fields[0]) != "gop-stats-v2" {
		return counters{}, ErrMalformed
	}
	var values [6]uint64
	for index := range values {
		value, err := strconv.ParseUint(string(fields[index+1]), 10, 64)
		if err != nil {
			return counters{}, ErrMalformed
		}
		values[index] = value
	}
	uptime, err := strconv.ParseFloat(string(fields[7]), 64)
	if err != nil || math.IsNaN(uptime) || math.IsInf(uptime, 0) || uptime < 0 {
		return counters{}, ErrMalformed
	}
	if values[4] == 0 || values[5] > values[4] {
		return counters{}, ErrMalformed
	}
	return counters{
		cpuBusy: values[0], cpuTotal: values[1], receive: values[2], send: values[3],
		memTotalKiB: values[4], memAvailKiB: values[5], uptime: uptime,
	}, nil
}

// sample is the use between two readings. It reports false when the readings
// are not in order or no time passed, which no real server does but a
// restarted counter could.
func sample(before, after counters) (Sample, bool) {
	cpuDelta := float64(after.cpuTotal) - float64(before.cpuTotal)
	seconds := after.uptime - before.uptime
	if cpuDelta <= 0 || seconds <= 0 {
		return Sample{}, false
	}
	cpu := min(max((float64(after.cpuBusy)-float64(before.cpuBusy))*100/cpuDelta, 0), 100)
	return Sample{
		CPUPercent:            cpu,
		MemoryUsedBytes:       (after.memTotalKiB - after.memAvailKiB) << 10,
		MemoryTotalBytes:      after.memTotalKiB << 10,
		ReceiveBytesPerSecond: rate(before.receive, after.receive, seconds),
		SendBytesPerSecond:    rate(before.send, after.send, seconds),
		UptimeSeconds:         after.uptime,
	}, true
}

func rate(before, after uint64, seconds float64) uint64 {
	if after < before {
		return 0
	}
	return uint64(float64(after-before) / seconds)
}

func splitFields(data []byte, fields *[8][]byte) int {
	count := 0
	for index := 0; index < len(data); {
		for index < len(data) && asciiSpace(data[index]) {
			index++
		}
		if index == len(data) {
			break
		}
		if count == len(fields) {
			return count + 1
		}
		start := index
		for index < len(data) && !asciiSpace(data[index]) {
			index++
		}
		fields[count] = data[start:index]
		count++
	}
	return count
}

func asciiSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\v' || value == '\f'
}
