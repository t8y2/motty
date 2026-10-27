package workbench

import (
	"fmt"
	"os"
	"path"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

// The file panel is a column a few hundred pixels wide (a side panel or a
// drawer), so it is built like one: a title line with icon buttons, a toolbar
// of icon buttons, the path, a list of rows (type icon, name, date) and a
// status line. Nothing in it assumes the width of a page.
const (
	sftpPad       = float32(12)
	sftpRowHeight = float32(30)
	sftpIconSize  = float32(16)
	sftpButton    = float32(28)
	// The title row is as tall as the terminal's info bar and its buttons are that
	// bar's size, so the two rows share a center line across the window.
	sftpHeaderButton = float32(24)
)

// sftpIconButton is an icon button declared into a slot of the page and painted
// once the page has been laid out.
type sftpIconButton struct {
	box      flex.Box
	id       ui.ID
	icon     uint8
	hint     string
	disabled bool
	style    iconButtonStyle
}

func (state *State) paintSFTPIconButtons(canvas *ui.Canvas, buttons []sftpIconButton) {
	for index := range buttons {
		button := &buttons[index]
		state.paintIconButton(canvas, button.id, button.icon, button.box.Rect(), button.hint, button.disabled, button.style, state.pal.workspace)
	}
}

func (state *State) paintSFTPPage(canvas *ui.Canvas, size ui.Size, session *Session) {
	view := session.files
	if view == nil {
		return
	}
	if view.editPath != "" {
		state.paintSFTPEditor(canvas, size, session, view)
		return
	}
	pal, messages := state.pal, state.messages
	folder, local := view.active(), view.local

	// What the page shows decides its parts. The selection bar and the file
	// actions are as tall as what they hold; the list takes what they leave.
	selectedCount, selected := selectedSFTP(view)
	one := selectedCount == 1
	starred := !local && state.bookmarked(session, path.Clean(view.path))
	starIcon, starHint := uint8(iconStar), messages.Tools.BookmarkAdd
	if starred {
		starIcon, starHint = iconStarSolid, messages.Tools.BookmarkRemove
	}
	paneIcon, paneHint := uint8(iconHardDrive), messages.Tools.PaneLocal
	if local {
		paneIcon, paneHint = iconHosts, messages.Tools.PaneRemote
	}
	title := messages.SFTPFiles
	if local {
		title = messages.Tools.LocalFiles
	}
	status := view.status
	statusColor := pal.muted
	if view.errorText != "" {
		status = view.errorText
		statusColor = pal.danger
	}

	var buttons [20]sftpIconButton
	buttonCount := 0
	var crumbs, popupTop, listSlot, spinnerSlot flex.Box

	f := state.filesLayout.Begin(canvas, state.Theme(), ui.Rect{Y: windowBarHeight, Width: size.Width, Height: max(0, size.Height-windowBarHeight)})
	// icon declares an icon button where it goes; it is painted after End.
	icon := func(side float32, id ui.ID, glyph uint8, hint string, disabled bool, style iconButtonStyle) {
		buttons[buttonCount] = sftpIconButton{box: f.Slot().Size(side, side), id: id, icon: glyph, hint: hint, disabled: disabled, style: style}
		buttonCount++
	}
	f.Column().Fill().PadSides(0, sftpPad, 6, sftpPad).Background(pal.workspace).Children(func() {
		// One row for the panel: its name, then the actions on the folder and
		// refresh and close, from the right. The words "Files" and the address say
		// nothing the tab does not already; the room goes to the list.
		f.Row().Height(infoBarHeight).Shrink(0).Items(flex.Center).Children(func() {
			f.Text(title).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.text).Ellipsis().Grow(1).Height(20).MarginSides(0, 8, 0, 0)
			f.Row().Gap(4).Shrink(0).MarginSides(0, 10, 0, 0).Children(func() {
				if local {
					icon(sftpHeaderButton, hitSFTPNewDirectory, iconFolderPlus, messages.SFTPNewDirectory, false, iconButtonQuiet)
					return
				}
				icon(sftpHeaderButton, hitSFTPUpload, iconUpload, messages.SFTPUpload, view.busy, iconButtonQuiet)
				icon(sftpHeaderButton, hitSFTPUploadDirectory, iconFolderUp, messages.SFTPUploadDirectory, view.busy, iconButtonQuiet)
				icon(sftpHeaderButton, hitSFTPNewDirectory, iconFolderPlus, messages.SFTPNewDirectory, view.busy, iconButtonQuiet)
				icon(sftpHeaderButton, hitSFTPNewLink, iconLink, messages.Tools.TipNewLink, view.busy, iconButtonQuiet)
			})
			f.Row().Gap(4).Shrink(0).Children(func() {
				if !view.localOnly {
					icon(sftpHeaderButton, hitSFTPPaneToggle, paneIcon, paneHint, false, iconButtonQuiet)
				}
				icon(sftpHeaderButton, hitSFTPRefresh, iconRefresh, messages.SFTPRefresh, view.busy && !local, iconButtonQuiet)
				icon(sftpHeaderButton, hitSFTPClose, iconClose, messages.TipSFTPClose, false, iconButtonQuiet)
			})
		})

		// Where we are: one level up, then the tail of the path as many folders as
		// fit. The bookmark controls close the row on the right: the list of the
		// folders kept, and a star for this one.
		f.Row().Height(sftpButton).Shrink(0).Gap(4).Items(flex.Center).MarginSides(6, 0, 0, 0).Children(func() {
			icon(sftpButton, hitSFTPUp, iconArrowUp, messages.SFTPUp, !local && view.busy || folder.path == "/" || folder.path == "", iconButtonQuiet)
			height := float32(24)
			if state.sftpPathInput {
				height = sftpButton
			}
			crumbs = f.Slot().Grow(1).Height(height)
			if !local {
				f.Row().Gap(2).Shrink(0).Children(func() {
					icon(sftpButton, hitSFTPBookmarkList, iconChevronDown, messages.Tools.Bookmarks, false, iconButtonQuiet)
					icon(sftpButton, hitSFTPBookmarkToggle, starIcon, starHint, view.busy || view.path == "", iconButtonQuiet)
				})
			}
		})
		// The bookmark list opens just under the path row.
		popupTop = f.Slot().Height(0).MarginSides(4, 0, -4, 0)
		f.Divider().Background(pal.line).MarginSides(2, 0, 3, 0)

		listSlot = f.Slot().Grow(1)

		if selectedCount > 0 {
			// What can be done with the selection, under the list so the rows above
			// do not move when one is picked.
			rowBottom := float32(2)
			if one {
				rowBottom = 0 // the details line keeps that room
			}
			f.Column().Shrink(0).Children(func() {
				f.Divider().Background(pal.line)
				f.Row().Gap(2).Height(sftpButton).Shrink(0).MarginSides(5, 0, rowBottom, 0).Children(func() {
					add := func(id ui.ID, glyph uint8, label string, danger bool) {
						style := iconButtonQuiet
						if danger {
							style = iconButtonDanger
						}
						icon(sftpButton, id, glyph, label, view.busy, style)
					}
					if one {
						add(hitSFTPOpenEntry, iconFolderOpen, messages.SFTPOpen, false)
					}
					if view.localOnly {
						add(hitSFTPInsertPath, iconTerminal, messages.SFTPInsertPath, session.term == nil || session.term.shell == nil)
					} else if local {
						add(hitSFTPSend, iconUpload, messages.Tools.SendToServer, false)
					} else {
						add(hitSFTPDownload, iconDownload, messages.SFTPDownload, false)
						if view.lfolder.path != "" {
							add(hitSFTPDownloadHere, iconFileDown, messages.Tools.DownloadHere, false)
						}
					}
					if one && !local {
						if !selected.IsDir() {
							add(hitSFTPEditExternal, iconExternal, messages.Tools.ExtEditButton, false)
						}
						add(hitSFTPRename, iconPencil, messages.SFTPRename, false)
						add(hitSFTPChmod, iconLock, messages.SFTPPermissions, false)
						add(hitSFTPDelete, iconTrash, messages.SFTPDelete, true)
						if _, archive := state.selectedArchive(session, view); archive {
							add(hitSFTPExtract, iconArchive, messages.Tools.Extract, false)
						}
					}
				})
				if one {
					f.Text(state.sftpDetailsLine(view, selected)).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Height(18).Shrink(0).MarginSides(2, 0, 2, 2)
				}
			})
		}

		// What is happening.
		f.Row().Height(28).Shrink(0).Gap(8).Items(flex.Center).MarginSides(6, 0, 0, 0).Children(func() {
			if view.busy {
				spinnerSlot = f.Slot().Size(16, 16)
			}
			f.Text(status).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(statusColor).Ellipsis().Grow(1)
			if view.busy {
				f.Button(hitSFTPCancel, messages.SFTPCancelTransfer).Small().Outline()
			}
		})
	})
	f.End()

	state.paintSFTPIconButtons(canvas, buttons[:buttonCount])
	if state.sftpPathInput {
		state.paintSFTPPathInput(canvas, crumbs.Rect(), view)
	} else {
		state.paintSFTPBreadcrumbs(canvas, crumbs.Rect(), folder.crumbs)
	}
	state.paintSFTPList(canvas, size, listSlot.Rect(), session, view)
	if view.bookmarksOpen && !local {
		state.paintSFTPBookmarkPopup(canvas, popupTop.Rect().Y, size, session, view)
	}
	if view.busy {
		components.PaintSpinner(canvas, state.Theme(), spinnerSlot.Rect(), components.SpinnerPhase(canvas.Now()/(125*time.Millisecond)))
		canvas.RequestFrame()
	}
}

// paintSFTPList declares the visible rows of the list into the slot the page
// left for it, then paints what is drawn rather than laid out: the place
// dropped files would go and the scroll bar. Which rows are visible is
// arithmetic; where each goes and what is inside it is flex.
func (state *State) paintSFTPList(canvas *ui.Canvas, size ui.Size, list ui.Rect, session *Session, view *sftpView) {
	pal := state.pal
	folder, local := view.active(), view.local
	visibleRows := max(0, int(list.Height/sftpRowHeight))
	folder.visibleRows = visibleRows
	first := min(folder.first, max(0, len(folder.rows)-visibleRows))
	folder.first = first
	end := min(len(folder.rows), first+visibleRows)
	showDate := list.Width >= 280
	fullDate := list.Width >= 340
	scrolls := len(folder.rows) > visibleRows && visibleRows > 0
	empty := len(folder.rows) == 0 && (!view.busy || local) && view.errorText == ""
	loading := len(folder.rows) == 0 && view.busy && !local && view.operation == sftpclient.OperationList && view.errorText == ""
	boxes := state.filesRowBoxes[:0]
	var column, track, spinner flex.Box

	// The frame spans the panel so the scroll bar can sit in its margin.
	f := state.filesList.Begin(canvas, state.Theme(), ui.Rect{Y: list.Y, Width: size.Width, Height: list.Height})
	f.Row().Fill().PadSides(0, sftpPad, 0, sftpPad).Children(func() {
		column = f.Column().Grow(1).Clip()
		if empty || loading {
			column.Center()
		}
		column.Children(func() {
			if loading {
				f.Row().Gap(8).Items(flex.Center).Children(func() {
					spinner = f.Slot().Size(16, 16)
					f.Text(state.messages.SFTPLoading).FontSize(metrics.FontSizeM).Color(pal.muted)
				})
				return
			}
			if empty {
				f.Text(state.messages.SFTPEmpty).FontSize(metrics.FontSizeM).Color(pal.muted)
				return
			}
			for index := first; index < end; index++ {
				boxes = append(boxes, state.declareSFTPRow(f, &folder.rows[index], hitSFTPEntryList.At(index-first), showDate, fullDate))
			}
		})
		if scrolls {
			// Beside the list, in the margin the panel keeps to its right.
			track = f.Slot().Width(3).MarginSides(0, -10, 0, 7)
		}
	})
	f.End()
	state.filesRowBoxes = boxes
	if loading {
		components.PaintSpinner(canvas, state.Theme(), spinner.Rect(), components.SpinnerPhase(canvas.Now()/(125*time.Millisecond)))
	}

	if view.dropping && !local {
		// Where dropped files would go: the folder row they are over, or the folder.
		target := column.Rect()
		if row := view.dropRow; row >= first && row < end {
			target = boxes[row-first].Rect()
		}
		// RoundedBorder paints an opaque inner fill, so the tint is mixed in advance.
		canvas.RoundedBorder(target, 8, 1.5, pal.accent, withAlpha(pal.accent, 0.16).Over(pal.workspace))
	}
	if scrolls {
		track := track.Rect()
		thumb := track
		thumb.Height = max(track.Height*float32(visibleRows)/float32(len(folder.rows)), 24)
		thumb.Y += (track.Height - thumb.Height) * float32(first) / float32(len(folder.rows)-visibleRows)
		canvas.Rounded(thumb, 1.5, withAlpha(pal.muted, 0.35))
	}
}

// declareSFTPRow declares one entry: its type icon, its name, which takes the
// room and ends with an ellipsis, and, when there is room, its modification
// time. A selected row is tinted; the pointer lightens one. It returns the row.
func (state *State) declareSFTPRow(f *flex.Frame, row *sftpRow, id ui.ID, showDate, fullDate bool) flex.Box {
	pal, canvas := state.pal, f.Canvas()
	name := row.cells[1]
	if row.entry.IsLink() && row.entry.LinkTarget != "" {
		name = row.cells[1] + " → " + row.entry.LinkTarget
	}
	nameColor := pal.text
	if row.entry.LinkBroken {
		nameColor = pal.danger
	}
	tint := ui.Color{Alpha: 1} // a colored glyph keeps its own colors
	if row.mono {
		tint = pal.muted
		if row.entry.Mode&os.ModeSymlink != 0 {
			tint = pal.accent
		}
	}
	var background ui.Color
	switch {
	case row.selected:
		background = pal.accentSoft
	case canvas.Hovered() == id:
		background = withAlpha(pal.surfaceHot, 0.7)
	}
	box := f.Row().Height(sftpRowHeight).Shrink(0).Items(flex.Center).HitWith(id, row.cells[1], ui.HitOptions{Role: ui.RoleButton, Selected: row.selected})
	contents := func() {
		f.Icon(*row.icon).Color(tint)
		label := f.Text(name).FontSize(metrics.FontSizeM).Color(nameColor).Ellipsis().Grow(1).MarginSides(0, 0, 0, 8)
		if showDate {
			date := row.cells[3]
			if !fullDate && len(date) > 5 {
				date = date[5:] // drop the year
			}
			label.MarginSides(0, 10, 0, 8)
			f.Text(date).FontSize(metrics.FontSizeS).Color(pal.muted).Shrink(0)
		}
	}
	if background.Alpha == 0 && canvas.FocusVisible() != id {
		// Most rows have nothing behind them: their parts go straight in the row.
		return box.PadX(8).Children(contents)
	}
	// A tinted or focused row is a rounded shape a pixel in from the top and
	// bottom of the row, so the parts go in a box of that shape.
	return box.PadY(1).Children(func() {
		inner := f.Row().Grow(1).Self(flex.Stretch).Items(flex.Center).PadX(8).Background(background).Radius(radius.control)
		if canvas.FocusVisible() == id {
			inner.Border(1.5, pal.accent)
		}
		inner.Children(contents)
	})
}

// paintSFTPBreadcrumbs draws the path as links, keeping the folders nearest the
// current one and folding the rest into an ellipsis when the path is wider than
// the panel.
func (state *State) paintSFTPBreadcrumbs(canvas *ui.Canvas, bounds ui.Rect, crumbList []sftpCrumb) {
	// The path reads at the size of the list under it, not the theme's caption.
	// A separator is a chevron the size of the text with gap-2.5 (10) on each side.
	const size, separator = float32(13), float32(33)
	first := 0
	total := float32(0)
	for index := len(crumbList) - 1; index >= 0; index-- {
		next := total + canvas.TextWidth(crumbList[index].label, size) + separator
		if index < len(crumbList)-1 && next > bounds.Width-separator*2 {
			first = index + 1
			break
		}
		total = next
	}
	breadcrumb := components.BeginBreadcrumb(canvas, state.Theme(), bounds).FontSize(size)
	if first > 0 {
		breadcrumb.AddEllipsis(ui.ID{})
	}
	for index := first; index < len(crumbList); index++ {
		if index == len(crumbList)-1 {
			breadcrumb.AddCurrent(crumbList[index].label)
		} else {
			breadcrumb.AddLink(hitSFTPBreadcrumbList.At(index), crumbList[index].label)
		}
	}
}

func (state *State) paintSFTPEditor(canvas *ui.Canvas, size ui.Size, session *Session, view *sftpView) {
	pal, messages := state.pal, state.messages
	dirty := sftpEditorDirty(view)
	status := view.status
	statusColor := pal.muted
	if dirty && !view.busy {
		status = messages.SFTPEditDirty
	}
	if view.errorText != "" {
		status = view.errorText
		statusColor = pal.danger
	}

	var textarea, spinner flex.Box
	f := state.filesLayout.Begin(canvas, state.Theme(), ui.Rect{Y: windowBarHeight, Width: size.Width, Height: max(0, size.Height-windowBarHeight)})
	f.Column().Fill().PadSides(14, 18, 10, 18).Background(pal.workspace).Children(func() {
		// Back on the left, the file's name and place beside it, the actions from
		// the right.
		f.Row().Height(36).Shrink(0).Children(func() {
			f.Button(hitSFTPEditBack, messages.SFTPEditBack).Small().Outline().Disabled(view.busy)
			f.Spacer()
			f.Row().Gap(8).Shrink(0).Children(func() {
				f.Button(hitSFTPEditReload, messages.SFTPEditReload).Small().Secondary().Disabled(view.busy)
				f.Button(hitSFTPEditSave, messages.SFTPEditSave).Small().Disabled(view.busy || !dirty)
			})
		})
		// Under the actions, whole: beside them the name of a file had no room in a
		// panel this narrow.
		f.Column().Shrink(0).MarginSides(8, 0, 0, 0).Children(func() {
			f.Text(view.editName).FontSize(18).Weight(ui.WeightSemibold).Color(pal.text).Ellipsis().LineHeight(26)
			f.Text(view.editLocation).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).Ellipsis().LineHeight(20)
		})
		f.Text(messages.SFTPEditSafety).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).LineHeight(22).MarginSides(10, 0, 6, 0)
		textarea = f.Slot().Grow(1).MinHeight(64)
		f.Row().Height(28).Shrink(0).Gap(8).Items(flex.Center).MarginSides(14, 0, 0, 0).Children(func() {
			if view.busy {
				spinner = f.Slot().Size(16, 16)
			}
			f.Text(status).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(statusColor).Ellipsis().Grow(1)
			if view.busy {
				// With the file list's own place for it: in the header it did not
				// fit beside the other three in a panel this narrow.
				f.Button(hitSFTPCancel, messages.SFTPCancelTransfer).Small().Secondary()
			}
		})
	})
	f.End()

	value := view.editEditor.String()
	focused := canvas.Focused() == hitSFTPEditor
	selection := view.editEditor.Selection()
	snapshot := components.TextareaSnapshot{
		ID: hitSFTPEditor, Label: messages.SFTPEditLabel, Value: value,
		Focused: focused, HideEditorText: true, ShowCaret: selection.Start == selection.End, ReadOnly: view.busy,
		Layout:         &view.editLayout,
		SelectionStart: aiByteOffset(value, selection.Start), SelectionEnd: aiByteOffset(value, selection.End),
		Caret: aiByteOffset(value, view.editEditor.Caret()),
	}
	if marked, composing := view.editEditor.Composition(); composing {
		snapshot.CompositionStart, snapshot.CompositionEnd = aiByteOffset(value, marked.Start), aiByteOffset(value, marked.End)
	}
	view.editRect = textarea.Rect()
	components.PaintTextarea(canvas, state.Theme(), view.editRect, snapshot)
	if view.busy {
		components.PaintSpinner(canvas, state.Theme(), spinner.Rect(), components.SpinnerPhase(canvas.Now()/(125*time.Millisecond)))
		canvas.RequestFrame()
	}
}

func (state *State) paintSFTPDialog(canvas *ui.Canvas, viewport ui.Rect, view *sftpView) {
	messages, theme := state.messages, state.Theme()
	title := ""
	description := ""
	fieldLabel := ""
	submitLabel := ""
	initialFocus := hitSFTPDialogField
	submitVariant := components.ButtonDefault
	switch view.dialog {
	case sftpDialogCreateDirectory:
		title = messages.SFTPNewDirectoryTitle
		fieldLabel = messages.SFTPDirectoryName
		submitLabel = messages.SFTPCreateDirectory
	case sftpDialogRename:
		title = messages.SFTPRenameTitle
		description = messages.SFTPRenameDescription
		fieldLabel = messages.SFTPDestinationPath
		submitLabel = messages.SFTPRenameSubmit
	case sftpDialogDelete:
		title = messages.SFTPDeleteFileTitle
		if view.dialogEntry.IsDir() {
			title = messages.SFTPDeleteDirectoryTitle
		}
		description = fmt.Sprintf(messages.SFTPDeleteDescriptionFmt, view.dialogEntry.Name)
		submitLabel = messages.SFTPDeleteConfirm
		initialFocus = hitSFTPDialogCancel
		submitVariant = components.ButtonDestructive
	case sftpDialogChmod:
		title = messages.SFTPPermissionsTitle
		description = messages.SFTPPermissionsDescription
		fieldLabel = messages.SFTPPermissionsMode
		submitLabel = messages.SFTPPermissionsSubmit
	case sftpDialogSymlink:
		title = messages.Tools.LinkTitle
		fieldLabel = messages.Tools.LinkName
		submitLabel = messages.Tools.LinkCreate
	case sftpDialogExtract:
		title = messages.Tools.ExtractTitle
		description = fmt.Sprintf(messages.Tools.ExtractDescriptionFmt, view.dialogEntry.Name)
		fieldLabel = messages.Tools.ExtractFolder
		submitLabel = messages.Tools.ExtractConfirm
	case sftpDialogDiscardEdit:
		title = messages.SFTPEditDiscardTitle
		description = messages.SFTPEditDiscardDescription
		submitLabel = messages.SFTPEditDiscardConfirm
		initialFocus = hitSFTPDialogCancel
		submitVariant = components.ButtonDestructive
	case sftpDialogReloadEdit:
		title = messages.SFTPEditReloadTitle
		description = messages.SFTPEditReloadDescription
		submitLabel = messages.SFTPEditReloadConfirm
		initialFocus = hitSFTPDialogCancel
		submitVariant = components.ButtonDestructive
	case sftpDialogRemoteChanged:
		title = messages.SFTPEditConflictTitle
		description = messages.SFTPEditConflictDescription
		submitLabel = messages.SFTPEditReloadConfirm
		initialFocus = hitSFTPDialogCancel
	}
	fieldDialog := view.dialog == sftpDialogCreateDirectory || view.dialog == sftpDialogRename || view.dialog == sftpDialogChmod ||
		view.dialog == sftpDialogSymlink || view.dialog == sftpDialogExtract
	showError := view.dialog == sftpDialogDelete && view.dialogError != ""
	body := float32(0) // a confirmation is only its text
	switch {
	case view.dialog == sftpDialogSymlink:
		body = 6 + 64 + 8 + 64 // the link's name and where it points
	case fieldDialog:
		body = 6 + 64
	case showError:
		body = 20
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer:          dialogLayer,
		Scope:          dialogScope,
		DismissID:      hitSFTPDialogDismiss,
		CloseID:        hitSFTPDialogClose,
		InitialFocusID: initialFocus,
		ContentSize:    ui.Size{Width: 520}, FitBody: true, BodyHeight: body,
	})
	layout := dialog.Begin(title, description)
	var first, second flex.Box
	f := state.partLayout.Begin(canvas, theme, layout.Body)
	column := f.Column().Fill()
	if showError {
		column.Justify(flex.Center) // as a label in the whole body
	}
	column.Children(func() {
		switch {
		case fieldDialog:
			// One labelled input, two for a link: its name and where it points.
			f.Column().Shrink(0).Gap(8).PadSides(6, 4, 0, 4).Children(func() {
				first = f.Slot().Height(64).Shrink(0)
				if view.dialog == sftpDialogSymlink {
					second = f.Slot().Height(64).Shrink(0)
				}
			})
		case showError:
			f.Text(view.dialogError).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(state.pal.danger).Ellipsis()
		}
	})
	f.End()
	if fieldDialog {
		field := components.BeginField(canvas, theme, first.Rect())
		if view.dialogError != "" {
			field = field.Invalid()
		}
		content := field.Paint(fieldLabel, "", view.dialogError)
		value := view.dialogEditor.String()
		focused := canvas.Focused() == hitSFTPDialogField
		components.PaintInput(canvas, theme, content, components.InputSnapshot{
			ID:              hitSFTPDialogField,
			Label:           fieldLabel,
			Value:           value,
			Focused:         focused,
			PlatformEditing: focused && value != "",
		})
		if view.dialog == sftpDialogSymlink {
			second := components.BeginField(canvas, theme, second.Rect()).Paint(messages.Tools.LinkTarget, "", "")
			secondValue := view.dialogEditor2.String()
			secondFocused := canvas.Focused() == hitSFTPDialogField2
			components.PaintInput(canvas, theme, second, components.InputSnapshot{
				ID: hitSFTPDialogField2, Label: messages.Tools.LinkTarget, Value: secondValue,
				Focused: secondFocused, PlatformEditing: secondFocused && secondValue != "",
			})
		}
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitSFTPDialogCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitSFTPDialogSubmit, Label: submitLabel, Variant: submitVariant},
	)
	dialog.End()
}
