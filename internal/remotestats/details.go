package remotestats

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// DetailRunner is deliberately separate from the fast streaming sampler. A
// slow filesystem must never stop CPU/memory sampling or the terminal.
type DetailRunner interface {
	RunCommand(context.Context, string, int) ([]byte, error)
}

type Disk struct {
	Path                   string
	Used, Total, Available uint64
	Percent                float64
	Virtual                bool
}

type Network struct {
	Name                            string
	Received, Sent                  uint64
	ReceivePerSecond, SendPerSecond uint64
	HasRate                         bool
	// Addresses are the interface's own addresses as the platform prints them,
	// without the prefix length: IPv4 first, then IPv6, at most four.
	Addresses []string
	// Windows formatted performance counters expose rates, not byte totals.
	TotalsUnavailable bool
}

// Listen is one socket a host is listening on: what it speaks, where it binds,
// and, where the platform tells an unprivileged reader, which process owns it.
type Listen struct {
	Proto   string
	Address string
	Port    int
	Process string
	PID     int
}

// Process is one row of the host's process list: what it is called, how much of
// a CPU it is using now, and how much of the host's memory it holds.
type Process struct {
	PID           int
	Name          string
	CPUPercent    float64
	MemoryPercent float64
	// MemoryBytes is the resident set size the platform reports: the memory the
	// process holds at this moment, not what it has laid claim to.
	MemoryBytes uint64
}

// MemoryDetail is the host's memory as its kernel breaks it down, in bytes.
// A platform that has no equivalent of one of the pools leaves it zero rather
// than filling it with a guess.
type MemoryDetail struct {
	Total, Available, Free, Buffers, Cached uint64
	SwapTotal, SwapFree                     uint64
}

type Details struct {
	LoadUnavailable bool
	PrimaryDisk     string
	CPUCount        int
	Load            [3]float64
	Uptime          float64
	Kernel          string
	Disks           []Disk
	Networks        []Network
	Listens         []Listen
	Logins          []Login
	LoginsAvailable bool
	LoginsTruncated bool
	Processes       []Process
	Memory          MemoryDetail
}

type Login struct {
	User, Terminal, Since string
}

type DetailResult struct {
	Details Details
	Err     error
}

// DetailsWorker polls bounded summaries only while the monitor is enabled.
// All commands are fixed strings; no host name, path or process name is executed.
type DetailsWorker struct {
	results chan DetailResult
	cancel  context.CancelFunc
	done    chan struct{}
	close   sync.Once
}

func StartDetails(runner DetailRunner, notify func()) *DetailsWorker {
	return StartDetailsWithInterval(runner, notify, 5)
}

// StartDetailsWithInterval keeps slow metadata independent of fast streaming.
// An explicit selection without disks or network needs no details worker.
func StartDetailsWithInterval(runner DetailRunner, notify func(), seconds int, selected ...Metrics) *DetailsWorker {
	script := detailsCommand
	if len(selected) > 0 {
		if selection(selected)&(Disks|NetworkIO) == 0 {
			return nil
		}
		script = selectedDetails(script, false, selection(selected))
	}
	return startDetails(runner, notify, seconds, linuxShellCommand(script), selected...)
}

func startDetails(runner DetailRunner, notify func(), seconds int, command string, selected ...Metrics) *DetailsWorker {
	if !validInterval(seconds) {
		seconds = 5
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &DetailsWorker{results: make(chan DetailResult, 1), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(w.done)
		defer close(w.results)
		var previous Details
		var previousAt time.Time
		for {
			request, finish := context.WithTimeout(ctx, 4*time.Second)
			data, err := runner.RunCommand(request, command, maxDetailsOutput)
			finish()
			if ctx.Err() != nil {
				return
			}
			var current Details
			if err == nil {
				current, err = parseDetails(data)
				current = selection(selected).FilterDetails(current)
			}
			at := time.Now()
			if err == nil {
				if !previousAt.IsZero() {
					networkRates(&current, previous, at.Sub(previousAt).Seconds())
				}
				previous, previousAt = current, at
			}
			select {
			case <-w.results:
			default:
			}
			w.results <- DetailResult{Details: current, Err: err}
			if notify != nil {
				notify()
			}
			timer := time.NewTimer(time.Duration(seconds) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return w
}

func (w *DetailsWorker) Results() <-chan DetailResult { return w.results }
func (w *DetailsWorker) Done() <-chan struct{}        { return w.done }
func (w *DetailsWorker) Close() {
	if w != nil {
		w.close.Do(w.cancel)
	}
}

const detailsCommand = `# motty-monitor-details-v1
LC_ALL=C
export LC_ALL
[ -r /proc/stat ] && [ -r /proc/loadavg ] && [ -r /proc/uptime ] || exit 1
cores=$(awk '/^cpu[0-9]+ / { n++ } END { print n+0 }' /proc/stat)
read -r load1 load5 load15 _ </proc/loadavg
read -r uptime _ </proc/uptime
printf 'info\t%s\t%s\t%s\t%s\t%s\t%s\n' "$cores" "$load1" "$load5" "$load15" "$uptime" "$(uname -r)"
df -Pk -l 2>/dev/null | awk 'NR>1 && $2 ~ /^[0-9]+$/ && $3 ~ /^[0-9]+$/ {
    mount=$0; for (i=0;i<5;i++) sub(/^[ \t]*[^ \t]+[ \t]+/, "", mount)
    virtual=($1=="tmpfs" || $1=="devtmpfs" || mount ~ /^\/(dev|sys|proc|run)(\/|$)/ || mount ~ /^\/snap\// || mount ~ /^\/etc\/(hosts|hostname|resolv.conf)$/)
    printf "disk\t%s\t%s\t%s\t%s\t%d\t%s\n", $2,$3,$4,$5,virtual,mount
    if (++n>=64) exit
}'
awk 'NR>2 {
    split($0,parts,":"); name=parts[1]; gsub(/^[ \t]+|[ \t]+$/, "", name)
    rest=parts[2]; sub(/^[ \t]+/, "", rest); split(rest,v,/[ \t]+/)
    if (name!="lo") printf "net\t%s\t%s\t%s\n",name,v[1],v[9]
    if (++n>=64) exit
}' /proc/net/dev
# The addresses of the interfaces /proc/net/dev just listed: iproute2 where it
# is installed, the older ifconfig otherwise.
if command -v ip >/dev/null 2>&1; then
ip -o addr show 2>/dev/null | awk '{
    name = $2; sub(/:$/, "", name)
    if (name ~ /^lo/) next
    for (i = 3; i < NF; i++) if ($i == "inet" || $i == "inet6") {
        address = $(i + 1); sub(/\/.*/, "", address)
        printf "netaddr\t%s\t%s\n", name, address
    }
}'
else
ifconfig -a 2>/dev/null | awk '
    /^[^ \t]/ { name = $1; sub(/:$/, "", name) }
    name ~ /^lo/ { next }
    $1 == "inet" || $1 == "inet6" {
        address = $2; sub(/^addr:/, "", address); sub(/\/.*/, "", address); sub(/%.*/, "", address)
        printf "netaddr\t%s\t%s\n", name, address
    }
'
fi
# The sockets this host listens on: ss where iproute2 is installed, netstat's
# -l list where it is not (that one has no process column without root).
if command -v ss >/dev/null 2>&1; then
ss -H -lntup 2>/dev/null | awk '
    {
        proto = $1; local = $5
        host = local; sub(/:[^:]*$/, "", host); gsub(/\[/, "", host); gsub(/\]/, "", host)
        port = local; sub(/.*:/, "", port)
        if (port !~ /^[0-9]+$/) next
        if (host == "" || host == "*") host = "0.0.0.0"
        users = ""; for (i = 7; i <= NF; i++) users = users " " $i
        process = "-"; pid = 0
        if ((k = index(users, "users:")) > 0) {
            inner = substr(users, k)
            if ((q = index(inner, "\"")) > 0) {
                rest = substr(inner, q + 1)
                if ((q2 = index(rest, "\"")) > 1) process = substr(rest, 1, q2 - 1)
            }
            if ((k2 = index(inner, "pid=")) > 0) {
                digits = substr(inner, k2 + 4); sub(/[^0-9].*/, "", digits)
                if (digits != "") pid = digits
            }
        }
        printf "listen\t%s\t%s\t%s\t%s\t%s\n", proto, host, port, process, pid
        if (++count >= 64) exit
    }'
else
netstat -lntu 2>/dev/null | awk '
    NR > 2 {
        proto = $1; local = $4
        host = local; sub(/:[^:]*$/, "", host); gsub(/\[/, "", host); gsub(/\]/, "", host)
        port = local; sub(/.*:/, "", port)
        if (port !~ /^[0-9]+$/) next
        if (host == "" || host == "*") host = "0.0.0.0"
        printf "listen\t%s\t%s\t%s\t-\t0\n", proto, host, port
        if (++count >= 64) exit
    }'
fi
` + loginsCommand + processesCommand

// processesCommand reads what the host is running and how its kernel breaks its
// memory down. ps orders by CPU, and a name is the rest of the row after the RSS:
// `comm` may contain spaces, and the last column runs to the end of the line.
const processesCommand = `# motty-monitor-processes-v1
ps -eo pid,pcpu,pmem,rss,comm --sort=-pcpu 2>/dev/null | awk 'NR>1 && NR<=1025 {
	name = $5
	for (i = 6; i <= NF; i++) name = name " " $i
	printf "proc\t%s\t%s\t%s\t%s\t%s\n", $1, $2, $3, $4, name
}'
awk '
/MemTotal:/ { total = $2 }
/MemAvailable:/ { available = $2 }
/MemFree:/ { free = $2 }
/^Buffers:/ { buffers = $2 }
/^Cached:/ { cached = $2 }
/^SwapTotal:/ { swaptotal = $2 }
/^SwapFree:/ { swapfree = $2 }
END { printf "mem\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", total, available, free, buffers, cached, swaptotal, swapfree }
' /proc/meminfo
`

const loginsCommand = `if command -v who >/dev/null 2>&1 && logins=$(who 2>/dev/null); then
	printf 'logins-ready\n'
	printf '%s\n' "$logins" | awk 'NF>=3 {
		if (++count>32) { print "logins-truncated"; exit }
		printf "login\t%s\t%s\t", $1, $2
		for (i=3; i<=NF; i++) printf "%s%s", (i==3 ? "" : " "), $i
		printf "\n"
	}'
else
	printf 'logins-unavailable\n'
fi
`

// maxNetworkAddresses bounds the addresses one interface may report: a host
// with a link-local IPv6, a global one and two IPv4 aliases fills it.
const maxNetworkAddresses = 4

// maxListens bounds the listening sockets one host reports: a desktop with a
// full container stack settles well below it.
const maxListens = 64

// maxProcesses bounds the process rows one host reports. It is a bound on one
// reading, not a screenful: a busy container host runs a few hundred, and the
// panel scrolls and searches them like any other list.
const maxProcesses = 1024

// maxDetailsOutput bounds one details reading. The payload carries every
// process of a large host along with the disks, interfaces, sockets and
// logins, so the limit follows maxProcesses rather than a screenful.
const maxDetailsOutput = 512 << 10

func parseDetails(data []byte) (Details, error) {
	var d Details
	if len(data) > maxDetailsOutput {
		return d, ErrMalformed
	}
	// The interface rows and their addresses arrive in either order, so the
	// addresses wait for the rows they name.
	addresses := make(map[string][]string, 8)
	for _, line := range strings.Split(string(data), "\n") {
		p := strings.SplitN(strings.TrimSuffix(line, "\r"), "\t", 8)
		switch p[0] {
		case "logins-ready":
			d.LoginsAvailable = true
		case "logins-unavailable":
			d.LoginsAvailable = false
		case "logins-truncated":
			d.LoginsTruncated = true
		case "login":
			if len(p) != 4 || len(d.Logins) >= 32 || !d.LoginsAvailable {
				continue
			}
			user, terminal, since := detailText(p[1], 64), detailText(p[2], 64), detailText(p[3], 160)
			if user != "" && terminal != "" && since != "" {
				d.Logins = append(d.Logins, Login{User: user, Terminal: terminal, Since: since})
			}
		case "info":
			if len(p) != 7 {
				return Details{}, ErrMalformed
			}
			n, err := strconv.Atoi(p[1])
			if err != nil || n < 1 || n > 1<<20 {
				return Details{}, ErrMalformed
			}
			d.CPUCount = n
			for i := range d.Load {
				v, ok := detailFloat(p[i+2])
				if !ok {
					return Details{}, ErrMalformed
				}
				d.Load[i] = v
			}
			v, ok := detailFloat(p[5])
			if !ok {
				return Details{}, ErrMalformed
			}
			d.Uptime = v
			d.Kernel = detailText(p[6], 120)
		case "disk":
			if len(p) != 7 || len(d.Disks) >= 64 || !strings.HasPrefix(p[6], "/") {
				continue
			}
			total, ok1 := detailBytes(p[1])
			used, ok2 := detailBytes(p[2])
			available, ok3 := detailBytes(p[3])
			percent, ok4 := detailFloat(strings.TrimSuffix(p[4], "%"))
			if !ok1 || !ok2 || !ok3 || !ok4 || total == 0 {
				continue
			}
			d.Disks = append(d.Disks, Disk{Path: detailText(p[6], 2048), Total: total, Used: used, Available: available, Percent: min(percent, 100), Virtual: p[5] == "1"})
		case "net":
			if len(p) != 4 || len(d.Networks) >= 64 {
				continue
			}
			rx, e1 := strconv.ParseUint(p[2], 10, 64)
			tx, e2 := strconv.ParseUint(p[3], 10, 64)
			if e1 == nil && e2 == nil {
				d.Networks = append(d.Networks, Network{Name: detailText(p[1], 64), Received: rx, Sent: tx})
			}
		case "netaddr":
			if len(p) != 3 {
				continue
			}
			name, address := detailText(p[1], 64), detailText(p[2], 64)
			if name == "" || address == "" {
				continue
			}
			if list := addresses[name]; len(list) < maxNetworkAddresses && !hasAddress(list, address) {
				addresses[name] = append(list, address)
			}
		case "listen":
			// listen\t<proto>\t<address>\t<port>\t<process>\t<pid>
			if len(p) != 6 || len(d.Listens) >= maxListens {
				continue
			}
			port, err := strconv.Atoi(p[3])
			if err != nil || port < 1 || port > 65535 {
				continue
			}
			pid, err := strconv.Atoi(p[5])
			if err != nil || pid < 0 {
				pid = 0
			}
			d.Listens = append(d.Listens, Listen{
				Proto: detailText(p[1], 8), Address: detailText(p[2], 64),
				Port: port, Process: detailText(p[4], 64), PID: pid,
			})
		case "proc":
			// proc\t<pid>\t<cpu>\t<mem%>\t<rss KB>\t<name>
			if len(p) != 6 || len(d.Processes) >= maxProcesses {
				continue
			}
			pid, err := strconv.Atoi(p[1])
			if err != nil || pid < 1 {
				continue
			}
			cpu, ok1 := detailFloat(p[2])
			percent, ok2 := detailFloat(p[3])
			rss, ok3 := detailBytes(p[4])
			name := detailText(p[5], 64)
			if !ok1 || !ok2 || !ok3 || name == "" {
				continue
			}
			d.Processes = append(d.Processes, Process{
				PID: pid, Name: name, CPUPercent: min(cpu, 100),
				MemoryPercent: min(percent, 100), MemoryBytes: rss,
			})
		case "mem":
			// mem\t<total>\t<available>\t<free>\t<buffers>\t<cached>\t<swap total>\t<swap free> in KB
			if len(p) != 8 {
				continue
			}
			total, ok1 := detailBytes(p[1])
			available, ok2 := detailBytes(p[2])
			free, ok3 := detailBytes(p[3])
			buffers, ok4 := detailBytes(p[4])
			cached, ok5 := detailBytes(p[5])
			if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || total == 0 {
				continue
			}
			swapTotal, ok6 := detailBytes(p[6])
			swapFree, ok7 := detailBytes(p[7])
			if !ok6 || !ok7 {
				continue
			}
			d.Memory = MemoryDetail{Total: total, Available: min(available, total), Free: min(free, total),
				Buffers: min(buffers, total), Cached: min(cached, total),
				SwapTotal: swapTotal, SwapFree: min(swapFree, swapTotal)}

		}
	}
	for i := range d.Networks {
		d.Networks[i].Addresses = networkAddresses(addresses[d.Networks[i].Name])
	}
	sort.SliceStable(d.Listens, func(i, j int) bool {
		if d.Listens[i].Port != d.Listens[j].Port {
			return d.Listens[i].Port < d.Listens[j].Port
		}
		return d.Listens[i].Proto < d.Listens[j].Proto
	})
	// The rows are kept in CPU order whichever sort the panel shows: a later
	// re-order is the panel's, over the reading the host has already given.
	sort.SliceStable(d.Processes, func(i, j int) bool {
		if d.Processes[i].CPUPercent != d.Processes[j].CPUPercent {
			return d.Processes[i].CPUPercent > d.Processes[j].CPUPercent
		}
		return d.Processes[i].PID < d.Processes[j].PID
	})
	if d.CPUCount == 0 {
		return Details{}, fmt.Errorf("%w: no system details", ErrMalformed)
	}
	sort.SliceStable(d.Disks, func(i, j int) bool {
		a, b := d.Disks[i], d.Disks[j]
		if (a.Path == "/") != (b.Path == "/") {
			return a.Path == "/"
		}
		if a.Virtual != b.Virtual {
			return !a.Virtual
		}
		return a.Percent > b.Percent
	})
	return d, nil
}

// networkAddresses keeps what one row shows of an interface's addresses: the
// platform's own text, at most maxNetworkAddresses, IPv4 before IPv6, no
// duplicates.
func networkAddresses(values []string) []string {
	out := make([]string, 0, maxNetworkAddresses)
	for _, value := range values {
		address := detailText(value, 64)
		if address == "" || hasAddress(out, address) {
			continue
		}
		out = append(out, address)
		if len(out) == maxNetworkAddresses {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return !strings.Contains(out[i], ":") && strings.Contains(out[j], ":")
	})
	return out
}

func hasAddress(list []string, address string) bool {
	for _, existing := range list {
		if existing == address {
			return true
		}
	}
	return false
}

func detailText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(value, ""))
	r := []rune(strings.TrimSpace(value))
	if len(r) > limit {
		r = r[:limit]
	}
	return string(r)
}
func detailFloat(value string) (float64, bool) {
	v, e := strconv.ParseFloat(value, 64)
	return v, e == nil && !math.IsInf(v, 0) && !math.IsNaN(v) && v >= 0
}
func detailBytes(value string) (uint64, bool) {
	v, e := strconv.ParseUint(value, 10, 64)
	return v << 10, e == nil && v <= math.MaxUint64>>10
}
func networkRates(current *Details, previous Details, seconds float64) {
	if seconds <= 0 {
		return
	}
	for i := range current.Networks {
		for _, before := range previous.Networks {
			after := &current.Networks[i]
			if before.Name == after.Name {
				after.ReceivePerSecond = rate(before.Received, after.Received, seconds)
				after.SendPerSecond = rate(before.Sent, after.Sent, seconds)
				after.HasRate = true
				break
			}
		}
	}
}
