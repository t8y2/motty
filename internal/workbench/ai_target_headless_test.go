//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	"motty/internal/i18n"
	"motty/internal/sshconn"
)

func TestAITargetComposerNativeLayoutAtDisplayScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		for _, size := range []ui.Size{panelWide, panelNarrow} {
			t.Run(fmt.Sprintf("%g/%g", scale, size.Width), func(t *testing.T) {
				state := targetTestWorld(t, quietModel{"ok"})
				state.setLanguage(i18n.Chinese)
				state.setAppearance(appearanceLight)
				state.renameSession(&state.sessions[0], "local-ubuntu")
				state.renameSession(&state.sessions[1], "local-ubuntu")
				state.sessions[0].host.System = sshconn.SystemUbuntu
				state.sessions[1].host.System = sshconn.SystemLinux
				state.rebuildTabOptions()
				state.aiChooseTarget(hitAITargets.At(int(state.sessions[1].ID)))
				state.ai.mode, state.ai.open = agent.ModeAgent, true
				state.aiAppendQuestion("看看服务器磁盘占用", "first", state.sessions[0].Title)
				state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "可以从磁盘使用情况开始分析。"})
				state.aiAppendQuestion("再看看这台服务器", "second", state.sessions[1].Title)
				view, err := headless.Open(app.Config{Width: int(size.Width), Height: int(size.Height), Tree: Tree(), Paint: state.Paint, OnResize: state.Resize,
					OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll,
					TextInput: state.AIPanelInput}, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				must := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				find := func(id ui.ID) ui.Hit {
					t.Helper()
					for _, hit := range view.Snapshot().Hits {
						if hit.ID == id {
							return hit
						}
					}
					t.Fatalf("missing %v", id)
					return ui.Hit{}
				}
				click := func(id ui.ID) {
					t.Helper()
					hit := find(id)
					must(view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2))
				}
				must(view.Rest())
				trigger, editor := find(hitAITarget), find(hitAIInput)
				if trigger.Bounds.Y+trigger.Bounds.Height > editor.TextBounds.Y || trigger.Bounds.Width > 200 {
					t.Fatal("selector overlaps text or is too large")
				}
				if editor.TextBounds.Y+editor.TextBounds.Height >= find(hitAISend).Bounds.Y {
					t.Fatal("text overlaps bottom controls")
				}
				if dir := os.Getenv("MOTTY_AI_TARGET_SCREENSHOTS"); dir != "" && size == panelWide && scale == 1.25 {
					must(os.MkdirAll(dir, 0700))
					must(view.Screenshot(filepath.Join(dir, "ai-target-composer.png")))
				}
				click(hitAITarget)
				must(view.Rest())
				option := find(hitAITargets.At(int(state.sessions[0].ID)))
				if option.Label != state.sessions[0].Title || option.Bounds.X+option.Bounds.Width > size.Width {
					t.Fatalf("menu name/placement = %+v", option)
				}
				click(option.ID)
				must(view.Rest())
				click(hitAIInput)
				must(view.Type("你好\n测试 @"))
				must(view.Rest())
				if state.ai.input.String() != "你好\n测试 @" {
					t.Fatalf("native input = %q", state.ai.input.String())
				}
				mention := find(hitAIMentions.At(1))
				if mention.Label != "@"+state.sessions[1].Title {
					t.Fatalf("mention differs from tab: %q", mention.Label)
				}
				caretBefore := state.ai.input.Selection()
				must(view.Keys("Down"))
				must(view.Rest())
				if !find(mention.ID).Selected || state.ai.input.Selection() != caretBefore {
					t.Fatal("Down did not highlight the second suggestion without moving the caret")
				}
				if dir := os.Getenv("MOTTY_AI_TARGET_SCREENSHOTS"); dir != "" && size == panelWide && scale == 1.25 {
					must(view.Screenshot(filepath.Join(dir, "ai-mention-keyboard.png")))
				}
				must(view.Keys("Enter"))
				must(view.Rest())
				if state.ai.run != nil || state.ai.input.String() == "" || view.Snapshot().Focused != hitAIInput {
					t.Fatal("Enter sent the question instead of accepting the mention")
				}
				ids := findMentions(state.ai.input.String(), state.mentionNames(nil))
				if len(ids) != 1 || ids[0] != state.sessions[1].ID || state.aiTarget().ID != sessionHandle(state.sessions[0].ID) {
					t.Fatal("reference resolved incorrectly or changed the target")
				}
				must(view.Driver().SelectText(0, state.ai.input.RuneCount()))
				must(view.Type("@"))
				must(view.Rest())
				click(hitAIMentions.At(0))
				must(view.Rest())
				if state.ai.input.String() != "@"+mentionSlug(state.sessions[0].Title)+" " || view.Snapshot().Focused != hitAIInput {
					t.Fatal("mouse acceptance did not restore the editor focus")
				}
				must(view.Driver().SelectText(0, state.ai.input.RuneCount()))
				must(view.Type("中文输入候选位置"))
				must(view.Rest())
				editor = find(hitAIInput)
				caret, ok := editor.Placement.CaretBounds()
				if !ok || caret.Y < editor.TextBounds.Y {
					t.Fatal("native candidate anchor overlaps the selector")
				}
			})
		}
	}
}
