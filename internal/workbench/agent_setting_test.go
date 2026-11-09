package workbench

import (
	"path/filepath"
	"testing"
)

func TestAgentChoiceIsSavedAndResolved(t *testing.T) {
	home := filepath.FromSlash("/home/u")
	none := func(string) bool { return false }
	for _, test := range []struct {
		choice agentChoice
		want   string
	}{
		{agentChoice{}, ""},
		{agentChoice{kind: agentKindOnePassword}, filepath.Join(home, ".1password", "agent.sock")},
		{agentChoice{kind: agentKindCustom, path: " ~/run/agent.sock "}, filepath.Join(home, "run", "agent.sock")},
		{agentChoice{kind: agentKindCustom, path: `\\.\pipe\openssh-ssh-agent`}, `\\.\pipe\openssh-ssh-agent`},
	} {
		if got := test.choice.address("linux", home, none); got != test.want {
			t.Errorf("%+v resolves to %q, want %q", test.choice, got, test.want)
		}
		kind, path := test.choice.saved()
		if back := agentChoiceFromSaved(kind, path); back.kind != test.choice.kind {
			t.Errorf("%+v came back as %+v", test.choice, back)
		}
	}
	if choice := agentChoiceFromSaved(savedAgentCustom, "  "); choice != (agentChoice{}) {
		t.Errorf("a custom agent without a path = %+v, want the system's", choice)
	}
}

func TestAgentSettingReachesConnectionsAndSurvivesARestart(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsConnection)

	clickSettingsChoice(t, state, state.settingsAgentKindLabel(agentKindBitwarden))
	if state.sshAgent.kind != agentKindBitwarden {
		t.Fatal("the Bitwarden choice was not taken")
	}
	clickSettingsChoice(t, state, state.settingsAgentKindLabel(agentKindCustom))
	if state.modal != modalForm {
		t.Fatal("Custom did not ask for a path")
	}
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.dialogError != state.messages.SettingsSSHAgentPathRequired {
		t.Fatalf("an empty path was accepted: modal %d, %q", state.modal, state.form.dialogError)
	}
	state.form.set(0, "/run/manager/agent.sock")
	state.Click(hitFormSubmit)
	if state.modal != modalNone || state.sshAgent != (agentChoice{kind: agentKindCustom, path: "/run/manager/agent.sock"}) {
		t.Fatalf("custom agent = %+v, modal %d", state.sshAgent, state.modal)
	}

	state.activeSection = sectionHosts
	connect(t, state, world)
	if got := world.configs[len(world.configs)-1].AgentAddress; got != "/run/manager/agent.sock" {
		t.Fatalf("the connection was given agent %q", got)
	}

	restarted := newTestState(t, options)
	if restarted.sshAgent != state.sshAgent {
		t.Fatalf("after a restart the agent is %+v", restarted.sshAgent)
	}

	state.Click(hitSettings)
	state.selectSettingsCategory(settingsConnection)
	clickSettingsChoice(t, state, state.settingsAgentKindLabel(agentKindSystem))
	if state.sshAgent != (agentChoice{}) {
		t.Fatal("System did not return to the default")
	}
}
