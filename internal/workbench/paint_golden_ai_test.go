package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/store"
)

// aiPanelScene paints the assistant panel itself, which the other golden files
// reach only through its dialogs. The panel is docked in a wide window, a
// drawer in a narrow one, and listing its saved conversations in a third.
func aiPanelScene(name string, size gfx.Size, setup func(*testing.T, *State)) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{size}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		_, options := newWorld(t)
		state := newTestState(t, options)
		withModel(t, state)
		state.ai.open = true
		setup(t, state)
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}

func init() {
	extraPaintScenes = append(extraPaintScenes,
		aiPanelScene("ai-panel-docked", paintDesktop, func(t *testing.T, state *State) {
			longConversation(state, 12)
		}),
		aiPanelScene("ai-panel-drawer", gfx.Size{Width: 800, Height: 600}, func(t *testing.T, state *State) {
			longConversation(state, 12)
		}),
		aiPanelScene("ai-panel-history", gfx.Size{Width: 1000, Height: 700}, func(t *testing.T, state *State) {
			for index := 0; index < 30; index++ {
				state.ai.conversations = append(state.ai.conversations, store.Conversation{ID: int64(index + 1), Title: "saved conversation"})
			}
			state.ai.panel.history = true
		}),
	)
}
