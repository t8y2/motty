package workbench

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp/syntax"
	"slices"
	"strings"

	"motty/internal/highlight"
	"motty/internal/workbench/icons"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// Keyword highlighting. The highlight package finds the words; this file keeps
// the user's rules, the built-in set, the dialog that adds a rule and the
// settings group that lists them.

// highlightRule is a rule the user wrote.
type highlightRule struct {
	Pattern       string
	Regex         bool
	CaseSensitive bool
	Color         highlight.Color
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
)

var (
	hitHighlightColorList   = tree.Sub(highlightUiHits, tree.Name("highlight-color")) // + index into highlight.Colors()
	hitHighlightCustomColor = tree.ID(highlightUiHits, "highlight-custom-color")
	hitHighlightDeleteList  = tree.Sub(highlightUiHits, tree.Name("highlight-delete")) // + index of a custom rule
)

// highlightUI is the state of the rules and of the dialog that adds one.
type highlightUI struct {
	builtinOff bool // zero value: the built-in rules are on
	rules      []highlightRule

	pattern              text.Editor
	regex, folded        bool // folded: match case
	color                highlight.Color
	colorPicker          ui.ColorPickerState
	colorValue           gfx.Color
	dialogError          string
	dialogErrorSelection ui.TextSelectionState // what the user selected of dialogError, to copy it
	editing              int
	editRule             highlightRule
	isEditing            bool

	// patternForm and patternText are the pattern field's row and text, bound to pattern.
	patternForm  ui.FormFieldState
	patternText  ui.Field
	patternBound bool
}

// recompileHighlights rebuilds the compiled rules after any change to them, and
// with them the terminal's look, so every line is painted again.
func (state *State) recompileHighlights() {
	rules := make([]highlight.Rule, 0, len(state.highlighting.rules)+8)
	for _, rule := range state.highlighting.rules {
		rules = append(rules, highlight.Rule{Pattern: rule.Pattern, Regex: rule.Regex, CaseSensitive: rule.CaseSensitive, Color: rule.Color})
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
	form.isEditing = false
	form.pattern = text.Editor{}
	form.regex, form.folded = false, false
	form.color = highlight.Red
	form.colorPicker = ui.ColorPickerState{}
	form.colorValue = state.ruleColor(form.color)
	form.dialogError = ""
	state.modal = modalHighlightRule
}

func (state *State) openHighlightEdit(index int) {
	if index < 0 || index >= len(state.highlighting.rules) {
		return
	}
	state.openHighlightDialog()
	form := &state.highlighting
	rule := form.rules[index]
	form.editing, form.editRule, form.isEditing = index, rule, true
	form.pattern.Insert(rule.Pattern)
	form.regex, form.folded, form.color = rule.Regex, rule.CaseSensitive, rule.Color
	form.colorValue = state.ruleColor(rule.Color)
}

func (state *State) clickHighlightDialog(id gfx.ID) {
	form := &state.highlighting
	switch {
	case id == hitHighlightSubmit:
		state.submitHighlightRule()
	case id == hitHighlightCancel || id == hitHighlightClose:
		state.closeModal()
	case id == hitHighlightRegex:
		form.regex = !form.regex
		form.dialogError = ""
	case id == hitHighlightCase:
		form.folded = !form.folded
	}
}

func (state *State) submitHighlightRule() {
	form, m := &state.highlighting, state.messages.Tools
	rule := highlightRule{
		Pattern: strings.TrimSpace(form.pattern.String()), Regex: form.regex, CaseSensitive: form.folded, Color: form.color,
	}
	if !form.isEditing && len(form.rules) >= maxCustomHighlightRules {
		form.dialogError = m.HighlightErrorTooMany
		return
	}
	err := highlight.Validate(highlight.Rule{Pattern: rule.Pattern, Regex: rule.Regex, CaseSensitive: rule.CaseSensitive, Color: rule.Color})
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
	for index, existing := range form.rules {
		if existing == rule && (!form.isEditing || index != form.editing) {
			form.dialogError = state.messages.RuleManagement.Duplicate
			return
		}
	}
	if form.isEditing {
		if form.editing < 0 || form.editing >= len(form.rules) || form.rules[form.editing] != form.editRule {
			state.closeModal()
			return
		}
		form.rules[form.editing] = rule
	} else {
		form.rules = append(form.rules, rule)
	}
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
// skipped, and the result is reported in a notice, as the warnings are; the
// file itself is never kept.
func (state *State) importHighlightRules() {
	m := state.messages.Tools
	title := m.HighlightImportPickerTitle
	paths, err := state.options.OpenFiles(app.OpenFileOptions{
		Title:   title,
		Filters: []app.FileFilter{{Name: "Highlight rules", Pattern: "*.txt"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		state.showActionFailure(title, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	data, tooLarge, err := readHighlightRules(paths[0])
	if err != nil {
		state.showFileReadFailure(title, err, state.messages.ActionFeedback.ReadFailed)
		return
	}
	if tooLarge {
		state.showActionWarning(title, m.HighlightImportTooLarge)
		return
	}
	parsed, skipped := highlight.ParseRules(string(data))
	if len(parsed) == 0 {
		if skipped == 0 {
			state.showActionWarning(title, m.HighlightImportNothing)
		} else {
			state.showActionWarning(title, fmt.Sprintf(m.HighlightImportAllSkippedFmt, skipped))
		}
		return
	}
	seen := make(map[highlightRule]bool, len(state.highlighting.rules)+len(parsed))
	for _, rule := range state.highlighting.rules {
		seen[rule] = true
	}
	imported, full := 0, false
	for _, rule := range parsed {
		candidate := highlightRule{
			Pattern: rule.Pattern, Regex: rule.Regex, CaseSensitive: rule.CaseSensitive, Color: rule.Color,
		}
		if seen[candidate] {
			skipped++
			continue
		}
		seen[candidate] = true
		if len(state.highlighting.rules) >= maxCustomHighlightRules {
			skipped++
			full = true
			continue
		}
		state.highlighting.rules = append(state.highlighting.rules, candidate)
		imported++
	}
	if imported == 0 {
		message := fmt.Sprintf(m.HighlightImportAllSkippedFmt, skipped)
		if full {
			message = m.HighlightErrorTooMany
		}
		state.showActionWarning(title, message)
		return
	}
	state.recompileHighlights()
	state.save()
	result := fmt.Sprintf(m.HighlightImportResultFmt, imported, skipped)
	if skipped > 0 {
		state.showActionWarning(title, result)
	} else {
		state.showActionInfo(title, result)
	}
}

// maxHighlightImportBytes bounds what an import reads: the file is picked by
// hand and only its first rules matter, so a mis-picked log is refused rather
// than read whole on the interface thread.
const maxHighlightImportBytes = 1 << 20

// readHighlightRules reads at most the bound and says when the file is larger.
func readHighlightRules(path string) (data []byte, tooLarge bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	data, err = io.ReadAll(io.LimitReader(file, maxHighlightImportBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > maxHighlightImportBytes {
		return nil, true, nil
	}
	return data, false, nil
}

// colorName is a color's word in the current language: a named color's, or an
// explicit color's #rrggbb form.
func (state *State) colorName(color highlight.Color) string {
	m := state.messages.Tools
	switch color {
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
	return color.Name()
}

// ruleColor is the color a rule paints with, as a swatch shows it: a named
// color's in the terminal's scheme, or an explicit color as the user chose it.
func (state *State) ruleColor(color highlight.Color) gfx.Color {
	if color.IsCustom() {
		red, green, blue := color.Channels()
		return gfx.RGB(red, green, blue)
	}
	ansi := state.termPal.ansi[color.ANSI()]
	return gfx.RGB(ansi[0], ansi[1], ansi[2])
}

// declareHighlightDialog declares the rule editor as a dialog of the workbench
// page.
func (state *State) declareHighlightDialog(c ui.C) {
	m := state.messages.Tools
	form := &state.highlighting
	title, save := m.HighlightDialogTitle, m.HighlightSave
	if form.isEditing {
		title, save = state.messages.RuleManagement.HighlightEditTitle, state.messages.Autofill.Save
	}
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 520, "", withError(136+dialogRowHeight(c.Theme()), form.dialogError)) // the pattern, two boxes and the colors
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 520, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitHighlightClose, InitialFocusID: hitHighlightPattern,
	})
	dialog.Body(func() { state.declareHighlightDialogBody(c) })
	dialog.Footer(func() {
		if dialogButton(c, state.messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickHighlightDialog(hitHighlightCancel) })
		}
		if dialogButton(c, save, components.ButtonDefault, false).Clicked() {
			c.After(func() { state.clickHighlightDialog(hitHighlightSubmit) })
		}
	})
}

// declareHighlightDialogBody lays out the rule dialog: the pattern field, the
// two checkboxes, the colors and the error. The pattern is a page field; the
// checkboxes and their hit regions are painted into the slots after the body.
func (state *State) declareHighlightDialogBody(c ui.C) {
	form, m, pal := &state.highlighting, state.messages.Tools, state.pal
	colors := highlight.Colors()
	column := ui.Column(c).Fill().PadSides(2, 4, 0, 4)
	column.Children(func() {
		if !form.patternBound {
			form.patternText.Bind(&form.pattern)
			form.patternBound = true
		}
		form.patternText.Invalid = form.dialogError != ""
		ui.FormField(c, &form.patternForm, m.HighlightPattern).FillWidth().Height(dialogRowHeight(c.Theme())).Children(func() {
			ui.TextField(c, &form.patternText, m.HighlightPattern, m.HighlightPatternPlaceholder, ui.WithID(hitHighlightPattern))
		})
		state.declareDialogCheck(c, hitHighlightRegex, m.HighlightRegex, form.regex, 11, 0)
		state.declareDialogCheck(c, hitHighlightCase, m.HighlightCase, form.folded, 12, 0)
		ui.Text(c, m.HighlightColor).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(18).MarginSides(19, 0, 4, 2)
		ui.Row(c).Gap(8).Items(ui.Center).MarginSides(0, 0, 0, 2).Children(func() {
			for index, color := range colors {
				selected := form.color == color
				label := state.colorName(color)
				swatch := ui.ColorSwatch(c, state.ruleColor(color), selected, label).Size(30, 30).Hit(hitHighlightColorList.At(index), label)
				if swatch.Clicked() {
					form.color = color
					form.colorValue = state.ruleColor(color)
				}
			}
			ui.ColorPicker(c, &form.colorPicker, &form.colorValue, m.HighlightCustomColor, ui.WithID(hitHighlightCustomColor)).
				RoundTrigger().TriggerIcon(&icons.Pipette).NoColor(!form.color.IsCustom()).Size(30, 30).
				PanelLabels(components.ColorPickerLabels{
					Color: m.HighlightCustomColor, Saturation: state.messages.ColorPickerSV, Hue: state.messages.ColorPickerHue,
					Hex: state.messages.FieldColorCustom, Clear: state.messages.ColorPickerClear,
					Cancel: state.messages.Cancel, Apply: state.messages.ColorPickerApply,
				})
			if form.colorPicker.Applied() {
				if form.colorValue.Alpha > 0 {
					channels := rgb8(form.colorValue)
					form.color = highlight.RGB(channels[0], channels[1], channels[2])
				} else {
					// Clear leaves the rule without a color, which it cannot be
					// saved as: the chosen color stays and the picker shows it
					// again next time.
					form.colorValue = state.ruleColor(form.color)
				}
			}
		})
		if form.dialogError != "" {
			ui.TextSelectionArea(c, &form.dialogErrorSelection).Children(func() {
				ui.Text(c, form.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.danger).LineHeight(18).MarginSides(12, 0, 0, 0)
			})
		}
	})
}

// paintHelpIcon draws a question mark in bounds that shows hint as a tooltip when
// the pointer rests on it.
func (state *State) paintHelpIcon(canvas *gfx.Canvas, id gfx.ID, hint string, bounds gfx.Rect) {
	if hint == "" || bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	canvas.HitWithOptions(id, bounds, hint, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true, Hint: hint})
	paintIcon(canvas, iconHelp, insetRect(bounds, 0.08), state.pal.muted)
}

// paintHighlightSettings is the Keyword highlighting group of Settings.
func (state *State) paintHighlightSettings(page *settingsPage) {
	m := state.messages.Tools
	rules := state.highlighting.rules
	page.group(m.HighlightGroup, func() {
		page.toggleRow(m.HighlightBuiltin, m.HighlightBuiltinDescription, !state.highlighting.builtinOff, hitSettingsHighlightToggle)
		page.actionRow(m.HighlightCustom, fmt.Sprintf(state.messages.RuleManagement.CountFmt, len(rules)), false,
			components.ButtonAction{ID: hitSettingsHighlightManage, Label: state.messages.RuleManagement.Manage, Variant: components.ButtonOutline})
		page.actionRow(m.HighlightAdd, m.HighlightCustomDescription, len(rules) >= maxCustomHighlightRules,
			components.ButtonAction{ID: hitSettingsHighlightAdd, Label: m.HighlightAdd, Variant: components.ButtonOutline})
		page.actionRow(m.HighlightImport, m.HighlightImportDescription, len(rules) >= maxCustomHighlightRules,
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
		rule := highlightRule{Pattern: entry.Pattern, Regex: entry.Regex, CaseSensitive: entry.CaseSensitive, Color: highlight.ColorFromName(entry.Color)}
		if len(state.highlighting.rules) == maxCustomHighlightRules ||
			highlight.Validate(highlight.Rule{Pattern: rule.Pattern, Regex: rule.Regex, CaseSensitive: rule.CaseSensitive, Color: rule.Color}) != nil {
			continue
		}
		state.highlighting.rules = append(state.highlighting.rules, rule)
	}
	clear(state.snippets.list)
	state.snippets.list = state.snippets.list[:0]
	state.snippets.customGroups = state.snippets.customGroups[:0]
	for _, group := range saved.SnippetGroups {
		if clean := restoreSnippetGroup(group.Name); clean != "" {
			state.snippets.customGroups = append(state.snippets.customGroups, clean)
		}
	}
	// The list is rebuilt from scratch, so a menu opened on one of its rows or
	// chips no longer names the snippet it showed.
	state.snippetMenu.open = false
	state.snippets.resetSnippetPreview()
	slices.SortStableFunc(saved.Snippets, func(left, right savedSnippet) int {
		switch {
		case left.Order < right.Order:
			return -1
		case left.Order > right.Order:
			return 1
		}
		return 0
	})
	for _, entry := range saved.Snippets {
		clean := snippet{Name: cleanSnippetText(entry.Name, maxSnippetName), Command: cleanSnippetCommand(entry.Command, maxSnippetCommand), Run: entry.Run, Lines: entry.Lines, Group: restoreSnippetGroup(entry.Group)}
		if clean.Name == "" || clean.Command == "" || len(state.snippets.list) == maxSnippets {
			continue
		}
		state.snippets.list = append(state.snippets.list, clean)
	}
	for _, entry := range saved.SnippetShortcuts {
		if len(entry.Key) != 1 {
			continue
		}
		chord := keymap.Chord{Modifiers: gfx.Modifiers(entry.Modifiers), Char: entry.Key[0]}
		for index := range state.snippets.list {
			// The first snippet of that name without a shortcut takes it; a chord
			// that a built-in action or an earlier snippet has is not given twice.
			if snippet := &state.snippets.list[index]; snippet.Name == entry.Name && !snippet.Shortcut.Valid() {
				if validForSnippet(chord) && state.chordOwner(chord, index) == "" {
					snippet.Shortcut = chord
				}
				break
			}
		}
	}
	state.refreshSnippets()
	state.commands.shown = saved.CommandsShown
	state.commands.sidebarLeft = saved.CommandsSidebarLeft
	state.commands.placement = commandsPlacementBottom
	if saved.CommandsSide {
		state.commands.placement = commandsPlacementSide
	}
	state.commands.sort = commandsSortFromString(saved.CommandsSort)
	state.commands.width = saved.CommandsWidth
	state.commands.rows = saved.CommandsRows
	state.commands.hiddenGroups = make(map[string]bool)
	for _, group := range saved.CommandsHiddenGroups {
		if group == "" {
			state.commands.hiddenGroups[""] = true
			continue
		}
		if key := strings.ToLower(restoreSnippetGroup(group)); key != "" {
			state.commands.hiddenGroups[key] = true
		}
	}
	state.commands.collapsedGroups = make(map[string]bool)
	for _, group := range saved.CommandsCollapsedGroups {
		if group == "" {
			state.commands.collapsedGroups[""] = true
			continue
		}
		if key := strings.ToLower(restoreSnippetGroup(group)); key != "" {
			state.commands.collapsedGroups[key] = true
		}
	}
	state.recompileHighlights()
}
