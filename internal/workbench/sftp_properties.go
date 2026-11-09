package workbench

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

var (
	filePropertiesHits       = tree.Feature("file-properties")
	hitFilePropertiesDismiss = tree.ID(filePropertiesHits, "dismiss")
	hitFilePropertiesClose   = tree.ID(filePropertiesHits, "close")

	hitSFTPCopyPath   = tree.ID(sftpHits, "copy-path")
	hitSFTPProperties = tree.ID(sftpHits, "properties")
)

const filePropertiesRowHeight float32 = 34

// sftpFileProperties keeps the listing entry the dialog describes. The entries
// already carry the metadata transferred with the folder listing, so opening the
// dialog needs no connection or filesystem round trip.
type sftpFileProperties struct {
	open   bool
	folder string
	local  bool
	entry  sftpclient.Entry
}

type sftpPropertyRow struct {
	label, value string
}

func captureSFTPProperties(folder *sftpFolder, local bool) (sftpFileProperties, bool) {
	if folder == nil {
		return sftpFileProperties{}, false
	}
	var entry sftpclient.Entry
	count := 0
	for index := range folder.rows {
		if folder.rows[index].selected {
			count++
			entry = folder.rows[index].entry
		}
	}
	if count != 1 {
		return sftpFileProperties{}, false
	}
	return sftpFileProperties{open: true, folder: folder.path, local: local, entry: entry}, true
}

func (state *State) openSFTPProperties(folder *sftpFolder, local bool) bool {
	properties, ok := captureSFTPProperties(folder, local)
	if !ok {
		return false
	}
	state.fileProperties = properties
	state.modal = modalFileProperties
	return true
}

func (state *State) copySFTPPaths(folder *sftpFolder, local bool) error {
	if folder == nil {
		return nil
	}
	locations := make([]string, 0, len(folder.rows))
	for index := range folder.rows {
		row := &folder.rows[index]
		if !row.selected {
			continue
		}
		location := path.Join(folder.path, row.entry.Name)
		if local {
			location = filepath.Join(folder.path, row.entry.Name)
		}
		locations = append(locations, location)
	}
	return state.options.SetClipboardText(strings.Join(locations, "\n"))
}

func (state *State) sftpFilePropertiesRows(properties *sftpFileProperties) []sftpPropertyRow {
	entry, m := properties.entry, state.messages.SFTPWindow
	location := path.Join(properties.folder, entry.Name)
	if properties.local {
		location = filepath.Join(properties.folder, entry.Name)
	}
	kind := m.PropertiesOther
	switch {
	case entry.IsLink():
		kind = m.PropertiesSymlink
	case entry.IsDir():
		kind = m.PropertiesFolder
	case entry.Mode.IsRegular():
		kind = m.PropertiesFile
	}
	size := m.PropertiesUnknown
	if !entry.IsDir() {
		size = formatBytes(entry.Size)
	}
	modified := m.PropertiesUnknown
	if !entry.ModTime.IsZero() {
		modified = entry.ModTime.Local().Format("2006-01-02 15:04:05")
	}
	rows := []sftpPropertyRow{
		{label: m.PropertiesName, value: entry.Name},
		{label: m.PropertiesPath, value: location},
		{label: m.PropertiesType, value: kind},
		{label: m.PropertiesSize, value: size},
		{label: m.PropertiesModified, value: modified},
		{label: m.PropertiesPermissions, value: entry.Mode.String()},
	}
	if entry.HasOwner {
		rows = append(rows,
			sftpPropertyRow{label: m.PropertiesOwner, value: ownerPropertyValue(entry.Owner, entry.UID)},
			sftpPropertyRow{label: m.PropertiesGroup, value: ownerPropertyValue(entry.Group, entry.GID)},
		)
	}
	if entry.LinkTarget != "" {
		rows = append(rows, sftpPropertyRow{label: m.PropertiesLinkTarget, value: entry.LinkTarget})
	}
	return rows
}

func ownerPropertyValue(name string, id uint32) string {
	if name == "" {
		return strconv.FormatUint(uint64(id), 10)
	}
	return fmt.Sprintf("%s (%d)", name, id)
}

func (state *State) paintFileProperties(canvas *gfx.Canvas, viewport gfx.Rect) {
	state.paintSFTPFileProperties(canvas, viewport, &state.fileProperties)
}

func (state *State) paintSFTPFileProperties(canvas *gfx.Canvas, viewport gfx.Rect, properties *sftpFileProperties) {
	if properties == nil || !properties.open {
		return
	}
	m := state.messages.SFTPWindow
	rows := state.sftpFilePropertiesRows(properties)
	contentWidth := min(float32(640), viewport.Width-32)
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitFilePropertiesDismiss, CloseID: hitFilePropertiesClose, InitialFocusID: hitFilePropertiesClose,
		ContentSize: gfx.Size{Width: contentWidth}, FitBody: true,
		BodyHeight: float32(len(rows)) * filePropertiesRowHeight,
	})
	layout := dialog.Begin(m.PropertiesTitle, properties.entry.Name)
	f := state.settingsRow.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		for index, row := range rows {
			if index > 0 {
				f.Slot().Height(1).Background(state.pal.line)
			}
			f.Row().Height(filePropertiesRowHeight-1).PadSides(10, 8, 10, 8).Gap(16).Items(ui.Center).Children(func() {
				f.Text(row.label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.muted).
					LineHeight(20).Width(92).Shrink(0).Ellipsis()
				f.Text(row.value).FontSize(metrics.FontSizeM).Color(state.pal.text).
					LineHeight(20).Grow(1).Ellipsis()
			})
		}
	})
	f.End()
	dialog.Actions(layout, components.ButtonAction{ID: hitFilePropertiesClose, Label: state.messages.LabelWindowClose, Variant: components.ButtonSecondary})
	dialog.End()
}

func (state *State) clickFileProperties(id gfx.ID) {
	if id != hitFilePropertiesDismiss && id != hitFilePropertiesClose {
		return
	}
	state.modal = modalNone
	state.fileProperties = sftpFileProperties{}
}

func (w *sftpWindow) clickFileProperties(id gfx.ID) bool {
	if !w.fileProperties.open {
		return false
	}
	if id == hitFilePropertiesDismiss || id == hitFilePropertiesClose {
		w.fileProperties = sftpFileProperties{}
	}
	return true
}

func (state *State) filePropertiesKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalFileProperties {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape || event.Key == gfx.KeyEnter && event.Target == hitFilePropertiesClose {
		state.clickFileProperties(hitFilePropertiesClose)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

func (w *sftpWindow) filePropertiesKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if !w.fileProperties.open {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape || event.Key == gfx.KeyEnter && event.Target == hitFilePropertiesClose {
		w.fileProperties = sftpFileProperties{}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{Handled: true}, true
}

// modalFilePropertiesKey keeps a single-column properties dialog modal: only
// its own Enter target and Escape reach it. Everything else is consumed before
// file-list shortcuts can act on the row behind the dialog.
func (state *State) modalFilePropertiesKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalFileProperties {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape && !event.Repeat ||
		event.Key == gfx.KeyEnter && event.Target == hitFilePropertiesClose {
		state.clickFileProperties(hitFilePropertiesClose)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{Handled: true}, true
}
