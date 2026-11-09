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
// and the reasons read as a reference block. The list scrolls rather than
// splitting into pages; settingsRow owns that scroll state and State.Scroll
// hands it the wheel while this dialog is open.

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

func (state *State) paintAIImport(canvas *gfx.Canvas, viewport gfx.Rect) {
	switch state.ai.cfg.importStep {
	case aiImportSources:
		state.paintAIImportSources(canvas, viewport)
	case aiImportPreview:
		state.paintAIImportPreview(canvas, viewport)
	case aiImportDone:
		state.paintAIImportResult(canvas, viewport)
	}
}

func (state *State) paintAIImportSources(canvas *gfx.Canvas, viewport gfx.Rect) {
	cfg, m, theme := &state.ai.cfg, state.messages.AISettings, state.Theme()

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
	body := rowsHeight + aiImportDividerHeight + aiImportFallbackRowHeight
	if cfg.importErr != "" {
		body += aiImportErrorHeight
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitAISetFormDismiss, CloseID: hitAISetFormClose, InitialFocusID: initial,
		ContentSize: gfx.Size{Width: 620}, FitBody: true, BodyHeight: body,
	})
	layout := dialog.Begin(m.ImportSourcesTitle, m.ImportSourcesDesc)

	var sourceSlots []ui.Box
	var choose ui.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		for _, source := range cfg.importSources {
			f.Row().Height(aiImportSourceRowHeight).Gap(12).Items(ui.Center).Children(func() {
				f.Row().Size(32, 32).Shrink(0).Background(state.pal.accentSoft).Radius(radius.control).Center().Children(func() {
					f.Icon(aiImportToolIcon(source.Tool)).Size(17, 17).Color(state.pal.accent)
				})
				f.Column().Grow(1).Shrink(1).Children(func() {
					f.Text(aiImportToolName(source.Tool)).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).
						Color(state.pal.text).LineHeight(20)
					f.Text(state.aiImportSourceStatus(source)).FontSize(metrics.FontSizeS).
						Color(state.aiImportSourceStatusColor(source)).LineHeight(17).Ellipsis()
				})
				sourceSlots = append(sourceSlots, state.aiButtonSlot(f, m.ImportPreview))
			})
		}
		if len(cfg.importSources) == 0 {
			f.Row().Height(40).Children(func() {
				f.Text(m.ImportNoSources).FontSize(metrics.FontSizeS).Color(state.pal.muted).LineHeight(20)
			})
		}

		f.Divider().Background(state.pal.line).MarginSides(6, 0, 6, 0)
		f.Row().Height(aiImportFallbackRowHeight).Gap(12).Items(ui.Center).Children(func() {
			// The empty tile keeps the fallback's text on the same column as
			// the source names above it.
			f.Row().Size(32, 32).Shrink(0).Center().Children(func() {
				f.Icon(icons.File).Size(16, 16).Color(state.pal.muted)
			})
			f.Column().Grow(1).Shrink(1).Children(func() {
				f.Text(m.ImportChooseFromFile).FontSize(metrics.FontSizeM).Color(state.pal.text).LineHeight(19)
				f.Text(m.ImportChooseFileHint).FontSize(metrics.FontSizeS).Color(state.pal.muted).
					LineHeight(16).Ellipsis()
			})
			choose = state.aiButtonSlot(f, m.ImportChooseFile)
		})

		if cfg.importErr != "" {
			f.Row().Height(22).Gap(8).Items(ui.Center).Children(func() {
				f.Icon(icons.CircleAlert).Size(14, 14).Color(state.pal.danger).Shrink(0)
				f.Text(cfg.importErr).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).
					Color(state.pal.danger).LineHeight(17).Ellipsis()
			})
		}
	})
	f.End()

	for index, source := range cfg.importSources {
		if index >= len(sourceSlots) {
			continue
		}
		state.aiButton(canvas, hitAISetImportSource.At(index), sourceSlots[index].Rect(), m.ImportPreview,
			aiButtonOutline, !source.Available())
	}
	state.aiButton(canvas, hitAISetImportChoose, choose.Rect(), m.ImportChooseFile, aiButtonOutline, false)
	dialog.Actions(layout, components.ButtonAction{ID: hitAISetImportClose, Label: state.messages.Cancel, Variant: components.ButtonSecondary})
	dialog.End()
}

func (state *State) paintAIImportPreview(canvas *gfx.Canvas, viewport gfx.Rect) {
	cfg, m, theme := &state.ai.cfg, state.messages.AISettings, state.Theme()
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
	body := aiImportSummaryHeight + listHeight
	if cfg.importErr != "" {
		body += aiImportErrorHeight
	}

	initial := hitAISetImportApply
	if selected == 0 {
		initial = hitAISetImportBack
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitAISetFormDismiss, CloseID: hitAISetFormClose, InitialFocusID: initial,
		ContentSize: gfx.Size{Width: 760}, FitBody: true, BodyHeight: body,
	})
	layout := dialog.Begin(m.ImportPreviewTitle, m.ImportPreviewDesc)

	var list ui.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		// Only a full selection earns the check. A partial one keeps the dot,
		// so the icon never claims more than the count says.
		summaryIcon, summaryColor := icons.CircleDot, state.pal.muted
		if importable > 0 && selected == importable {
			summaryIcon, summaryColor = icons.CircleCheck, state.pal.success
		}
		f.Row().Height(aiImportSummaryHeight).MarginSides(0, aiImportRowReserve, 0, 10).
			PadSides(0, 10, 0, 10).Gap(8).Items(ui.Center).Children(func() {
			f.Icon(summaryIcon).Size(14, 14).Color(summaryColor).Shrink(0)
			textColor := state.pal.muted
			if selected > 0 {
				textColor = state.pal.text
			}
			f.Text(fmt.Sprintf(m.ImportSelectedFmt, selected, importable)).FontSize(metrics.FontSizeS).
				Color(textColor).LineHeight(17)
		})

		if total == 0 {
			f.Row().Height(40).Gap(8).Items(ui.Center).Children(func() {
				f.Icon(icons.File).Size(15, 15).Color(state.pal.muted).Shrink(0)
				f.Text(m.ImportNoCandidates).FontSize(metrics.FontSizeS).Color(state.pal.muted).LineHeight(18)
			})
		} else {
			list = f.ScrollArea(hitAISetImportList).Height(listHeight).FillWidth().Gap(aiImportRowGap)
			list.HitWith(hitAISetImportList, m.ImportPreviewTitle, gfx.HitOptions{Role: gfx.RoleGroup})
			list.Children(func() {
				for index, candidate := range cfg.importCandidates {
					if candidate.Importable() {
						state.aiImportCandidateRow(f, index, candidate, cfg.importSelected[index])
						continue
					}
					if index == importable {
						f.Row().Height(aiImportGroupHeight).MarginSides(4, aiImportRowReserve, 0, 2).
							Gap(8).Items(ui.Center).Children(func() {
							f.Icon(icons.CircleAlert).Size(13, 13).Color(state.pal.warning).Shrink(0)
							f.Text(fmt.Sprintf(m.ImportUnavailableFmt, unavailable)).FontSize(metrics.FontSizeS).
								Weight(gfx.WeightMedium).Color(state.pal.muted).LineHeight(16)
						})
					}
					state.aiImportUnavailableRow(f, candidate)
				}
			})
		}

		if cfg.importErr != "" {
			f.Row().Height(22).Gap(8).Items(ui.Center).Children(func() {
				f.Icon(icons.CircleAlert).Size(14, 14).Color(state.pal.danger).Shrink(0)
				f.Text(cfg.importErr).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).
					Color(state.pal.danger).LineHeight(17).Ellipsis()
			})
		}
	})
	f.End()

	if total > 0 && contentHeight > listHeight {
		state.paintAIImportScrollbar(canvas, list.Rect())
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitAISetImportBack, Label: m.ImportBack, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitAISetImportApply, Label: m.ImportApply, Disabled: selected == 0},
	)
	dialog.End()
}

// aiImportCandidateRow paints one selectable provider. A ticked row carries a
// quiet surface tint, so the selection is legible at a glance without turning
// the checkbox into the only mark of state.
func (state *State) aiImportCandidateRow(f *ui.Frame, index int, candidate aiproviderimport.Candidate, checked bool) {
	background := gfx.Color{}
	if checked {
		background = state.pal.surfaceSoft
	}
	f.Row().Height(aiImportRowHeight).MarginSides(0, aiImportRowReserve, 0, 0).
		PadSides(0, 10, 0, 10).Gap(10).Items(ui.Center).Background(background).
		Radius(radius.control).Children(func() {
		f.Checkbox(hitAISetImportRow.At(index), checked).Label(candidate.Name)
		f.Icon(aiImportKindIcon(candidate.Kind)).Size(18, 18).Color(state.pal.muted).Shrink(0)
		f.Column().Grow(1).Shrink(1).Children(func() {
			f.Text(candidate.Name).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).
				Color(state.pal.text).LineHeight(20).Ellipsis()
			f.Text(state.aiImportCandidateDetail(candidate)).FontSize(metrics.FontSizeS).
				Color(state.pal.muted).LineHeight(16).Ellipsis()
		})
	})
}

// aiImportUnavailableRow paints an entry that cannot be written. It has no
// checkbox: the row is information, not a disabled control, and the reason sits
// on its second line.
func (state *State) aiImportUnavailableRow(f *ui.Frame, candidate aiproviderimport.Candidate) {
	f.Row().Height(aiImportUnavailableRowHeight).MarginSides(0, aiImportRowReserve, 0, 0).
		PadSides(0, 10, 0, 10).Gap(10).Items(ui.Center).Children(func() {
		f.Slot().Size(16, 16)
		f.Icon(icons.CircleAlert).Size(16, 16).Color(state.pal.warning).Shrink(0)
		// The reason rows skip the checkbox and the kind tile. The left margin
		// puts their names on the same column as the importable rows, so the
		// block still reads as one list.
		f.Column().Grow(1).Shrink(1).MarginSides(0, 0, 0, 26).Children(func() {
			f.Text(candidate.Name).FontSize(metrics.FontSizeM).Color(state.pal.muted).
				LineHeight(19).Ellipsis()
			f.Text(state.aiImportCandidateDetail(candidate)).FontSize(metrics.FontSizeS).
				Color(state.pal.muted).LineHeight(16).Ellipsis()
		})
	})
}

// paintAIImportScrollbar is the list's only sign that there is more below. It
// is painted from the scroll state the layout just reported, after the clip is
// closed, so it does not move with the rows.
func (state *State) paintAIImportScrollbar(canvas *gfx.Canvas, bounds gfx.Rect) {
	scroll := state.settingsRow.ScrollState(hitAISetImportList)
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

func (state *State) paintAIImportResult(canvas *gfx.Canvas, viewport gfx.Rect) {
	cfg, m, theme := &state.ai.cfg, state.messages.AISettings, state.Theme()
	result := cfg.importResult

	body := aiImportResultBannerHeight + 8
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
		body += float32(extra)*aiImportResultLine + float32(extra)*aiImportResultGap
	}

	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitAISetFormDismiss, CloseID: hitAISetFormClose, InitialFocusID: hitAISetImportClose,
		ContentSize: gfx.Size{Width: 520}, FitBody: true, BodyHeight: body,
	})
	layout := dialog.Begin(m.ImportDoneTitle, "")

	icon, color := icons.CircleCheck, state.pal.success
	if result.Failed > 0 || result.KeyFailed > 0 || result.Err != "" {
		icon, color = icons.CircleAlert, state.pal.danger
	} else if result.Created+result.Updated == 0 {
		icon, color = icons.CircleDot, state.pal.muted
	}

	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Gap(aiImportResultGap).Children(func() {
		f.Row().Height(aiImportResultBannerHeight).PadSides(10, 12, 10, 12).
			Background(state.pal.surfaceSoft).Radius(radius.control).Gap(10).
			Items(ui.Center).Children(func() {
			f.Icon(icon).Size(18, 18).Color(color).Shrink(0)
			f.Text(fmt.Sprintf(m.ImportSummaryFmt, result.Created, result.Updated, result.Skipped)).
				FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(state.pal.text).LineHeight(20)
		})
		if result.GatewayCreated {
			state.aiImportResultLineRow(f, icons.Info, state.pal.muted, m.ImportGatewayImported)
		}
		if result.KeyFailed > 0 {
			state.aiImportResultLineRow(f, icons.Key, state.pal.danger,
				fmt.Sprintf(m.ImportKeyFailedFmt, result.KeyFailed))
		}
		if result.Failed > 0 {
			state.aiImportResultProblemRow(f, fmt.Sprintf(m.ImportFailedFmt, result.Failed, result.Err))
		} else if result.Err != "" {
			state.aiImportResultProblemRow(f, result.Err)
		}
		for _, reason := range result.Reasons {
			state.aiImportResultLineRow(f, icons.CircleDot, state.pal.muted,
				fmt.Sprintf(m.ImportReasonCountFmt, state.aiImportReasonText(reason.Reason), reason.Count))
		}
	})
	f.End()
	dialog.Actions(layout, components.ButtonAction{ID: hitAISetImportClose, Label: m.ImportClose})
	dialog.End()
}

// aiImportResultLineRow is one compact note under the outcome. The icon carries
// the kind of note so the line does not need a colored label.
func (state *State) aiImportResultLineRow(f *ui.Frame, icon gfx.Icon, color gfx.Color, text string) {
	f.Row().Height(aiImportResultLine).Gap(8).Items(ui.Center).Children(func() {
		f.Icon(icon).Size(14, 14).Color(color).Shrink(0)
		f.Text(text).FontSize(metrics.FontSizeS).Color(color).LineHeight(18).Ellipsis()
	})
}

// aiImportResultProblemRow wraps a save failure instead of clipping it: the
// reason is what the user needs to act on.
func (state *State) aiImportResultProblemRow(f *ui.Frame, text string) {
	f.Row().Height(2 * aiImportResultLine).Gap(8).Items(ui.Start).Children(func() {
		f.Icon(icons.CircleAlert).Size(14, 14).Color(state.pal.danger).Shrink(0).MarginSides(3, 0, 0, 0)
		f.Column().Grow(1).Shrink(1).Children(func() {
			f.Paragraph(text, 2).FontSize(metrics.FontSizeS).Color(state.pal.danger).LineHeight(18)
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
