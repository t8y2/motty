package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// sftpDialogScene opens an SFTP dialog on notes.txt of a live fake session.
func sftpDialogScene(name string, kind sftpDialogKind, setup func(view *sftpView)) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state, session, _, _ := openTestSFTP(t)
		view := session.files
		clickSFTPRow(t, state, view, "notes.txt")
		state.openSFTPDialog(view, kind)
		if setup != nil {
			setup(view)
		}
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}

func init() {
	extraPaintScenes = append(extraPaintScenes,
		plainScene("g3-form-secure", func(t *testing.T, s *State) {
			s.openExportBackup()
			s.form.set(0, "hunter2")
		}),
		plainScene("g3-form-secure-error", func(t *testing.T, s *State) {
			s.openExportBackup()
			s.form.set(0, "hunter2")
			s.form.dialogError = "The passwords differ."
		}),
		plainScene("g3-form-multiline", func(t *testing.T, s *State) {
			s.openPasteKeyForm()
			s.form.set(0, "line one\nline two")
			s.form.set(1, "my key")
		}),
		plainScene("g3-form-plain", func(t *testing.T, s *State) {
			s.openSSHAgentPathForm()
			s.form.set(0, "/tmp/agent.sock")
		}),
		sftpDialogScene("g3-sftp-rename", sftpDialogRename, nil),
		sftpDialogScene("g3-sftp-rename-error", sftpDialogRename, func(v *sftpView) { v.dialogError = "That name is invalid." }),
		sftpDialogScene("g3-sftp-symlink", sftpDialogSymlink, func(v *sftpView) { v.dialogEditor2.Insert("target") }),
		sftpDialogScene("g3-sftp-chmod", sftpDialogChmod, nil),
		sftpDialogScene("g3-sftp-chown", sftpDialogChown, nil),
	)
}
