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

// Right-clicking anywhere on the empty Snippets page or when the directory tree
// is not mounted (collapsed / narrow without drawer) must not panic by reading
// an unmounted or stale ui.Box (#1719).
func TestSnippetsRightClickWithoutMountedTreeDoesNotPanic(t *testing.T) {
	openView := func(t *testing.T, state *State, width, height int) *headless.Session {
		t.Helper()
		view, err := headless.Open(app.Config{
			Width: width, Height: height, Tree: Tree(), Paint: state.Paint,
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
		t.Cleanup(func() { view.Close() })
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		return view
	}

	rightClick := func(t *testing.T, view *headless.Session, x, y float32) {
		t.Helper()
		if err := view.Driver().Press(x, y, gfx.PointerButtonSecondary); err != nil {
			t.Fatal(err)
		}
		if err := view.Driver().Release(x, y, gfx.PointerButtonSecondary); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("empty page", func(t *testing.T) {
		options := freshOptions(t)
		state := newTestState(t, options)
		state.setLanguage(i18n.English)
		state.activeSection = sectionSnippets

		view := openView(t, state, 1200, 800)
		rightClick(t, view, 600, 400)
		if state.snippets.blankMenu.open {
			t.Fatal("right-clicking empty snippets page opened blank tree menu")
		}
	})

	t.Run("collapsed tree", func(t *testing.T) {
		options := freshOptions(t)
		state := newTestState(t, options)
		state.setLanguage(i18n.English)
		state.activeSection = sectionSnippets
		addTestSnippets(state, snippet{Name: "Disk", Command: "df -h", Run: true})
		state.snippets.treeCollapsed = true

		view := openView(t, state, 1200, 800)
		rightClick(t, view, 600, 720)
		if state.snippets.blankMenu.open {
			t.Fatal("right-clicking collapsed snippets editor opened blank tree menu")
		}
	})

	t.Run("narrow drawer tree blank right-click", func(t *testing.T) {
		options := freshOptions(t)
		state := newTestState(t, options)
		state.setLanguage(i18n.English)
		state.activeSection = sectionSnippets
		addTestSnippets(state, snippet{Name: "Disk", Command: "df -h", Run: true})
		state.snippets.treeDrawer = true

		view := openView(t, state, 560, 700)
		treeRect := state.sectionBodies.snippets.treeRect
		if treeRect.Width <= 0 || treeRect.Height <= 0 {
			t.Fatalf("drawer treeRect was not recorded: %+v", treeRect)
		}
		rightClick(t, view, treeRect.X+treeRect.Width/2, treeRect.Y+treeRect.Height-40)
		if !state.snippets.blankMenu.open {
			t.Fatalf("right-clicking blank drawer tree area (%+v) did not open blank menu", treeRect)
		}
	})
}
