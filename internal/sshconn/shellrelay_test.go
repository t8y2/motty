package sshconn

import (
	"strings"
	"testing"
)

func TestARelayIsRefusedForAnEndpointNoHopReaches(t *testing.T) {
	relay := Relay{Command: "nc %h %p"}
	target := Endpoint{Host: "target.example", Port: 22, User: "u"}
	first := Endpoint{Host: "first.example", Port: 22, User: "u"}
	for name, config := range map[string]Config{
		"a direct target": {Host: target.Host, Port: 22, User: "u", Relays: map[Endpoint]Relay{target: relay}},
		"the first jump":  {Host: target.Host, Port: 22, User: "u", ProxyJumps: []Endpoint{first}, Relays: map[Endpoint]Relay{first: relay}},
	} {
		if err := config.validate(); err == nil || !strings.Contains(err.Error(), "not reached through a jump host") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	reached := Config{Host: target.Host, Port: 22, User: "u", ProxyJumps: []Endpoint{first}, Relays: map[Endpoint]Relay{target: relay}}
	if err := reached.validate(); err != nil {
		t.Fatalf("a relay to the target through a jump host: %v", err)
	}
}

func TestARelayNeedsAUsableCommandAndAOneLinePrelude(t *testing.T) {
	for name, relay := range map[string]Relay{
		"no command":       {},
		"two lines":        {Prelude: "sudo -i\nrm -rf /", Command: "nc %h %p"},
		"a control byte":   {Prelude: "sudo \x03", Command: "nc %h %p"},
		"an unknown token": {Command: "nc %h %q"},
	} {
		if CheckRelay(relay) == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := CheckRelay(Relay{Prelude: "sudo -i", Command: "nc %h %p"}); err != nil {
		t.Fatal(err)
	}
}
