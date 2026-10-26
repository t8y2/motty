package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

// Paint after the workspace clip and inside the menu pass, as other selects do.
func (state *State) paintMCPClientSelect(canvas *ui.Canvas) {
	cfg, m := &state.ai.cfg, state.messages.AISettings
	bounds := cfg.mcpSelectBounds
	if bounds.Width <= 0 || bounds.Y < windowBarHeight || bounds.Y+bounds.Height > state.viewport.Height {
		cfg.mcpSelectOpen = false
		return
	}
	options := aiMCPClientOptions()
	below := max(state.viewport.Height-8-bounds.Y-bounds.Height-4, 0)
	above := max(bounds.Y-windowBarHeight-8, 0)
	height := min(float32(8+len(options)*32), max(above, below))
	content := ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: bounds.Width, Height: height}
	if below < height {
		content.Y = bounds.Y - height - 4
	}
	selectControl := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: hitAISetMCPSelect, Label: m.MCPClientLabel, Value: options[cfg.mcpClient].Label,
		Open: cfg.mcpSelectOpen, Content: content, Layer: popupLayer,
		DismissID: hitAISetMCPSelectDismiss, Viewport: state.windowRect(),
		Scroll: state.selectScroll(cfg.mcpSelectOpen, hitAISetMCPSelectDismiss),
	})
	defer selectControl.End()
	if cfg.mcpSelectOpen {
		state.menuPlaced(selectControl.ContentBounds())
	}
	for index, option := range options {
		option.Selected = index == int(cfg.mcpClient)
		selectControl.AddItem(option)
	}
}

// Keep explanations and actions readable when the settings navigation leaves
// too little room for a two-column row.
func (page *settingsPage) mcpFrame(label, description string, color ui.Color, control func()) {
	if page.width >= 500 {
		page.aiFrame(settingsRowHeight, label, description, color, control)
		return
	}
	page.separator()
	f := page.f
	f.Column().Pad(18).Gap(10).Children(func() {
		f.Column().Gap(2).Children(func() {
			f.Text(label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(page.state.pal.text)
			f.Wrap(description).FontSize(metrics.FontSizeS).LineHeight(18).Color(color)
		})
		f.Row().Children(control)
	})
}

func (page *settingsPage) mcpCopyButton(id ui.ID, label string, disabled bool) {
	state, m := page.state, page.state.messages.AISettings
	width, _ := state.aiButtonSize(page.f.Canvas(), label)
	for _, value := range []string{m.MCPCopied, m.MCPCopyFailed} {
		w, _ := state.aiButtonSize(page.f.Canvas(), value)
		width = max(width, w)
	}
	if state.ai.cfg.copyTarget == id {
		switch state.ai.cfg.copied {
		case aiCopyDone:
			label = m.MCPCopied
		case aiCopyFailed:
			label = m.MCPCopyFailed
		}
	}
	page.button(id, label, aiButtonOutline, disabled).Width(width)
}

func (state *State) paintAIMCPConfig(page *settingsPage, endpoint string, ready bool) {
	f, pal, m, cfg := page.f, state.pal, state.messages.AISettings, &state.ai.cfg
	hint := m.MCPClaudeHint
	switch cfg.mcpClient {
	case aiMCPCursor:
		hint = m.MCPCursorHint
	case aiMCPCodex:
		hint = m.MCPCodexHint
	case aiMCPCodeBuddy:
		hint = m.MCPCodeBuddyHint
	case aiMCPZCode:
		hint = m.MCPZCodeHint
	case aiMCPTrae:
		hint = m.MCPTraeHint
	case aiMCPVSCode:
		hint = m.MCPVSCodeHint
	case aiMCPWindsurf:
		hint = m.MCPWindsurfHint
	case aiMCPDeepSeek:
		hint = m.MCPDeepSeekHint
	case aiMCPOpenCode:
		hint = m.MCPOpenCodeHint
	case aiMCPCherry:
		hint = m.MCPCherryHint
	case aiMCPQoder:
		hint = m.MCPQoderHint
	case aiMCPWorkBuddy:
		hint = m.MCPWorkBuddyHint
	}
	preview := endpoint // The off / not-running state, not a runnable example.
	if ready {
		preview = aiMCPConfig(cfg.mcpClient, state.aiMCPURL(), "********")
	}
	page.group(m.MCPConfig, func() {
		f.Column().Pad(18).Gap(14).Children(func() {
			f.Wrap(m.MCPConfigDesc).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18)
			page.add(settingsControl{kind: controlMCPSelect, slot: f.Slot().FillWidth().Height(36)})
			f.Wrap(hint).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18)
			f.Column().Background(pal.surfaceSoft).Border(1, pal.line).Radius(radius.control).Children(func() {
				f.Row().Pad(12).Children(func() {
					f.Column().Grow(1)
					page.mcpCopyButton(hitAISetMCPCopy, m.MCPCopy, !ready)
				})
				f.Column().MarginSides(0, 14, 14, 14).Children(func() {
					if !ready {
						f.Wrap(preview).FontSize(12).LineHeight(19).Color(pal.muted)
						return
					}
					// Wrap by monospace cells, preserving JSON indentation and newlines.
					cell, _ := f.Canvas().MonoCellSize(12, 0)
					cols := max(1, int((page.width-36-28-2)/max(cell, 1)))
					cfg.mcpPreviewLines, _ = aiMonoWrap(cfg.mcpPreviewLines[:0], preview, cols, len(preview)+1)
					for _, line := range cfg.mcpPreviewLines {
						f.Text(line).Mono().FontSize(12).LineHeight(19).Color(pal.text)
					}
				})
			})
			f.Wrap(m.MCPPreviewHint).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18)
		})
	})
}
