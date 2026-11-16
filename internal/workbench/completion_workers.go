package workbench

import (
	"context"

	"motty/internal/cmdhistory"
)

func (state *State) completionContext() context.Context {
	if state.completion.ctx == nil {
		state.completion.ctx, state.completion.cancel = context.WithCancel(context.Background())
	}
	return state.completion.ctx
}

func (state *State) closeCompletionWorkers() {
	if state.completion.cancel != nil {
		state.completion.cancel()
	}
	state.completion.workers.Wait()
}

func readSuggestionHistory(queue *cmdhistory.Queue, path, key string) []cmdhistory.Entry {
	if queue != nil {
		entries, _ := queue.List(key)
		return entries
	}
	// Opening the existing history for suggestions is background work. The
	// recording path retains ownership of its long-lived write queue.
	store, err := cmdhistory.Open(path)
	if err != nil {
		return nil
	}
	defer store.Close()
	entries, _ := store.List(key)
	return entries
}
