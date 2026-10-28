package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

// Paint after the workspace clip and inside the menu pass, as other selects do.
func (state *State) paintMCPClientSelect(canvas *ui.Canvas) {
	cfg := &state.ai.cfg
	state.paintMCPExportSelect(canvas, cfg.mcpSelectBounds, &cfg.mcpSelectOpen, cfg.mcpClient, hitAISetMCPSelect, hitAISetMCPSelectDismiss, aiMCPClientOptions())
	state.paintMCPExportSelect(canvas, cfg.agentSelectBounds, &cfg.agentSelectOpen, cfg.agentClient, hitAISetAgentSelect, hitAISetAgentSelectDismiss, aiAgentClientOptions())
}

func (state *State) paintMCPExportSelect(canvas *ui.Canvas, bounds ui.Rect, open *bool, client aiMCPClient, trigger, dismiss ui.ID, options [aiMCPClientCount]components.SelectItem) {
	if bounds.Width <= 0 || bounds.Y < windowBarHeight || bounds.Y+bounds.Height > state.viewport.Height {
		*open = false
		return
	}
	below := max(state.viewport.Height-8-bounds.Y-bounds.Height-4, 0)
	above := max(bounds.Y-windowBarHeight-8, 0)
	height := min(float32(8+len(options)*32), max(above, below))
	content := ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: bounds.Width, Height: height}
	if below < height {
		content.Y = bounds.Y - height - 4
	}
	opening := *open && state.popups.owner != dismiss
	scroll := state.selectScroll(*open, dismiss)
	if opening {
		scroll.Offset = max(0, min(float32(client)*32, float32(len(options)*32)+8-height))
		canvas.SetInitialFocus(options[client].ID)
	}
	selectControl := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: trigger, Label: state.messages.AISettings.MCPClientLabel, Value: options[client].Label,
		Open: *open, Content: content, Layer: popupLayer,
		Scope: dismiss, DismissID: dismiss, Viewport: state.windowRect(), Scroll: scroll,
	})
	defer selectControl.End()
	if *open {
		state.menuPlaced(selectControl.ContentBounds())
	}
	for index, option := range options {
		option.Selected = index == int(client)
		selectControl.AddItem(option)
	}
}

// Keep explanations and actions readable when the settings navigation leaves
// too little room for a two-column row.
func (page *settingsPage) mcpFrame(label, description string, color ui.Color, control func()) {
	page.separator()
	f := page.f
	if page.width >= 500 {
		row := f.Row().FillWidth().MinHeight(settingsRowHeight).Pad(18).Gap(24).Items(flex.Center)
		page.matchAnchor(label, row, 0)
		row.Children(func() {
			f.Column().Grow(1).Gap(2).Children(func() {
				f.Text(label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(page.state.pal.text)
				f.Wrap(description).FontSize(metrics.FontSizeS).LineHeight(18).Color(color)
			})
			f.Row().Shrink(0).Children(control)
		})
		return
	}
	column := f.Column().Pad(18).Gap(10)
	page.matchAnchor(label, column, 0)
	column.Children(func() {
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

// aiMCPPreviewFormat names what the configuration preview holds: a command to
// run in a terminal, or the body of a file to paste.
func aiMCPPreviewFormat(client aiMCPClient, m i18n.AISettingsMessages) string {
	switch client {
	case aiMCPClaude, aiMCPCodeBuddy:
		return m.MCPCommand
	case aiMCPCodex:
		return "TOML"
	case aiMCPDeepSeek:
		return "YAML"
	}
	return "JSON"
}

func (state *State) paintAIMCPConfig(page *settingsPage, endpoint string, ready bool) {
	m, cfg := state.messages.AISettings, &state.ai.cfg
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
	page.separator()
	state.paintMCPConfigBlock(page, aiMCPConfigView{
		title: m.MCPConfig, description: m.MCPConfigDesc, hint: hint, footer: m.MCPPreviewHint,
		client: cfg.mcpClient, selectKind: controlMCPSelect, copyID: hitAISetMCPCopy,
		ready: ready, preview: preview, lines: &cfg.mcpPreviewLines,
	})
}

type aiMCPConfigView struct {
	title, description, hint, footer, preview string
	client                                    aiMCPClient
	selectKind                                settingsControlKind
	copyID                                    ui.ID
	ready                                     bool
	lines                                     *[]string
}

func (state *State) paintMCPConfigBlock(page *settingsPage, view aiMCPConfigView) {
	f, pal, m := page.f, state.pal, state.messages.AISettings
	column := f.Column().Pad(18).Gap(14)
	page.matchAnchor(view.title, column, 0)
	column.Children(func() {
		f.Text(view.title).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(pal.text)
		f.Wrap(view.description).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18)
		page.add(settingsControl{kind: view.selectKind, slot: f.Slot().FillWidth().Height(36)})
		if view.hint != "" {
			f.Wrap(view.hint).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18)
		}
		f.Column().Background(pal.surfaceSoft).Border(1, pal.line).Radius(radius.control).Children(func() {
			// The header names what the block holds, so the room the copy
			// button takes says something instead of staying blank.
			f.Row().PadSides(10, 14, 10, 14).Items(flex.Center).Children(func() {
				f.Text(aiMCPPreviewFormat(view.client, m)).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted)
				f.Spacer()
				page.mcpCopyButton(view.copyID, m.MCPCopy, !view.ready)
			})
			f.Column().MarginSides(0, 14, 14, 14).Children(func() {
				if !view.ready {
					f.Wrap(view.preview).FontSize(12).LineHeight(19).Color(pal.muted)
					return
				}
				// Wrap by monospace cells, preserving JSON indentation and newlines.
				cell, _ := f.Canvas().MonoCellSize(12, 0)
				cols := max(1, int((page.width-36-28-2)/max(cell, 1)))
				*view.lines, _ = aiMonoWrap((*view.lines)[:0], view.preview, cols, len(view.preview)+1)
				for _, line := range *view.lines {
					f.Text(line).Mono().FontSize(12).LineHeight(19).Color(pal.text)
				}
			})
		})
		if view.footer != "" {
			f.Wrap(view.footer).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18)
		}
	})
}
