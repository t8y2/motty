package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func emptyStateActionHit(canvas *gfx.Canvas, id gfx.ID) (gfx.Hit, bool) {
	for _, hit := range canvas.Hits() {
		if hit.ID == id &&
			hit.Bounds.Width == emptyStateActionWidth &&
			hit.Bounds.Height == toolbarHeight {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

func TestCatalogEmptyStatesUseSharedVisualMetrics(t *testing.T) {
	tests := []struct {
		name        string
		section     uint8
		title       func(*State) string
		action      gfx.ID
		actionLabel func(*State) string
		prepare     func(*State)
		hasAction   bool
	}{
		{
			name: "hosts", section: sectionHosts, action: hitAddHostEmpty, hasAction: true,
			title:       func(state *State) string { return state.messages.HostsEmpty },
			actionLabel: func(state *State) string { return state.messages.NewHost },
			prepare:     func(state *State) { state.hosts, state.visible = nil, nil },
		},
		{
			name: "monitor", section: sectionDashboard, action: hitDashboardNew, hasAction: true,
			title:       func(state *State) string { return state.messages.Dashboard.Empty },
			actionLabel: func(state *State) string { return state.messages.Dashboard.Create },
			prepare:     func(state *State) { state.dashboard.panels, state.dashboard.active = nil, "" },
		},
		{
			name: "keys", section: sectionKeys, action: hitKeysAdd, hasAction: true,
			title:       func(state *State) string { return state.messages.KeyEmpty },
			actionLabel: func(state *State) string { return state.messages.KeyAdd },
			prepare:     func(state *State) { state.identities, state.selectedIdentityID = nil, 0 },
		},
		{
			name: "proxies", section: sectionProxies, action: hitProxyAdd, hasAction: true,
			title:       func(state *State) string { return state.messages.ProxyEmpty },
			actionLabel: func(state *State) string { return state.messages.ProxyAdd },
			prepare:     func(state *State) { state.proxies, state.selectedProxyID = nil, 0 },
		},
		{
			name: "forwards", section: sectionForwards, action: hitForwardAdd, hasAction: true,
			title:       func(state *State) string { return state.messages.ForwardEmpty },
			actionLabel: func(state *State) string { return state.messages.ForwardAdd },
			prepare:     func(state *State) { state.forwards, state.selectedForwardID = nil, 0 },
		},
		{
			name: "known hosts", section: sectionKnownHosts,
			title: func(state *State) string { return state.messages.KnownHostsEmpty },
			prepare: func(state *State) {
				state.knownHosts.rows, state.knownHosts.visible, state.knownHosts.errorText = nil, nil, ""
			},
		},
		{
			name: "snippets", section: sectionSnippets, action: hitSnippetAdd, hasAction: true,
			title:       func(state *State) string { return state.messages.Tools.SnippetEmpty },
			actionLabel: func(state *State) string { return state.messages.Tools.SnippetAdd },
			prepare:     func(state *State) { state.snippets.list, state.snippets.selected = nil, 0 },
		},
		{
			// The journal is filled by the connection lifecycle, so its empty
			// page has no create action; the record is what the page restores.
			name: "session logs", section: sectionSessionLogs,
			title: func(state *State) string { return state.messages.SessionLogs.Empty },
			prepare: func(state *State) {
				state.sessionLogs.view.rows, state.sessionLogs.view.page = nil, nil
				state.prepareSessionLogsView()
			},
		},
	}

	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 760, Height: 620}} {
		for _, test := range tests {
			// A case runs once per size, so the size is part of its name: two
			// cases of one name make Go append "#01", and a "#" in a test
			// directory's path is not a database path the journal accepts.
			t.Run(fmt.Sprintf("%s/%gx%g", test.name, size.Width, size.Height), func(t *testing.T) {
				state := newTestState(t, testOptions(t))
				test.prepare(state)
				state.activeSection = test.section
				title := test.title(state)
				var canvas gfx.Canvas
				state.Paint(&canvas, size)

				texts := make([]string, 0, len(canvas.Texts()))
				for _, text := range canvas.Texts() {
					texts = append(texts, text.Value)
				}
				if !contains(texts, title) {
					t.Fatalf("empty title %q was not painted", title)
				}

				tileFound := false
				for _, rectangle := range canvas.Rectangles() {
					if rectangle.Bounds.Width == emptyStateTileSize &&
						rectangle.Bounds.Height == emptyStateTileSize &&
						rectangle.Radius == emptyStateTileRadius {
						tileFound = true
						break
					}
				}
				if !tileFound {
					t.Fatal("the shared empty-state icon tile was not painted")
				}

				if !test.hasAction {
					for _, hit := range canvas.Hits() {
						if hit.Bounds.Width == emptyStateActionWidth && hit.Bounds.Height == toolbarHeight {
							t.Fatalf("a catalog page without a create workflow painted an empty-state action: %+v", hit)
						}
					}
					return
				}
				actionLabel := test.actionLabel(state)
				if width := toolbarButtonWidth(&canvas, actionLabel, true); width > emptyStateActionWidth {
					t.Fatalf("action %q needs %.1f, more than the shared %.0f width", actionLabel, width, emptyStateActionWidth)
				}
				button, ok := emptyStateActionHit(&canvas, test.action)
				if !ok {
					t.Fatalf("the shared %.0fx%.0f empty-state action was not painted", emptyStateActionWidth, toolbarHeight)
				}
				if button.Bounds.X < 0 || button.Bounds.Y < 0 ||
					button.Bounds.X+button.Bounds.Width > size.Width ||
					button.Bounds.Y+button.Bounds.Height > size.Height {
					t.Fatalf("empty-state action is outside the %v window: %+v", size, button.Bounds)
				}
			})
		}
	}
}

func TestHostSearchNoMatchesUsesTheSharedEmptyState(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts = []Host{{ID: 1, Label: "web", Address: "web.example", Port: 22}}
	state.search.Insert("no-such-host")
	state.activeSection = sectionHosts
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	if !contains(texts, state.messages.NoMatches) {
		t.Fatalf("no-match title was not painted: %v", texts)
	}
	tile := false
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds.Width == emptyStateTileSize && rectangle.Bounds.Height == emptyStateTileSize {
			tile = true
			break
		}
	}
	if !tile {
		t.Fatal("the shared empty-state icon tile was not painted")
	}
	for _, hit := range canvas.Hits() {
		if hit.Bounds.Width == emptyStateActionWidth && hit.Bounds.Height == toolbarHeight {
			t.Fatalf("the no-match state painted an action: %+v", hit)
		}
	}
}
