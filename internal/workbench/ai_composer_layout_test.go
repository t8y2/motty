package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/ai/agent"
	"motty/internal/ai/reasoning"
	"motty/internal/ai/store"
	"motty/internal/i18n"
)

func TestAIComposerControlsStayInsideTheMessageBox(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		for _, width := range []float32{300, 380, 400, 410, 420, 600} {
			t.Run(fmt.Sprintf("language-%d/width-%g", language, width), func(t *testing.T) {
				state, _ := panelState(t, nil)
				withModel(t, state)
				db, err := state.aiStore()
				if err != nil {
					t.Fatal(err)
				}
				if err := db.SaveModels("p", []store.Model{{ProviderID: "p", Name: "gpt-x", Enabled: true, Reasoning: &store.ReasoningConfig{
					Levels: []string{reasoning.LevelLow, reasoning.LevelMedium, reasoning.LevelHigh}, Default: reasoning.LevelMedium,
				}}}); err != nil {
					t.Fatal(err)
				}
				state.reloadAI()
				state.messages = i18n.For(language)
				state.ai.open = true
				state.ai.mode = agent.ModeAgent
				state.ai.panelWidth = width
				canvas, _ := paintAt(state, panelWide)
				box := panelHit(t, canvas, hitAIInput).Bounds
				ids := []gfx.ID{hitAIMode, hitAIModelChip, hitAIReasoningChip, hitAIAutoScope, hitAIAttach, hitAIUpload, hitAISend}
				for index, id := range ids {
					hit := panelHit(t, canvas, id)
					bounds := hit.Bounds
					if bounds.Width < 20 || bounds.Height < 20 || bounds.X < box.X || bounds.Y < box.Y || bounds.X+bounds.Width > box.X+box.Width+0.01 || bounds.Y+bounds.Height > box.Y+box.Height+0.01 {
						t.Errorf("control %q at %+v does not fit inside the message box %+v", hit.Label, bounds, box)
					}
					for _, previous := range ids[:index] {
						other := panelHit(t, canvas, previous)
						if bounds.X < other.Bounds.X+other.Bounds.Width-0.01 && bounds.X+bounds.Width > other.Bounds.X+0.01 && bounds.Y < other.Bounds.Y+other.Bounds.Height-0.01 && bounds.Y+bounds.Height > other.Bounds.Y+0.01 {
							t.Errorf("controls %q and %q overlap: %+v, %+v", hit.Label, other.Label, bounds, other.Bounds)
						}
					}
				}
			})
		}
	}
}

func TestAIComposerNarrowPanelTakesPointerClicks(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 2} {
		t.Run(fmt.Sprintf("scale-%g", scale), func(t *testing.T) {
			state, _ := panelState(t, quietModel{"ok"})
			withModel(t, state)
			db, err := state.aiStore()
			if err != nil {
				t.Fatal(err)
			}
			if err := db.SaveModels("p", []store.Model{{ProviderID: "p", Name: "gpt-x", Enabled: true, Reasoning: &store.ReasoningConfig{
				Levels: []string{reasoning.LevelLow, reasoning.LevelMedium, reasoning.LevelHigh}, Default: reasoning.LevelMedium,
			}}}); err != nil {
				t.Fatal(err)
			}
			state.reloadAI()
			state.messages = i18n.For(i18n.Chinese)
			state.ai.open = true
			state.ai.mode = agent.ModeAgent
			state.ai.panelWidth = 300
			view, err := headless.Open(app.Config{
				Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize,
				OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll,
				TextInput: state.AIPanelInput,
			}, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			find := func(id gfx.ID) gfx.Hit {
				t.Helper()
				hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
				if !ok {
					t.Fatalf("control %v is missing", id)
				}
				return hit
			}
			click := func(id gfx.ID) {
				t.Helper()
				hit := find(id)
				if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
			}
			box := find(hitAIInput).Bounds
			for _, id := range []gfx.ID{hitAIMode, hitAIModelChip, hitAIReasoningChip, hitAIAutoScope, hitAIAttach, hitAIUpload, hitAISend} {
				hit := find(id)
				if hit.Bounds.X < box.X || hit.Bounds.X+hit.Bounds.Width > box.X+box.Width+0.01 || hit.Bounds.Y < box.Y || hit.Bounds.Y+hit.Bounds.Height > box.Y+box.Height+0.01 {
					t.Errorf("control %q at %+v is outside the message box %+v", hit.Label, hit.Bounds, box)
				}
			}
			for _, id := range []gfx.ID{hitAIReasoningChip, hitAIAutoScope, hitAIModelChip} {
				click(id)
				if !state.ai.panel.reasoningMenu && !state.ai.panel.autoMenu && !state.ai.panel.modelMenu {
					t.Fatalf("clicking %v did not open a menu", id)
				}
				if err := view.Keys("Escape"); err != nil {
					t.Fatal(err)
				}
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
			}
			click(hitAIInput)
			if err := view.Type("hello there"); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if dir := os.Getenv("MOTTY_AI_COMPOSER_SCREENSHOTS"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("composer-scale-%g.png", scale))); err != nil {
					// The offscreen renderer exists only in some builds; a machine
					// without one skips the dump rather than failing the suite.
					t.Skipf("no offscreen renderer on this machine: %v", err)
				}
			}
			click(hitAISend)
			if got := state.ai.input.String(); got != "" {
				t.Fatalf("Send left the question in the box: %q", got)
			}
		})
	}
}
