package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// keysSectionUI is what the keychain page's declaration leaves for its paint.
type keysSectionUI struct {
	empty, addButton, removeButton, passphraseButton, certificateButton, copyButton, exportButton ui.Box
	copyWidth, exportWidth, passphraseWidth, certificateWidth, removeWidth, addWidth              float32
	copyIcon, exportIcon, passphraseIcon, certificateIcon, removeIcon, addIcon                    uint8
	condensed                                                                                     bool
	// draw is the page hook that paints the empty state and the Add Key menu. It
	// is kept so a paint does not build a closure for the hook.
	draw func(*ui.DrawContext)
}

// declareKeysWorkspace declares the keychain page in the workbench page: its
// header, its open/empty body and its footer. The buttons, the table and the
// empty state's icon are painted into the slots it leaves.
func (state *State) declareKeysWorkspace(c ui.C) {
	canvas := c.Canvas()
	pal, messages := state.pal, state.messages
	bounds := state.catalogSectionBounds()
	// The title keeps a readable column beside the buttons; a card too narrow for
	// both shows the buttons as their icons, whose tooltips name them.
	const (
		titleLeast = float32(220)
		gap        = float32(10)
	)
	labeled := toolbarButtonWidth(canvas, messages.KeyCopyPublic, true) + gap + toolbarButtonWidth(canvas, messages.KeyExport, true) + gap + toolbarButtonWidth(canvas, messages.KeySavePassphrase, true) + gap + toolbarButtonWidth(canvas, messages.KeyCertificate, true) + gap + toolbarButtonWidth(canvas, messages.KeyRemove, false) + gap + toolbarButtonWidth(canvas, messages.KeyAdd, true) + gap + titleLeast
	condensed := labeled > bounds.Width-36
	copyWidth, copyIcon := toolbarButtonLayout(canvas, messages.KeyCopyPublic, iconCopy, iconCopy, condensed)
	exportWidth, exportIcon := toolbarButtonLayout(canvas, messages.KeyExport, iconDownload, iconDownload, condensed)
	passphraseWidth, passphraseIcon := toolbarButtonLayout(canvas, messages.KeySavePassphrase, iconLock, iconLock, condensed)
	certificateWidth, certificateIcon := toolbarButtonLayout(canvas, messages.KeyCertificate, iconCircleCheck, iconCircleCheck, condensed)
	removeWidth, removeIcon := toolbarButtonLayout(canvas, messages.KeyRemove, 0, iconTrash, condensed)
	addWidth, addIcon := toolbarButtonLayout(canvas, messages.KeyAdd, iconPlus, iconPlus, condensed)
	titleRoom := max(bounds.Width-36-copyWidth-gap-exportWidth-gap-passphraseWidth-gap-certificateWidth-gap-removeWidth-gap-addWidth, 0)
	statusColor, statusValue := pal.muted, ""
	selected, hasSelection := state.identityByID(state.selectedIdentityID)
	if hasSelection {
		statusValue = selected.Path
		if selected.Stored {
			statusValue = messages.KeyStoredIn
		}
		if selected.Error != "" {
			statusColor, statusValue = pal.danger, selected.Error
		}
	}
	selectedEncrypted := hasSelection && selected.Encrypted && !selected.SecurityKey
	canCopy := hasSelection && selected.Error == ""
	canCertify := hasSelection && selected.Error == "" && selected.Stored
	view := &state.sectionBodies.keys
	draw := view.draw
	*view = keysSectionUI{
		copyWidth: copyWidth, exportWidth: exportWidth, passphraseWidth: passphraseWidth, certificateWidth: certificateWidth, removeWidth: removeWidth, addWidth: addWidth,
		copyIcon: copyIcon, exportIcon: exportIcon, passphraseIcon: passphraseIcon, certificateIcon: certificateIcon, removeIcon: removeIcon, addIcon: addIcon,
		condensed: condensed,
	}
	if draw == nil {
		draw = func(d *ui.DrawContext) { state.paintKeysWorkspace(d.Canvas) }
	}
	view.draw = draw
	body := ui.Column(c).Fill().PadSides(12, 18, 0, 18)
	body.Children(func() {
		ui.Row(c).Children(func() {
			ui.Column(c).Grow(1).PadSides(3, 0, 0, 0).Children(func() {
				ui.Text(c, messages.KeyTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(28).Width(min(bounds.Width*0.45, titleRoom)).Ellipsis()
				ui.Paragraph(c, messages.KeyDescription, 3).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(22).Width(min(bounds.Width*0.62, titleRoom))
			})
			view.copyButton = state.declareButtonSized(c, hitKeysCopyPublic, messages.KeyCopyPublic, copyIcon, false, false, canCopy, toolbarFont, condensed).Size(copyWidth, toolbarHeight).MarginSides(0, gap, 0, 0).Shrink(0)
			view.exportButton = state.declareButtonSized(c, hitKeysExport, messages.KeyExport, exportIcon, false, false, canCopy, toolbarFont, condensed).Size(exportWidth, toolbarHeight).MarginSides(0, gap, 0, 0).Shrink(0)
			view.passphraseButton = state.declareButtonSized(c, hitKeysPassphrase, messages.KeySavePassphrase, passphraseIcon, false, false, selectedEncrypted, toolbarFont, condensed).Size(passphraseWidth, toolbarHeight).MarginSides(0, gap, 0, 0).Shrink(0)
			view.certificateButton = state.declareButtonSized(c, hitKeysCertificate, messages.KeyCertificate, certificateIcon, false, false, canCertify, toolbarFont, condensed).Size(certificateWidth, toolbarHeight).MarginSides(0, gap, 0, 0).Shrink(0)
			view.removeButton = state.declareButtonSized(c, hitKeysRemove, messages.KeyRemove, removeIcon, false, false, state.selectedIdentityID != 0, toolbarFont, condensed).Size(removeWidth, toolbarHeight).MarginSides(0, gap, 0, 0).Shrink(0)
			view.addButton = state.declareButtonSized(c, hitKeysAdd, messages.KeyAdd, addIcon, true, false, true, toolbarFont, condensed).Size(addWidth, toolbarHeight).Shrink(0)
			state.declareKeysMenu(c, view.addButton)
		})
		if len(state.identities) == 0 {
			view.empty = state.reserveEmptyPage(c, bounds, messages.KeyEmpty, messages.KeyEmptySub, messages.KeyAdd, hitKeysAdd, iconKey, true)
		} else {
			state.keysColumns = [...]components.TableColumn{
				{Title: messages.KeyColumnName, Width: 190},
				{Title: messages.KeyColumnType, Width: 150},
				{Title: messages.KeyColumnProtection, Width: 120},
				{Title: messages.KeyColumnCertificate, Width: 105},
				{Title: messages.KeyColumnFingerprint},
			}
			ui.Table(c, &state.keysTable, ui.TableSpec{
				Rows: hitKeyRowList, Columns: state.keysColumns[:], FitHeadings: true, Source: &state.keysRows,
				Scrollbar: hitKeysScrollThumb, ScrollbarLabel: messages.KeyListScroll,
				ScrollbarGutter: catalogBarGutter,
			}).Grow(1).Shrink(1)
			// A click or an arrow key chose a row: the detail follows it. Otherwise
			// the table shows the key the application selected; the table lays out
			// after the declaration, so this paint already shows it.
			if state.keysTable.Changed() && state.modal == modalNone {
				state.takeKeyTableSelection()
			} else {
				state.syncKeyTable()
			}
		}
		ui.Divider(c).Background(pal.line)
		ui.Row(c).Height(50).PadSides(8, 0, 0, 0).Gap(4).Items(ui.Start).Children(func() {
			ui.Text(c, state.identityCountText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).LineHeight(22).Width(120).Shrink(0)
			if statusValue != "" {
				ui.Text(c, statusValue).FontSize(metrics.FontSizeS).Color(statusColor).LineHeight(22).Grow(1)
			}
		})
	})
	body.Clip().DrawOver(view.draw)
}

// paintKeysWorkspace paints the keychain page's empty state and Add Key menu into
// the slots its declaration left. A table is painted by the page itself.
func (state *State) paintKeysWorkspace(canvas *gfx.Canvas) {
	view := &state.sectionBodies.keys
	if len(state.identities) == 0 {
		state.drawReservedEmptyPage(canvas, view.empty, iconKey)
	}
}

// declareKeysMenu declares the ways to add a key under the Add Key button. A
// choice runs as if its control had been clicked.
func (state *State) declareKeysMenu(c ui.C, anchor ui.Box) {
	if !state.keysMenu {
		return
	}
	m := state.messages
	ui.PopupLayer(c, dialogLayer)
	ui.Menu(c, anchor, &state.keysMenu, func(menu *ui.MenuItems) {
		menu.WidthRange(220, 220)
		for _, way := range [...]struct {
			id    gfx.ID
			label string
		}{{hitKeysImport, m.KeyImport}, {hitKeysPaste, m.KeyPaste}, {hitKeysGenerate, m.KeyGenerate}} {
			if menu.Item(way.label).Clicked() {
				c.After(func() { state.click(way.id, true) })
			}
		}
	})
}
