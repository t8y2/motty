package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
	"slices"
	"strings"
)

var (
	hitTerminalSidebars        = tree.ID(settingsHits, "terminal-sidebars")
	hitTerminalSidebarsDismiss = tree.ID(settingsHits, "terminal-sidebars-dismiss")
	hitTerminalSidebarItems    = tree.Sub(settingsHits, tree.Name("terminal-sidebar-item"))
)
var terminalSidebarKeys = [...]string{"off", "ai", "notes", "monitor", "sftp"}

// An explicit empty slice means Off; nil is the legacy settings format.
func cleanTerminalSidebars(values []string) []string {
	result := make([]string, 0, 2)
	if slices.Contains(values, "off") {
		return result
	}
	for _, value := range values {
		if slices.Contains(terminalSidebarKeys[1:], value) && !slices.Contains(result, value) && len(result) < 2 {
			result = append(result, value)
		}
	}
	return result
}
func (s *State) restoreTerminalSidebars(values []string, legacy bool) {
	if values == nil && legacy {
		values = []string{"sftp"}
	}
	s.terminalSidebars = cleanTerminalSidebars(values)
	s.sftpPanelAutoOpen = slices.Contains(s.terminalSidebars, "sftp")
}
func (s *State) savedTerminalSidebars() []string {
	values := cleanTerminalSidebars(s.terminalSidebars)
	// Preserve callers of the legacy switch as well as the serialized mirror.
	if s.sftpPanelAutoOpen && !slices.Contains(values, "sftp") {
		if len(values) == 2 {
			values = values[1:]
		}
		values = append(values, "sftp")
	}
	if len(values) == 0 {
		return []string{"off"}
	}
	return values
}

func (s *State) toggleTerminalSidebar(key string) {
	values := cleanTerminalSidebars(s.terminalSidebars)
	if s.terminalSidebars == nil && s.sftpPanelAutoOpen {
		values = []string{"sftp"}
	}
	if key == "off" {
		values = []string{}
	} else if slices.Contains(values, key) {
		values = slices.DeleteFunc(values, func(v string) bool { return v == key })
	} else if slices.Contains(terminalSidebarKeys[1:], key) {
		// A third choice replaces the oldest selection.
		if len(values) == 2 {
			values = values[1:]
		}
		values = append(values, key)
	}
	s.terminalSidebars = values
	s.sftpPanelAutoOpen = slices.Contains(values, "sftp")
}

// AI conversation state remains window-wide. Only visibility of newly created
// workspaces is overridden; existing tabs and section pages retain their state.
// Restored decks deliberately do not acquire these ephemeral overrides.
func (s *State) initializeTerminalSidebars(id layoutcore.WorkspaceID) {
	if s.terminalSidebars == nil {
		return // Legacy in-memory callers have not opted into startup defaults.
	}
	if s.startupAIVisibility == nil {
		s.startupAIVisibility = make(map[layoutcore.WorkspaceID]bool)
	}
	// Record even defaults matching the global state so later toggles cannot
	// leak between newly created workspaces.
	s.startupAIVisibility[id] = slices.Contains(s.terminalSidebars, "ai")
	if slices.Contains(s.terminalSidebars, "notes") {
		s.noteSidebarFor(id).open = true
	}
	c, _ := s.deck.Context(id)
	c.MonitorOpen = slices.Contains(s.terminalSidebars, "monitor")
	_ = s.deck.SetContext(id, c)
	// Apply the existing coexistence policy, including windows not yet measured:
	// monitor wins rather than creating two drawers.
	if c.MonitorOpen && !s.monitorAndAICanCoexist(id, s.viewport) {
		s.startupAIVisibility[id] = false
	}
}
func (s *State) terminalSidebarsSettingVisible() bool {
	return s.activeSection == sectionSettings && s.settingsCategory == settingsTerminal && !s.settingsSearching()
}
func (s *State) paintTerminalSidebarSelect(canvas *gfx.Canvas) {
	p, m := &s.terminalSidebarPicker, s.messages
	b := p.bounds
	if !s.terminalSidebarsSettingVisible() || s.modal != modalNone || b.Width <= 0 || b.Y < windowBarHeight || b.Y+b.Height > s.viewport.Height {
		p.open = false
		return
	}
	labels := m.SettingsTerminalSidebarChoices
	var selected []string
	for i, key := range terminalSidebarKeys[1:] {
		if slices.Contains(s.terminalSidebars, key) {
			selected = append(selected, labels[i+1])
		}
	}
	value := labels[0]
	if len(selected) > 0 {
		value = strings.Join(selected, m.SettingsStartupJoiner)
	}
	below := max(s.viewport.Height-8-b.Y-b.Height-4, 0)
	above := max(b.Y-windowBarHeight-8, 0)
	height := min(float32(168), max(above, below))
	content := gfx.Rect{X: b.X, Y: b.Y + b.Height + 4, Width: b.Width, Height: height}
	if below < height {
		content.Y = b.Y - height - 4
	}
	control := components.BeginSelect(canvas, s.Theme(), b, components.SelectOptions{
		TriggerID: hitTerminalSidebars, Label: m.SettingsTerminalSidebars, Value: value,
		Open: p.open, Content: content, Layer: popupLayer, Scope: hitTerminalSidebarsDismiss,
		DismissID: hitTerminalSidebarsDismiss, Viewport: s.windowRect(), Scroll: s.selectScroll(p.open, hitTerminalSidebarsDismiss),
	})
	defer control.End()
	if p.open {
		s.menuPlaced(control.ContentBounds())
		for i, key := range terminalSidebarKeys {
			on := slices.Contains(s.terminalSidebars, key) || i == 0 && len(s.terminalSidebars) == 0
			control.AddItem(components.SelectItem{ID: hitTerminalSidebarItems.At(i), Label: labels[i], Selected: on})
		}
	}
}
func (s *State) clickTerminalSidebars(id gfx.ID) bool {
	if !s.terminalSidebarsSettingVisible() {
		return false
	}
	switch id {
	case hitTerminalSidebars:
		s.terminalSidebarPicker.open = !s.terminalSidebarPicker.open
		return true
	case hitTerminalSidebarsDismiss:
		s.terminalSidebarPicker.open = false
		return true
	}
	if i, ok := id.In(hitTerminalSidebarItems); ok && i >= 0 && i < len(terminalSidebarKeys) {
		s.toggleTerminalSidebar(terminalSidebarKeys[i])
		s.save()
		return true
	}
	return false
}
