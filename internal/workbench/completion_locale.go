package workbench

import (
	"motty/internal/completion"
	"motty/internal/i18n"
)

func (state *State) adoptCompletionDescriptions() {
	current := &state.completion
	if current.descriptionsReady != nil {
		select {
		case table := <-current.descriptionsReady:
			current.descriptionsReady = nil
			current.descriptions = table
			state.refreshOpenCompletion()
		default:
		}
	}
	if state.language != i18n.Chinese || current.catalog == nil || current.descriptionsStarted || current.descriptions != nil {
		return
	}
	current.descriptionsStarted = true
	ready := make(chan *completion.Descriptions, 1)
	current.descriptionsReady = ready
	invalidate := state.options.Invalidate
	current.workers.Add(1)
	go func() {
		defer current.workers.Done()
		table, _ := completion.BuiltinChineseDescriptions()
		ready <- table
		if invalidate != nil {
			invalidate()
		}
	}()
}

func (state *State) localizeCompletionCandidate(candidate completion.Candidate) completion.Candidate {
	if state.language == i18n.Chinese && candidate.FromCatalog {
		candidate.Desc = state.completion.descriptions.Lookup(candidate.Desc)
	}
	return candidate
}
