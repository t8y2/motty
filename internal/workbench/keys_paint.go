package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (state *State) paintKeysWorkspace(canvas *gfx.Canvas, bounds gfx.Rect) {
	pal, messages := state.pal, state.messages
	// The title keeps a readable column beside the buttons; a card too narrow for
	// both shows the buttons as their icons, whose tooltips name them.
	const (
		titleLeast = float32(220)
		gap        = float32(10)
	)
	labeled := toolbarButtonWidth(canvas, messages.KeyCopyPublic, true) + gap + toolbarButtonWidth(canvas, messages.KeySavePassphrase, true) + gap + toolbarButtonWidth(canvas, messages.KeyRemove, false) + gap + toolbarButtonWidth(canvas, messages.KeyAdd, true) + gap + titleLeast
	condensed := labeled > bounds.Width-36
	copyWidth, copyIcon := toolbarButtonLayout(canvas, messages.KeyCopyPublic, iconCopy, iconCopy, condensed)
	passphraseWidth, passphraseIcon := toolbarButtonLayout(canvas, messages.KeySavePassphrase, iconLock, iconLock, condensed)
	removeWidth, removeIcon := toolbarButtonLayout(canvas, messages.KeyRemove, 0, iconTrash, condensed)
	addWidth, addIcon := toolbarButtonLayout(canvas, messages.KeyAdd, iconPlus, iconPlus, condensed)
	titleRoom := max(bounds.Width-36-copyWidth-gap-passphraseWidth-gap-removeWidth-gap-addWidth, 0)
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
	selectedEncrypted := hasSelection && selected.Encrypted
	canCopy := hasSelection && selected.Error == ""

	var content, addButton, removeButton, passphraseButton, copyButton, empty ui.Box
	state.drawSectionBody(canvas, bounds, func(c ui.C) {
		ui.Column(c).Fill().PadSides(12, 18, 0, 18).Children(func() {
			ui.Row(c).Children(func() {
				ui.Column(c).Grow(1).PadSides(3, 0, 0, 0).Children(func() {
					ui.Text(c, messages.KeyTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(28).Width(min(bounds.Width*0.45, titleRoom)).Ellipsis()
					ui.Paragraph(c, messages.KeyDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(22).Width(min(bounds.Width*0.62, titleRoom))
				})
				copyButton = ui.Slot(c).Size(copyWidth, toolbarHeight).MarginSides(0, gap, 0, 0).Shrink(0)
				passphraseButton = ui.Slot(c).Size(passphraseWidth, toolbarHeight).MarginSides(0, gap, 0, 0).Shrink(0)
				removeButton = ui.Slot(c).Size(removeWidth, toolbarHeight).MarginSides(0, gap, 0, 0).Shrink(0)
				addButton = ui.Slot(c).Size(addWidth, toolbarHeight).Shrink(0)
			})
			if len(state.identities) == 0 {
				empty = state.reserveEmptyPage(c, bounds, messages.KeyEmpty, messages.KeyEmptySub, messages.KeyAdd)
			} else {
				content = ui.Slot(c).Grow(1).MarginSides(17, 0, 9, 0)
			}
			ui.Divider(c).Background(pal.line)
			ui.Row(c).Height(50).PadSides(8, 0, 0, 0).Gap(4).Items(ui.Start).Children(func() {
				ui.Text(c, state.identityCountText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).LineHeight(22).Width(120).Shrink(0)
				if statusValue != "" {
					ui.Text(c, statusValue).FontSize(metrics.FontSizeS).Color(statusColor).LineHeight(22).Grow(1)
				}
			})
		})
	})

	state.keysAddButton = addButton.Rect()
	state.paintToolbarButton(canvas, hitKeysCopyPublic, copyButton.Rect().X, copyButton.Rect().Y, copyWidth, messages.KeyCopyPublic, copyIcon, false, canCopy, condensed)
	state.paintToolbarButton(canvas, hitKeysPassphrase, passphraseButton.Rect().X, passphraseButton.Rect().Y, passphraseWidth, messages.KeySavePassphrase, passphraseIcon, false, selectedEncrypted, condensed)
	state.paintToolbarButton(canvas, hitKeysRemove, removeButton.Rect().X, removeButton.Rect().Y, removeWidth, messages.KeyRemove, removeIcon, false, state.selectedIdentityID != 0, condensed)
	state.paintToolbarButton(canvas, hitKeysAdd, addButton.Rect().X, addButton.Rect().Y, addWidth, messages.KeyAdd, addIcon, true, true, condensed)

	if len(state.identities) == 0 {
		state.drawReservedEmptyPage(canvas, empty, iconKey, hitKeysAdd, messages.KeyAdd, true)
		state.paintKeysMenu(canvas)
		return
	}
	columns := [...]components.TableColumn{{Width: 210}, {Width: 170}, {Width: 145}, {Width: 0}}
	headings := [...]string{messages.KeyColumnName, messages.KeyColumnType, messages.KeyColumnProtection, messages.KeyColumnFingerprint}
	components.FitTableHeadings(canvas, state.Theme(), columns[:], headings[:])
	table := components.BeginTable(canvas, state.Theme(), content.Rect(), columns[:])
	table.Header(headings[:])
	for _, identity := range state.identities {
		if table.RowAt(hitKeyRowList, int(identity.ID), identity.cells[:], identity.ID == state.selectedIdentityID) && state.modal == modalNone {
			state.selectIdentity(identity.ID)
		}
	}
	state.paintKeysMenu(canvas)
}

// paintKeysMenu lists the three ways to add a key under the Add Key button,
// right-aligned with it.
func (state *State) paintKeysMenu(canvas *gfx.Canvas) {
	if !state.keysMenu {
		return
	}
	m := state.messages
	menu := components.BeginDropdownMenu(canvas, state.Theme(), state.popupBelow(canvas, state.keysAddButton, 220, 3*32+8, ui.End),
		components.DropdownMenuOptions{Layer: dialogLayer, Scope: dialogScope, DismissID: hitKeysMenuDismiss, Viewport: state.windowRect(), Scroll: state.menuScrollFor(hitKeysMenuDismiss)})
	state.menuPlaced(menu.Bounds())
	menu.AddItem(components.DropdownMenuItem{ID: hitKeysImport, Label: m.KeyImport})
	menu.AddItem(components.DropdownMenuItem{ID: hitKeysPaste, Label: m.KeyPaste})
	menu.AddItem(components.DropdownMenuItem{ID: hitKeysGenerate, Label: m.KeyGenerate})
	menu.End()
}
