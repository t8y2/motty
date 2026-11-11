//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/ai/gate"
	"motty/internal/i18n"
)

func TestMCPTransportSectionsAndConsentThroughNativeInput(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, scale := range []float64{1, 1.5, 2} {
			t.Run(fmt.Sprintf("%s-%.1f", language, scale), func(t *testing.T) {
				world := newSettingsWorld(t, newLister())
				state := world.state
				state.setLanguage(language)
				state.setAppearance(appearanceLight)
				state.selectSettingsCategory(settingsMCP)
				state.Click(hitAISetMCPEnabled)
				state.ai.mcpAddr, state.ai.mcpToken = "127.0.0.1:4455", "transport-preview-token"
				state.Click(aiMCPClientID(aiMCPCodex))
				state.Click(aiAgentClientID(aiMCPCodex))
				view, err := headless.Open(state.WindowConfig(app.Config{Title: "MCP transports", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize}), nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				click := func(id gfx.ID) {
					t.Helper()
					view.Rest()
					for _, hit := range view.Snapshot().Hits {
						if hit.ID == id && !hit.Block && !hit.IsLabel {
							if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
								t.Fatal(err)
							}
							return
						}
					}
					t.Fatal("missing MCP control", tree.Text(id))
				}
				// The access levels are page Choices: click the segment by its
				// words, the last "Allow all" being the stdio one.
				clickLabel := func(label string, last bool) {
					t.Helper()
					view.Rest()
					hit, ok := settingsChoiceInHits(view.Snapshot().Hits, label, last)
					if !ok {
						t.Fatalf("missing access choice %q", label)
					}
					if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
				}
				shot := func(name string) {
					t.Helper()
					view.Rest()
					if directory := os.Getenv("MOTTY_MCP_LAYOUT_SCREENSHOTS"); directory != "" && scale == 1 {
						if err := os.MkdirAll(directory, 0700); err != nil {
							t.Fatal(err)
						}
						if err := view.Screenshot(filepath.Join(directory, language.String()+"-"+name+".png")); err != nil {
							t.Fatal(err)
						}
					}
				}
				shot("http")
				state.settings.jump = state.messages.AISettings.AgentsTitle
				if err := view.Resize(1280, 901); err != nil {
					t.Fatal(err)
				}
				shot("stdio")
				agentRun := state.messages.AISettings.AgentModeRun
				allowAll := state.messages.AISettings.MCPModeAllowAll
				clickButton := func(label string) {
					t.Helper()
					view.Rest()
					hit, ok := snapshotButton(view.Snapshot(), label)
					if !ok {
						t.Fatalf("missing button %q", label)
					}
					if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
				}
				clickLabel(agentRun, false)
				if err := view.Keys("right"); err != nil {
					t.Fatal(err)
				}
				cancel, ok := snapshotButton(view.Snapshot(), state.messages.Cancel)
				if state.ai.cfg.dialog != aiDialogAgentAllowAll || state.ai.settings.agentMode != gate.ModeAsk || !ok || view.Snapshot().Focused != cancel.ID {
					t.Fatal("stdio full access did not wait on the safe confirmation action")
				}
				shot("stdio-confirm")
				if err := view.Keys("enter"); err != nil {
					t.Fatal(err)
				}
				if state.ai.settings.agentMode != gate.ModeAsk || state.ai.cfg.dialog != aiDialogNone {
					t.Fatal("Enter on the default action did not cancel")
				}
				clickLabel(allowAll, true)
				if err := view.Keys("esc"); err != nil {
					t.Fatal(err)
				}
				if state.ai.settings.agentMode != gate.ModeAsk || state.ai.cfg.dialog != aiDialogNone {
					t.Fatal("Escape did not cancel")
				}
				clickLabel(allowAll, true)
				clickButton(state.messages.AISettings.MCPAllowAllConfirm)
				if state.ai.settings.agentMode != gate.ModeAllowAll || state.ai.settings.mcpMode != gate.ModeObserve {
					t.Fatal("confirmation changed the wrong MCP service")
				}
				shot("stdio-enabled")
				click(hitAISetAgentSelect)
				if err := view.Keys("home"); err != nil {
					t.Fatal(err)
				}
				if err := view.Keys("enter"); err != nil {
					t.Fatal(err)
				}
				if state.ai.cfg.agentClient != aiMCPClaude || state.ai.cfg.mcpClient != aiMCPCodex {
					t.Fatal("stdio selector changed the HTTP client")
				}
			})
		}
	}
}

func TestMCPConsentSaveErrorsAreForgottenAfterNativeEscape(t *testing.T) {
	for _, consent := range []struct {
		name     string
		httpMode bool // the first "Allow all", the HTTP level, rather than stdio's
	}{
		{"http", true}, {"stdio", false},
	} {
		t.Run(consent.name, func(t *testing.T) {
			world := newSettingsWorld(t, newLister())
			state := world.state
			state.selectSettingsCategory(settingsMCP)
			allowAll := state.messages.AISettings.MCPModeAllowAll
			allow := func() {
				if consent.httpMode {
					clickSettingsChoiceFirst(t, state, allowAll)
					return
				}
				clickSettingsChoice(t, state, allowAll)
			}
			allow()
			view, err := headless.Open(state.WindowConfig(app.Config{Title: "MCP save failure", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnResize: state.Resize}), nil, headless.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err := world.db(t).Close(); err != nil {
				t.Fatal(err)
			}
			hit, found := snapshotButton(view.Snapshot(), state.messages.AISettings.MCPAllowAllConfirm)
			if found {
				if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
			}
			if !found || state.ai.cfg.notice == "" {
				t.Fatal("save failure was not shown")
			}
			if err := view.Keys("esc"); err != nil {
				t.Fatal(err)
			}
			if state.ai.cfg.dialog != aiDialogNone {
				t.Fatal("Escape did not cancel the failed save")
			}
			allow()
			if state.ai.cfg.notice != "" || state.ai.cfg.dialog == aiDialogNone {
				t.Fatal("a reopened confirmation retained a canceled save error")
			}
		})
	}
}
