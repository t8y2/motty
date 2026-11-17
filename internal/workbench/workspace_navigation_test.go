package workbench

import (
	"strings"
	"testing"
	"unicode"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestWorkspaceNavigationFollowsHostsWithoutTemplateCount(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.templates = []WorkspaceTemplate{
		mustWorkspaceTemplate(t, "Production", []string{"host_a"}, 1),
		mustWorkspaceTemplate(t, "Database", []string{"host_b"}, 1),
		mustWorkspaceTemplate(t, "Edge", []string{"host_c"}, 1),
	}

	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	hosts := panelHit(t, canvas, hitHosts)
	workspaces := panelHit(t, canvas, hitWorkspaces)
	if workspaces.Label != state.messages.NavWorkspaces || workspaces.Role != gfx.RoleMenuItem {
		t.Fatalf("workspace row = %+v", workspaces)
	}
	if workspaces.Bounds.Width != hosts.Bounds.Width || workspaces.Bounds.Height != hosts.Bounds.Height {
		t.Fatalf("workspace row bounds %+v do not match hosts %+v", workspaces.Bounds, hosts.Bounds)
	}
	if gap := workspaces.Bounds.Y - (hosts.Bounds.Y + hosts.Bounds.Height); gap != 4 {
		t.Fatalf("workspace row follows hosts with a %v gap, want 4", gap)
	}
	assertNoTemplateCountInSidebar(t, canvas, workspaces)

	state.Click(hitWorkspaces)
	if state.activeSection != sectionWorkspaces || state.workspaceName() != "workspaces" {
		t.Fatalf("workspace click left section=%d name=%q", state.activeSection, state.workspaceName())
	}
	canvas, _ = paintAt(state, gfx.Size{Width: 1400, Height: 900})
	workspaces = panelHit(t, canvas, hitWorkspaces)
	if !workspaces.Selected {
		t.Fatalf("workspace row is not selected after opening it: %+v", workspaces)
	}
	assertNoTemplateCountInSidebar(t, canvas, workspaces)

	state.restoreWorkspace("workspaces")
	if state.activeSection != sectionWorkspaces {
		t.Fatalf("restored section = %d", state.activeSection)
	}

	state.Click(hitHosts)
	state.Click(hitCommandPalette)
	app.TextEvent{Kind: app.TextInsert, Text: "workspaces"}.Apply(state.EditorFor(hitCommandPaletteSearch))
	state.refreshCommandPalette()
	if len(state.commandPalette.visible) != 1 {
		t.Fatalf("workspace palette matches = %d", len(state.commandPalette.visible))
	}
	state.Key(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyEnter})
	if state.activeSection != sectionWorkspaces {
		t.Fatalf("palette opened section %d", state.activeSection)
	}
}

func assertNoTemplateCountInSidebar(t *testing.T, canvas *gfx.Canvas, row gfx.Hit) {
	t.Helper()
	rowTexts := 0
	for _, text := range canvas.Texts() {
		if !inside(text.Bounds, row.Bounds) {
			continue
		}
		if strings.ContainsFunc(text.Value, unicode.IsDigit) {
			t.Fatalf("the workspace row emits count text %q", text.Value)
		}
		rowTexts++
		if text.Value != row.Label {
			t.Fatalf("the workspace row emits %q beside %q", text.Value, row.Label)
		}
	}
	if rowTexts != 1 {
		t.Fatalf("the workspace row emits %d labels, want only %q", rowTexts, row.Label)
	}
}

func inside(inner, outer gfx.Rect) bool {
	return inner.X >= outer.X && inner.Y >= outer.Y &&
		inner.X+inner.Width <= outer.X+outer.Width &&
		inner.Y+inner.Height <= outer.Y+outer.Height
}
