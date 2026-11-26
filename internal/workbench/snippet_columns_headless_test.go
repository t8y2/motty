//go:build darwin || windows || linux

package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

// The snippets directory-tree and inline editor must lay out cleanly without
// clipping or overlaps even when snippets carry very long names and commands.
func TestSnippetsTreeWorkspaceLayoutStaysCleanWithLongCommands(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.setLanguage(i18n.English)
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "First " + strings.Repeat("a", 40), Group: "Docker/Prod", Command: "first-command-" + strings.Repeat("x", 700), Run: true},
		snippet{Name: "Second", Group: "Docker/Staging", Command: "second-command-" + strings.Repeat("y", 1600), Run: true},
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
	if problems := headless.CheckLayout(view.Snapshot()); len(problems) > 0 {
		t.Fatalf("snippets tree workspace layout problems: %v", problems)
	}
	texts := strings.Join(view.Texts(), "\n")
	for _, want := range []string{"Docker", "Prod", "Staging", "Second"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("missing %q in painted snippets tree workspace: %v", want, view.Texts())
		}
	}

	// Right-clicking the blank area below the tree rows opens the blank menu
	// offering New Group.
	treeRect := state.sectionBodies.snippets.tree.Rect()
	blankX := treeRect.X + treeRect.Width/2
	blankY := treeRect.Y + treeRect.Height - 40
	if err := view.Driver().Press(blankX, blankY, gfx.PointerButtonSecondary); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Release(blankX, blankY, gfx.PointerButtonSecondary); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !state.snippets.blankMenu.open {
		t.Fatalf("right-clicking blank tree area (%v,%v) did not open blank menu", blankX, blankY)
	}
}
