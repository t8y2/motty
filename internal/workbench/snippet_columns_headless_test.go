//go:build darwin || windows || linux

package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

// A command longer than the command column must be ellipsized in place, not
// squeeze the fixed columns before it: every row keeps the group and command
// positions the headings declare, whatever the command's length (#1152).
func TestSnippetColumnsStayAlignedWhenCommandsAreLong(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.setLanguage(i18n.English)
	state.activeSection = sectionSnippets
	commands := []string{
		"first-command-" + strings.Repeat("x", 700),
		"second-command-" + strings.Repeat("y", 1600),
	}
	addTestSnippets(state,
		snippet{Name: "First", Group: "Docker", Command: commands[0], Run: true},
		snippet{Name: "Second", Group: "Docker", Command: commands[1], Run: true},
	)

	view, err := headless.Open(app.Config{
		Width: 1200, Height: 800, Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(c app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(c.ID); editor != nil {
				return &app.TextInput{Editor: editor}
			}
			return nil
		},
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}

	painted := view.Snapshot().Texts
	// find returns the painted text that starts with prefix, with its x and y.
	find := func(prefix string) (string, float32, float32) {
		t.Helper()
		for _, text := range painted {
			if strings.HasPrefix(text.Value, prefix) {
				return text.Value, text.Bounds.X, text.Bounds.Y
			}
		}
		t.Fatalf("nothing painted starts with %q; painted texts: %v", prefix, view.Texts())
		return "", 0, 0
	}
	// inRow returns the x of the painted text that shares the row at y.
	inRow := func(prefix string, y float32) float32 {
		t.Helper()
		for _, text := range painted {
			if text.Bounds.Y == y && strings.HasPrefix(text.Value, prefix) {
				return text.Bounds.X
			}
		}
		t.Fatalf("nothing starting with %q shares the row at y=%v; painted texts: %v", prefix, y, view.Texts())
		return 0
	}

	m := state.messages.Tools
	_, groupHeadingX, _ := find(m.SnippetColumnGroup)
	_, commandHeadingX, _ := find(m.SnippetColumnCommand)

	for index, command := range commands {
		text, commandX, y := find(command[:14])
		if !strings.HasSuffix(text, "…") || len(text) >= len(command) {
			t.Fatalf("command %d was not ellipsized in its column: %q", index, text)
		}
		if commandX != commandHeadingX {
			t.Errorf("row %d starts its command at %v, but the Command heading starts at %v", index, commandX, commandHeadingX)
		}
		if groupX := inRow("Docker", y); groupX != groupHeadingX {
			t.Errorf("row %d starts its group at %v, but the Group heading starts at %v", index, groupX, groupHeadingX)
		}
	}
}
