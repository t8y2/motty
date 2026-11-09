//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"motty/internal/i18n"
)

func TestAIRunStatusHeadlessLayoutAndScreenshots(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, scale := range []float64{1, 2} {
			t.Run(fmt.Sprintf("%s-%g", language, scale), func(t *testing.T) {
				state, _ := panelState(t, nil)
				withModel(t, state)
				state.setLanguage(language)
				state.setAppearance(appearanceLight)
				state.ai.open, state.ai.panelWidth = true, 300
				state.ai.items = []aiItem{{kind: aiUser, text: "测试会话"}}
				state.options.Now = func() time.Duration { return 10 * time.Second }
				run := &aiRun{cancel: func() {}, open: -1, stats: aiRunStats{startedAt: 2 * time.Second}}
				state.ai.run = run
				view, err := headless.Open(app.Config{Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize}, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				check := func(name string) {
					t.Helper()
					if err := view.Resize(1400, 900); err != nil {
						t.Fatal(err)
					}
					if err := view.Rest(); err != nil {
						t.Fatal(err)
					}
					snapshot := view.Snapshot()
					var status gfx.Hit
					for _, hit := range snapshot.Hits {
						if hit.ID == hitAIRunStatus {
							status = hit
						}
					}
					if status.Bounds.Height <= 0 {
						t.Fatal("status is missing from the headless frame")
					}
					if name == "completed" && !strings.Contains(status.Label, state.messages.AIPanel.RunCompleted) {
						t.Fatalf("completion frame still shows an earlier status: %q", status.Label)
					}
					if name == "waiting" && strings.Contains(status.Label, "Token") {
						t.Fatalf("waiting frame shows token details: %q", status.Label)
					}
					if name == "approval" && !strings.Contains(status.Label, state.messages.AIPanel.RunApproval) {
						t.Fatalf("approval frame shows the wrong status: %q", status.Label)
					}
					if name == "approval" && strings.Contains(status.Label, "Token") {
						t.Fatalf("approval frame shows token details: %q", status.Label)
					}
					if name == "completed" && !strings.Contains(status.Label, "12,284") {
						t.Fatalf("completion frame lost final token usage: %q", status.Label)
					}
					for _, problem := range headless.CheckLayout(snapshot) {
						if intersectRect(problem.Bounds, status.Bounds).Height > 0 {
							t.Errorf("%s status layout: %+v", name, problem)
						}
					}
					if directory := os.Getenv("MOTTY_AI_RUN_SCREENSHOTS"); directory != "" && scale == 1 {
						if err := os.MkdirAll(directory, 0700); err != nil {
							t.Fatal(err)
						}
						if err := view.Screenshot(filepath.Join(directory, fmt.Sprintf("%s-%s.png", language, name))); err != nil {
							t.Fatal(err)
						}
					}
				}
				check("waiting")
				state.aiEvent(run, agent.Event{Kind: agent.EventText, Text: "你好，我可以帮你分析终端输出。"})
				state.aiEvent(run, agent.Event{Kind: agent.EventDone, Usage: llm.Usage{InputTokens: 12000, OutputTokens: 284, CacheReadTokens: 9800}})
				state.aiFinished(run, nil, nil)
				check("completed")
				pending := aiRunStats{startedAt: 2 * time.Second, phase: aiRunNeedsApproval}
				state.ai.lastRun, state.ai.pendingRun = &pending, &pending
				state.ai.approvals = []*aiApproval{{escalation: true}}
				state.options.Now = func() time.Duration { return 20 * time.Second }
				check("approval")
			})
		}
	}
}

func TestAIRunUsageWrapReservesSpaceForFontFallback(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		t.Run(language.String(), func(t *testing.T) {
			state, _ := panelState(t, nil)
			withModel(t, state)
			state.setLanguage(language)
			state.ai.open, state.ai.panelWidth = true, 300
			state.ai.items = []aiItem{{kind: aiUser, text: "question"}, {kind: aiAssistant, text: "answer"}}
			state.ai.lastRun = &aiRunStats{phase: aiRunCompleted, duration: 8 * time.Second,
				usage: llm.Usage{InputTokens: 12000, OutputTokens: 284, CacheReadTokens: 9800}}
			state.ai.totalUsage = state.ai.lastRun.usage
			canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
			width := state.ai.panel.list.Width - 2*aiPad
			if len(state.ai.panel.usageLines) == 0 {
				t.Fatal("finished request has no usage")
			}
			for _, line := range state.ai.panel.usageLines {
				if measured := canvas.TextWidthWeight(line, aiSmallSize, gfx.WeightRegular); measured > width-16 {
					t.Fatalf("usage line leaves insufficient cross-platform room: %q measured %.1f, available %.1f", line, measured, width)
				}
			}
		})
	}
}
