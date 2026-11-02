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
	// Windows formatted performance counters expose rates, not byte totals.
	TotalsUnavailable bool
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
func StartDetailsWithInterval(runner DetailRunner, notify func(), seconds int) *DetailsWorker {
	return startDetails(runner, notify, seconds, detailsCommand)
}

func startDetails(runner DetailRunner, notify func(), seconds int, command string) *DetailsWorker {
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
			data, err := runner.RunCommand(request, command, 96<<10)
			finish()
			if ctx.Err() != nil {
				return
			}
			var current Details
			if err == nil {
				current, err = parseDetails(data)
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
`

func parseDetails(data []byte) (Details, error) {
	var d Details
	if len(data) > 96<<10 {
		return d, ErrMalformed
	}
	for _, line := range strings.Split(string(data), "\n") {
		p := strings.SplitN(strings.TrimSuffix(line, "\r"), "\t", 7)
		switch p[0] {
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

		}
	}
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
