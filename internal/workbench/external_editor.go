package workbench

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"motty/internal/i18n"
)

// ExternalEditor is an editor a folder can be handed to. The values are written
// to the state file, so they never change.
type ExternalEditor string

const (
	editorNone   ExternalEditor = ""
	editorVSCode ExternalEditor = "vscode"
	editorZed    ExternalEditor = "zed"
)

// editorInfo is the URL prefix a folder is handed to this editor with, remote or
// on this computer, and the name settings and the menu call it.
type editorInfo struct {
	scheme string
	local  string
	label  func(i18n.ToolsMessages) string
}

// editors is every editor this build can hand a folder to.
var editors = map[ExternalEditor]editorInfo{
	editorVSCode: {"vscode://vscode-remote/ssh-remote+", "vscode://file", func(m i18n.ToolsMessages) string { return m.FilesEditorVSCode }},
	editorZed:    {"zed://ssh/", "zed://file", func(m i18n.ToolsMessages) string { return m.FilesEditorZed }},
}

// name is how the settings and the menu call this editor: "" for none, and for a
// value this build does not know.
func (editor ExternalEditor) name(m i18n.ToolsMessages) string {
	if info, ok := editors[editor]; ok {
		return info.label(m)
	}
	return ""
}

// editorURI is the editor's own URL for a folder: its scheme, then the authority
// the editor's ssh resolves, or the path for a folder on this computer.
func editorURI(editor ExternalEditor, authority, directory string) (string, error) {
	info, ok := editors[editor]
	absolute := path.IsAbs
	if authority == "" {
		absolute = filepath.IsAbs
	}
	switch {
	case !ok:
		return "", fmt.Errorf("unknown editor %q", editor)
	case !absolute(directory):
		return "", errors.New("the folder must be an absolute path")
	case strings.ContainsAny(directory+authority, "\x00\n\r"):
		return "", errors.New("the folder or the authority holds control characters")
	}
	prefix := info.scheme
	if authority == "" {
		prefix = info.local
		// A file URL path starts at the root, so a Windows drive letter moves
		// behind it: C:\src becomes /C:/src.
		directory = "/" + strings.TrimPrefix(filepath.ToSlash(directory), "/")
	}
	// The folder is escaped as a URL path; the authority is not, because IPv6
	// brackets and VS Code's + must arrive as they are.
	return prefix + authority + (&url.URL{Path: directory}).EscapedPath(), nil
}

// open resolves the authority, builds the URL and hands it to open.
func (request editorRequest) open(open func(string) error) error {
	authority, err := resolveEditorAuthority(request)
	if err != nil {
		return err
	}
	link, err := editorURI(request.editor, authority, request.directory)
	if err != nil {
		return err
	}
	return open(link)
}
