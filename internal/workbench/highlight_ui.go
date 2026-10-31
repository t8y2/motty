package workbench

import (
	"errors"
	"fmt"
	"os"
	"regexp/syntax"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/highlight"
)

// Keyword highlighting. The highlight package finds the words; this file keeps
// the user's rules, the built-in set, the dialog that adds a rule and the
// settings group that lists them.

// highlightRule is a rule the user wrote.
type highlightRule struct {
	Pattern       string
	Regex         bool
	CaseSensitive bool
	Slot          highlight.Slot
}

// maxCustomHighlightRules leaves room for the built-in rules under the package's
// own limit.
const maxCustomHighlightRules = highlight.MaxRules - 8

// highlightUiHits is the place of the controls this file paints.
var highlightUiHits = tree.Feature("highlight-ui")

var (
	hitSettingsHighlightToggle = tree.ID(highlightUiHits, "settings-highlight")
	hitSettingsHighlightAdd    = tree.ID(highlightUiHits, "settings-highlight-add")
	hitSettingsHighlightImport = tree.ID(highlightUiHits, "settings-highlight-import")
	hitHighlightPattern        = tree.ID(highlightUiHits, "highlight-pattern")
	hitHighlightRegex          = tree.ID(highlightUiHits, "highlight-regex")
	hitHighlightCase           = tree.ID(highlightUiHits, "highlight-case")
	hitHighlightSubmit         = tree.ID(highlightUiHits, "highlight-submit")
	hitHighlightCancel         = tree.ID(highlightUiHits, "highlight-cancel")
	hitHighlightClose          = tree.ID(highlightUiHits, "highlight-close")
	hitHighlightDismiss        = tree.ID(highlightUiHits, "highlight-dismiss")
)

var (
	hitHighlightColorList  = tree.Sub(highlightUiHits, tree.Name("highlight-color"))  // + index into highlight.Slots()
	hitHighlightDeleteList = tree.Sub(highlightUiHits, tree.Name("highlight-delete")) // + index of a custom rule
)

// highlightUI is the state of the rules and of the dialog that adds one.
type highlightUI struct {
	builtinOff bool // zero value: the built-in rules are on
	rules      []highlightRule

	pattern       text.Editor
	regex, folded bool // folded: match case
	slot          highlight.Slot
	dialogError   string
}

// recompileHighlights rebuilds the compiled rules after any change to them, and
// with them the terminal's look, so every line is painted again.
func (state *State) recompileHighlights() {
	rules := make([]highlight.Rule, 0, len(state.highlighting.rules)+8)
	for _, rule := range state.highlighting.rules {
		rules = append(rules, highlight.Rule{Pattern: rule.Pattern, Regex: rule.Regex, CaseSensitive: rule.CaseSensitive, Slot: rule.Slot})
	}
	if !state.highlighting.builtinOff {
		rules = append(rules, highlight.Builtin()...)
	}
	state.highlights = highlight.Compile(rules)
	state.rebuildTerminalLook()
}

func (state *State) setBuiltinHighlight(on bool) bool {
	if state.highlighting.builtinOff == !on {
		return false
	}
	state.highlighting.builtinOff = !on
	state.recompileHighlights()
	return true
}

func (state *State) openHighlightDialog() {
	form := &state.highlighting
	form.pattern = text.Editor{}
	form.regex, form.folded = false, false
	form.slot = highlight.Red
	form.dialogError = ""
	state.modal = modalHighlightRule
}

func (state *State) clickHighlightDialog(id ui.ID) {
	form := &state.highlighting
	switch {
	case id == hitHighlightSubmit:
		state.submitHighlightRule()
	case id == hitHighlightCancel || id == hitHighlightClose || id == hitHighlightDismiss:
		state.closeModal()
	case id == hitHighlightRegex:
		form.regex = !form.regex
		form.dialogError = ""
	case id == hitHighlightCase:
		form.folded = !form.folded
	case id.Scope() == hitHighlightColorList:
		form.slot = highlight.Slots()[uint32(item(id))]
	}
}

func (state *State) submitHighlightRule() {
	form, m := &state.highlighting, state.messages.Tools
	rule := highlightRule{
		Pattern: strings.TrimSpace(form.pattern.String()), Regex: form.regex, CaseSensitive: form.folded, Slot: form.slot,
	}
	if len(form.rules) >= maxCustomHighlightRules {
		form.dialogError = m.HighlightErrorTooMany
		return
	}
	err := highlight.Validate(highlight.Rule{Pattern: rule.Pattern, Regex: rule.Regex, CaseSensitive: rule.CaseSensitive, Slot: rule.Slot})
	var syntaxError *syntax.Error
	switch {
	case err == nil:
	case errors.Is(err, highlight.ErrEmptyPattern) && rule.Pattern == "":
		form.dialogError = m.HighlightErrorEmpty
		return
	case errors.Is(err, highlight.ErrEmptyPattern):
		form.dialogError = m.HighlightErrorEverywhere
		return
	case errors.Is(err, highlight.ErrTooLong):
		form.dialogError = m.HighlightErrorTooLong
		return
	case errors.As(err, &syntaxError):
		form.dialogError = fmt.Sprintf(m.HighlightErrorInvalidFmt, syntaxError.Code)
		return
	default:
		form.dialogError = fmt.Sprintf(m.HighlightErrorInvalidFmt, err)
		return
	}
	form.rules = append(form.rules, rule)
	state.closeModal()
	state.recompileHighlights()
	state.save()
}

func (state *State) deleteHighlightRule(index int) {
	rules := state.highlighting.rules
	if index < 0 || index >= len(rules) {
		return
	}
	state.highlighting.rules = append(rules[:index:index], rules[index+1:]...)
	state.recompileHighlights()
	state.save()
}

// importHighlightRules reads the rules of a text file the user picked, in the
// format highlight.ParseRules documents, and appends the ones that parse to
// the user's rules. What the file holds beyond the room left under the cap is
// skipped, and the result says so; the file itself is never kept.
func (state *State) importHighlightRules() {
	m := state.messages.Tools
	paths, err := state.options.OpenFiles(app.OpenFileOptions{
		Title:   m.HighlightImportPickerTitle,
		Filters: []app.FileFilter{{Name: "Highlight rules", Pattern: "*.txt;*.csv;*.json"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		state.showActionFailure(m.HighlightImportPickerTitle, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		state.showFileReadFailure(m.HighlightImportPickerTitle, err, state.messages.ActionFeedback.ImportHostCatalogFailed)
		return
	}
	parsed, skipped := highlight.ParseRules(string(data))
	if len(parsed) == 0 && skipped == 0 {
		state.showActionWarning(m.HighlightImportPickerTitle, m.HighlightImportNothing)
		return
	}
	room := maxCustomHighlightRules - len(state.highlighting.rules)
	imported := min(len(parsed), max(room, 0))
	skipped += len(parsed) - imported
	for _, rule := range parsed[:imported] {
		state.highlighting.rules = append(state.highlighting.rules, highlightRule{
			Pattern: rule.Pattern, Regex: rule.Regex, CaseSensitive: rule.CaseSensitive, Slot: rule.Slot,
		})
	}
	state.recompileHighlights()
	state.save()
	if imported == 0 {
		state.showActionWarning(m.HighlightImportPickerTitle, m.HighlightErrorTooMany)
		return
	}
	state.status = fmt.Sprintf(m.HighlightImportResultFmt, imported, skipped)
}

// slotName is a color's word in the current language.
func (state *State) slotName(slot highlight.Slot) string {
	m := state.messages.Tools
	switch slot {
	case highlight.Red:
		return m.ColorRed
	case highlight.Yellow:
		return m.ColorYellow
	case highlight.Green:
		return m.ColorGreen
	case highlight.Cyan:
		return m.ColorCyan
	case highlight.Blue:
		return m.ColorBlue
	case highlight.Magenta:
		return m.ColorMagenta
	}
	return ""
}

// slotColor is the color a slot has in the terminal's scheme, as a swatch shows.
func (state *State) slotColor(slot highlight.Slot) ui.Color {
	ansi := state.termPal.ansi[slot.ANSI()]
	return ui.RGB(ansi[0], ansi[1], ansi[2])
}

func (state *State) paintHighlightDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, pal, theme := state.messages.Tools, state.pal, state.Theme()
	form := &state.highlighting
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitHighlightDismiss, CloseID: hitHighlightClose, InitialFocusID: hitHighlightPattern,
		ContentSize: ui.Size{Width: 520}, FitBody: true, BodyHeight: withError(198, form.dialogError), // the pattern, two boxes and the colors
	})
	layout := dialog.Begin(m.HighlightDialogTitle, "")
	slots := highlight.Slots()
	var field, regex, folded flex.Box
	var colors [6]flex.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		field = f.Slot().Height(62)
		regex = state.declareDialogCheckbox(f, m.HighlightRegex, 11, 0)
		folded = state.declareDialogCheckbox(f, m.HighlightCase, 12, 0)
		f.Text(m.HighlightColor).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).LineHeight(18).MarginSides(19, 0, 4, 2)
		f.Row().Gap(8).MarginSides(0, 0, 0, 2).Children(func() {
			for index, slot := range slots {
				selected := form.slot == slot
				border, width := withAlpha(pal.line, 0.9), float32(1)
				if selected {
					border, width = pal.accent, 2
				}
				colors[index] = f.Row().Size(50, 28).Shrink(0).Center().Background(pal.terminal).Radius(radius.control).Border(width, border).Children(func() {
					f.Column().Size(20, 14).Background(state.slotColor(slot)).Radius(7)
				})
			}
		})
		if form.dialogError != "" {
			f.Text(form.dialogError).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.danger).LineHeight(18).MarginSides(12, 0, 0, 0)
		}
	})
	f.End()
	content := components.BeginField(canvas, theme, field.Rect()).Paint(m.HighlightPattern, "", "")
	value := form.pattern.String()
	focused := canvas.Focused() == hitHighlightPattern
	components.PaintInput(canvas, theme, content, components.InputSnapshot{
		ID: hitHighlightPattern, Label: m.HighlightPattern, Value: value, Placeholder: m.HighlightPatternPlaceholder,
		Focused: focused, PlatformEditing: focused && value != "", Invalid: form.dialogError != "",
	})
	state.paintDialogCheckbox(canvas, hitHighlightRegex, m.HighlightRegex, form.regex, regex)
	state.paintDialogCheckbox(canvas, hitHighlightCase, m.HighlightCase, form.folded, folded)
	for index, slot := range slots {
		canvas.HitWithOptions(hitHighlightColorList.At(index), colors[index].Rect(), state.slotName(slot), ui.HitOptions{Role: ui.RoleButton, Selected: form.slot == slot})
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitHighlightCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitHighlightSubmit, Label: m.HighlightSave},
	)
	dialog.End()
}

// declareDialogCheckbox declares a checkbox and its label inside a dialog's body,
// the given room above and below it, and returns the box the checkbox is
// painted in (paintDialogCheckbox), after the fields above it, so the keyboard
// visits them in the order they are on the page. It returns the row, which
// holds the checkbox and its words.
func (state *State) declareDialogCheckbox(f *flex.Frame, label string, above, below float32) flex.Box {
	side := components.CheckboxExtent(state.Theme())
	return f.Row().Height(20).Gap(24-side).Items(flex.Center).MarginSides(above, 0, below, 2).Children(func() {
		f.Slot().Size(side, side).Shrink(0)
		f.Text(label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(state.pal.text).LineHeight(20).Grow(1)
	})
}

// declareDialogCheckboxHelp is declareDialogCheckbox with a help icon right after
// the label, which paintHelpIcon fills in.
func (state *State) declareDialogCheckboxHelp(f *flex.Frame, label string, above, below float32) (row, help flex.Box) {
	side := components.CheckboxExtent(state.Theme())
	row = f.Row().Height(20).Gap(24-side).Items(flex.Center).MarginSides(above, 0, below, 2).Children(func() {
		f.Slot().Size(side, side).Shrink(0)
		f.Text(label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(state.pal.text).LineHeight(20)
		help = f.Slot().Size(16, 16).Shrink(0)
		f.Spacer().Grow(1)
	})
	return row, help
}

// paintHelpIcon draws a question mark in bounds that shows hint as a tooltip when
// the pointer rests on it.
func (state *State) paintHelpIcon(canvas *ui.Canvas, id ui.ID, hint string, bounds ui.Rect) {
	if hint == "" || bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	canvas.HitWithOptions(id, bounds, hint, ui.HitOptions{Role: ui.RoleButton, SkipFocus: true, Hint: hint})
	paintIcon(canvas, iconHelp, insetRect(bounds, 0.08), state.pal.muted)
}

// paintDialogCheckbox paints the checkbox of a declared row and makes the words
// beside it its label, so pressing them toggles it.
func (state *State) paintDialogCheckbox(canvas *ui.Canvas, id ui.ID, label string, checked bool, row flex.Box) {
	rect := row.Rect()
	state.rememberHostControl(id, rect)
	side := components.CheckboxExtent(state.Theme())
	components.BeginCheckbox(canvas, state.Theme(), id, ui.Rect{X: rect.X, Y: rect.Y + (rect.Height-side)/2}, checked).Label(label).Paint()
	// The checkbox's own hit reaches 12px past its box; the label starts there.
	words := rect.X + side + 12
	canvas.HitLabel(id, ui.Rect{X: words, Y: rect.Y, Width: max(rect.X+rect.Width-words, 0), Height: rect.Height}, label, false)
}

// paintHighlightSettings is the Keyword highlighting group of Settings.
func (state *State) paintHighlightSettings(page *settingsPage) {
	m := state.messages.Tools
	rules := state.highlighting.rules
	page.group(m.HighlightGroup, func() {
		page.toggleRow(m.HighlightBuiltin, m.HighlightBuiltinDescription, !state.highlighting.builtinOff, hitSettingsHighlightToggle)
		for index, rule := range rules {
			kind := m.HighlightKindText
			switch {
			case rule.Regex && rule.CaseSensitive:
				kind = m.HighlightKindRegexCase
			case rule.Regex:
				kind = m.HighlightKindRegex
			case rule.CaseSensitive:
				kind = m.HighlightKindTextCase
			}
			page.actionRow(rule.Pattern, state.slotName(rule.Slot)+" · "+kind, false,
				components.ButtonAction{ID: hitHighlightDeleteList.At(index), Label: m.HighlightDelete, Variant: components.ButtonOutline})
		}
		page.actionRow(m.HighlightCustom, m.HighlightCustomDescription, len(rules) >= maxCustomHighlightRules,
			components.ButtonAction{ID: hitSettingsHighlightAdd, Label: m.HighlightAdd, Variant: components.ButtonOutline})
		page.actionRow(m.HighlightImport, m.HighlightImportDescription, false,
			components.ButtonAction{ID: hitSettingsHighlightImport, Label: m.HighlightImport, Variant: components.ButtonOutline})
	})
}

// restoreTerminalTools loads the highlight rules and snippets of a saved state.
// An entry that cannot be used (a pattern that no longer compiles, an unknown
// color, a snippet with no name) is dropped rather than carried along.
func (state *State) restoreTerminalTools(saved savedState) {
	state.highlighting.builtinOff = saved.HighlightBuiltin == "off"
	clear(state.highlighting.rules)
	state.highlighting.rules = state.highlighting.rules[:0]
	for _, entry := range saved.Highlights {
		rule := highlightRule{Pattern: entry.Pattern, Regex: entry.Regex, CaseSensitive: entry.CaseSensitive, Slot: highlight.SlotFromName(entry.Color)}
		if len(state.highlighting.rules) == maxCustomHighlightRules ||
			highlight.Validate(highlight.Rule{Pattern: rule.Pattern, Regex: rule.Regex, CaseSensitive: rule.CaseSensitive, Slot: rule.Slot}) != nil {
			continue
		}
		state.highlighting.rules = append(state.highlighting.rules, rule)
	}
	clear(state.snippets.list)
	state.snippets.list = state.snippets.list[:0]
	for _, entry := range saved.Snippets {
		clean := snippet{Name: cleanSnippetText(entry.Name, maxSnippetName), Command: cleanSnippetText(entry.Command, maxSnippetCommand), Run: entry.Run}
		if clean.Name == "" || clean.Command == "" || len(state.snippets.list) == maxSnippets {
			continue
		}
		state.snippets.list = append(state.snippets.list, clean)
	}
	for _, entry := range saved.SnippetShortcuts {
		if len(entry.Key) != 1 {
			continue
		}
		chord := shortcutChord{Modifiers: ui.Modifiers(entry.Modifiers), Key: entry.Key[0]}
		for index := range state.snippets.list {
			// The first snippet of that name without a shortcut takes it; a chord
			// that a built-in action or an earlier snippet has is not given twice.
			if snippet := &state.snippets.list[index]; snippet.Name == entry.Name && !snippet.Shortcut.valid() {
				if chord.validForSnippet() && state.chordOwner(chord, index) == "" {
					snippet.Shortcut = chord
				}
				break
			}
		}
	}
	state.refreshSnippets()
	state.recompileHighlights()
}
