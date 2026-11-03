package workbench

import (
	"net/url"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"motty/internal/term"
)

// A program may point at a file in its output rather than a web page. A coding
// agent links the files it wrote, as file:// URLs or in an editor's own scheme
// with the line to open at, and one that thinks the terminal has no hyperlinks
// underlines the name instead. The browser opens none of these, and on a server
// the file is not on this computer at all, so a click on one shows it in the
// session's file panel.

// editorLinkSchemes are editors whose links name a file as scheme://file/<path>,
// with :line or :line:column after it.
var editorLinkSchemes = map[string]bool{"vscode": true, "vscode-insiders": true, "vscodium": true, "cursor": true, "windsurf": true}

// fileLinkTarget is the file a terminal link names, as a path on the machine the
// session runs on: a POSIX path on a server, a path of this computer for a local
// terminal.
func fileLinkTarget(session *Session, target string) (string, bool) {
	local := session.isLocal()
	name := ""
	if strings.HasPrefix(target, "/") || local && filepath.IsAbs(target) {
		name = target
	} else {
		parsed, err := url.Parse(target)
		if err != nil {
			return "", false
		}
		switch scheme := strings.ToLower(parsed.Scheme); {
		case scheme == "file":
			// file://host/path is a file on host. On a server that can only be the
			// server; a local terminal takes this computer's files alone.
			if local && !localDirectoryHost(parsed.Host) {
				return "", false
			}
			name = parsed.Path
		case editorLinkSchemes[scheme] && strings.EqualFold(parsed.Host, "file"):
			name = pathLine.ReplaceAllString(parsed.Path, "")
		default:
			return "", false
		}
	}
	return sessionFilePath(session, name)
}

// underlinedFileTarget is the file underlined text at a cell names, if it reads
// as one: an absolute path, or a file name, which is taken to be in the folder
// the shell last reported.
func underlinedFileTarget(session *Session, position term.Position) (string, bool) {
	text := session.term.screen.UnderlinedTextAt(position)
	if text == "" || strings.Contains(text, "://") || strings.ContainsAny(text, "\t\n") {
		return "", false
	}
	if strings.HasPrefix(text, "/") || session.isLocal() && filepath.IsAbs(text) {
		return sessionFilePath(session, text)
	}
	// A bare word is not a file name; one with an extension, as an agent links
	// what it wrote, is.
	if strings.HasPrefix(text, "~") || path.Ext(text) == "" || strings.HasSuffix(text, ".") {
		return "", false
	}
	cwd, ok := session.term.screen.WorkingDirectory()
	if !ok {
		return "", false
	}
	if session.isLocal() {
		if !localDirectoryHost(cwd.Host) {
			return "", false
		}
		return sessionFilePath(session, filepath.Join(cwd.Path, filepath.FromSlash(text)))
	}
	return sessionFilePath(session, path.Join(cwd.Path, text))
}

// sessionFilePath cleans a path for the machine the session runs on, and
// refuses one that is not absolute there.
func sessionFilePath(session *Session, name string) (string, bool) {
	if name == "" || strings.IndexFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", false
	}
	if !session.isLocal() {
		return path.Clean(name), path.IsAbs(name)
	}
	if runtime.GOOS == "windows" && len(name) > 2 && name[0] == '/' && name[2] == ':' {
		name = name[1:] // file:///C:/Users/... names C:/Users/...
	}
	name = filepath.Clean(filepath.FromSlash(name))
	return name, filepath.IsAbs(name)
}

// openSessionLink opens a link clicked in a session's terminal: a file is shown
// in the session's file panel, anything else goes to the system's opener.
func (state *State) openSessionLink(sessionID uint32, target string) {
	if session, ok := state.sessionByIDPointer(sessionID); ok && state.filesReachable(session) {
		if name, ok := fileLinkTarget(session, target); ok {
			state.showInFiles(session, name, sftpReveal{})
			return
		}
	}
	state.openTerminalLink(target)
}

// openUnderlinedFile shows the file that underlined text at a cell names, and
// reports whether there was one.
func (state *State) openUnderlinedFile(session *Session, position term.Position) bool {
	if !state.filesReachable(session) {
		return false
	}
	name, ok := underlinedFileTarget(session, position)
	if !ok {
		return false
	}
	state.showInFiles(session, name, sftpReveal{search: true})
	return true
}

// filesReachable is whether the session's file panel can show its files now.
func (state *State) filesReachable(session *Session) bool {
	return session.containerCommand == "" && (session.isLocal() || session.conn != nil && session.hasServer())
}

// linkActions is what the terminal menu offers for a link: a file is shown in
// the file panel or downloaded, a web address opened.
func (state *State) linkActions(session *Session, target string) []detected {
	if name, ok := fileLinkTarget(session, target); ok {
		base := path.Base(filepath.ToSlash(name))
		return []detected{
			{kind: actionFiles, target: name, label: base},
			{kind: actionDownload, target: name, label: base},
			{kind: actionCopy, target: name, label: name},
		}
	}
	lower := strings.ToLower(target)
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "mailto:") {
		return []detected{{kind: actionBrowser, target: target, label: target}, {kind: actionCopy, target: target, label: target}}
	}
	return []detected{{kind: actionCopy, target: target, label: target}}
}
