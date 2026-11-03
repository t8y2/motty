//go:build darwin

package workbench

import "testing"

func TestDarwinAcceptsApplicationsAndCommandLineEditors(t *testing.T) {
	if !customEditorSupported() {
		t.Fatal("the custom editor is unavailable on macOS")
	}
	for _, path := range []string{"/Applications/TextEdit.app", "/opt/homebrew/bin/code"} {
		if !validCustomEditorPath(path) {
			t.Errorf("%q is not accepted", path)
		}
	}
	if customEditorFilterPattern() != "*.app;*" {
		t.Fatalf("picker pattern = %q", customEditorFilterPattern())
	}
}
