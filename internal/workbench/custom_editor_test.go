package workbench

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
)

func TestSFTPFileSettingsDefaultsRestoreAndPersistLocally(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	if state.sftpWindowOrder != sftpLocalLeft || state.sftpOpenMode != sftpOpenWindow || state.customEditorPath != "" {
		t.Fatalf("defaults = order %v, mode %v, editor %q", state.sftpWindowOrder, state.sftpOpenMode, state.customEditorPath)
	}

	state.restoreSFTPSettings(savedState{SFTPWindowOrder: "server-left", SFTPOpenMode: "dialog"})
	if state.sftpWindowOrder != sftpLocalLeft || state.sftpOpenMode != sftpOpenWindow {
		t.Fatalf("invalid values did not fall back: order %v, mode %v", state.sftpWindowOrder, state.sftpOpenMode)
	}

	state.sftpWindowOrder = sftpRemoteLeft
	state.sftpOpenMode = sftpOpenTab
	state.customEditorPath = filepath.Join(t.TempDir(), "Editor 空格.exe")
	state.defaultEditor = editorCustom
	state.save()

	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != 4 || saved.SFTPWindowOrder != "remote-left" || saved.SFTPOpenMode != "tab" ||
		saved.CustomEditorPath != state.customEditorPath || saved.DefaultEditor != string(editorCustom) {
		t.Fatalf("saved = version %d, order %q, mode %q, editor %q/%q",
			saved.Version, saved.SFTPWindowOrder, saved.SFTPOpenMode, saved.DefaultEditor, saved.CustomEditorPath)
	}
	restarted := newTestState(t, options)
	if restarted.sftpWindowOrder != sftpRemoteLeft || restarted.sftpOpenMode != sftpOpenTab ||
		restarted.customEditorPath != state.customEditorPath || restarted.defaultEditor != editorCustom {
		t.Fatalf("restored = order %v, mode %v, editor %q/%q",
			restarted.sftpWindowOrder, restarted.sftpOpenMode, restarted.defaultEditor, restarted.customEditorPath)
	}
	for _, field := range []string{"sftp_window_order", "sftp_open_mode", "custom_editor_path"} {
		if syncedSettings[field] {
			t.Errorf("%s was classified as a synchronized setting", field)
		}
	}
}

func TestCustomEditorPathRequiresAnAbsoluteApplication(t *testing.T) {
	for _, value := range []string{"", "relative.exe", "C:\\bad\x00.exe", "C:\\bad\n.exe"} {
		if validCustomEditorPath(value) {
			t.Errorf("validCustomEditorPath(%q) = true", value)
		}
	}
	absolute := filepath.Join(t.TempDir(), "Editor With Spaces.exe")
	if !validCustomEditorPath(absolute) {
		t.Fatalf("valid absolute executable rejected: %q", absolute)
	}
	for _, suffix := range []string{"\t", "\x7f"} {
		if validCustomEditorPath(absolute + suffix) {
			t.Errorf("a path with control character %q was accepted", suffix)
		}
	}
	if customEditorExtension() == ".exe" && validCustomEditorPath(strings.TrimSuffix(absolute, ".exe")+".cmd") {
		t.Fatal("a non-.exe program was accepted on Windows")
	}
}

func TestCustomEditorCancellationAndFailureKeepThePreviousChoice(t *testing.T) {
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return nil, app.ErrCanceled }
	state := newTestState(t, options)
	state.defaultEditor = editorZed
	state.customEditorPath = ""

	state.chooseCustomEditor()
	if state.defaultEditor != editorZed || state.customEditorPath != "" {
		t.Fatalf("cancel changed the editor to %q/%q", state.defaultEditor, state.customEditorPath)
	}

	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return nil, errors.New("picker failed") }
	state.chooseCustomEditor()
	if state.defaultEditor != editorZed || state.customEditorPath != "" {
		t.Fatalf("picker failure changed the editor to %q/%q", state.defaultEditor, state.customEditorPath)
	}
}
