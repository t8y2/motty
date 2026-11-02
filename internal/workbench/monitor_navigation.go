package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
)

func (s *State) monitorPageOptions() [3]components.TabOption {
	m := s.messages.Monitor
	return [3]components.TabOption{{ID: hitMonitorOverview, Label: m.Overview, Icon: icons.Activity}, {ID: hitMonitorDiskPage, Label: m.Disks, Icon: icons.HardDrive}, {ID: hitMonitorNetworkPage, Label: m.Network, Icon: icons.Wifi}}
}

func (s *State) paintMonitorNavigation(c *ui.Canvas, p *monitorView, b ui.Rect) {
	options := s.monitorPageOptions()
	theme := s.Theme().WithFontSize(12)
	components.BeginTabs(c, theme, b, options[:], options[p.page].ID).Paint()
}
