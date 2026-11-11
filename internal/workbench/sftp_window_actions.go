package workbench

import (
	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

var sftpWindowActionIDs = [...]gfx.ID{hitSFTPOpenEntry, hitSFTPDownload, hitSFTPDownloadHere, hitSFTPSend, hitSFTPEditExternal, hitSFTPRename, hitSFTPMove, hitSFTPChmod, hitSFTPExtract, hitSFTPCompress, hitSFTPDelete, hitSFTPNewDirectory, hitSFTPUpload, hitSFTPUploadDirectory, hitSFTPNewLink, hitSFTPProperties, hitSFTPCalculateDirectorySize}

func sftpWindowActionIndex(id gfx.ID) int {
	for i, action := range sftpWindowActionIDs {
		if action == id {
			return i
		}
	}
	return -1
}

type sftpWindowAction struct {
	index int
	glyph uint8
	label string
}

type sftpWindowSelection struct {
	valid        bool
	count, index int
}

func (w *sftpWindow) selection(local bool) sftpWindowSelection {
	cache := &w.ui.selection[sideIndex(local)]
	if !cache.valid {
		cache.count, cache.index = 0, -1
		rows := w.folder(local).rows
		for i := range rows {
			if rows[i].selected {
				cache.count++
				cache.index = i
			}
		}
		cache.valid = true
	}
	return *cache
}

func (w *sftpWindow) selectionChanged(local bool) { w.ui.selection[sideIndex(local)].valid = false }

func (w *sftpWindow) selectionActions(local bool) []sftpWindowAction {
	var actions []sftpWindowAction
	m := w.owner.messages
	folder := w.folder(local)
	selection := w.selection(local)
	count, selected := selection.count, selection.index
	add := func(id gfx.ID, glyph uint8, label string) {
		actions = append(actions, sftpWindowAction{sftpWindowActionIndex(id), glyph, label})
	}
	if count == 1 {
		add(hitSFTPOpenEntry, iconFolderOpen, m.SFTPOpen)
	}
	if local {
		if count == 1 && folder.rows[selected].entry.Mode.IsRegular() {
			add(hitSFTPEditExternal, iconExternal, m.Tools.ExtEditButton)
		}
		add(hitSFTPSend, iconUpload, m.Tools.SendToServer)
		return actions
	}
	add(hitSFTPDownload, iconDownload, m.SFTPDownload)
	add(hitSFTPDownloadHere, iconFileDown, m.Tools.DownloadHere)
	if count == 1 {
		entry := folder.rows[selected].entry
		if !entry.IsDir() {
			add(hitSFTPEditExternal, iconExternal, m.Tools.ExtEditButton)
		}
		add(hitSFTPRename, iconPencil, m.SFTPRename)
		add(hitSFTPMove, iconFolderOpen, m.SFTPMove)
		add(hitSFTPChmod, iconLock, m.SFTPPermissions)
		if !entry.IsDir() && archiveKindOf(entry.Name) != archiveNone && w.session.host.System != sshconn.SystemWindows {
			add(hitSFTPExtract, iconArchive, m.Tools.Extract)
		}
		if entry.IsDir() && !entry.IsLink() && w.session.host.System != sshconn.SystemWindows {
			add(hitSFTPCompress, iconArchive, m.Tools.Compress)
		}
	}
	if count > 0 {
		add(hitSFTPDelete, iconTrash, m.SFTPDelete)
	}
	return actions
}

func (w *sftpWindow) fileAction(local bool, id gfx.ID) {
	defer w.invalidate()
	w.selectionChanged(local)
	w.activeLocal, w.view.local = local, local
	if id == hitSFTPCalculateDirectorySize {
		w.owner.calculateSFTPDirectorySize(w.view)
		return
	}
	if id == hitSFTPProperties {
		if properties, ok := captureSFTPProperties(w.folder(local), local); ok {
			w.fileProperties.close()
			w.fileProperties = properties
			if canCalculateSFTPFolderSize(w.view) {
				w.owner.calculateSFTPFolderSize(w.view, &w.fileProperties)
			}
		}
		return
	}
	if w.browse.pending != nil || w.pendingLocal != "" || !local && (w.view.busy || !w.connected()) {
		return
	}
	w.cancelDrag()
	switch id {
	case hitSFTPOpenEntry:
		for i := range w.folder(local).rows {
			if w.folder(local).rows[i].selected {
				w.openEntry(local, i)
				break
			}
		}
	case hitSFTPSend:
		w.queueSelection(true)
	case hitSFTPDownloadHere:
		w.queueSelection(false)
	case hitSFTPDownload:
		if !local {
			w.owner.downloadSFTP(w.view)
			w.localRefresh = true
		}
	case hitSFTPUpload:
		if !local {
			w.owner.uploadSFTP(w.view)
		}
	case hitSFTPUploadDirectory:
		if !local {
			w.owner.uploadSFTPDirectory(w.view)
		}
	case hitSFTPNewDirectory:
		w.owner.openSFTPDialog(w.view, sftpDialogCreateDirectory)
	case hitSFTPNewLink:
		if !local {
			w.owner.openSFTPLinkDialog(w.view)
		}
	case hitSFTPEditExternal:
		if local {
			w.owner.openLocalExternal(w.view)
		} else {
			w.owner.startExternalEdit(&w.session, w.view)
		}
	case hitSFTPRename:
		if !local {
			w.owner.openSFTPDialog(w.view, sftpDialogRename)
		}
	case hitSFTPMove:
		if !local {
			w.owner.openSFTPDialog(w.view, sftpDialogMove)
		}
	case hitSFTPChmod:
		if !local {
			w.owner.openSFTPDialog(w.view, sftpDialogChmod)
		}
	case hitSFTPDelete:
		if !local {
			w.owner.openSFTPDialog(w.view, sftpDialogDelete)
		}
	case hitSFTPExtract:
		if !local {
			w.owner.openSFTPExtractDialog(w.view)
		}
	case hitSFTPCompress:
		if !local {
			w.owner.openSFTPCompressDialog(w.view)
		}
	}
}
