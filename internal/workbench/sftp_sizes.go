package workbench

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

const (
	sftpSizeBatchCount  = 64
	sftpSizeBatchBytes  = 32 << 10
	sftpSizeCommandMax  = 4 << 20
	sftpSizeWalkThreads = 4
)

// Each positional root result carries its find status and hexadecimal wc
// totals. Shell quoting isolates paths from source. od makes packet framing
// independent of labels; Go sums the batch totals with int64 overflow checks.
// find and wc remain server-side, avoiding a round trip for every directory.
const sftpSizeScript = `index=0
for root in "$@"; do
	report=$(find "$root" -type f -exec sh -c 'wc -c "$@"' marker {} + 2>/dev/null)
	status=$?
	totals=$(printf '%s\n' "$report" | awk 'NF == 2 && $2 == "total" {print $1; seen=1; next} {single=$1} END {if (!seen && NR == 1 && single ~ /^[0-9]+$/) print single}')
	hex=$(printf '%s' "$totals" | od -An -v -tx1 | tr -d ' \n')
	if [ -z "$hex" ]; then hex=-; fi
	if [ -z "$totals" ] && [ -z "$report" ] && [ "$status" -eq 0 ]; then hex=30; fi
	printf '\036%s %s %s\n' "$index" "$status" "$hex"
	index=$((index + 1))
done`

func sftpSizeCommand(roots []string) string {
	args := make([]string, len(roots))
	for index, root := range roots {
		args[index] = shellQuote(root)
	}
	return "sh -c " + shellQuote(sftpSizeScript) + " x " + strings.Join(args, " ")
}

func sftpSizeBatches(roots []string) [][]string {
	var batches [][]string
	for start := 0; start < len(roots); {
		end, bytes := start+1, len(shellQuote(roots[start]))
		for end < len(roots) && end-start < sftpSizeBatchCount && bytes+len(shellQuote(roots[end])) <= sftpSizeBatchBytes {
			bytes += len(shellQuote(roots[end]))
			end++
		}
		batches = append(batches, roots[start:end])
		start = end
	}
	return batches
}

// sumWCTotal adds the totals emitted by wc, already one number per invocation.
// All names have been discarded on the server; no filename path is interpreted.
func sumWCTotal(report []byte) (int64, error) {
	var sum int64
	for _, text := range strings.Split(string(report), "\n") {
		value := strings.TrimSpace(text)
		if value == "" {
			continue
		}
		size, err := strconv.ParseInt(value, 10, 64)
		if err != nil || size < 0 || sum > math.MaxInt64-size {
			return 0, errors.New("wc returned an invalid byte count")
		}
		sum += size
	}
	return sum, nil
}

func parseSFTPSizeResults(stdout string, count int) ([]sftpSizeResult, error) {
	records := strings.Split(stdout, "\x1e")
	if len(records) != count+1 || records[0] != "" {
		return nil, errors.New("find returned incomplete output")
	}
	results := make([]sftpSizeResult, count)
	for index, record := range records[1:] {
		line, rest, found := strings.Cut(record, "\n")
		if !found || rest != "" {
			return nil, errors.New("find returned malformed framing")
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != strconv.Itoa(index) {
			return nil, errors.New("find returned malformed status")
		}
		status, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, err
		}
		if status < 0 || status > 255 {
			return nil, errors.New("find returned invalid exit status")
		}
		var payload []byte
		if fields[2] == "-" {
			results[index].size = -1
			continue
		}
		if len(fields[2])%2 != 0 {
			results[index].size = -1
			continue
		}
		payload, err = hex.DecodeString(fields[2])
		if err != nil {
			results[index].size = -1
			continue
		}
		total, err := sumWCTotal(payload)
		if err != nil {
			results[index].size = -1
			continue
		}
		results[index].size = total
		results[index].text = formatBytes(total)
		results[index].incomplete = status != 0
	}
	return results, nil
}

type sftpSizeResult struct {
	size       int64
	text       string
	incomplete bool
}

type sftpSizeTask struct {
	ctx        context.Context
	cancel     context.CancelFunc
	post       func(func(*State)) bool
	connection Connection
	walker     *sftpSizeWalker
	names      []string
	roots      []string
	apply      func(map[string]sftpSizeResult)
	partial    string
}

func (task *sftpSizeTask) publish(index int, result sftpSizeResult) bool {
	if result.text == "" {
		result.text = formatBytes(result.size)
	}
	if result.incomplete {
		result.text = fmt.Sprintf(task.partial, result.text)
	}
	return task.post(func(state *State) { task.apply(map[string]sftpSizeResult{task.names[index]: result}) })
}

func (task *sftpSizeTask) run() {
	start := 0
	for task.connection != nil && start < len(task.roots) {
		batch := sftpSizeBatches(task.roots[start:])[0]
		output, err := task.connection.Exec(task.ctx, sftpSizeCommand(batch), sftpSizeCommandMax)
		if task.ctx.Err() != nil {
			return
		}
		results, parseErr := parseSFTPSizeResults(string(output.Stdout), len(batch))
		if err != nil || output.ExitCode != 0 || output.Truncated || parseErr != nil {
			break
		}
		for index, result := range results {
			if result.size < 0 {
				if !task.walkOne(start + index) {
					return
				}
			} else if !task.publish(start+index, result) {
				return
			}
		}
		start += len(batch)
	}
	for index := start; index < len(task.roots); index++ {
		if !task.walkOne(index) {
			return
		}
	}
}

func (task *sftpSizeTask) walkOne(index int) bool {
	if task.ctx.Err() != nil || task.walker == nil {
		return false
	}
	return task.publish(index, task.walker.walk(task.ctx, task.roots[index]))
}

type sftpSizeWalker struct {
	local      bool
	open       func() (sftpclient.Remote, error)
	join       func(dir, name string) string
	incomplete string
}

func (walker *sftpSizeWalker) walk(ctx context.Context, root string) (result sftpSizeResult) {
	var reader sftpclient.DirReader = sftpclient.LocalReader{}
	parallel := sftpSizeWalkThreads
	if !walker.local {
		remote, err := walker.open()
		if err != nil {
			return sftpSizeResult{text: err.Error()}
		}
		reader = remote
		if sftpclient.IsSCP(remote) {
			parallel = 1
		}
	}
	summary := sftpclient.Walk(ctx, reader, root, sftpclient.WalkOptions{
		Join: walker.join, MaxEntries: math.MaxInt, MaxDepth: math.MaxInt, Parallel: parallel,
	}, func(found []sftpclient.Found) {
		for _, item := range found {
			if item.Entry.Mode.IsRegular() {
				result.size += item.Entry.Size
			}
		}
	})
	result.text = formatBytes(result.size)
	switch {
	case summary.RootErr != nil:
		result.text = summary.RootErr.Error()
	case summary.Failed > 0:
		result.text = fmt.Sprintf(walker.incomplete, result.text, summary.Failed)
	}
	return result
}

func sftpSizeExecutor(state *State, view *sftpView, local bool) Connection {
	if local || view.sudo {
		return nil
	}
	session, ok := state.sftpSession(view)
	if !ok || session.host.System == sshconn.SystemUnknown || session.host.System == sshconn.SystemWindows || session.host.InteractiveOnly {
		return nil
	}
	return session.conn
}

type sftpSizeChannel struct {
	mu     sync.Mutex
	open   func() (sftpclient.Remote, error)
	opened bool
	remote sftpclient.Remote
	err    error
	closed bool
}

func (channel *sftpSizeChannel) reader() (sftpclient.Remote, error) {
	channel.mu.Lock()
	defer channel.mu.Unlock()
	if channel.closed {
		return nil, context.Canceled
	}
	if !channel.opened {
		channel.opened = true
		channel.remote, channel.err = channel.open()
	}
	if channel.closed && channel.remote != nil {
		_ = channel.remote.Close()
		channel.remote = nil
	}
	if channel.closed {
		return nil, context.Canceled
	}
	return channel.remote, channel.err
}

func (channel *sftpSizeChannel) close() {
	channel.mu.Lock()
	channel.closed = true
	remote := channel.remote
	channel.remote = nil
	channel.mu.Unlock()
	if remote != nil {
		_ = remote.Close()
	}
}

func (state *State) calculateSFTPSizes(view *sftpView, local bool, roots map[string]string, apply func(map[string]sftpSizeResult)) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	invalidate := state.options.Invalidate
	channel := &sftpSizeChannel{open: view.openFiles}
	go func() {
		defer channel.close()
		if !local {
			defer context.AfterFunc(ctx, channel.close)()
		}
		join := func(dir, name string) string { return filepath.Join(dir, name) }
		m := state.messages.SFTPWindow
		if !local {
			join = func(dir, name string) string { return path.Join(dir, name) }
		}
		task := &sftpSizeTask{
			ctx: ctx, cancel: cancel, connection: sftpSizeExecutor(state, view, local),
			partial: m.FolderSizePartialFmt, apply: apply,
			walker: &sftpSizeWalker{local: local, open: channel.reader, join: join, incomplete: m.FolderSizeIncompleteFmt},
		}
		for name, root := range roots {
			task.names, task.roots = append(task.names, name), append(task.roots, root)
		}
		remaining := len(roots)
		task.post = func(fn func(*State)) bool {
			if !state.aiPostWithInvalidator(func(state *State) {
				if ctx.Err() == nil {
					fn(state)
					view.invalidateDetached()
					remaining--
					if remaining == 0 {
						cancel()
					}
				}
			}, invalidate) {
				cancel()
				return false
			}
			return true
		}
		task.run()
	}()
	return cancel
}
