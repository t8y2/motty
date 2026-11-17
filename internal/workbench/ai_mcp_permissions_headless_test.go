//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/ai/gate"
	"motty/internal/i18n"
)

func TestMCPAllowAllConsentThroughNativeInput(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, scale := range []float64{1, 1.5, 2} {
			t.Run(fmt.Sprintf("%s-%.1f", language, scale), func(t *testing.T) {
				world := newSettingsWorld(t, newLister())
				state, db := world.state, world.db(t)
				state.setLanguage(language)
				state.setAppearance(appearanceLight)
				state.selectSettingsCategory(settingsMCP)
				view, err := headless.Open(state.WindowConfig(app.Config{Title: "MCP permissions", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize}), nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				// The access level is a page Choice: click its segment by its
				// words. The first "Allow all" is the HTTP level.
				clickChoice := func(label string, first bool) {
					t.Helper()
					view.Rest()
					hit, ok := settingsChoiceInHits(view.Snapshot().Hits, label, !first)
					if !ok {
						t.Fatalf("missing access choice %q", label)
					}
					if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
				}
				allowAll := state.messages.AISettings.MCPModeAllowAll
				clickButton := func(label string) {
					t.Helper()
					view.Rest()
					hit, ok := snapshotButton(view.Snapshot(), label)
					if !ok {
						t.Fatalf("missing button %q", label)
					}
					if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
				}
				checkUnselected := func() {
					t.Helper()
					if state.ai.settings.mcpMode != gate.ModeAsk || settingValue(t, db, "ai.mcp.mode") != "ask" {
						t.Fatal("access changed before confirmation")
					}
					for _, hit := range view.Snapshot().Hits {
						if hit.Label == allowAll && hit.Role == gfx.RoleRadio && hit.Selected {
							t.Fatal("unconfirmed option looks selected")
						}
					}
				}
				shot := func(name string) {
					t.Helper()
					if dir := os.Getenv("MOTTY_MCP_PERMISSION_SCREENSHOTS"); dir != "" && scale == 1 {
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("%s-%s.png", language, name))); err != nil {
							t.Fatal(err)
						}
					}
				}
				clickChoice(state.messages.AISettings.MCPModeAsk, false)
				if err := view.Keys("right"); err != nil {
					t.Fatal(err)
				}
				checkUnselected()
				cancel, ok := snapshotButton(view.Snapshot(), state.messages.Cancel)
				if state.ai.cfg.dialog != aiDialogMCPAllowAll || !ok || view.Snapshot().Focused != cancel.ID {
					t.Fatal("keyboard selection did not focus the safe action")
				}
				view.Rest()
				shot("confirm")
				if err := view.Keys("esc"); err != nil {
					t.Fatal(err)
				}
				checkUnselected()
				if state.ai.cfg.dialog != aiDialogNone {
					t.Fatal("Escape did not cancel")
				}
				clickChoice(allowAll, true)
				checkUnselected()
				if err := view.Keys("enter"); err != nil {
					t.Fatal(err)
				}
				checkUnselected() // Enter on the initial Cancel button must cancel.
				if state.ai.cfg.dialog != aiDialogNone {
					t.Fatal("the default button did not cancel")
				}
				clickChoice(allowAll, true)
				clickButton(state.messages.AISettings.MCPAllowAllConfirm)
				if state.ai.cfg.dialog != aiDialogNone || state.ai.settings.mcpMode != gate.ModeAllowAll {
					t.Fatal("confirmation did not select full access")
				}
				view.Rest()
				shot("enabled")
				state.setAppearance(appearanceDark)
				if err := view.Resize(1024, 760); err != nil {
					t.Fatal(err)
				}
				view.Rest()
				for _, label := range []string{state.messages.AISettings.MCPModeObserve, state.messages.AISettings.MCPModeAsk, state.messages.AISettings.MCPModeAllowAll} {
					found := false
					for _, text := range view.Snapshot().Texts {
						found = found || text.Value == label
					}
					if !found {
						t.Fatalf("a narrow settings page clipped the access option %q", label)
					}
				}
				shot("enabled-dark")
				clickChoice(state.messages.AISettings.MCPModeObserve, false)
				if state.ai.settings.mcpMode != gate.ModeObserve {
					t.Fatal("could not revoke access")
				}
			})
		}
	}
}

func TestMCPAgentHostsLargeListUsesVirtualizedScrolling(t *testing.T) {
	const count = 1000
	world := newSettingsWorld(t, newLister())
	state := world.state
	hosts := make([]savedHost, 0, count)
	for index := range count {
		hosts = append(hosts, savedHost{
			ID: fmt.Sprintf("host-%04d", index), Label: fmt.Sprintf("Host %04d", index),
			Address: fmt.Sprintf("host-%04d.example.com", index), User: "ops", Port: 22,
		})
	}
	state.restoreSettings(savedState{Hosts: hosts})
	state.selectSettingsCategory(settingsMCP)
	config := state.WindowConfig(app.Config{
		Title: "MCP host list", Width: 1280, Height: 1600, Tree: Tree(),
		Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize,
	})
	config.TextInput = func(context app.TextInputContext) *app.TextInput {
		if editor := state.EditorFor(context.ID); editor != nil {
			return &app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
		}
		return nil
	}
	view, err := headless.Open(config, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}

	allowAll, ok := settingsChoiceInHits(view.Snapshot().Hits, state.messages.AISettings.AgentHostsAll, true)
	if !ok {
		t.Fatal("the agent host mode has no Allow all choice")
	}
	if err := view.Click(allowAll.Bounds.X+allowAll.Bounds.Width/2, allowAll.Bounds.Y+allowAll.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.agentHostMode != agentHostModeAll || len(state.agentHosts) != count {
		t.Fatalf("Allow all opened %d hosts in mode %v", len(state.agentHosts), state.agentHostMode)
	}
	catalog, err := readAgentCatalog(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.agentHostList(); len(got) != count {
		t.Fatalf("the independent MCP service read %d hosts", len(got))
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	m := state.messages.AISettings
	viewButton, ok := snapshotButton(view.Snapshot(), m.AgentHostsView)
	if !ok {
		t.Fatal("the host access row has no View button")
	}
	if err := view.Click(viewButton.Bounds.X+viewButton.Bounds.Width/2, viewButton.Bounds.Y+viewButton.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalAgentHosts {
		t.Fatal("View did not open the open-hosts dialog")
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}

	painted := func(value string) bool {
		for _, text := range view.Snapshot().Texts {
			if text.Value == value {
				return true
			}
		}
		return false
	}
	hitByID := func(id gfx.ID) (gfx.Hit, bool) {
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == id {
				return hit, true
			}
		}
		return gfx.Hit{}, false
	}
	if !painted("Host 0000") {
		t.Fatal("the first open host is not painted in the dialog")
	}
	if painted("Host 0060") || painted("Host 0999") {
		t.Fatal("the dialog painted hosts far beyond its visible rows")
	}
	var at gfx.Point
	for _, text := range view.Snapshot().Texts {
		if text.Value == "Host 0000" {
			at = gfx.Point{X: text.Bounds.X + text.Bounds.Width/2, Y: text.Bounds.Y + text.Bounds.Height/2}
			break
		}
	}
	if at == (gfx.Point{}) {
		t.Fatal("the first host has no pointer position")
	}
	outerBefore := state.settingsScroll
	if err := view.Scroll(gfx.ScrollEvent{X: at.X, Y: at.Y, DeltaY: 100000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !painted("Host 0999") || painted("Host 0000") {
		t.Fatal("the wheel did not reach the last host in the dialog")
	}
	if state.settingsScroll != outerBefore {
		t.Fatalf("the dialog list moved the settings page from %v to %v", outerBefore, state.settingsScroll)
	}

	if err := view.Scroll(gfx.ScrollEvent{X: at.X, Y: at.Y, DeltaY: -100000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(at.X, at.Y); err != nil {
		t.Fatal(err)
	}
	bar, ok := findSnapshotLabel(view.Snapshot(), "Vertical scrollbar")
	if !ok {
		t.Fatal("the long host list has no draggable scrollbar")
	}
	x := bar.Bounds.X + bar.Bounds.Width/2
	y := bar.Bounds.Y + bar.Bounds.Height/2
	if err := view.Driver().Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Move(x, y+40); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Release(x, y+40, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if painted("Host 0000") {
		t.Fatal("dragging the scrollbar left the list at its first row")
	}

	if err := view.Scroll(gfx.ScrollEvent{X: at.X, Y: at.Y, DeltaY: -100000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if _, ok := hitByID(hitAgentHostsQuery); !ok {
		t.Fatal("the dialog has no search field")
	}
	if err := view.ClickLabel(m.AgentHostsSearch); err != nil {
		t.Fatal(err)
	}
	if err := view.Type("0999"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !painted("Host 0999") || painted("Host 0000") {
		t.Fatal("the search did not filter the list to the matching host")
	}
	if len(state.agentHosts) != count || state.agentHostMode != agentHostModeAll {
		t.Fatal("the search changed the stored permissions")
	}
	if want := fmt.Sprintf(m.AgentHostsSearchCountFmt, 1, count); !painted(want) {
		t.Fatalf("the dialog does not show the search count %q", want)
	}
	if err := view.Keys("esc"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.ai.cfg.agentHostsView.query.String() != "" {
		t.Fatal("Escape did not clear the search query")
	}
	if !painted("Host 0000") {
		t.Fatal("clearing the query did not restore the full list")
	}
	if err := view.Keys("esc"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone {
		t.Fatal("Escape did not close the dialog")
	}

	if err := view.Resize(900, 1600); err != nil {
		t.Fatal(err)
	}
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{m.AgentHostsNone, m.AgentHostsFollow, m.AgentHostsAll} {
		if !painted(label) {
			t.Fatalf("the narrow host access row clipped the mode choice %q", label)
		}
	}
}
