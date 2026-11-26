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
	"github.com/vivaui/viva/headless"
	"motty/internal/ai/llm"
	"motty/internal/i18n"
)

// TestAIInterjectHeadlessPanel shows a running answer with an interjection
// waiting, and the same conversation once the agent has read it. Setting
// MOTTY_INTERJECT_SHOTS to a directory writes the two frames there.
func TestAIInterjectHeadlessPanel(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.setLanguage(i18n.Chinese)
	state.setAppearance(appearanceLight)
	state.ai.open, state.ai.panelWidth = true, 360
	const interjection = "另外重点看一下日志目录"
	state.ai.items = []aiItem{
		{kind: aiUser, text: "检查一下这台机器的磁盘占用"},
		{kind: aiAssistant, text: "我先看一下各个挂载点的使用情况。"},
		{kind: aiTool, call: llm.ToolCall{ID: "t1", Name: "terminal_exec", Arguments: `{"command":"df -h"}`}, status: aiToolRunning},
		{kind: aiUser, text: interjection, pending: aiSteerPending},
	}
	run := &aiRun{cancel: func() {}, open: -1, stats: aiRunStats{startedAt: 2 * time.Second}, steerable: true}
	state.ai.run = run
	state.options.Now = func() time.Duration { return 10 * time.Second }
	view, err := headless.Open(app.Config{Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	check := func(name string, wantPending bool) {
		t.Helper()
		if err := view.Resize(1400, 900); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if _, texts := paintAt(state, panelWide); !hasText(texts, interjection) {
			t.Fatalf("%s frame does not show the interjection", name)
		}
		for _, problem := range headless.CheckLayout(view.Snapshot()) {
			// The panel's resize edge straddles the divider by design.
			if strings.Contains(problem.Detail, state.messages.AIPanel.ResizePanel) {
				continue
			}
			t.Errorf("%s layout: %+v", name, problem)
		}
		if directory := os.Getenv("MOTTY_INTERJECT_SHOTS"); directory != "" {
			if err := view.Screenshot(filepath.Join(directory, fmt.Sprintf("interject-%s.png", name))); err != nil {
				t.Fatal(err)
			}
		}
		if wantPending != (state.ai.items[3].pending == aiSteerPending) {
			t.Fatalf("%s frame has the wrong delivery state", name)
		}
	}
	check("pending", true)
	state.aiDeliverSteer(run)
	check("delivered", false)
}
