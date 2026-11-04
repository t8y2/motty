package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"motty/internal/i18n"
)

type aiSuggestionContext string

const (
	aiSuggestionConnected aiSuggestionContext = "connected"
	aiSuggestionIdle      aiSuggestionContext = "idle"
	aiSuggestionStoreKey                      = "ai.suggestions.library"
)

type aiSuggestion struct {
	ID       string              `json:"id"`
	Locale   string              `json:"locale"`
	Context  aiSuggestionContext `json:"context"`
	Text     string              `json:"text"`
	Command  string              `json:"command,omitempty"`
	Disabled bool                `json:"disabled,omitempty"`
	Order    int                 `json:"order"`
}

type aiSuggestionLibrary struct {
	Overrides map[string]aiSuggestion `json:"overrides,omitempty"`
	Custom    []aiSuggestion          `json:"custom,omitempty"`
}

type aiSuggestionsState struct {
	library      aiSuggestionLibrary
	page         int
	visible      []aiSuggestion
	visibleKey   string
	armed        aiSuggestion
	armedKey     string
	generated    []aiSuggestion
	generationID uint64
	contextKey   string
	cancel       context.CancelFunc
	generating   bool
	settled      bool
}

type aiSuggestionSource string

func (suggestions *aiSuggestionsState) disarm() {
	suggestions.armed = aiSuggestion{}
	suggestions.armedKey = ""
}

const (
	aiSuggestionLibrarySource aiSuggestionSource = "library"
	aiSuggestionAI            aiSuggestionSource = "ai"
)

func (state *State) aiSuggestionLocale() string {
	if state.language == i18n.Chinese {
		return "zh"
	}
	return "en"
}

func (state *State) aiSuggestionDefaults(context aiSuggestionContext) []aiSuggestion {
	m := state.messages.AIPanel
	labels := []string{m.Example1, m.Example2, m.Example3, m.Example4, m.Example5, m.Example6}
	if context == aiSuggestionIdle {
		labels = []string{m.IdleExample1, m.IdleExample2, m.IdleExample3, m.IdleExample4, m.IdleExample5, m.IdleExample6}
	}
	entries := make([]aiSuggestion, 0, len(labels))
	for index, label := range labels {
		entries = append(entries, aiSuggestion{ID: string(context) + "-" + string(rune('1'+index)), Locale: state.aiSuggestionLocale(), Context: context, Text: label, Order: index + 1})
	}
	return entries
}

func (state *State) aiSuggestionPool(context aiSuggestionContext) []aiSuggestion {
	locale := state.aiSuggestionLocale()
	library := &state.ai.suggestions.library
	entries := state.aiSuggestionDefaults(context)
	for index := range entries {
		if override, ok := library.Overrides[locale+":"+entries[index].ID]; ok {
			entries[index] = override
		}
	}
	for _, entry := range library.Custom {
		if entry.Locale == locale && entry.Context == context {
			entries = append(entries, entry)
		}
	}
	slices.SortStableFunc(entries, func(left, right aiSuggestion) int { return left.Order - right.Order })
	return entries
}

func (state *State) aiSuggestionVisible(context aiSuggestionContext, page int) []aiSuggestion {
	pool := state.aiSuggestionPool(context)
	enabled := pool[:0]
	for _, entry := range pool {
		if !entry.Disabled && strings.TrimSpace(entry.Text) != "" {
			enabled = append(enabled, entry)
		}
	}
	if len(enabled) == 0 {
		return nil
	}
	visible := make([]aiSuggestion, 0, min(aiExamples, len(enabled)))
	for index := range min(aiExamples, len(enabled)) {
		visible = append(visible, enabled[(page*aiExamples+index)%len(enabled)])
	}
	return visible
}

func (state *State) aiSaveSuggestion(entry aiSuggestion) error {
	entry.Text, entry.Command = strings.TrimSpace(entry.Text), strings.TrimSpace(entry.Command)
	if entry.Text == "" || utf8.RuneCountInString(entry.Text) > 200 || len(entry.Command) > 4096 ||
		strings.IndexFunc(entry.Text, unicode.IsControl) >= 0 || strings.IndexFunc(entry.Command, unicode.IsControl) >= 0 ||
		(entry.Context != aiSuggestionConnected && entry.Context != aiSuggestionIdle) ||
		(entry.Locale != "zh" && entry.Locale != "en") || entry.ID == "" {
		return errors.New("invalid suggestion")
	}
	if entry.Context == aiSuggestionIdle && entry.Command != "" {
		return errors.New("commands need a connected-terminal suggestion")
	}
	library := state.ai.suggestions.library
	library.Overrides = make(map[string]aiSuggestion, len(library.Overrides)+1)
	for key, current := range state.ai.suggestions.library.Overrides {
		library.Overrides[key] = current
	}
	if strings.HasPrefix(entry.ID, string(entry.Context)+"-") {
		library.Overrides[entry.Locale+":"+entry.ID] = entry
	} else {
		library.Custom = slices.Clone(library.Custom)
		found := false
		for index := range library.Custom {
			if library.Custom[index].ID == entry.ID && library.Custom[index].Locale == entry.Locale {
				library.Custom[index], found = entry, true
				break
			}
		}
		if !found {
			library.Custom = append(library.Custom, entry)
		}
	}
	return state.aiStoreSuggestionLibrary(library)
}

func (state *State) aiResetBuiltinSuggestion(id, locale string) error {
	library := state.ai.suggestions.library
	library.Overrides = make(map[string]aiSuggestion, len(library.Overrides))
	for key, entry := range state.ai.suggestions.library.Overrides {
		if key != locale+":"+id {
			library.Overrides[key] = entry
		}
	}
	return state.aiStoreSuggestionLibrary(library)
}

func (state *State) aiStoreSuggestionLibrary(library aiSuggestionLibrary) error {
	db, err := state.aiStore()
	if err != nil {
		return err
	}
	data, err := json.Marshal(library)
	if err != nil {
		return err
	}
	if err := db.SetSetting(aiSuggestionStoreKey, string(data)); err != nil {
		return err
	}
	state.ai.suggestions.library = library
	state.ai.suggestions.disarm()
	state.ai.suggestions.visibleKey = ""
	state.aiWrote()
	return nil
}
