package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var (
	hitSnippetVarClose  = tree.ID(snippetsHits, "snippet-var-close")
	hitSnippetVarCancel = tree.ID(snippetsHits, "snippet-var-cancel")
	hitSnippetVarSubmit = tree.ID(snippetsHits, "snippet-var-submit")
	hitSnippetVarList   = tree.Sub(snippetsHits, tree.Name("snippet-var-field"))
)

type snippetPromptState struct {
	snippetIndex    int
	targetSessionID uint32
	fromPage        bool
	defs            []snippetVariableDef
	editors         []text.Editor
	slots           []ui.Box
	dialogError     string
	// list is the fields' scroll area, and preview the box the resolved
	// command is shown in; both are what the last paint laid out, so the
	// wheel handler and the tests read them between paints.
	list, preview ui.Box
	listBounds    gfx.Rect
	listScroll    *ui.ScrollState
	listSet       bool
}

// The fields step 68px (62px input and a 6px gap); the list holds five of
// them before it scrolls, so the preview and the actions stay in the dialog
// whatever the number of variables.
const (
	snippetVarFieldHeight   = 62
	snippetVarFieldGap      = 6
	snippetVarFieldStep     = snippetVarFieldHeight + snippetVarFieldGap
	snippetVarListMaxHeight = 5 * snippetVarFieldStep
)

func (state *State) openSnippetVariablesDialog(index int, targetSessionID uint32, fromPage bool) {
	if index < 0 || index >= len(state.snippets.list) {
		return
	}
	entry := state.snippets.list[index]
	defs := parseSnippetVariables(entry.Command)
	if len(defs) == 0 {
		return
	}
	editors := make([]text.Editor, len(defs))
	var cached map[string]string
	if state.snippetVarValues != nil {
		cached = state.snippetVarValues[entry.Name]
	}
	for i, def := range defs {
		if val, ok := cached[def.Name]; ok && val != "" {
			editors[i].Insert(val)
		} else if def.HasDefault {
			editors[i].Insert(def.DefaultValue)
		}
	}
	state.snippetPrompt = snippetPromptState{
		snippetIndex:    index,
		targetSessionID: targetSessionID,
		fromPage:        fromPage,
		defs:            defs,
		editors:         editors,
		slots:           make([]ui.Box, len(defs)),
	}
	state.modal = modalSnippetVariables
}

func (state *State) declareSnippetVariablesDialog(c ui.C) {
	prompt := &state.snippetPrompt
	if prompt.snippetIndex < 0 || prompt.snippetIndex >= len(state.snippets.list) {
		return
	}
	entry := state.snippets.list[prompt.snippetIndex]
	m := state.messages.Tools

	title := m.SnippetVarTitle
	description := fmt.Sprintf(m.SnippetVarDescFmt, entry.Name)

	values := make(map[string]string, len(prompt.defs))
	for i, def := range prompt.defs {
		val := prompt.editors[i].String()
		if strings.TrimSpace(val) != "" {
			values[def.Name] = val
		}
	}
	preview := previewSnippetCommand(entry.Command, values)

	errorExtra := float32(0)
	if prompt.dialogError != "" {
		errorExtra = 26
	}
	listHeight := min(float32(len(prompt.defs))*snippetVarFieldStep, snippetVarListMaxHeight)
	bodyHeight := 24 + listHeight + 22 + 60 + errorExtra + 16

	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 520, description, bodyHeight)

	initialFocus := hitSnippetVarClose
	if len(prompt.defs) > 0 {
		initialFocus = hitSnippetVarList.At(0)
	}

	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: description, Width: 520, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitSnippetVarClose, InitialFocusID: initialFocus,
	})

	dialog.Body(func() {
		state.declareSnippetVariablesDialogBody(c, preview)
	})

	dialog.Footer(func() {
		if dialogButton(c, state.messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickSnippetVariablesDialog(hitSnippetVarCancel) })
		}
		if dialogButton(c, m.SnippetVarRun, components.ButtonDefault, false).Clicked() {
			c.After(func() { state.clickSnippetVariablesDialog(hitSnippetVarSubmit) })
		}
	})
}

func (state *State) declareSnippetVariablesDialogBody(c ui.C, preview string) {
	prompt := &state.snippetPrompt
	m, pal := state.messages.Tools, state.pal

	column := ui.Column(c).Fill().PadSides(2, 4, 0, 4)
	column.Children(func() {
		ui.Text(c, m.SnippetVarHint).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18).MarginSides(0, 0, 8, 0)

		// The fields scroll once they outgrow the dialog, so a snippet with
		// many variables keeps every field, the preview and the actions
		// reachable instead of pushing them below the window. The wheel and
		// the scrollbar reach every field; keyboard focus stays with the
		// fields on screen, as in every other scrollable list of the app: a
		// control scrolled out of a clip leaves the tab ring with its hits.
		list := ui.Scroll(c).MaxHeight(snippetVarListMaxHeight).FillWidth().Scrollbars()
		list.Children(func() {
			for i := range prompt.defs {
				prompt.slots[i] = ui.Slot(c).Height(snippetVarFieldHeight).MarginSides(0, 0, snippetVarFieldGap, 0)
			}
		})
		// The inputs are painted inside the area's clip, over the slots; the
		// bounds the wheel handler tests are where the area ended up, which
		// only the laid-out draw pass knows.
		list.DrawOver(func(d *ui.DrawContext) {
			prompt.listBounds = d.Rect
			state.paintSnippetVariablesDialogFields(d.Canvas)
		})
		prompt.list, prompt.listSet = list, true
		prompt.listScroll = list.ScrollState()

		ui.Text(c, m.SnippetVarPreview).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(18).MarginSides(4, 0, 4, 0)

		prompt.preview = ui.Row(c).Fill().Pad(8).Background(pal.terminal).Radius(radius.control).Border(1, pal.line)
		prompt.preview.Children(func() {
			ui.Text(c, preview).Mono().FontSize(metrics.FontSizeS).Color(pal.text).LineHeight(16)
		})

		if prompt.dialogError != "" {
			ui.Text(c, prompt.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.danger).LineHeight(18).MarginSides(8, 0, 0, 0)
		}
	})
}

// scrollSnippetVariables gives the wheel to the fields' scroll area when the
// pointer is over it. The dialog owns the wheel over the rest of itself, as
// every modal does, so nothing behind it moves.
func (state *State) scrollSnippetVariables(event gfx.ScrollEvent) gfx.EventResult {
	prompt := &state.snippetPrompt
	if !prompt.listSet || prompt.listScroll == nil || !pointInRect(event.X, event.Y, prompt.listBounds) {
		return gfx.EventResult{Handled: true}
	}
	result := prompt.listScroll.Wheel(event)
	result.Handled = true
	return result
}

func (state *State) paintSnippetVariablesDialogFields(canvas *gfx.Canvas) {
	prompt := &state.snippetPrompt
	m, theme := state.messages.Tools, state.Theme()

	for i, def := range prompt.defs {
		if i >= len(prompt.slots) {
			break
		}
		label := def.Name
		placeholder := m.SnippetVarPlaceholder
		if def.HasDefault {
			placeholder = fmt.Sprintf(m.SnippetVarDefaultFmt, def.DefaultValue)
		}
		slotRect := prompt.slots[i].Rect()
		if slotRect.Width <= 0 || slotRect.Height <= 0 {
			continue
		}
		content := components.BeginField(canvas, theme, slotRect).Paint(label, "", "")
		id := hitSnippetVarList.At(i)
		focused := canvas.Focused() == id
		val := prompt.editors[i].String()
		components.PaintInput(canvas, theme, content, components.InputSnapshot{
			ID: id, Label: label, Value: val, Placeholder: placeholder,
			Focused: focused, PlatformEditing: focused && val != "",
		})
	}
}

func (state *State) clickSnippetVariablesDialog(id gfx.ID) {
	switch id {
	case hitSnippetVarCancel, hitSnippetVarClose:
		state.closeModal()
	case hitSnippetVarSubmit:
		state.submitSnippetVariables()
	}
}

func (state *State) submitSnippetVariables() {
	prompt := &state.snippetPrompt
	m := state.messages.Tools
	if prompt.snippetIndex < 0 || prompt.snippetIndex >= len(state.snippets.list) {
		state.closeModal()
		return
	}

	for i := range prompt.editors {
		if marked, composing := prompt.editors[i].Composition(); composing && marked.Start != marked.End {
			return
		}
	}

	values := make(map[string]string)
	for i, def := range prompt.defs {
		val := prompt.editors[i].String()
		if strings.TrimSpace(val) != "" {
			values[def.Name] = val
		} else if def.HasDefault {
			values[def.Name] = def.DefaultValue
		} else {
			prompt.dialogError = m.SnippetVarRequired
			return
		}
	}

	entry := state.snippets.list[prompt.snippetIndex]
	resolved, ok := applySnippetVariables(entry.Command, values)
	if !ok {
		prompt.dialogError = m.SnippetVarRequired
		return
	}

	if state.snippetVarValues == nil {
		state.snippetVarValues = make(map[string]map[string]string)
	}
	state.snippetVarValues[entry.Name] = values

	sessionID := prompt.targetSessionID
	fromPage := prompt.fromPage
	state.closeModal()

	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || session.term == nil {
		state.showActionWarning(m.SnippetsTitle, m.StatusSnippetNoTerminal)
		return
	}

	if state.sendResolvedSnippet(entry, resolved, session) && fromPage {
		state.ActivatePane(session.ID)
		state.activeSection = sectionTerminal
	}
}
