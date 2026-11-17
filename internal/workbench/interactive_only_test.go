package workbench

import (
	"testing"
	"time"
)

// A host kept to its interactive session is off until the user or a refusing
// server turns it on, and the choice survives a restart: a gateway that permits
// only an interactive login must not be asked for probes again.
func TestInteractiveOnlyIsOffUntilSwitchedOnAndSurvivesARestart(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	state.editor.label.Insert("Gateway")
	state.editor.address.Insert("gateway.example.com")
	state.Click(hitDialogSubmit)
	find := func(s *State) Host {
		for _, host := range s.hosts {
			if host.Label == "Gateway" {
				return host
			}
		}
		t.Fatal("host missing")
		return Host{}
	}
	if find(state).InteractiveOnly {
		t.Fatal("a new host was confined to its interactive session")
	}

	state.selectedHostID = find(state).ID
	state.openEditHost()
	state.Click(hitDialogInteractiveOnly)
	if !state.editor.interactiveOnly {
		t.Fatal("the switch did not turn on")
	}
	state.Click(hitDialogSubmit)
	if !find(state).InteractiveOnly {
		t.Fatal("the choice was not saved")
	}
	again := New(state.options)
	defer again.Close()
	if !find(again).InteractiveOnly {
		t.Fatal("the choice did not survive a restart")
	}
}

func TestInteractiveOnlyHostsUseTCPKeepAlive(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.restoreSettings(savedState{
		ConnectionKeepAliveSeconds: 30,
		Hosts: []savedHost{
			{ID: "gateway", Label: "Gateway", Address: "gateway.example.com", User: "gateway", Port: 22, savedTransport: savedTransport{InteractiveOnly: true}},
			{ID: "target", Label: "Target", Address: "target.example.com", User: "deploy", Port: 22},
		},
		Proxies: []savedProxy{{
			TargetID: "target", JumpID: "gateway",
			TargetAddress: "target.example.com", TargetUser: "deploy", TargetPort: 22,
			JumpAddress: "gateway.example.com", JumpUser: "gateway", JumpPort: 22,
		}},
	})
	gateway := mustHostLabel(t, state, "Gateway")
	direct, _, valid := state.hostCatalog.sshConfigFor(gateway, gateway, connectEnv{})
	if !valid || direct.KeepAlive.Interval != 30*time.Second || !direct.KeepAlive.UseTCP {
		t.Fatalf("direct gateway keepalive = %+v, valid=%v", direct.KeepAlive, valid)
	}
	target := mustHostLabel(t, state, "Target")
	through, _, valid := state.hostCatalog.sshConfigFor(target, target, connectEnv{})
	if !valid || through.KeepAlive.UseTCP || len(through.ProxyJumps) != 1 {
		t.Fatalf("target connection = %+v, valid=%v", through, valid)
	}
	jump := through.EndpointKeepAlive[through.ProxyJumps[0]]
	if jump.Interval != 30*time.Second || !jump.UseTCP {
		t.Fatalf("jump host keepalive = %+v", jump)
	}
}
