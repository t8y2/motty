package workbench

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"motty/internal/term"
)

// The snippet picker also takes a command typed on the spot and sends it to the
// current terminal, to every connected one, or to the ones the user ticks. It is
// the picker's own form of the one-off broadcast: broadcast input follows the
// keys typed into one tab's panes, while this sends one line, once, to whichever
// terminals were named.

var (
	hitSnippetSendInput  = tree.ID(commandPaletteHits, "send-input")
	hitSnippetSendButton = tree.ID(commandPaletteHits, "send-button")
	hitSendRun           = tree.ID(commandPaletteHits, "send-run")
	hitSendPickList      = tree.Sub(commandPaletteHits, tree.Name("send-target-session")) // + session id
)

type sendTarget uint8

const (
	sendCurrent sendTarget = iota
	sendAll
	sendChosen
)

// snippetSender is what the picker's send form remembers while the app runs.
// The command is cleared whenever the picker opens; the target, the Enter
// choice and the ticked terminals stay, so a second command goes where the
// first one did.
type snippetSender struct {
	input  text.Editor
	target sendTarget
	// noRun is the inverse of "press Enter", so the zero value runs the command.
	noRun  bool
	chosen []uint32
}

func (sender *snippetSender) runs() bool { return !sender.noRun }

func (sender *snippetSender) isChosen(id uint32) bool { return slices.Contains(sender.chosen, id) }

// sendTargetChoices are the form's targets, in the order they are shown.
var sendTargetChoices = [...]sendTarget{sendCurrent, sendAll, sendChosen}

func (state *State) sendTargetLabel(target sendTarget) string {
	m := state.messages.Tools
	switch target {
	case sendAll:
		return m.SendTargetAll
	case sendChosen:
		return m.SendTargetChosen
	}
	return m.SendTargetCurrent
}

// canTakeCommand reports whether a command typed now would reach this session.
func canTakeCommand(session *Session) bool {
	return session.term != nil && session.term.shell != nil && !session.term.ended
}

// sendTargets returns the sessions the form would send to now.
func (state *State) sendTargets() []*Session {
	sender := &state.snippetSend
	var targets []*Session
	switch sender.target {
	case sendCurrent:
		if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && canTakeCommand(session) {
			targets = append(targets, session)
		}
	default:
		for index := range state.sessions {
			session := &state.sessions[index]
			if canTakeCommand(session) && (sender.target == sendAll || sender.isChosen(session.ID)) {
				targets = append(targets, session)
			}
		}
	}
	return targets
}

// applySendTarget switches the picker's list for the target the page's choice
// already wrote: the choice of terminals lists the terminals themselves.
func (state *State) applySendTarget() {
	palette := &state.commandPalette
	palette.search = text.Editor{}
	palette.filtered = false
	state.rebuildCommandPalette()
}

// toggleSendSession ticks or unticks one terminal in the list of choices, and
// keeps the list where it was.
func (state *State) toggleSendSession(id uint32) {
	sender := &state.snippetSend
	if sender.isChosen(id) {
		sender.chosen = slices.DeleteFunc(sender.chosen, func(chosen uint32) bool { return chosen == id })
	} else {
		sender.chosen = append(sender.chosen, id)
	}
	palette := &state.commandPalette
	scroll, target := palette.scroll, hitSendPickList.At(int(id))
	state.rebuildCommandPalette()
	for row, index := range palette.visible {
		if palette.entries[index].target == target {
			palette.selected = row
			break
		}
	}
	palette.scroll = min(max(scroll, 0), palette.maxScroll())
}

// rebuildSendTargetPicker lists the terminals a command can be sent to, each
// with a tick when chosen. Chosen terminals that are gone are forgotten here.
func (state *State) rebuildSendTargetPicker() {
	palette, sender := &state.commandPalette, &state.snippetSend
	sender.chosen = slices.DeleteFunc(sender.chosen, func(id uint32) bool {
		session, ok := state.sessionByIDPointer(id)
		return !ok || !canTakeCommand(session)
	})
	for index := range state.sessions {
		session := &state.sessions[index]
		if !canTakeCommand(session) {
			continue
		}
		palette.addDetailed(session.Title, state.sessionAddress(session.host), hitSendPickList.At(int(session.ID)))
		last := &palette.entries[len(palette.entries)-1]
		last.checkable, last.checked = true, sender.isChosen(session.ID)
	}
	palette.rawQuery = ""
	palette.query = ""
	palette.filtered = false
	state.refreshCommandPalette()
}

// submitSend types the form's command into the chosen terminals.
func (state *State) submitSend() {
	m := state.messages.Tools
	sender := &state.snippetSend
	command := sender.input.String()
	if composing := func() bool { marked, ok := sender.input.Composition(); return ok && marked.Start != marked.End }; composing() || strings.TrimSpace(command) == "" {
		return
	}
	targets := state.sendTargets()
	if len(targets) == 0 {
		if sender.target == sendChosen && len(sender.chosen) == 0 {
			state.showActionWarning(m.SnippetsTitle, m.StatusSendChooseNone)
		} else {
			state.showActionWarning(m.SnippetsTitle, m.StatusSendNoTarget)
		}
		return
	}
	sent := 0
	for _, session := range targets {
		input := term.EncodePaste(command, session.term.screen.Modes().BracketedPaste && strings.Contains(command, "\n"))
		if sender.runs() {
			input = append(input, '\r')
		}
		// The current terminal types like the user does, so a broadcasting tab
		// repeats the command in its panes; the others are named one by one.
		var accepted bool
		if sender.target == sendCurrent {
			accepted = state.typed(session, input)
		} else {
			accepted = session.term.send(input)
		}
		if accepted {
			sent++
		}
	}
	if sent == 0 {
		state.showActionWarning(m.SnippetsTitle, m.StatusSnippetNotConnected)
		return
	}
	sender.input = text.Editor{}
	state.closeCommandPalette()
	state.status = fmt.Sprintf(m.StatusSendSentFmt, sent)
}

// clickSnippetSend handles the controls of the send form.
func (state *State) clickSnippetSend(id gfx.ID) bool {
	if !state.commandPalette.snippets {
		return false
	}
	switch id {
	case hitSendRun:
		state.snippetSend.noRun = !state.snippetSend.noRun
	case hitSnippetSendButton:
		state.submitSend()
	case hitSnippetSendInput:
	default:
		return false
	}
	return true
}

// paintSnippetSend paints the send form's fields into the boxes the palette
// declared. The target switch is a page Choice declared with the body.
func (state *State) paintSnippetSend(canvas *gfx.Canvas, input, button, run ui.Box) {
	m, theme := state.messages.Tools, state.Theme()
	sender := &state.snippetSend
	value := sender.input.String()
	focused := canvas.Focused() == hitSnippetSendInput
	components.PaintInput(canvas, theme, input.Rect(), components.InputSnapshot{
		ID: hitSnippetSendInput, Label: m.SendCommandLabel, Value: value,
		Placeholder: m.SendCommandPlaceholder, Focused: focused, PlatformEditing: focused && value != "",
	})
	empty := strings.TrimSpace(value) == ""
	components.BeginButtonRow(canvas, theme, button.Rect()).Paint(components.ButtonAction{
		ID: hitSnippetSendButton, Label: m.SendCommandButton, Disabled: empty,
	})
	state.paintDialogCheckbox(canvas, hitSendRun, m.SendRun, sender.runs(), run)
}
