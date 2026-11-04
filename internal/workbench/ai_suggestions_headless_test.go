//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	"motty/internal/i18n"
)

func TestSuggestionPanelHeadlessAtNarrowWidths(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, scale := range []float64{1, 2} {
			t.Run(fmt.Sprintf("%s-%g", language, scale), func(t *testing.T) {
				state, _ := panelState(t, nil)
				withModel(t, state)
				state.setLanguage(language)
				state.ai.open, state.ai.panelWidth = true, 300
				view, err := headless.Open(app.Config{Width: 1200, Height: 800, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize}, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				panel := state.ai.panel.rect
				for _, problem := range headless.CheckLayout(view.Snapshot()) {
					if intersectRect(problem.Bounds, panel).Height > 0 && (problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill) {
						t.Errorf("suggestion panel layout: %+v", problem)
					}
				}
				if directory := os.Getenv("MOTTY_AI_SUGGESTION_SCREENSHOTS"); directory != "" && scale == 1 {
					if err := os.MkdirAll(directory, 0700); err != nil {
						t.Fatal(err)
					}
					if err := view.Screenshot(filepath.Join(directory, fmt.Sprintf("%s-panel.png", language))); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestSuggestionSettingsHeadlessShowsPrivacyAndManager(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.setLanguage(i18n.English)
	state.Click(hitAISetSuggestAI)
	view, err := headless.Open(app.Config{Width: 1100, Height: 800, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize, OnScroll: state.Scroll}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	state.scrollSettings(ui.ScrollEvent{DeltaY: 4000, Precise: true})
	if err := view.Resize(1100, 800); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	var hasSource, hasManager bool
	var sourceBounds ui.Rect
	for _, hit := range view.Snapshot().Hits {
		hasSource = hasSource || hit.ID == hitAISetSuggestAI
		hasManager = hasManager || hit.ID == hitAISetSuggestEdit.At(0)
		if hit.ID == hitAISetSuggestAI {
			sourceBounds = hit.Bounds
		}
	}
	if !hasSource || !hasManager {
		t.Fatalf("settings controls missing source=%v manager=%v", hasSource, hasManager)
	}
	for _, problem := range headless.CheckLayout(view.Snapshot()) {
		if intersectRect(problem.Bounds, sourceBounds).Height > 0 && (problem.Kind == headless.LayoutClipped || problem.Kind == headless.LayoutSpill) {
			t.Errorf("source selector text is clipped: %+v", problem)
		}
	}
	var disclosure bool
	for _, label := range view.Snapshot().Texts {
		disclosure = disclosure || strings.Contains(label.Value, "4 KiB")
	}
	if !disclosure {
		t.Fatal("AI mode hides the automatic terminal-data transfer")
	}
	if directory := os.Getenv("MOTTY_AI_SUGGESTION_SCREENSHOTS"); directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(directory, "settings.png")); err != nil {
			t.Fatal(err)
		}
	}
	state.Click(hitAISetSuggestEdit.At(0))
	if err := view.Resize(1100, 800); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	var prompt, command bool
	for _, hit := range view.Snapshot().Hits {
		prompt = prompt || hit.ID == hitAISetSuggestText
		command = command || hit.ID == hitAISetSuggestCommand
	}
	if !prompt || !command {
		t.Fatal("editing a connected suggestion does not expose both fields")
	}
	if directory := os.Getenv("MOTTY_AI_SUGGESTION_SCREENSHOTS"); directory != "" {
		if err := view.Screenshot(filepath.Join(directory, "edit-command.png")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBoundSuggestionHeadlessShowsCommandBeforeSend(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.setLanguage(i18n.Chinese)
	entry := state.aiSuggestionPool(aiSuggestionConnected)[0]
	entry.Command = "echo safe-preview"
	if err := state.aiSaveSuggestion(entry); err != nil {
		t.Fatal(err)
	}
	state.ai.mode, state.ai.open, state.ai.panelWidth = agent.ModeAgent, true, 300
	paintAt(state, panelWide)
	state.Click(hitAIExamples.At(0))
	view, err := headless.Open(app.Config{Width: 1200, Height: 800, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	var hasCommand bool
	for _, item := range view.Snapshot().Texts {
		hasCommand = hasCommand || strings.Contains(item.Value, "echo safe-preview")
	}
	if !hasCommand || state.ai.run != nil {
		t.Fatal("bound command is not visible before Send, or it ran on click")
	}
	if directory := os.Getenv("MOTTY_AI_SUGGESTION_SCREENSHOTS"); directory != "" {
		if err := view.Screenshot(filepath.Join(directory, "bound-before-send.png")); err != nil {
			t.Fatal(err)
		}
	}
}
