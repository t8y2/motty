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

func TestAIUsageChipAndCardFitTheColumn(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		t.Run(language.String(), func(t *testing.T) {
			state, _ := panelState(t, nil)
			withModel(t, state)
			state.setLanguage(language)
			state.ai.open, state.ai.panelWidth = true, 300
			state.ai.items = []aiItem{{kind: aiUser, text: "question"}, {kind: aiAssistant, text: "answer"}}
			state.ai.lastRun = &aiRunStats{phase: aiRunCompleted, duration: 8 * time.Second,
				provider: "magpie", model: "custom/deepseek-flash",
				usage: llm.Usage{InputTokens: 12000, OutputTokens: 284, CacheReadTokens: 9800}}
			state.ai.totalUsage = state.ai.lastRun.usage
			canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
			usage, ok := state.aiRunUsage()
			if !ok {
				t.Fatal("a finished request has no usage to show")
			}
			chip := state.ai.panel.usageChip
			if chip.Width <= 0 {
				t.Fatal("a finished request has no usage chip")
			}
			if chip.Right() > state.ai.panel.list.Right() {
				t.Fatalf("chip ends at %.1f, past the column's %.1f", chip.Right(), state.ai.panel.list.Right())
			}
			label := usage.chipLabel(state.messages.AIPanel)
			if measured := canvas.TextWidthWeight(label, aiSmallSize, gfx.WeightMedium); measured > chip.Width {
				t.Fatalf("chip label %q measures %.1f, wider than its %.1f", label, measured, chip.Width)
			}

			// Resting on the chip opens the card, whose rows must fit its fixed width.
			state.ai.panel.pointer = gfx.Point{X: chip.X + 2, Y: chip.Y + 2}
			canvas, _ = paintAt(state, gfx.Size{Width: 1400, Height: 900})
			if !state.ai.panel.usageCard {
				t.Fatal("the chip does not open its card on hover")
			}
			metrics := state.Theme().Metrics
			room := aiUsageCardWidth - 2*metrics.PadSurface
			for _, row := range usage.rows(state.messages.AIPanel) {
				labelWidth := canvas.TextWidthWeight(row.label, metrics.FontSizeS, gfx.WeightRegular)
				value := canvas.FitLine(row.value, metrics.FontSizeS, gfx.WeightRegular, room)
				if labelWidth+canvas.TextWidthWeight(value, metrics.FontSizeS, gfx.WeightRegular) > room {
					t.Fatalf("card row %q %q does not fit %.1f", row.label, row.value, room)
				}
			}
		})
	}
}

// TestAUsageChipOffScreenOpensNoCard proves the chip answers the pointer only
// where it is on screen: the List declares only the rows it can show, so a
// status row scrolled out of the list leaves no chip behind, and a pointer
// resting where the chip was must not open the card.
func TestAUsageChipOffScreenOpensNoCard(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.setLanguage(i18n.English)
	state.ai.open, state.ai.panelWidth = true, 300
	state.ai.items = nil
	for index := 0; index < 60; index++ {
		state.ai.items = append(state.ai.items, aiItem{kind: aiUser, text: fmt.Sprintf("line %d of a long conversation", index)})
	}
	state.ai.lastRun = &aiRunStats{phase: aiRunCompleted,
		usage: llm.Usage{InputTokens: 1200, OutputTokens: 84}}
	state.ai.totalUsage = state.ai.lastRun.usage
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if state.ai.panel.maxScroll <= 0 {
		t.Fatalf("the conversation does not scroll: max scroll %.1f", state.ai.panel.maxScroll)
	}
	scroll := state.ai.panel.messageScroll
	if scroll == nil {
		t.Fatal("the conversation declared no scroll")
	}
	// Read the conversation's end, where the status row and its chip live. The
	// List measures its rows over a few paints before it shows the last one,
	// so rest a moment before asking for the chip.
	scroll.ScrollToEnd()
	var chip gfx.Rect
	for range 5 {
		paintAt(state, gfx.Size{Width: 1400, Height: 900})
		if state.ai.panel.usageChip.Width > 0 {
			chip = state.ai.panel.usageChip
			break
		}
	}
	if chip.Width <= 0 {
		t.Fatal("the chip at the conversation's end was not laid out")
	}
	state.ai.panel.pointer = gfx.Point{X: chip.X + 2, Y: chip.Y + 2}
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if !state.ai.panel.usageCard {
		t.Fatal("the chip does not open its card on hover")
	}
	// Then leave: scrolled to the top, the status row is not declared at all
	// and the chip keeps no rect for the pointer.
	scroll.ScrollTo(0)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if state.ai.panel.usageChip.Width > 0 {
		t.Fatalf("an off-screen chip is still laid out: %.1f wide", state.ai.panel.usageChip.Width)
	}
	if state.ai.panel.usageCard {
		t.Fatal("an off-screen chip kept its card open")
	}
}
