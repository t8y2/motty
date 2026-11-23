package workbench

import (
	"testing"

	"motty/internal/remotestats"
)

// The bar's address follows the settings' rule: the default SSH port stays off
// the copy, a custom port is named, and a local shell names the account on this
// computer rather than a network address.
func TestConnectionCopyTextFollowsThePortRule(t *testing.T) {
	cases := []struct {
		name      string
		host      Host
		localUser string
		want      string
	}{
		{name: "ssh default port", host: Host{Address: "example.com", User: "deploy", Port: 22, Protocol: protocolSSH}, want: "deploy@example.com"},
		{name: "ssh custom port", host: Host{Address: "example.com", User: "deploy", Port: 2222, Protocol: protocolSSH}, want: "deploy@example.com:2222"},
		{name: "ssh no user", host: Host{Address: "example.com", Port: 2222, Protocol: protocolSSH}, want: "example.com:2222"},
		{name: "ipv6 default port", host: Host{Address: "2001:db8::1", User: "deploy", Port: 22, Protocol: protocolSSH}, want: "deploy@[2001:db8::1]"},
		{name: "ipv6 custom port", host: Host{Address: "2001:db8::1", User: "deploy", Port: 2222, Protocol: protocolSSH}, want: "deploy@[2001:db8::1]:2222"},
		{name: "local account", host: Host{ID: localHostID, Address: "this-machine"}, localUser: "Administrator", want: "Administrator@localhost"},
		{name: "local domain account", host: Host{ID: localHostID, Address: "this-machine"}, localUser: `DOMAIN\Administrator`, want: "Administrator@localhost"},
		{name: "local unnamed account", host: Host{ID: localHostID, Address: "this-machine"}, want: "localhost"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := connectionCopyText(test.host, test.localUser); got != test.want {
				t.Fatalf("copy text = %q, want %q", got, test.want)
			}
		})
	}
}

// The compact line reuses the monitor's readings, with the disk from the slow
// details worker and the latency from the existing probe.
func TestConnectionStatsLineFollowsTheMonitor(t *testing.T) {
	sample := remotestats.Sample{
		CPUPercent:            1,
		MemoryUsedBytes:       1932735283,  // 1.8 GiB
		MemoryTotalBytes:      25125558681, // 23.4 GiB
		ReceiveBytesPerSecond: 7066,        // 6.9K/s
		SendBytesPerSecond:    4506,        // 4.4K/s
	}
	details := remotestats.Details{
		CPUCount:    4,
		PrimaryDisk: "/",
		Disks:       []remotestats.Disk{{Path: "/", Used: 38556742410, Total: 207939940352, Percent: 19}},
	}
	got := connectionStatsLine(sample, details, true, "223ms")
	want := "CPU 1% (4C)  RAM 1.8/23.4G  Disk 35.9/193.7G (19%)  ↓6.9K/s ↑4.4K/s  223ms"
	if got != want {
		t.Fatalf("stats line = %q, want %q", got, want)
	}
}

func TestConnectionStatsLineMarksUnavailableReadings(t *testing.T) {
	sample := remotestats.Sample{
		CPUUnavailable:     true,
		NetworkUnavailable: true,
		MemoryUsedBytes:    1932735283,
		MemoryTotalBytes:   25125558681,
	}
	if got, want := connectionStatsLine(sample, remotestats.Details{}, false, ""), "CPU —  RAM 1.8/23.4G  Disk —  ↓— ↑—"; got != want {
		t.Fatalf("stats line = %q, want %q", got, want)
	}
}
