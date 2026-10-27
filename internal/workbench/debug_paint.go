//go:build dev

package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

const (
	// debugPreviewWidth is how wide the showcase group is when the page has the
	// room; a narrower window shrinks it instead of cutting the last action off.
	debugPreviewWidth = float32(360)
	// debugSelectWidth is the select trigger's share of its card.
	debugSelectWidth = float32(240)
	// debugCardMaxWidth keeps the previews a readable column on a wide window.
	debugCardMaxWidth = float32(560)
	// debugSwatchSize is the hit target of one palette color.
	debugSwatchSize = float32(36)
)

// debugSwatchColors draws the palette in the order debugSwatchLabels names it:
// no color, green, blue, and a red that shows the disabled state.
var debugSwatchColors = [debugSwatches]string{"", "#22c55e", "#3b82f6", "#ef4444"}

// paintDebugWorkspace is the component gallery: the page's heading, then one
// card per Viva component. The Button group card is the first one.
func (state *State) paintDebugWorkspace(canvas *ui.Canvas, bounds ui.Rect) {
	pal, messages, theme := state.pal, state.messages, state.Theme()
	groupLabels := state.debugActionLabels()
	groupChoice := groupLabels[min(max(state.dev.action, 0), len(groupLabels)-1)]
	selectLabels := state.debugSelectLabels()
	selectChoice := selectLabels[min(max(state.dev.selectOption, 0), len(selectLabels)-1)]
	swatchLabels := state.debugSwatchLabels()
	swatchChoice := swatchLabels[min(max(state.dev.swatch, 0), len(swatchLabels)-1)]
	// The page pads 18px on each side and the gallery column 4px more. The card
	// gives 18px each side to its content, where the controls live.
	cardWidth := min(max(bounds.Width-36-8, 1), debugCardMaxWidth)
	contentWidth := max(cardWidth-36, 1)
	groupWidth := min(debugPreviewWidth, contentWidth)
	selectWidth := min(debugSelectWidth, contentWidth)

	var group, selectTrigger flex.Box
	var swatches [debugSwatches]flex.Box
	f := state.dev.layout.Begin(canvas, theme, bounds)
	f.Column().Fill().PadSides(12, 18, 0, 18).Children(func() {
		f.Column().MarginSides(4, 0, 13, 0).Gap(3).Children(func() {
			f.Text(messages.DebugTitle).FontSize(18).Weight(ui.WeightSemibold).Color(pal.text)
			f.Paragraph(messages.DebugDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(22)
		})
		f.Divider().Background(pal.line)
		f.Column().Grow(1).MarginSides(4, 0, 18, 0).Children(func() {
			f.Text(messages.DebugComponents).FontSize(metrics.FontSizeS).Weight(ui.WeightSemibold).Color(pal.faint).LineHeight(20).MarginSides(0, 0, 8, 0)
			f.Column().Gap(16).Children(func() {
				f.Column().Width(cardWidth).Pad(18).Gap(10).Shrink(0).
					Background(pal.surface).Border(1, pal.line).Radius(radius.surface).Children(func() {
					f.Text(messages.DebugButtonGroupTitle).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.text)
					f.Paragraph(messages.DebugButtonGroupDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(20)
					group = f.Slot().Size(groupWidth, theme.Metrics.ControlHeight).MarginSides(2, 0, 4, 0).Shrink(0)
					f.Row().Gap(6).Items(flex.Center).Children(func() {
						f.Text(messages.DebugButtonGroupChoice).FontSize(metrics.FontSizeS).Color(pal.faint)
						f.Text(groupChoice).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted)
					})
				})
				f.Column().Width(cardWidth).Pad(18).Gap(10).Shrink(0).
					Background(pal.surface).Border(1, pal.line).Radius(radius.surface).Children(func() {
					f.Text(messages.DebugSelectTitle).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.text)
					f.Paragraph(messages.DebugSelectDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(20)
					// The select trigger and its popup are painted after the page
					// (see paintDebugSelect), so the popup is not clipped by the
					// workspace's frame.
					selectTrigger = f.Slot().Size(selectWidth, theme.Metrics.ControlHeight).MarginSides(2, 0, 4, 0).Shrink(0)
					f.Row().Gap(6).Items(flex.Center).Children(func() {
						f.Text(messages.DebugSelectChoice).FontSize(metrics.FontSizeS).Color(pal.faint)
						f.Text(selectChoice).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted)
					})
				})
				f.Column().Width(cardWidth).Pad(18).Gap(10).Shrink(0).
					Background(pal.surface).Border(1, pal.line).Radius(radius.surface).Children(func() {
					f.Text(messages.DebugColorSwatchTitle).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.text)
					f.Paragraph(messages.DebugColorSwatchDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(20)
					// The colors are painted after the page, over the slots the
					// card reserved for them.
					f.Row().Gap(12).MarginSides(2, 0, 4, 0).Children(func() {
						for index := range swatches {
							swatches[index] = f.Slot().Size(debugSwatchSize, debugSwatchSize)
						}
					})
					f.Row().Gap(6).Items(flex.Center).Children(func() {
						f.Text(messages.DebugColorSwatchChoice).FontSize(metrics.FontSizeS).Color(pal.faint)
						f.Text(swatchChoice).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted)
					})
				})
			})
		})
	})
	f.End()
	state.dev.selectRect = selectTrigger.Rect()

	options := state.debugButtonOptions()
	components.BeginButtonGroup(canvas, theme, group.Rect(), options[:]).Paint()

	selected := min(max(state.dev.swatch, 0), len(swatchLabels)-1)
	for index, color := range debugSwatchColors {
		swatch := components.BeginColorSwatch(canvas, theme, hitDebugSwatchList.At(index), swatches[index].Rect(), hostAccent(color)).
			Label(swatchLabels[index]).Selected(index == selected).TabStop(index == selected)
		if index == debugSwatches-1 {
			swatch = swatch.Disabled()
		}
		swatch.Paint()
	}
}

// paintDebugSelect paints the preview select after the page, over the slot the
// page reserved. Viva owns the trigger, the popup and its keyboard and pointer
// behavior; the page owns the chosen value and whether the list is open.
func (state *State) paintDebugSelect(canvas *ui.Canvas) {
	bounds := state.dev.selectRect
	if bounds.Width <= 0 {
		return
	}
	messages, theme := state.messages, state.Theme()
	labels := state.debugSelectLabels()
	selected := min(max(state.dev.selectOption, 0), len(labels)-1)
	content := ui.Rect{
		X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: bounds.Width,
		Height: float32(8 + len(labels)*32),
	}
	// A short window puts the list above the trigger instead of off its bottom.
	if content.Y+content.Height > state.viewport.Height-8 {
		content.Y = max(8, bounds.Y-content.Height-4)
	}
	control := components.BeginSelect(canvas, theme, bounds, components.SelectOptions{
		TriggerID: hitDebugSelect, Label: messages.DebugSelectTitle, Value: labels[selected],
		Open: state.dev.selectOpen, Content: content, Layer: popupLayer,
		DismissID: hitDebugSelectDismiss, Viewport: state.windowRect(),
		Scroll: state.selectScroll(state.dev.selectOpen, hitDebugSelectDismiss),
	})
	defer control.End()
	if state.dev.selectOpen {
		state.menuPlaced(control.ContentBounds())
	}
	for index, label := range labels {
		control.AddItem(components.SelectItem{
			ID: hitDebugSelectItemList.At(index), Label: label, Value: label, Selected: index == selected,
		})
	}
}
