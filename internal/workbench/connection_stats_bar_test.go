package workbench

import (
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/remotestats"
)

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

// The bar is always present: the address and its actions when the setting is
// off, and the six figures beside their icons when it is on. A bar too narrow
// for the figures keeps the icons and drops the values.
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

	canvas, texts := paintAt(state, panelWide)
	if !contains(texts, address) {
		t.Fatalf("the bar does not show the copyable address %q: %v", address, texts)
	}
	for _, value := range []string{"1% (4C)", "1.8/23.4G", "35.9/193.7G (19%)", "4.4K/s", "6.9K/s"} {
		if !contains(texts, value) {
			t.Fatalf("the bar does not show %q: %v", value, texts)
		}
	}
	atomic.StoreInt64(&session.rttMs, 223)
	atomic.StoreInt32(&session.rttAvailable, 1)
	_, texts = paintAt(state, panelWide)
	if !contains(texts, "223ms") {
		t.Fatalf("the bar's PING does not use the session's RTT: %v", texts)
	}

	for _, id := range []gfx.ID{
		hitConnectionStatsAddress.At(int(session.ID)),
		hitConnectionStatsCopy.At(int(session.ID)),
		hitConnectionStatsDisconnect.At(int(session.ID)),
		hitConnectionStatsRestart.At(int(session.ID)),
		hitConnectionStatsMetric.At(int(session.ID) * barMetricCount),
	} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Fatalf("the bar is missing hit %v", id)
		}
	}

	copied := ""
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	state.Click(hitConnectionStatsCopy.At(int(session.ID)))
	if copied != address {
		t.Fatalf("copy wrote %q, want %q", copied, address)
	}

	state.connectionStats = false
	_, texts = paintAt(state, panelWide)
	if contains(texts, "1% (4C)") || contains(texts, "1.8/23.4G") {
		t.Fatalf("the figures are painted while the setting is off: %v", texts)
	}
	if !contains(texts, address) {
		t.Fatalf("the always bar lost its address when the setting was off: %v", texts)
	}

	state.connectionStats = true
	canvas, texts = paintAt(state, gfx.Size{Width: 520, Height: 700})
	if contains(texts, "1% (4C)") {
		t.Fatalf("a narrow bar painted the detailed figures: %v", texts)
	}
	if contains(texts, "…") {
		t.Fatalf("a narrow bar painted an ellipsis: %v", texts)
	}
	if _, ok := hitByID(canvas, hitConnectionStatsMetric.At(int(session.ID)*barMetricCount)); !ok {
		t.Fatal("a narrow bar lost its metric icons")
	}

	// An unsupported connection still offers the six icons, with an em dash
	// value and no water.
	session.stats = nil
	_, texts = paintAt(state, panelWide)
	if !contains(texts, "--") {
		t.Fatalf("an unsupported session painted no em dash figures: %v", texts)
	}
	if contains(texts, "1% (4C)") {
		t.Fatalf("an unsupported session painted figures: %v", texts)
	}
}
