package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"motty/internal/i18n"
	"motty/internal/sshconn"
)

// Which ssh-agent connections offer their keys to. The system's agent is the
// default; a password manager that runs its own (1Password, Bitwarden) is a
// choice, because a program started from the Dock or the Start menu does not see
// the SSH_AUTH_SOCK a terminal was given.

type agentKind uint8

const (
	agentKindSystem agentKind = iota
	agentKindOnePassword
	agentKindBitwarden
	agentKindCustom
)

type agentChoice struct {
	kind agentKind
	// path is the socket or named pipe of a custom agent.
	path string
}

type agentDescriptionCache struct {
	choice      agentChoice
	language    i18n.Language
	description string
}

const (
	savedAgentOnePassword = "1password"
	savedAgentBitwarden   = "bitwarden"
	savedAgentCustom      = "custom"
)

func (choice agentChoice) saved() (kind, path string) {
	switch choice.kind {
	case agentKindOnePassword:
		return savedAgentOnePassword, ""
	case agentKindBitwarden:
		return savedAgentBitwarden, ""
	case agentKindCustom:
		return savedAgentCustom, choice.path
	}
	return "", ""
}

func agentChoiceFromSaved(kind, path string) agentChoice {
	switch kind {
	case savedAgentOnePassword:
		return agentChoice{kind: agentKindOnePassword}
	case savedAgentBitwarden:
		return agentChoice{kind: agentKindBitwarden}
	case savedAgentCustom:
		if path = strings.TrimSpace(path); path != "" {
			return agentChoice{kind: agentKindCustom, path: path}
		}
	}
	return agentChoice{}
}

// address is where the chosen agent listens, or "" for the system's.
func (choice agentChoice) address(goos, home string, exists func(string) bool) string {
	switch choice.kind {
	case agentKindOnePassword:
		return sshconn.AgentOnePassword.Address(goos, home, exists)
	case agentKindBitwarden:
		return sshconn.AgentBitwarden.Address(goos, home, exists)
	case agentKindCustom:
		path := strings.TrimSpace(choice.path)
		if rest, ok := strings.CutPrefix(path, "~/"); ok && home != "" {
			return filepath.Join(home, rest)
		}
		return path
	}
	return ""
}

// resolved is address for this computer.
func (choice agentChoice) resolved() string {
	home, _ := os.UserHomeDir()
	return choice.address(runtime.GOOS, home, func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	})
}

// settingsAgentKindChoices are the choices the agent row offers. The chosen
// agent's path is the custom choice's own state, so the choice binds the kind.
var settingsAgentKindChoices = [...]agentKind{agentKindSystem, agentKindOnePassword, agentKindBitwarden, agentKindCustom}

func (state *State) settingsAgentKindLabel(kind agentKind) string {
	switch kind {
	case agentKindOnePassword:
		return "1Password"
	case agentKindBitwarden:
		return "Bitwarden"
	case agentKindCustom:
		return state.messages.SettingsSSHAgentCustom
	}
	return state.messages.SettingsSSHAgentSystem
}

func (state *State) paintSSHAgentRow(page *settingsPage) {
	m := state.messages
	kind := state.sshAgent.kind
	settingsChoiceRow(page, m.SettingsSSHAgent, state.sshAgentRowDescription(), &kind, settingsAgentKindChoices[:], state.settingsAgentKindLabel,
		func() {
			if kind == agentKindCustom {
				state.openSSHAgentPathForm()
				return
			}
			state.setSSHAgent(agentChoice{kind: kind})
			state.save()
		},
		func() {
			if kind == agentKindCustom {
				state.openSSHAgentPathForm()
			}
		})
}

func (state *State) sshAgentRowDescription() string {
	cache, m := &state.agentSetting, state.messages
	if cache.description == "" || cache.choice != state.sshAgent || cache.language != state.language {
		cache.choice, cache.language = state.sshAgent, state.language
		cache.description = m.SettingsSSHAgentDescription
		if address := state.sshAgent.resolved(); address != "" {
			cache.description = fmt.Sprintf(m.SettingsSSHAgentUsingFmt, address)
		}
	}
	return cache.description
}

// setSSHAgent applies a choice to the connections made from now on; the ones
// open keep the agent they were given.
func (state *State) setSSHAgent(choice agentChoice) bool {
	if state.sshAgent == choice {
		return false
	}
	state.sshAgent = choice
	return true
}

func (state *State) openSSHAgentPathForm() {
	m := state.messages
	form := state.openForm(formDialog{
		title: m.SettingsSSHAgentCustomTitle, description: m.SettingsSSHAgentCustomDescription, submit: m.KeySavePassphraseSubmit,
		run: func(state *State, form *formDialog) string {
			path := strings.TrimSpace(form.value(0))
			if path == "" {
				return state.messages.SettingsSSHAgentPathRequired
			}
			if state.setSSHAgent(agentChoice{kind: agentKindCustom, path: path}) {
				state.save()
			}
			return ""
		},
	}, formField{label: m.SettingsSSHAgentPathLabel, placeholder: m.SettingsSSHAgentPathPlaceholder})
	if state.sshAgent.kind == agentKindCustom {
		form.set(0, state.sshAgent.path)
	}
}
