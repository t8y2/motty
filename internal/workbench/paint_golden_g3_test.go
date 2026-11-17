package workbench

import "testing"

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
	)
}
