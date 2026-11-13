package workbench

import (
	"context"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

var (
	filePropertiesHits       = tree.Feature("file-properties")
	hitFilePropertiesDismiss = tree.ID(filePropertiesHits, "dismiss")
	hitFilePropertiesClose   = tree.ID(filePropertiesHits, "close")

	hitSFTPCopyPath               = tree.ID(sftpHits, "copy-path")
	hitSFTPProperties             = tree.ID(sftpHits, "properties")
	hitSFTPCalculateDirectorySize = tree.ID(sftpHits, "calculate-directory-size")
	hitSFTPCopyFiles              = tree.ID(sftpHits, "copy-files")
	hitSFTPPasteFiles             = tree.ID(sftpHits, "paste-files")
)

const filePropertiesRowHeight float32 = 34

// sftpFileProperties keeps the listing entry the dialog describes. The entries
// already carry the metadata transferred with the folder listing, so opening the
// dialog needs no connection or filesystem round trip.
type sftpFileProperties struct {
	open       bool
	folder     string
	local      bool
	entry      sftpclient.Entry
	folderSize string
	cancelSize context.CancelFunc
}

func (properties *sftpFileProperties) close() {
	if properties.cancelSize != nil {
		properties.cancelSize()
	}
	*properties = sftpFileProperties{}
}

func canCalculateSFTPFolderSize(view *sftpView) bool {
	count, entry := selectedSFTP(view)
	return count == 1 && entry.Mode.Type() == os.ModeDir && (view.local || view.openFiles != nil)
}

type sftpSizeResult struct {
	size int64
	text string
}

func (folder *sftpFolder) stopSizeCalculation() {
	if folder.cancelSize != nil {
		folder.cancelSize()
		folder.cancelSize = nil
	}
}

func (state *State) calculateSFTPDirectorySize(view *sftpView) {
	folder, local := view.active(), view.local
	if folder.path == "" || !local && view.openFiles == nil {
		return
	}
	folder.stopSizeCalculation()
	roots := make(map[string]string)
	for _, row := range folder.allRows {
		if row.entry.Mode.Type() == os.ModeDir {
			root := path.Join(folder.path, row.entry.Name)
			if local {
				root = filepath.Join(folder.path, row.entry.Name)
			}
			roots[row.entry.Name] = root
		}
	}
	if len(roots) == 0 {
		return
	}
	for _, rows := range [][]sftpRow{folder.allRows, folder.rows} {
		for index := range rows {
			if _, ok := roots[rows[index].entry.Name]; ok {
				rows[index].cells[2] = state.messages.SFTPWindow.CalculatingSize
			}
		}
	}
	folder.cancelSize = state.calculateSFTPSizes(view, local, roots, func(results map[string]sftpSizeResult) {
		for _, rows := range [][]sftpRow{folder.allRows, folder.rows} {
			for index := range rows {
				if result, ok := results[rows[index].entry.Name]; ok && rows[index].entry.Mode.Type() == os.ModeDir {
					rows[index].entry.Size, rows[index].cells[2] = result.size, result.text
				}
			}
		}
	})
}

func (state *State) calculateSFTPFolderSize(view *sftpView, properties *sftpFileProperties) {
	name := properties.entry.Name
	root := path.Join(properties.folder, name)
	if properties.local {
		root = filepath.Join(properties.folder, name)
	}
	properties.folderSize = state.messages.SFTPWindow.CalculatingSize
	properties.cancelSize = state.calculateSFTPSizes(view, properties.local, map[string]string{name: root}, func(results map[string]sftpSizeResult) {
		properties.folderSize = results[name].text
	})
}

func (state *State) calculateSFTPSizes(view *sftpView, local bool, roots map[string]string, apply func(map[string]sftpSizeResult)) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	open, m, invalidate := view.openFiles, state.messages.SFTPWindow, state.options.Invalidate
	join := func(dir, name string) string { return filepath.Join(dir, name) }
	if !local {
		join = func(dir, name string) string { return path.Join(dir, name) }
	}
	go func() {
		publish := func(results map[string]sftpSizeResult, done bool) {
			if !state.aiPostWithInvalidator(func(state *State) {
				if done {
					defer cancel()
				}
				if ctx.Err() == nil {
					apply(results)
					view.invalidateDetached()
				}
			}, invalidate) {
				cancel()
			}
		}
		var reader sftpclient.DirReader = sftpclient.LocalReader{}
		if !local {
			remote, err := open()
			if err != nil {
				results := make(map[string]sftpSizeResult, len(roots))
				for name := range roots {
					results[name] = sftpSizeResult{text: err.Error()}
				}
				publish(results, true)
				return
			}
			var once sync.Once
			release := func() { once.Do(func() { _ = remote.Close() }) }
			defer release()
			defer context.AfterFunc(ctx, release)()
			reader = remote
		}
		remaining := len(roots)
		for name, root := range roots {
			if ctx.Err() != nil {
				return
			}
			var result sftpSizeResult
			summary := sftpclient.Walk(ctx, reader, root, sftpclient.WalkOptions{
				Join: join, MaxEntries: math.MaxInt, MaxDepth: math.MaxInt,
			}, func(found []sftpclient.Found) {
				for _, item := range found {
					if item.Entry.Mode.IsRegular() {
						result.size += item.Entry.Size
					}
				}
			})
			result.text = formatBytes(result.size)
			if summary.RootErr != nil {
				result.text = summary.RootErr.Error()
			} else if summary.Failed > 0 {
				result.text = fmt.Sprintf(m.FolderSizeIncompleteFmt, result.text, summary.Failed)
			}
			remaining--
			publish(map[string]sftpSizeResult{name: result}, remaining == 0)
		}
	}()
	return cancel
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
	state.fileProperties.close()
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
	} else if properties.folderSize != "" {
		size = properties.folderSize
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

// declareFileProperties declares the properties dialog of the main window's
// docked file panel.
func (state *State) declareFileProperties(c ui.C) {
	state.declareSFTPFileProperties(c, dialogViewport(c), &state.fileProperties)
}

// declareSFTPFileProperties declares a file listing's properties dialog in the
// page of the window that owns it. viewport is the room the owning surface
// gives the dialog, as the paint path measured it.
func (state *State) declareSFTPFileProperties(c ui.C, viewport gfx.Rect, properties *sftpFileProperties) {
	if properties == nil || !properties.open {
		return
	}
	m := state.messages.SFTPWindow
	rows := state.sftpFilePropertiesRows(properties)
	contentWidth := min(float32(640), viewport.Width-32)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, contentWidth, properties.entry.Name,
		float32(len(rows))*filePropertiesRowHeight)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.PropertiesTitle, Description: properties.entry.Name,
		Width: contentWidth, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitFilePropertiesDismiss, CloseID: hitFilePropertiesClose, InitialFocusID: hitFilePropertiesClose,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().PadSides(2, 4, 0, 4).Children(func() {
			for index, row := range rows {
				if index > 0 {
					ui.Slot(c).Height(1).Background(state.pal.line)
				}
				ui.Row(c).Height(filePropertiesRowHeight-1).PadSides(10, 8, 10, 8).Gap(16).Items(ui.Center).Children(func() {
					ui.Text(c, row.label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.muted).
						LineHeight(20).Width(92).Shrink(0).Ellipsis()
					ui.Text(c, row.value).FontSize(metrics.FontSizeM).Color(state.pal.text).
						LineHeight(20).Grow(1).Ellipsis()
				})
			}
		})
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c, components.ButtonAction{ID: hitFilePropertiesClose, Label: state.messages.LabelWindowClose, Variant: components.ButtonSecondary})
	})
}

func (state *State) clickFileProperties(id gfx.ID) {
	if id != hitFilePropertiesDismiss && id != hitFilePropertiesClose {
		return
	}
	state.modal = modalNone
	state.fileProperties.close()
}

func (w *sftpWindow) clickFileProperties(id gfx.ID) bool {
	if !w.fileProperties.open {
		return false
	}
	if id == hitFilePropertiesDismiss || id == hitFilePropertiesClose {
		w.fileProperties.close()
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
		w.fileProperties.close()
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
