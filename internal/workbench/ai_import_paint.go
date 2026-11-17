package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/store"
	"motty/internal/aiproviderimport"
	"motty/internal/workbench/icons"
)

// Painting the provider import dialog. The body only reads the state the click
// handlers filled; it never scans, parses or opens a file.
//
// The preview is one list, not pages. Importable rows lead and unavailable rows
// follow under one explanation heading, so the selectable part stays together
// and the reasons read as a reference block. The list is a scroll area of the
// preview's own page: WindowConfig's wheel route moves it and keeps the offset,
// and the scrollbar reads the state the page's layout reported.

const (
	aiImportSourceRowHeight   = float32(64)
	aiImportFallbackRowHeight = float32(54)
	aiImportDividerHeight     = float32(13) // a 1px rule with 6px above and below
	aiImportErrorHeight       = float32(26)

	aiImportSummaryHeight        = float32(34)
	aiImportRowHeight            = float32(56)
	aiImportUnavailableRowHeight = float32(50)
	aiImportGroupHeight          = float32(32)
	aiImportRowGap               = float32(2)
	aiImportListMaxHeight        = float32(448)
	aiImportRowReserve           = float32(12) // room for the list's scrollbar

	aiImportResultBannerHeight = float32(44)
	aiImportResultLine         = float32(22)
	aiImportResultGap          = float32(4)
)

// The provider import dialog's body is a page; the dialog's shell, its footer
// buttons and the list's controls are painted by hand and keep their identities.
const (
	aiImportSourcesRoot = "ai-import-sources"
	aiImportPreviewRoot = "ai-import-preview"
	aiImportResultRoot  = "ai-import-result"
)

// aiImportPages are the import dialog's page per step, its declaration (kept so
// a paint makes no method value) and the slots its hand-painted buttons and
// list go into.
type aiImportPages struct {
	listHeight float32
	list       ui.Box // the page's scroll area, for its state after a layout
	listSet    bool
}

// declareAIImport declares the open provider-import step in the window page:
// its body is declared there, its buttons are page controls and its list is
// the page's scroll area.
func (state *State) declareAIImport(c ui.C) {
	switch state.ai.cfg.importStep {
	case aiImportSources:
		state.declareAIImportSources(c)
	case aiImportPreview:
		state.declareAIImportPreview(c)
	case aiImportDone:
		state.declareAIImportResult(c)
	}
}

func (state *State) declareAIImportSources(c ui.C) {
	cfg, m := &state.ai.cfg, state.messages.AISettings
	// Open on the first source that can be read, so Enter reaches the common
	// next step; Choose file stays the fallback when nothing was detected.
	initial := hitAISetImportChoose
	for index, source := range cfg.importSources {
		if source.Available() {
			initial = hitAISetImportSource.At(index)
			break
		}
	}
	rowsHeight := float32(len(cfg.importSources)) * aiImportSourceRowHeight
	if len(cfg.importSources) == 0 {
		rowsHeight = 40
	}
	bodyHeight := rowsHeight + aiImportDividerHeight + aiImportFallbackRowHeight
	if cfg.importErr != "" {
		bodyHeight += aiImportErrorHeight
	}
	const width float32 = 620
	viewport := gfx.Rect{Width: c.Bounds().Width, Height: c.Bounds().Height}
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: width}, FitBody: true,
	}).Layout(m.ImportSourcesDesc)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.ImportSourcesTitle, Description: m.ImportSourcesDesc, Width: width,
		Height: layout.Content.Height + bodyHeight, Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitAISetFormDismiss, CloseID: hitAISetFormClose,
	})
	dialog.Body(func() { state.declareAIImportSourcesBody(c, initial) })
	dialog.Footer(func() {
		if ui.Button(c, state.messages.Cancel).Secondary().Shrink(0).Clicked() {
			c.After(func() { state.aiDialogClick(hitAISetImportClose) })
		}
	})
	if dialog.Dismissed() {
		state.aiCloseDialog()
	}
}

func (state *State) declareAIImportPreview(c ui.C) {
	cfg, m := &state.ai.cfg, state.messages.AISettings
	total := len(cfg.importCandidates)
	importable := aiImportImportableCount(cfg.importCandidates)
	unavailable := total - importable
	selected := state.aiImportSelectedCount()
	children := importable
	if unavailable > 0 {
		children += 1 + unavailable
	}
	contentHeight := float32(importable)*aiImportRowHeight + float32(unavailable)*aiImportUnavailableRowHeight
	if unavailable > 0 {
		contentHeight += aiImportGroupHeight
	}
	if children > 1 {
		contentHeight += float32(children-1) * aiImportRowGap
	}
	listHeight := min(contentHeight, aiImportListMaxHeight)
	if total == 0 {
		listHeight = 40
	}
	bodyHeight := aiImportSummaryHeight + listHeight
	if cfg.importErr != "" {
		bodyHeight += aiImportErrorHeight
	}
	initial := hitAISetImportApply
	if selected == 0 {
		initial = hitAISetImportBack
	}
	pages := &state.ai.panel.importPages
	pages.listHeight = listHeight
	const width float32 = 760
	viewport := gfx.Rect{Width: c.Bounds().Width, Height: c.Bounds().Height}
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: width}, FitBody: true,
	}).Layout(m.ImportPreviewDesc)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.ImportPreviewTitle, Description: m.ImportPreviewDesc, Width: width,
		Height: layout.Content.Height + bodyHeight, Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitAISetFormDismiss, CloseID: hitAISetFormClose,
	})
	dialog.Body(func() { state.declareAIImportPreviewBody(c) })
	dialog.Footer(func() {
		back := ui.Button(c, m.ImportBack).Secondary().Shrink(0)
		if initial == hitAISetImportBack {
			back = back.AutoFocus()
		}
		if back.Clicked() {
			c.After(func() { state.aiDialogClick(hitAISetImportBack) })
		}
		apply := ui.Button(c, m.ImportApply).Shrink(0).Disabled(selected == 0)
		if initial == hitAISetImportApply {
			apply = apply.AutoFocus()
		}
		if apply.Clicked() {
			c.After(func() { state.aiDialogClick(hitAISetImportApply) })
		}
	})
	if dialog.Dismissed() {
		state.aiCloseDialog()
	}
}

func (state *State) declareAIImportResult(c ui.C) {
	cfg, m := &state.ai.cfg, state.messages.AISettings
	result := cfg.importResult
	bodyHeight := aiImportResultBannerHeight + 8
	extra := 0
	if result.GatewayCreated {
		extra++
	}
	if result.KeyFailed > 0 {
		extra++
	}
	if result.Failed > 0 {
		extra += 2
	} else if result.Err != "" {
		extra += 2
	}
	extra += len(result.Reasons)
	if extra > 0 {
		bodyHeight += float32(extra)*aiImportResultLine + float32(extra)*aiImportResultGap
	}
	const width float32 = 520
	viewport := gfx.Rect{Width: c.Bounds().Width, Height: c.Bounds().Height}
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: width}, FitBody: true,
	}).Layout("")
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.ImportDoneTitle, Width: width, Height: layout.Content.Height + bodyHeight,
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitAISetFormDismiss, CloseID: hitAISetFormClose,
	})
	dialog.Body(func() { state.declareAIImportResultBody(c) })
	dialog.Footer(func() {
		if ui.Button(c, m.ImportClose).Shrink(0).AutoFocus().Clicked() {
			c.After(func() { state.aiDialogClick(hitAISetImportClose) })
		}
	})
	if dialog.Dismissed() {
		state.aiCloseDialog()
	}
}

// declareAIImportSourcesBody declares the step that names the sources found on
// this computer. Its Preview buttons are page controls, so a press reads here.
func (state *State) declareAIImportSourcesBody(c ui.C, initial gfx.ID) {
	cfg, m, pal := &state.ai.cfg, state.messages.AISettings, state.pal
	ui.Column(c).Fill().PadSides(2, 4, 0, 4).Children(func() {
		for index, source := range cfg.importSources {
			ui.Row(c).Height(aiImportSourceRowHeight).Gap(12).Items(ui.Center).Children(func() {
				ui.Row(c).Size(32, 32).Shrink(0).Background(pal.accentSoft).Radius(radius.control).Center().Children(func() {
					ui.Icon(c, aiImportToolIcon(source.Tool)).Size(17, 17).Color(pal.accent)
				})
				ui.Column(c).Grow(1).Shrink(1).Children(func() {
					ui.Text(c, aiImportToolName(source.Tool)).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).
						Color(pal.text).LineHeight(20)
					ui.Text(c, state.aiImportSourceStatus(source)).FontSize(metrics.FontSizeS).
						Color(state.aiImportSourceStatusColor(source)).LineHeight(17).Ellipsis()
				})
				id := hitAISetImportSource.At(index)
				button := ui.Button(c, m.ImportPreview).Small().Outline().Shrink(0).Disabled(!source.Available())
				if id == initial {
					button = button.AutoFocus()
				}
				if button.Clicked() {
					c.After(func() { state.aiDialogClick(id) })
				}
			})
		}
		if len(cfg.importSources) == 0 {
			ui.Row(c).Height(40).Children(func() {
				ui.Text(c, m.ImportNoSources).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(20)
			})
		}

		ui.Divider(c).Background(pal.line).MarginSides(6, 0, 6, 0)
		ui.Row(c).Height(aiImportFallbackRowHeight).Gap(12).Items(ui.Center).Children(func() {
			// The empty tile keeps the fallback's text on the same column as
			// the source names above it.
			ui.Row(c).Size(32, 32).Shrink(0).Center().Children(func() {
				ui.Icon(c, icons.File).Size(16, 16).Color(pal.muted)
			})
			ui.Column(c).Grow(1).Shrink(1).Children(func() {
				ui.Text(c, m.ImportChooseFromFile).FontSize(metrics.FontSizeM).Color(pal.text).LineHeight(19)
				ui.Text(c, m.ImportChooseFileHint).FontSize(metrics.FontSizeS).Color(pal.muted).
					LineHeight(16).Ellipsis()
			})
			choose := ui.Button(c, m.ImportChooseFile).Small().Outline().Shrink(0)
			if initial == hitAISetImportChoose {
				choose = choose.AutoFocus()
			}
			if choose.Clicked() {
				c.After(func() { state.aiDialogClick(hitAISetImportChoose) })
			}
		})

		if cfg.importErr != "" {
			ui.Row(c).Height(22).Gap(8).Items(ui.Center).Children(func() {
				ui.Icon(c, icons.CircleAlert).Size(14, 14).Color(pal.danger).Shrink(0)
				ui.Text(c, cfg.importErr).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).
					Color(pal.danger).LineHeight(17).Ellipsis()
			})
		}
	})
}

// declareAIImportPreviewBody declares the count above the candidate list, the
// list itself, and any read error. The list is a scroll area of this page, so
// the wheel it takes and the offset it keeps belong to the dialog step.
func (state *State) declareAIImportPreviewBody(c ui.C) {
	cfg, m, pal := &state.ai.cfg, state.messages.AISettings, state.pal
	pages := &state.ai.panel.importPages
	total := len(cfg.importCandidates)
	importable := aiImportImportableCount(cfg.importCandidates)
	selected := state.aiImportSelectedCount()
	pages.list, pages.listSet = ui.Box{}, false
	ui.Column(c).Fill().PadSides(2, 4, 0, 4).Children(func() {
		// Only a full selection earns the check. A partial one keeps the dot,
		// so the icon never claims more than the count says.
		summaryIcon, summaryColor := icons.CircleDot, pal.muted
		if importable > 0 && selected == importable {
			summaryIcon, summaryColor = icons.CircleCheck, pal.success
		}
		ui.Row(c).Height(aiImportSummaryHeight).MarginSides(0, aiImportRowReserve, 0, 10).
			PadSides(0, 10, 0, 10).Gap(8).Items(ui.Center).Children(func() {
			ui.Icon(c, summaryIcon).Size(14, 14).Color(summaryColor).Shrink(0)
			textColor := pal.muted
			if selected > 0 {
				textColor = pal.text
			}
			ui.Text(c, fmt.Sprintf(m.ImportSelectedFmt, selected, importable)).FontSize(metrics.FontSizeS).
				Color(textColor).LineHeight(17)
		})

		if total == 0 {
			ui.Row(c).Height(40).Gap(8).Items(ui.Center).Children(func() {
				ui.Icon(c, icons.File).Size(15, 15).Color(pal.muted).Shrink(0)
				ui.Text(c, m.ImportNoCandidates).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18)
			})
		} else {
			state.declareAIImportList(c)
		}

		if cfg.importErr != "" {
			ui.Row(c).Height(22).Gap(8).Items(ui.Center).Children(func() {
				ui.Icon(c, icons.CircleAlert).Size(14, 14).Color(pal.danger).Shrink(0)
				ui.Text(c, cfg.importErr).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).
					Color(pal.danger).LineHeight(17).Ellipsis()
			})
		}
	})
}

// declareAIImportList declares the scrollable candidates: the importable rows
// first, then one heading and the entries that cannot be written. It is the
// page's scroll area, so the offset lives in the page's own view.
func (state *State) declareAIImportList(c ui.C) {
	cfg, m, pal := &state.ai.cfg, state.messages.AISettings, state.pal
	pages := &state.ai.panel.importPages
	importable := aiImportImportableCount(cfg.importCandidates)
	unavailable := len(cfg.importCandidates) - importable
	list := ui.Scroll(c).Height(pages.listHeight).FillWidth().Shrink(0).Gap(aiImportRowGap)
	list.HitWith(hitAISetImportList, m.ImportPreviewTitle, gfx.HitOptions{Role: gfx.RoleGroup})
	list.Children(func() {
		for index, candidate := range cfg.importCandidates {
			if candidate.Importable() {
				state.aiImportCandidateRow(c, index, candidate, cfg.importSelected[index])
				continue
			}
			if index == importable {
				ui.Row(c).Height(aiImportGroupHeight).MarginSides(4, aiImportRowReserve, 0, 2).
					Gap(8).Items(ui.Center).Children(func() {
					ui.Icon(c, icons.CircleAlert).Size(13, 13).Color(pal.warning).Shrink(0)
					ui.Text(c, fmt.Sprintf(m.ImportUnavailableFmt, unavailable)).FontSize(metrics.FontSizeS).
						Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(16)
				})
			}
			state.aiImportUnavailableRow(c, candidate)
		}
	})
	// The list's only sign that there is more below; it is painted over the
	// clipped rows once the page has laid them out.
	list.DrawOver(func(d *ui.DrawContext) { state.paintAIImportScrollbar(d.Canvas, d.Rect) })
	pages.list, pages.listSet = list, true
}

// aiImportListScroll is the candidate list's scroll state, nil when the list
// was not declared in the last paint.
func (state *State) aiImportListScroll() *ui.ScrollState {
	pages := &state.ai.panel.importPages
	if !pages.listSet {
		return nil
	}
	return pages.list.ScrollState()
}

// aiImportCandidateRow declares one selectable provider. A ticked row carries a
// quiet surface tint, so the selection is legible at a glance without turning
// the checkbox into the only mark of state.
func (state *State) aiImportCandidateRow(c ui.C, index int, candidate aiproviderimport.Candidate, checked bool) {
	background := gfx.Color{}
	if checked {
		background = state.pal.surfaceSoft
	}
	ui.Row(c).Height(aiImportRowHeight).MarginSides(0, aiImportRowReserve, 0, 0).
		PadSides(0, 10, 0, 10).Gap(10).Items(ui.Center).Background(background).
		Radius(radius.control).Children(func() {
		// The checkbox keeps the identity the central click dispatcher toggles.
		// The page has no checkbox that takes an explicit ID and leaves the
		// value to the application, so the row reserves the component's hit
		// extent and draws the same box into it: one click still toggles once,
		// and the choice stays in the import state.
		theme := *c.Theme()
		side := components.CheckboxExtent(theme)
		hitWidth, hitHeight := components.CheckboxHitExtent(theme)
		id, label := hitAISetImportRow.At(index), candidate.Name
		ui.Slot(c).Size(hitWidth, hitHeight).Draw(func(d *ui.DrawContext) {
			bounds := gfx.Rect{
				X: d.Rect.X + (d.Rect.Width-side)/2, Y: d.Rect.Y + (d.Rect.Height-side)/2,
				Width: side, Height: side,
			}
			components.BeginCheckbox(d.Canvas, state.Theme(), id, bounds, checked).Label(label).Paint()
		})
		ui.Icon(c, aiImportKindIcon(candidate.Kind)).Size(18, 18).Color(state.pal.muted).Shrink(0)
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			ui.Text(c, candidate.Name).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).
				Color(state.pal.text).LineHeight(20).Ellipsis()
			ui.Text(c, state.aiImportCandidateDetail(candidate)).FontSize(metrics.FontSizeS).
				Color(state.pal.muted).LineHeight(16).Ellipsis()
		})
	})
}

// aiImportUnavailableRow declares an entry that cannot be written. It has no
// checkbox: the row is information, not a disabled control, and the reason sits
// on its second line.
func (state *State) aiImportUnavailableRow(c ui.C, candidate aiproviderimport.Candidate) {
	ui.Row(c).Height(aiImportUnavailableRowHeight).MarginSides(0, aiImportRowReserve, 0, 0).
		PadSides(0, 10, 0, 10).Gap(10).Items(ui.Center).Children(func() {
		ui.Slot(c).Size(16, 16)
		ui.Icon(c, icons.CircleAlert).Size(16, 16).Color(state.pal.warning).Shrink(0)
		// The reason rows skip the checkbox and the kind tile. The left margin
		// puts their names on the same column as the importable rows, so the
		// block still reads as one list.
		ui.Column(c).Grow(1).Shrink(1).MarginSides(0, 0, 0, 26).Children(func() {
			ui.Text(c, candidate.Name).FontSize(metrics.FontSizeM).Color(state.pal.muted).
				LineHeight(19).Ellipsis()
			ui.Text(c, state.aiImportCandidateDetail(candidate)).FontSize(metrics.FontSizeS).
				Color(state.pal.muted).LineHeight(16).Ellipsis()
		})
	})
}

// paintAIImportScrollbar is the list's only sign that there is more below. It
// is painted from the state the page's layout reported, after the clip is
// closed, so it does not move with the rows.
func (state *State) paintAIImportScrollbar(canvas *gfx.Canvas, bounds gfx.Rect) {
	scroll := state.aiImportListScroll()
	if scroll == nil || scroll.Max() <= 0 || bounds.Height <= 12 {
		return
	}
	track := gfx.Rect{
		X: bounds.X + bounds.Width - 5, Y: bounds.Y + 6,
		Width: 3, Height: bounds.Height - 12,
	}
	canvas.Rounded(track, 1.5, state.pal.line)
	thumbHeight := max(track.Height*min(scroll.Viewport/scroll.Content, 1), 20)
	thumbHeight = min(thumbHeight, track.Height)
	offset := scroll.Offset / scroll.Max()
	thumb := gfx.Rect{
		X: track.X, Y: track.Y + (track.Height-thumbHeight)*offset,
		Width: track.Width, Height: thumbHeight,
	}
	canvas.Rounded(thumb, 1.5, state.pal.muted)
}

// declareAIImportResultBody declares the outcome of the import: what changed,
// and the notes that need attention under it.
func (state *State) declareAIImportResultBody(c ui.C) {
	result, m, pal := state.ai.cfg.importResult, state.messages.AISettings, state.pal

	icon, color := icons.CircleCheck, pal.success
	if result.Failed > 0 || result.KeyFailed > 0 || result.Err != "" {
		icon, color = icons.CircleAlert, pal.danger
	} else if result.Created+result.Updated == 0 {
		icon, color = icons.CircleDot, pal.muted
	}

	ui.Column(c).Fill().PadSides(2, 4, 0, 4).Gap(aiImportResultGap).Children(func() {
		ui.Row(c).Height(aiImportResultBannerHeight).PadSides(10, 12, 10, 12).
			Background(pal.surfaceSoft).Radius(radius.control).Gap(10).
			Items(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(18, 18).Color(color).Shrink(0)
			ui.Text(c, fmt.Sprintf(m.ImportSummaryFmt, result.Created, result.Updated, result.Skipped)).
				FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(20)
		})
		if result.GatewayCreated {
			state.aiImportResultLineRow(c, icons.Info, pal.muted, m.ImportGatewayImported)
		}
		if result.KeyFailed > 0 {
			state.aiImportResultLineRow(c, icons.Key, pal.danger,
				fmt.Sprintf(m.ImportKeyFailedFmt, result.KeyFailed))
		}
		if result.Failed > 0 {
			state.aiImportResultProblemRow(c, fmt.Sprintf(m.ImportFailedFmt, result.Failed, result.Err))
		} else if result.Err != "" {
			state.aiImportResultProblemRow(c, result.Err)
		}
		for _, reason := range result.Reasons {
			state.aiImportResultLineRow(c, icons.CircleDot, pal.muted,
				fmt.Sprintf(m.ImportReasonCountFmt, state.aiImportReasonText(reason.Reason), reason.Count))
		}
	})
}

// aiImportResultLineRow is one compact note under the outcome. The icon carries
// the kind of note so the line does not need a colored label.
func (state *State) aiImportResultLineRow(c ui.C, icon gfx.Icon, color gfx.Color, text string) {
	ui.Row(c).Height(aiImportResultLine).Gap(8).Items(ui.Center).Children(func() {
		ui.Icon(c, icon).Size(14, 14).Color(color).Shrink(0)
		ui.Text(c, text).FontSize(metrics.FontSizeS).Color(color).LineHeight(18).Ellipsis()
	})
}

// aiImportResultProblemRow wraps a save failure instead of clipping it: the
// reason is what the user needs to act on.
func (state *State) aiImportResultProblemRow(c ui.C, text string) {
	ui.Row(c).Height(2 * aiImportResultLine).Gap(8).Items(ui.Start).Children(func() {
		ui.Icon(c, icons.CircleAlert).Size(14, 14).Color(state.pal.danger).Shrink(0).MarginSides(3, 0, 0, 0)
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			ui.Paragraph(c, text, 2).FontSize(metrics.FontSizeS).Color(state.pal.danger).LineHeight(18)
		})
	})
}

// aiImportToolName is the proper name of the application a source came from.
func aiImportToolName(tool aiproviderimport.Tool) string {
	if tool == aiproviderimport.ToolCCSwitch {
		return "cc-switch"
	}
	return "magpie"
}

// aiImportToolIcon is the source application's own mark, so the row names the
// app people recognize rather than an abstract stand-in. magpie's is a single
// path and takes the tile's accent; cc-switch's is its full-color starburst and
// ignores the tint.
func aiImportToolIcon(tool aiproviderimport.Tool) gfx.Icon {
	if tool == aiproviderimport.ToolCCSwitch {
		return icons.CcSwitch
	}
	return icons.Magpie
}

func aiImportKindIcon(kind string) gfx.Icon {
	if kind == store.KindAnthropic {
		return icons.AiClaude
	}
	return icons.AiOpenai
}

func (state *State) aiImportSourceStatus(source aiproviderimport.SourceInfo) string {
	m := state.messages.AISettings
	switch {
	case !source.Found:
		return m.ImportSourceMissing
	case source.Err != nil:
		return fmt.Sprintf(m.ImportSourceErrorFmt, source.Err)
	default:
		return fmt.Sprintf(m.ImportSourceCountFmt, source.Importable(), len(source.Candidates))
	}
}

// aiImportSourceStatusColor keeps a source that cannot be read in the danger
// color; a missing source is simply quiet.
func (state *State) aiImportSourceStatusColor(source aiproviderimport.SourceInfo) gfx.Color {
	if source.Found && source.Err != nil {
		return state.pal.danger
	}
	return state.pal.muted
}

// aiImportCandidateDetail is the second line of a preview row. It shows key
// presence, never a key. An unavailable row spends that line on the reason
// instead of its irrelevant model and key counts.
func (state *State) aiImportCandidateDetail(candidate aiproviderimport.Candidate) string {
	m := state.messages.AISettings
	parts := []string{state.aiImportSourceLabel(candidate.Source)}
	if kind := state.aiImportKindLabel(candidate.Kind); kind != "" {
		parts = append(parts, kind)
	}
	if host := aiImportHost(candidate.BaseURL); host != "" {
		parts = append(parts, host)
	}
	if !candidate.Importable() {
		return strings.Join(parts, "  ·  ") + "  ·  " + state.aiImportReasonText(candidate.SkipReason)
	}
	key := m.ProviderNoKey
	if candidate.HasKey() {
		key = m.ProviderKeySaved
	}
	parts = append(parts, key, fmt.Sprintf(m.ImportModelsFmt, len(candidate.Models)))
	detail := strings.Join(parts, "  ·  ")
	if candidate.Note == aiproviderimport.NoteMagpieGateway {
		detail += "  ·  " + m.ImportGatewayNote
	}
	return detail
}

func (state *State) aiImportSourceLabel(source aiproviderimport.Source) string {
	m := state.messages.AISettings
	switch source {
	case aiproviderimport.SourceMagpieGateway:
		return m.ImportGatewaySource
	case aiproviderimport.SourceMagpieUpstream:
		return "magpie"
	default:
		return "cc-switch"
	}
}

func (state *State) aiImportKindLabel(kind string) string {
	m := state.messages.AISettings
	switch kind {
	case store.KindAnthropic:
		return m.ProviderKindClaude
	case store.KindOpenAI:
		return m.ProviderKindOpenAI
	}
	return ""
}

func (state *State) aiImportReasonText(reason string) string {
	m := state.messages.AISettings
	switch reason {
	case aiproviderimport.ReasonDisabled:
		return m.ImportReasonDisabled
	case aiproviderimport.ReasonNoEndpoint:
		return m.ImportReasonNoEndpoint
	case aiproviderimport.ReasonUnsupportedProtocol:
		return m.ImportReasonUnsupportedProtocol
	case aiproviderimport.ReasonResponses:
		return m.ImportReasonResponses
	case aiproviderimport.ReasonOAuth:
		return m.ImportReasonOAuth
	case aiproviderimport.ReasonGemini:
		return m.ImportReasonGemini
	case aiproviderimport.ReasonProxy:
		return m.ImportReasonProxy
	case aiproviderimport.ReasonUnsupportedApp:
		return m.ImportReasonUnsupportedApp
	case aiproviderimport.ReasonAmbiguous:
		return m.ImportReasonAmbiguous
	case aiproviderimport.ReasonNoChat:
		return m.ImportReasonNoChat
	case aiproviderimport.ReasonUnsupportedAuth:
		return m.ImportReasonUnsupportedAuth
	}
	return m.ImportReasonMalformed
}
