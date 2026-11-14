package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/remotestats"
)

// The bar names the address it copies and repeats the side monitor's reading.
// A bar too narrow for the figures shows the ellipsis instead of half a number.
// A local terminal names this computer's account, not the machine's name.
func TestConnectionStatsBarFollowsTheLocalAccount(t *testing.T) {
	skipWithoutUnixShell(t)
	state := newTestState(t, testOptions(t))
	session := state.openLocalTerminal()
	closeShells(t, state)
	state.Wake()
	if session != nil && session.term != nil {
		session.term.live = true
	}
	if session == nil || session.term == nil || !state.monitorOffered(session) {
		t.Fatalf("the local shell is not monitorable: %+v", session)
	}
	state.connectionStats = true
	session.stats = &remoteStatsView{state: remoteStatsReady, panel: monitorView{}}
	address := state.connectionStatsAddress(session)
	if got, want := address, localAccountName(systemLogin())+"@localhost"; got != want {
		t.Fatalf("local address = %q, want %q", got, want)
	}
	_, texts := paintAt(state, panelWide)
	if !contains(texts, address) {
		t.Fatalf("the local bar does not show %q: %v", address, texts)
	}
}

func TestConnectionStatsBarShowsTheCopyableAddressAndReading(t *testing.T) {
	state, _ := panelState(t, nil)
	session := &state.sessions[0]
	sample := remotestats.Sample{
		CPUPercent:            1,
		MemoryUsedBytes:       1932735283,
		MemoryTotalBytes:      25125558681,
		ReceiveBytesPerSecond: 7066,
		SendBytesPerSecond:    4506,
	}
	details := remotestats.Details{
		CPUCount:    4,
		PrimaryDisk: "/",
		Disks:       []remotestats.Disk{{Path: "/", Used: 38556742410, Total: 207939940352, Percent: 19}},
	}
	state.connectionStats = true
	session.stats = &remoteStatsView{
		state: remoteStatsReady,
		panel: monitorView{sample: sample, data: details, haveDetails: true},
	}
	address := state.connectionStatsAddress(session)
	reading := connectionStatsLine(sample, details, true, "")

	_, texts := paintAt(state, panelWide)
	if !contains(texts, address) {
		t.Fatalf("the bar does not show the copyable address %q: %v", address, texts)
	}
	if !contains(texts, reading) {
		t.Fatalf("the bar does not show the reading %q: %v", reading, texts)
	}

	state.connectionStats = false
	_, texts = paintAt(state, panelWide)
	if contains(texts, reading) {
		t.Fatalf("the reading is painted while the setting is off: %v", texts)
	}

	state.connectionStats = true
	_, texts = paintAt(state, gfx.Size{Width: 520, Height: 700})
	if contains(texts, "CPU 1%") {
		t.Fatalf("a narrow bar painted the detailed reading: %v", texts)
	}
	if !contains(texts, "…") {
		t.Fatalf("a narrow bar did not paint the ellipsis: %v", texts)
	}

	copied := ""
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	state.Click(hitConnectionStatsAddress.At(int(session.ID)))
	if copied != address {
		t.Fatalf("copy wrote %q, want %q", copied, address)
	}
}
