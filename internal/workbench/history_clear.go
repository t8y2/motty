package workbench

import (
	"sync"
	"time"

	"motty/internal/cmdhistory"
)

// historyClearJob is shared by windows using one history file. The worker owns
// err until it closes done; the UI reads it only after that synchronization.
type historyClearJob struct {
	done chan struct{}
	err  error
	mu   sync.Mutex
	wake []func()
}

func (state *State) historyPeers() []*State {
	group := state.options.Group
	if group == nil {
		return []*State{state}
	}
	group.mu.Lock()
	members := append([]*State(nil), group.members...)
	group.mu.Unlock()
	peers := []*State{}
	for _, member := range members {
		if !member.closed && member.historyPath() == state.historyPath() {
			peers = append(peers, member)
		}
	}
	return peers
}

func (state *State) historyClearJob() *historyClearJob {
	if group := state.options.Group; group != nil {
		group.mu.Lock()
		job := group.historyClears[state.historyPath()]
		group.mu.Unlock()
		return job
	}
	return state.history.clear
}

func (state *State) historyClearing() bool {
	if state.history.clear != nil && state.history.clear != state.history.lastClear {
		return true
	}
	job := state.historyClearJob()
	if job == nil {
		return false
	}
	select {
	case <-job.done:
		return false
	default:
		return true
	}
}

func (state *State) resetHistorySuggestions() {
	state.forgetCompletionHistory()
	state.closeCompletionList()
	state.history.entries = nil
	state.history.ranked = nil
	for index := range state.sessions {
		if t := state.sessions[index].term; t != nil {
			t.recorder.Reset()
			t.suggestionPending = nil
		}
	}
}

func (state *State) startHistoryClear(store *cmdhistory.Queue) {
	if state.historyClearing() {
		return
	}
	job := &historyClearJob{done: make(chan struct{})}
	if group := state.options.Group; group != nil {
		group.mu.Lock()
		if group.historyClears == nil {
			group.historyClears = make(map[string]*historyClearJob)
		}
		group.historyClears[state.historyPath()] = job
		group.mu.Unlock()
	}
	peers := state.historyPeers()
	queues := make([]*cmdhistory.Queue, 0, len(peers))

	for _, peer := range peers {
		peer.history.clear = job
		peer.resetHistorySuggestions()
		peer.status = peer.messages.History.StatusClearing
		if peer.history.store != nil {
			queues = append(queues, peer.history.store)
		}
		if peer.options.Invalidate != nil {
			job.subscribe(peer.options.Invalidate)
		}
	}
	go func() {
		// Every writer was paused before collecting the queues. A window
		// joining now sees the same job and cannot enqueue a pre-clear line.
		for _, queue := range queues {
			if job.err = queue.Flush(); job.err != nil {
				break
			}
		}
		if job.err == nil {
			job.err = store.Clear()
		}
		job.finish()
	}()
}

// historyChanged keeps the single suggestion cache coherent in every window.
// Late reads are invalidated before applying a newly recorded command.
func (state *State) historyChanged(key, command string, used time.Time) {
	for _, peer := range state.historyPeers() {
		peer.completion.generation++
		if command == "" {
			peer.history.ghost = nil
		} else {
			peer.noteGhostCommand(key, command, used)
		}
		peer.closeCompletionList()
		if invalidate := peer.options.Invalidate; invalidate != nil {
			invalidate()
		}
	}
}

// New windows subscribe too; capturing the initial window set would leave a
// later window paused forever when its last startup wake preceded completion.
func (job *historyClearJob) subscribe(invalidate func()) {
	if invalidate == nil {
		return
	}
	job.mu.Lock()
	select {
	case <-job.done:
		job.mu.Unlock()
		invalidate()
	default:
		job.wake = append(job.wake, invalidate)
		job.mu.Unlock()
	}
}

func (job *historyClearJob) finish() {
	job.mu.Lock()
	close(job.done)
	wake := job.wake
	job.wake = nil
	job.mu.Unlock()
	for _, invalidate := range wake {
		invalidate()
	}
}
