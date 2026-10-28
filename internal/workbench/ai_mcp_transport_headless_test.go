//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
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
				view, err := headless.Open(app.Config{Title: "MCP transports", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				click := func(id ui.ID) {
					t.Helper()
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
				click(hitAISetAgentRun)
				if err := view.Keys("right"); err != nil {
					t.Fatal(err)
				}
				if state.ai.cfg.dialog != aiDialogAgentAllowAll || state.ai.settings.agentMode != gate.ModeAsk || view.Snapshot().Focused != hitAISetConfirmCancel {
					t.Fatal("stdio full access did not wait on the safe confirmation action")
				}
				shot("stdio-confirm")
				if err := view.Keys("enter"); err != nil {
					t.Fatal(err)
				}
				if state.ai.settings.agentMode != gate.ModeAsk || state.ai.cfg.dialog != aiDialogNone {
					t.Fatal("Enter on the default action did not cancel")
				}
				click(hitAISetAgentAllowAll)
				if err := view.Keys("esc"); err != nil {
					t.Fatal(err)
				}
				if state.ai.settings.agentMode != gate.ModeAsk || state.ai.cfg.dialog != aiDialogNone {
					t.Fatal("Escape did not cancel")
				}
				click(hitAISetAgentAllowAll)
				click(hitAISetConfirmAction)
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
