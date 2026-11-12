package workbench

import (
	"os"
	"path/filepath"
)

// markdownLaunchPaths reports whether args name existing Markdown files to link
// as notes. The operating system uses this path when Motty is chosen from Open
// with or as the default application for .md and .markdown. A bare name that is
// not a file is left for the usual connection parser.
func markdownLaunchPaths(args []string) ([]string, bool) {
	var paths []string
	for _, path := range args {
		switch path {
		case "", "--background", "-ssh":
			continue
		}
		if !isMarkdownName(path) {
			return nil, false
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, false
		}
		if !filepath.IsAbs(path) {
			path, err = filepath.Abs(path)
			if err != nil {
				return nil, false
			}
		}
		paths = append(paths, filepath.Clean(path))
	}
	return paths, len(paths) > 0
}

// openMarkdownFiles links documents the system opened with Motty. A linked note
// keeps the file as its source, so the same file opened again selects the
// existing note instead of creating another.
func (state *State) openMarkdownFiles(paths []string) {
	if !state.flushNoteDraft() {
		return
	}
	state.activeSection = sectionNotes
	state.noteLinksRecheck()
	state.refreshNoteHosts()
	state.linkNotePaths(paths)
}
