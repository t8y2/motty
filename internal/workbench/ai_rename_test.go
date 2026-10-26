package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func TestAIRenameDialogButtons(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hit   ui.ID
		title string
		want  string
	}{
		{"close", hitAIRenameClose, "discarded", "original"},
		{"cancel", hitAIRenameCancel, "discarded", "original"},
		{"dismiss", hitAIRenameDismiss, "discarded", "original"},
		{"submit", hitAIRenameSubmit, "  重命名后的对话  ", "重命名后的对话"},
		{"empty", hitAIRenameSubmit, "   ", "original"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			db, err := state.aiStore()
			if err != nil {
				t.Fatal(err)
			}
			conversation, err := db.CreateConversation("original", "")
			if err != nil {
				t.Fatal(err)
			}
			state.Click(hitAIToggle)
			state.Click(hitAIHistory)
			state.Click(hitAIHistoryRename.At(0))
			if state.modal != modalAIRename || state.ai.rename.id != conversation.ID {
				t.Fatal("history action did not open the selected conversation")
			}
			// Opening the dialog selects its title, so typing replaces it.
			state.ai.rename.editor.Insert(tc.title)
			driver, err := app.NewDriver(app.Config{
				Title: "AI rename", Width: 1400, Height: 900, Tree: Tree(),
				Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(driver.Close)
			var clicked bool
			for _, hit := range driver.Snapshot().Hits {
				if hit.ID != tc.hit || hit.IsLabel {
					continue
				}
				x, y := hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2
				if tc.hit == hitAIRenameDismiss {
					x, y = hit.Bounds.X+1, hit.Bounds.Y+1 // outside the dialog
				}
				if err := driver.Click(x, y); err != nil {
					t.Fatal(err)
				}
				clicked = true
				break
			}
			if !clicked {
				t.Fatalf("button %s was not painted", tree.Text(tc.hit))
			}
			if tc.name == "empty" {
				if state.modal != modalAIRename || state.dialogError != state.messages.AIPanel.RenameRequired {
					t.Fatalf("empty title: modal=%v error=%q", state.modal, state.dialogError)
				}
				state.Click(hitAIRenameCancel)
			}
			if state.modal != modalNone || state.dialogError != "" {
				t.Fatalf("dialog did not close cleanly: modal=%v error=%q", state.modal, state.dialogError)
			}
			if !state.ai.open || !state.ai.panel.history {
				t.Fatal("the history panel should stay open")
			}
			list, err := db.Conversations("", 10)
			if err != nil || len(list) != 1 {
				t.Fatalf("saved conversations: %v, %v", list, err)
			}
			if list[0].ID != conversation.ID || list[0].Title != tc.want {
				t.Fatalf("saved conversation = %+v, want title %q", list[0], tc.want)
			}
			if state.ai.conversations[0].Title != tc.want {
				t.Fatalf("history title = %q, want %q", state.ai.conversations[0].Title, tc.want)
			}
		})
	}
}
