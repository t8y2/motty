package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/base"
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
	snippetIndex         int
	targetSessionID      uint32
	fromPage             bool
	defs                 []snippetVariableDef
	editors              []text.Editor
	fields               []ui.Field
	fieldBound           []bool
	rows                 []ui.FormFieldState
	dialogError          string
	dialogErrorSelection ui.TextSelectionState // what the user selected of dialogError, to copy it
	// list is the fields' scroll area, and preview the box the resolved
	// command is shown in; both are what the last paint laid out, so the
	// wheel handler and the tests read them between paints.
	list, preview ui.Box
	listScroll    *ui.ScrollState
	listSet       bool
	previewScroll *ui.ScrollState
	previewSet    bool
}

// The fields are one dialog row each, so the list holds five of them before it
// scrolls and the preview and the actions stay in the dialog whatever the
// number of variables.
const (
	// The preview box the body reserves; a long command wraps and scrolls
	// inside it, so the whole of it stays readable instead of running off the
	// right edge. The scroll area gets what Pad(8) and the 1px border leave.
	snippetVarPreviewHeight    = 60
	snippetVarPreviewMaxHeight = snippetVarPreviewHeight - 18
	snippetVarListRows         = 5
)

func snippetVarListMaxHeight(theme *base.Theme) float32 {
	return snippetVarListRows * dialogRowHeight(theme)
}

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
		fields:          make([]ui.Field, len(defs)),
		fieldBound:      make([]bool, len(defs)),
		rows:            make([]ui.FormFieldState, len(defs)),
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
	listHeight := min(float32(len(prompt.defs))*dialogRowHeight(c.Theme()), snippetVarListMaxHeight(c.Theme()))
	bodyHeight := 24 + listHeight + 22 + snippetVarPreviewHeight + errorExtra + 16

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
		list := ui.Scroll(c).MaxHeight(snippetVarListMaxHeight(c.Theme())).FillWidth().Scrollbars()
		list.Children(func() {
			for i, def := range prompt.defs {
				if !prompt.fieldBound[i] {
					prompt.fields[i].Bind(&prompt.editors[i])
					prompt.fieldBound[i] = true
				}
				placeholder := m.SnippetVarPlaceholder
				if def.HasDefault {
					placeholder = fmt.Sprintf(m.SnippetVarDefaultFmt, def.DefaultValue)
				}
				// A labelled form field, as the hand-painted dialog showed each
				// variable's name above its input.
				ui.FormField(c, &prompt.rows[i], def.Name).Height(dialogRowHeight(c.Theme())).FillWidth().Children(func() {
					ui.TextField(c, &prompt.fields[i], def.Name, placeholder, ui.WithID(hitSnippetVarList.At(i))).FillWidth()
				})
			}
		})
		prompt.list, prompt.listSet = list, true
		prompt.listScroll = list.ScrollState()

		ui.Text(c, m.SnippetVarPreview).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(18).MarginSides(4, 0, 4, 0)

		prompt.preview = ui.Row(c).Fill().Height(snippetVarPreviewHeight).Pad(8).Background(pal.terminal).Radius(radius.control).Border(1, pal.line)
		prompt.preview.Children(func() {
			area := ui.Scroll(c).FillWidth().MaxHeight(snippetVarPreviewMaxHeight).Scrollbars()
			area.Children(func() {
				ui.Paragraph(c, preview, 0).Mono().FontSize(metrics.FontSizeS).Color(pal.text).LineHeight(16)
			})
			prompt.previewScroll, prompt.previewSet = area.ScrollState(), true
		})

		if prompt.dialogError != "" {
			ui.TextSelectionArea(c, &prompt.dialogErrorSelection).Children(func() {
				ui.Text(c, prompt.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.danger).LineHeight(18).MarginSides(8, 0, 0, 0)
			})
		}
	})
}

// scrollSnippetVariables gives the wheel to the fields' scroll area when the
// pointer is over it. The dialog owns the wheel over the rest of itself, as
// every modal does, so nothing behind it moves.
func (state *State) scrollSnippetVariables(event gfx.ScrollEvent) gfx.EventResult {
	prompt := &state.snippetPrompt
	if prompt.previewSet && prompt.previewScroll != nil && pointInRect(event.X, event.Y, prompt.preview.Rect()) {
		result := prompt.previewScroll.Wheel(event)
		result.Handled = true
		return result
	}
	if !prompt.listSet || prompt.listScroll == nil || !pointInRect(event.X, event.Y, prompt.list.Rect()) {
		return gfx.EventResult{Handled: true}
	}
	result := prompt.listScroll.Wheel(event)
	result.Handled = true
	return result
}

func (state *State) clickSnippetVariablesDialog(id gfx.ID) {
	switch id {
	case hitSnippetVarCancel, hitSnippetVarClose:
		fromPage := state.snippetPrompt.fromPage
		state.closeModal()
		// Opened from the command bar, the dialog sat over the terminal; the
		// keyboard belongs back there once it is gone. From the snippets page
		// the user stays on that page.
		if !fromPage {
			state.returnKeyboardToTerminal()
		}
	case hitSnippetVarSubmit:
		state.submitSnippetVariables()
	}
}

// returnKeyboardToTerminal asks the next paint for the active terminal's own
// focus. It only has an effect while the terminal page is showing and no modal
// is up, which the paint enforces before it consumes the request.
func (state *State) returnKeyboardToTerminal() {
	if focus := state.activeTerminalFocusHit(); !focus.IsZero() {
		state.pendingFocus = focus
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
		if !fromPage {
			state.returnKeyboardToTerminal()
		}
		state.showActionWarning(m.SnippetsTitle, m.StatusSnippetNoTerminal)
		return
	}

	if state.sendResolvedSnippet(entry, resolved, session) {
		if fromPage {
			state.ActivatePane(session.ID)
			state.setActiveSection(sectionTerminal)
		}
		// Ask for the target terminal's focus after any pane switch, so the
		// keyboard lands on the session the command went to, not the one that
		// happened to be active when the dialog opened.
		state.returnKeyboardToTerminal()
	} else if !fromPage {
		state.returnKeyboardToTerminal()
	}
}
