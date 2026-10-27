package workbench

import (
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

var sftpWindowActionIDs = [...]ui.ID{hitSFTPOpenEntry, hitSFTPDownload, hitSFTPDownloadHere, hitSFTPSend, hitSFTPEditExternal, hitSFTPRename, hitSFTPChmod, hitSFTPExtract, hitSFTPCompress, hitSFTPDelete, hitSFTPNewDirectory, hitSFTPUpload, hitSFTPUploadDirectory, hitSFTPNewLink}

func sftpWindowActionIndex(id ui.ID) int {
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
	add := func(id ui.ID, glyph uint8, label string) {
		actions = append(actions, sftpWindowAction{sftpWindowActionIndex(id), glyph, label})
	}
	if count == 1 {
		add(hitSFTPOpenEntry, iconFolderOpen, m.SFTPOpen)
	}
	if local {
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
		add(hitSFTPChmod, iconLock, m.SFTPPermissions)
		if !entry.IsDir() && archiveKindOf(entry.Name) != archiveNone && w.session.host.System != sshconn.SystemWindows {
			add(hitSFTPExtract, iconArchive, m.Tools.Extract)
		}
		if entry.IsDir() && !entry.IsLink() && w.session.host.System != sshconn.SystemWindows {
			add(hitSFTPCompress, iconArchive, m.Tools.Compress)
		}
		add(hitSFTPDelete, iconTrash, m.SFTPDelete)
	}
	return actions
}

func (w *sftpWindow) fileAction(local bool, id ui.ID) {
	defer w.window.Invalidate()
	w.selectionChanged(local)
	if w.pendingLocal != "" || !local && (w.view.busy || !w.connected()) {
		return
	}
	w.cancelDrag()
	w.activeLocal, w.view.local = local, local
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
		if !local {
			w.owner.startExternalEdit(&w.session, w.view)
		}
	case hitSFTPRename:
		if !local {
			w.owner.openSFTPDialog(w.view, sftpDialogRename)
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
