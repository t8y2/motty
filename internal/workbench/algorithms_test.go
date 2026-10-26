package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
)

func TestAHostRemembersItsAlgorithmChoiceAcrossRestartAndCatalog(t *testing.T) {
	var host Host
	if !host.applyAlgo("compatible", "") || host.algoMode() != algoCompatible || host.algorithms().IsZero() {
		t.Fatalf("compatible: %+v", host.Algo)
	}
	if !host.applyAlgo("custom", " aes256-ctr, hmac-sha2-256 ") || host.Algo.list != "aes256-ctr, hmac-sha2-256" {
		t.Fatalf("custom: %+v", host.Algo)
	}
	got := host.algorithms()
	if len(got.Ciphers) != 1 || len(got.MACs) != 1 || got.KeyExchanges != nil {
		t.Fatalf("parsed %+v", got)
	}
	mode, list := host.savedAlgo()
	if mode != "custom" || list != "aes256-ctr, hmac-sha2-256" {
		t.Fatalf("saved %q %q", mode, list)
	}
	for _, bad := range [][2]string{{"custom", ""}, {"custom", "not-an-algorithm"}, {"wide-open", ""}} {
		var damaged Host
		if damaged.applyAlgo(bad[0], bad[1]) {
			t.Errorf("%v was accepted", bad)
		}
	}
	var plain Host
	if !plain.applyAlgo("", "") || plain.Algo != nil || !plain.algorithms().IsZero() {
		t.Fatalf("default: %+v", plain.Algo)
	}
}

func TestTheHostDialogEditsAndValidatesTheAlgorithmChoice(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	state.editor.label.Insert("Old switch")
	state.editor.address.Insert("switch.example.com")
	state.Click(hitDialogAlgoCompatible)
	if state.editor.algoMode != algoCompatible {
		t.Fatal("compatible was not chosen")
	}
	state.Click(hitDialogAlgoCustom)
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.Tools.ErrAlgoRequired {
		t.Fatalf("an empty custom list: modal %d, error %q", state.modal, state.dialogError)
	}
	state.editor.algorithms.Insert("aes256-ctr, bogus-cipher")
	state.Click(hitDialogSubmit)
	if !strings.Contains(state.dialogError, "bogus-cipher") {
		t.Fatalf("error %q", state.dialogError)
	}
	state.editor.algorithms = text.Editor{}
	state.editor.algorithms.Insert("aes256-ctr")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("a valid list was refused: %q", state.dialogError)
	}
	var added Host
	for _, host := range state.hosts {
		if host.Label == "Old switch" {
			added = host
		}
	}
	if added.algoMode() != algoCustom || added.Algo.list != "aes256-ctr" {
		t.Fatalf("saved host %+v", added.Algo)
	}

	// The choice survives a restart, and reopens in the dialog as it was set.
	again := New(state.options)
	defer again.Close()
	var reloaded Host
	for _, host := range again.hosts {
		if host.Label == "Old switch" {
			reloaded = host
		}
	}
	if reloaded.algoMode() != algoCustom || reloaded.Algo.list != "aes256-ctr" {
		t.Fatalf("reloaded host %+v", reloaded.Algo)
	}
	again.editor.load(reloaded)
	if again.editor.algoMode != algoCustom || again.editor.value(fieldAlgorithms) != "aes256-ctr" {
		t.Fatalf("dialog loaded %v %q", again.editor.algoMode, again.editor.value(fieldAlgorithms))
	}
}

func TestOnlySSHHostsAskTheConnectionForAlgorithms(t *testing.T) {
	state := newTestState(t, testOptions(t))
	legacy := Host{Address: "old.example.com", Port: 22, Algo: &hostAlgo{mode: algoCompatible}}
	jump := Host{Address: "bastion.example.com", Port: 2222, Algo: &hostAlgo{mode: algoCustom, list: "aes256-ctr"}}
	telnet := Host{Address: "tel.example.com", Port: 23, Protocol: protocolTelnet, Algo: &hostAlgo{mode: algoCompatible}}
	got := state.algorithmsFor(legacy, []Host{jump, telnet})
	if len(got) != 2 || got[sshconn.EndpointKey("old.example.com", 22)].IsZero() || len(got[sshconn.EndpointKey("bastion.example.com", 2222)].Ciphers) != 1 {
		t.Fatalf("map %+v", got)
	}
	if state.algorithmsFor(Host{Address: "plain.example.com", Port: 22}, nil) != nil {
		t.Fatal("a default host adds an entry")
	}
}

func TestANegotiatedWeakAlgorithmIsSaidAfterConnecting(t *testing.T) {
	world, options := newWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhaseConnected
		s.Negotiated = sshconn.Negotiated{KeyExchange: "diffie-hellman-group14-sha1", HostKey: "ssh-rsa", Cipher: "aes128-cbc", MAC: "hmac-sha2-256"}
	})
	state.Wake()
	if !strings.Contains(state.status, "aes128-cbc") || !strings.Contains(state.status, "ssh-rsa") {
		t.Fatalf("status %q", state.status)
	}
}

// The Advanced section keeps the rarely used SSH settings out of the way, but a
// host that already uses one must show it, and so must a value that is refused.
func TestAdvancedSectionOpensWhereItMatters(t *testing.T) {
	state := newTestState(t, testOptions(t))
	clickHostAction(state, hitAddHost)
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitDialogOTP); ok {
		t.Fatal("a new host's Advanced section starts open")
	}

	state.editor.load(Host{Label: "plain", Address: "plain.example", Port: 22, ForwardX11: true})
	if !state.editor.advancedOpen {
		t.Fatal("a host that forwards X11 opens with Advanced closed")
	}
	state.editor.load(Host{Label: "plain", Address: "plain.example", Port: 22})
	if state.editor.advancedOpen {
		t.Fatal("a host with default advanced settings opens with Advanced open")
	}

	state.editor.reset()
	state.editor.label.Insert("custom")
	state.editor.address.Insert("custom.example")
	state.editor.algoMode = algoCustom
	state.submitHostDialog()
	if state.dialogError == "" || !state.editor.advancedOpen {
		t.Fatalf("an empty custom algorithm list: error %q, advanced open %v", state.dialogError, state.editor.advancedOpen)
	}
}
