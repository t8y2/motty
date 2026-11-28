package workbench

import (
	"github.com/vivaui/viva/components"
	"motty/internal/workbench/icons"
)

func (s *State) monitorPageOptions() [4]components.TabOption {
	m := s.messages.Monitor
	return [4]components.TabOption{{ID: hitMonitorOverview, Label: m.Overview, Icon: icons.Gauge}, {ID: hitMonitorProcessPage, Label: m.ProcessSection, Icon: icons.Process}, {ID: hitMonitorDiskPage, Label: m.Disks, Icon: icons.HardDrive}, {ID: hitMonitorNetworkPage, Label: m.Network, Icon: icons.Wifi}}
}
