package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/autofill"
)

var ruleManagerHits = tree.Feature("rule-manager")

var (
	hitSettingsHighlightManage = tree.ID(ruleManagerHits, "highlight-manage")
	hitSettingsAutofillManage  = tree.ID(ruleManagerHits, "autofill-manage")
	hitRuleManagerClose        = tree.ID(ruleManagerHits, "close")
	hitRuleManagerAll          = tree.ID(ruleManagerHits, "all")
	hitRuleManagerAdd          = tree.ID(ruleManagerHits, "add")
	hitRuleManagerDelete       = tree.ID(ruleManagerHits, "delete")
	hitRuleManagerScroll       = tree.ID(ruleManagerHits, "scroll")
	hitRuleManagerSelect       = tree.Sub(ruleManagerHits, tree.Name("select"))
	hitRuleManagerEdit         = tree.Sub(ruleManagerHits, tree.Name("edit"))
)

type ruleManagerUI struct {
	kind      ruleDeleteKind
	selected  map[int]bool
	returning bool
	offset    float32
	list      *ui.ScrollState
	bounds    gfx.Rect
	geometry  components.ScrollAreaGeometry
	dragging  bool
	grab      float32
}

func (state *State) openRuleManager(kind ruleDeleteKind) {
	state.ruleManager = ruleManagerUI{kind: kind, selected: make(map[int]bool)}
	state.modal = modalRuleManager
}

func (state *State) ruleManagerCount() int {
	if state.ruleManager.kind == ruleDeleteAutofill {
		return len(state.autofillRules.rules)
	}
	return len(state.highlighting.rules)
}

func (state *State) clickRuleManager(id gfx.ID) {
	manager := &state.ruleManager
	count := state.ruleManagerCount()
	switch {
	case id == hitRuleManagerClose:
		state.closeModal()
	case id == hitRuleManagerAll:
		if len(manager.selected) == count {
			clear(manager.selected)
		} else {
			for index := range count {
				manager.selected[index] = true
			}
		}
	case id.Scope() == hitRuleManagerSelect:
		if index, ok := id.In(hitRuleManagerSelect); ok && index >= 0 && index < count {
			if manager.selected[index] {
				delete(manager.selected, index)
			} else {
				manager.selected[index] = true
			}
		}
	case id.Scope() == hitRuleManagerEdit:
		if index, ok := id.In(hitRuleManagerEdit); ok && index >= 0 && index < count {
			manager.returning = true
			if manager.kind == ruleDeleteAutofill {
				state.openAutofillDialog(state.autofillRules.rules[index])
			} else {
				state.openHighlightEdit(index)
			}
		}
	case id.Scope() == hitAutofillToggleList && manager.kind == ruleDeleteAutofill:
		if index, ok := id.In(hitAutofillToggleList); ok {
			state.toggleAutofillRule(index)
		}
	case id == hitRuleManagerAdd:
		if manager.kind == ruleDeleteAutofill && count < autofill.MaxRules {
			manager.returning = true
			state.openAutofillDialog(nil)
		} else if manager.kind == ruleDeleteHighlight && count < maxCustomHighlightRules {
			manager.returning = true
			state.openHighlightDialog()
		}
	case id == hitRuleManagerDelete:
		state.askDeleteSelectedRules()
	}
}

func (state *State) declareRuleManager(c ui.C) {
	manager, m := &state.ruleManager, state.messages.RuleManagement
	count := state.ruleManagerCount()
	title, add, full := state.messages.Tools.HighlightGroup, state.messages.Tools.HighlightAdd, count >= maxCustomHighlightRules
	if manager.kind == ruleDeleteAutofill {
		title, add, full = state.messages.Autofill.Group, state.messages.Autofill.Add, count >= autofill.MaxRules
	}
	viewport := dialogViewport(c)
	height := min(float32(640), dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 760, "", 44+float32(max(1, min(count, 7)))*68))
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 760, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitRuleManagerClose, InitialFocusID: hitRuleManagerClose,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Gap(12).Children(func() {
			ui.Row(c).FillWidth().Height(32).Items(ui.Center).Gap(12).Children(func() {
				all := count > 0 && len(manager.selected) == count
				label := m.SelectAll
				if all {
					label = m.ClearSelection
				}
				checkbox := ui.Slot(c).Size(22, 24).Shrink(0)
				checkbox.Draw(func(d *ui.DrawContext) {
					components.BeginCheckbox(d.Canvas, *d.Theme, hitRuleManagerAll, d.Rect, all).Label(label).Paint()
				})
				ui.Text(c, fmt.Sprintf(m.CountFmt, count)).FontSize(metrics.FontSizeM).Color(state.pal.text)
				ui.Spacer(c).Grow(1)
				ui.Text(c, fmt.Sprintf(m.SelectedFmt, len(manager.selected))).FontSize(metrics.FontSizeS).Color(state.pal.muted)
			})
			list := ui.ScrollAt(c, manager.offset).FillWidth().Grow(1).Basis(0).PadSides(0, 14, 0, 0)
			list.Children(func() {
				if count == 0 {
					ui.Text(c, m.Empty).FontSize(metrics.FontSizeM).Color(state.pal.muted).Margin(16)
				}
				for index := range count {
					state.declareRuleManagerRow(c, index)
				}
			})
			list.DrawOver(func(d *ui.DrawContext) {
				manager.bounds, manager.list = d.Rect, list.ScrollState()
				manager.offset = manager.list.Offset
				area := components.BeginScrollArea(d.Canvas, *d.Theme, d.Rect, gfx.Size{Width: d.Rect.Width, Height: manager.list.Content}).
					Offset(0, manager.offset).ScrollbarSize(10).VerticalScrollbar(hitRuleManagerScroll, "Vertical scrollbar", "")
				manager.geometry = area.Geometry()
				area.EndContent()
			})
		})
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitRuleManagerDelete, Label: m.DeleteSelected, Variant: components.ButtonDestructiveOutline, Disabled: len(manager.selected) == 0},
			components.ButtonAction{ID: hitRuleManagerAdd, Label: add, Variant: components.ButtonOutline, Disabled: full},
			components.ButtonAction{ID: hitRuleManagerClose, Label: m.Close, Variant: components.ButtonSecondary})
	})
}

func (state *State) ruleManagerPointer(event gfx.PointerEvent) gfx.EventResult {
	manager := &state.ruleManager
	if state.modal != modalRuleManager || event.Target != hitRuleManagerScroll && !manager.dragging {
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerExit {
		manager.dragging = false
		return gfx.EventResult{Handled: true}
	}
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary {
			return gfx.EventResult{}
		}
		manager.dragging = true
		manager.grab = event.Y - manager.geometry.VerticalThumb.Y
		if manager.grab < 0 || manager.grab > manager.geometry.VerticalThumb.Height {
			manager.grab = manager.geometry.VerticalThumb.Height / 2
		}
	case gfx.PointerMove:
		if !manager.dragging {
			return gfx.EventResult{}
		}
	case gfx.PointerUp:
		if !manager.dragging || event.Button != gfx.PointerButtonPrimary {
			return gfx.EventResult{}
		}
		manager.dragging = false
	default:
		return gfx.EventResult{}
	}
	manager.offset = manager.geometry.VerticalOffsetForThumb(event.Y - manager.grab)
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *State) declareRuleManagerRow(c ui.C, index int) {
	manager, m := &state.ruleManager, state.messages.Autofill
	name, detail := "", ""
	if manager.kind == ruleDeleteAutofill {
		rule := state.autofillRules.rules[index]
		name = rule.Name
		scope := m.ScopeAll
		if len(rule.Scope) > 0 {
			scope = fmt.Sprintf(m.ScopeFmt, strings.Join(rule.Scope, ", "))
		}
		detail = state.sendKindName(rule.Send)
		if rule.Enter {
			detail += " + " + m.WithEnter
		}
		detail += " · " + scope + " · " + rule.Pattern
	} else {
		rule, tools := state.highlighting.rules[index], state.messages.Tools
		name, detail = rule.Pattern, tools.HighlightKindText
		switch {
		case rule.Regex && rule.CaseSensitive:
			detail = tools.HighlightKindRegexCase
		case rule.Regex:
			detail = tools.HighlightKindRegex
		case rule.CaseSensitive:
			detail = tools.HighlightKindTextCase
		}
		detail = state.slotName(rule.Slot) + " · " + detail
	}
	row := ui.Row(c).FillWidth().Height(68).Shrink(0).Items(ui.Center).Gap(14).PadSides(0, 14, 0, 8)
	if manager.selected[index] {
		row = row.Background(state.pal.accentSoft)
	}
	row.Children(func() {
		selectBox := ui.Slot(c).Size(22, 24).Shrink(0)
		selectBox.Draw(func(d *ui.DrawContext) {
			components.BeginCheckbox(d.Canvas, *d.Theme, hitRuleManagerSelect.At(index), d.Rect, manager.selected[index]).Label(name).Paint()
		})
		if manager.kind == ruleDeleteHighlight {
			ui.Slot(c).Size(10, 24).Shrink(0).Background(state.slotColor(state.highlighting.rules[index].Slot)).Radius(2)
		}
		ui.Column(c).Grow(1).Basis(0).Gap(4).Children(func() {
			ui.Text(c, name).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(state.pal.text).MaxLines(1)
			ui.Text(c, detail).FontSize(metrics.FontSizeS).Color(state.pal.muted).MaxLines(1)
		})
		if manager.kind == ruleDeleteAutofill {
			rule := state.autofillRules.rules[index]
			toggle := ui.Slot(c).Size(52, 32).Shrink(0)
			toggle.Draw(func(d *ui.DrawContext) {
				label := m.TurnOn
				if rule.Enabled {
					label = m.TurnOff
				}
				components.BeginSwitch(d.Canvas, *d.Theme, hitAutofillToggleList.At(index), gfx.Rect{X: d.Rect.X + 10, Y: d.Rect.Y + 7}, rule.Enabled).Label(label + ": " + name).Paint()
			})
		}
		state.declareIconButton(c, hitRuleManagerEdit.At(index), iconPencil, m.Edit+": "+name, false, iconButtonQuiet).Size(32, 32)
	})
}

func (state *State) scrollRuleManager(event gfx.ScrollEvent) gfx.EventResult {
	manager := &state.ruleManager
	if manager.list == nil || !pointInRect(event.X, event.Y, manager.bounds) {
		return gfx.EventResult{Handled: true}
	}
	result := manager.list.Wheel(event)
	manager.offset = manager.list.Offset
	result.Handled = true
	return result
}
