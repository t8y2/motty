package workbench

import "testing"

// The switch is a saved preference, so a restart keeps the bars.
func TestConnectionStatsSettingPersists(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.setConnectionStats(true)
	state.Close()

	again := newTestState(t, options)
	t.Cleanup(again.Close)
	if !again.connectionStats {
		t.Fatal("the connection setting did not survive a restart")
	}
}

// The connection setting is the switch for every live terminal's stats: turning
// it on starts the same workers the side monitor uses, and turning it off stops
// them when the side monitor is not showing.
func TestConnectionStatsSettingControlsTheMonitor(t *testing.T) {
	state, _ := panelState(t, nil)
	state.openSettings()
	state.selectSettingsCategory(settingsConnection)
	if state.connectionStats {
		t.Fatal("resource stats start on by default")
	}
	state.Click(hitSettingsConnectionStatsToggle)
	if !state.connectionStats {
		t.Fatal("the connection setting did not turn on")
	}
	session := &state.sessions[0]
	if session.stats == nil || session.stats.worker == nil {
		t.Fatalf("the terminal has no monitor worker: %+v", session.stats)
	}
	state.Click(hitSettingsConnectionStatsToggle)
	if state.connectionStats {
		t.Fatal("the connection setting did not turn off")
	}
	if session.stats != nil {
		t.Fatal("the monitor kept running after the setting was turned off")
	}
}
