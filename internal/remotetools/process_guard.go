package remotetools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrProcessChanged      = errors.New("remotetools: process instance changed; refresh and reselect")
	ErrProcessUnverifiable = errors.New("remotetools: process identity unavailable; refresh or use a supported Linux procfs host")
)

// Linux exposes boot identity plus process start ticks. A command name can
// contain parentheses, shell syntax or newlines: never evaluate it. A newline
// or an unreadable/malformed stat is deliberately not actionable.
const readProcessTicks = `line=
IFS= read -r line < "$path" 2>/dev/null || return 1
case "$line" in "$pid ("*") "*) ;; *) return 1 ;; esac
rest=${line##*) }
set -f
set -- $rest
set +f
[ "$#" -ge 20 ] || return 1
case "$1" in R|S|D|Z|T|t|W|X|x|K|P|I) ;; *) return 1 ;; esac
shift
field=4
# stat fields 4..8 and 16..21 are signed; flags, counters and start ticks are not.
while [ "$field" -le 22 ]; do
  number=$1
  case "$field" in 4|5|6|7|8|16|17|18|19|20|21) number=${number#-} ;; esac
  case "$number" in ''|*[!0-9]*) return 1 ;; esac
  [ "${#number}" -le 20 ] || return 1
  if [ "$field" -eq 22 ]; then ticks=$number; fi
  shift
  field=$((field+1))
done
`

const processIdentityCommand = `# motty-process-identities
LC_ALL=C; export LC_ALL
boot=
IFS= read -r boot < /proc/sys/kernel/random/boot_id 2>/dev/null || exit 1
printf 'boot %s\n' "$boot"
read_ticks() {
` + readProcessTicks + `}
count=0
for path in /proc/[0-9]*/stat; do
  [ "$count" -lt 5000 ] || break
  pid=${path#/proc/}; pid=${pid%/stat}
  case "$pid" in ''|*[!0-9]*) continue ;; esac
  count=$((count+1))
  if read_ticks; then printf '%s %s\n' "$pid" "$ticks"; fi
done
`

// ReadSafeProcessSnapshot brackets ps with identity probes: attaching only a
// later identity could bind a replacement PID to the previous process's text.
// Unsupported platforms keep all the listing/search functionality read-only.
func ReadSafeProcessSnapshot(ctx context.Context, runner Runner, key SortKey) (ProcessSnapshot, error) {
	if system := runnerSystem(runner); system == "windows" || system == "darwin" {
		return ReadProcessSnapshot(ctx, runner, key)
	}
	before, err := readProcessIdentities(ctx, runner)
	if canceled := processContextError(ctx, err); canceled != nil {
		return ProcessSnapshot{}, canceled
	}
	snapshot, err := ReadProcessSnapshot(ctx, runner, key)
	if err != nil {
		return ProcessSnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return ProcessSnapshot{}, err
	}
	if len(before) == 0 {
		return snapshot, nil
	}
	after, err := readProcessIdentities(ctx, runner)
	if canceled := processContextError(ctx, err); canceled != nil {
		return ProcessSnapshot{}, canceled
	}
	for i := range snapshot.Processes {
		process := &snapshot.Processes[i]
		if identity := before[process.PID]; identity != "" && identity == after[process.PID] {
			process.StartIdentity = identity
		}
	}
	return snapshot, nil
}

func processContextError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func readProcessIdentities(ctx context.Context, runner Runner) (map[int]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output, err := runner.RunCommand(ctx, processIdentityCommand, maxOutput)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 1 || !strings.HasPrefix(lines[0], "boot ") {
		return nil, ErrProcessUnverifiable
	}
	boot := strings.TrimPrefix(lines[0], "boot ")
	if !validBootIdentity(boot) || len(lines) > maxSnapshotProcesses+1 {
		return nil, ErrProcessUnverifiable
	}
	identities := make(map[int]string, len(lines)-1)
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) != 2 || !decimalIdentity(fields[1]) {
			return nil, ErrProcessUnverifiable
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 || identities[pid] != "" {
			return nil, ErrProcessUnverifiable
		}
		identities[pid] = "linux:" + boot + ":" + fields[1]
	}
	return identities, nil
}

func validBootIdentity(boot string) bool {
	if len(boot) != 36 {
		return false
	}
	for i, ch := range boot {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if ch != '-' {
				return false
			}
		} else if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func decimalIdentity(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// ProcessKey distinguishes process instances even when the OS reuses a PID.
func ProcessKey(process Process) string {
	key := strconv.Itoa(process.PID)
	if process.StartIdentity != "" {
		key += ":" + process.StartIdentity
	}
	return key
}

// SendSignalToProcess checks the captured instance immediately before signaling.
// This is not atomic: portable shell kill still has a check-to-signal race. A
// pidfd/native host helper would be required to eliminate that residual race.
func SendSignalToProcess(ctx context.Context, runner Runner, process Process, signal Signal) error {
	if signal != Terminate && signal != Kill && signal != Hangup {
		return ErrProcessUnverifiable
	}
	return guardedProcessAction(ctx, runner, process, "kill -"+signal.name()+" "+strconv.Itoa(process.PID)+" 2>&1")
}

// ReniceProcess provides the same instance check as SendSignalToProcess.
func ReniceProcess(ctx context.Context, runner Runner, process Process, nice int) error {
	if nice < -20 || nice > 19 {
		return ErrProcessUnverifiable
	}
	return guardedProcessAction(ctx, runner, process, "renice "+strconv.Itoa(nice)+" -p "+strconv.Itoa(process.PID)+" 2>&1")
}

func guardedProcessAction(ctx context.Context, runner Runner, process Process, action string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parts := strings.Split(process.StartIdentity, ":")
	if process.PID <= 1 || len(parts) != 3 || parts[0] != "linux" || !validBootIdentity(parts[1]) || !decimalIdentity(parts[2]) {
		return ErrProcessUnverifiable
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	marker := "motty-guard-" + hex.EncodeToString(random[:]) + ":"
	command := `# motty-process-guard
LC_ALL=C; export LC_ALL; set -f
refuse() { printf '%s%s\n' '` + marker + `' "$1"; exit 1; }
boot=
IFS= read -r boot < /proc/sys/kernel/random/boot_id 2>/dev/null || refuse unverifiable
[ "$boot" = '` + parts[1] + `' ] || refuse changed
pid=` + strconv.Itoa(process.PID) + `
path=/proc/` + strconv.Itoa(process.PID) + `/stat
read_ticks() {
` + readProcessTicks + `}
read_ticks || refuse unverifiable
[ "$ticks" = '` + parts[2] + `' ] || refuse changed
` + action + `
result=$?
[ "$result" -eq 0 ] || exit "$result"
printf '%sok\n' '` + marker + `'
`
	output, err := runner.RunCommand(ctx, command, maxOutput)
	if canceled := processContextError(ctx, err); canceled != nil {
		return canceled
	}
	text := strings.TrimSpace(string(output))
	if text == marker+"changed" {
		return ErrProcessChanged
	}
	if text == marker+"unverifiable" || text == "" || err == nil && !strings.HasSuffix(text, marker+"ok") {
		return ErrProcessUnverifiable
	}
	if err != nil {
		if text != "" {
			line, _, _ := strings.Cut(text, "\n")
			return &Failure{Message: line, Err: err}
		}
		return err
	}
	if text != marker+"ok" && !strings.HasSuffix(text, "\n"+marker+"ok") {
		return fmt.Errorf("%w: invalid action acknowledgement", ErrProcessUnverifiable)
	}
	return nil
}
