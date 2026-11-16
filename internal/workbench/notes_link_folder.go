package workbench

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"motty/internal/configstore"
)

// A linked folder mirrors a directory of Markdown files as a folder of linked
// notes. The directory is the single source of truth, as a linked file is: files
// that appear become notes, files that change are read like any linked note, and
// files that disappear leave their note behind marked as missing, never deleted.
// What belongs to Motty alone (tags, pins, hosts, the folder tree's order) stays
// in this device's state and is never written into the user's files.

const (
	// linkedFolderFileLimit and linkedFolderByteBudget bound what one scan reads,
	// so a vault of any size cannot exhaust memory or hold the window.
	linkedFolderFileLimit  = 5000
	linkedFolderByteBudget = 64 << 20
	// linkedFolderPending bounds the names kept between two probes; past it the
	// whole directory is read again instead.
	linkedFolderPending = 256
)

var (
	hitNoteLinkFolder   = tree.ID(notesHits, "link-folder")
	hitNoteFolderUnlink = tree.ID(notesHits, "folder-unlink")
)

// noteTreeSkip names what a linked directory leaves out: hidden entries, which
// holds version control and editor settings, and dependency folders.
func noteTreeSkip(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

func isMarkdownName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// folderScan is what reading a linked directory found.
type folderScan struct {
	// files maps a slash path below the root to what reading it found.
	files map[string]linkedFile
	// dirs are the directories below the root that were seen, including empty ones.
	dirs []string
	// gone are paths that no longer exist; anything below one is gone too.
	gone []string
	// full: the scan covers the whole root, so a member it did not find is gone.
	full bool
	// skipped counts Markdown files that could not be taken as notes.
	skipped int
	// truncated: a limit stopped the scan, so what it did not find is unknown.
	truncated bool
	// rootMissing: the directory itself cannot be found.
	rootMissing bool
	err         error
}

type scanBudget struct{ files, bytes int }

// scanTree reads the Markdown files below rel (a slash path, "" for the root).
func scanTree(root, rel string, budget *scanBudget, out *folderScan) {
	base := filepath.Join(root, filepath.FromSlash(rel))
	_ = filepath.WalkDir(base, func(abs string, entry fs.DirEntry, err error) error {
		if err != nil {
			if abs == base {
				out.err = err
				out.rootMissing = rel == "" && isLinkMissing(err)
			}
			return nil
		}
		sub, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			return nil
		}
		sub = filepath.ToSlash(sub)
		if sub == "." {
			return nil
		}
		if noteTreeSkip(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			out.dirs = append(out.dirs, sub)
			return nil
		}
		if !isMarkdownName(entry.Name()) {
			return nil
		}
		if budget.files >= linkedFolderFileLimit || budget.bytes >= linkedFolderByteBudget {
			out.truncated = true
			return filepath.SkipAll
		}
		budget.files++
		file := readLinkedFile(abs)
		budget.bytes += len(file.body)
		out.files[sub] = file
		return nil
	})
}

// probeLinkedFolder reads the root whole, or only what the named paths say
// changed. It runs away from the window thread and touches no state.
func probeLinkedFolder(root string, names []string, full bool) folderScan {
	scan := folderScan{files: map[string]linkedFile{}, full: full}
	var budget scanBudget
	if full {
		if _, err := os.Stat(root); err != nil {
			scan.err, scan.rootMissing = err, isLinkMissing(err)
			return scan
		}
		scanTree(root, "", &budget, &scan)
		return scan
	}
	for _, name := range names {
		parts := strings.Split(name, "/")
		if slices.ContainsFunc(parts, noteTreeSkip) {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		switch {
		case err != nil:
			if isLinkMissing(err) {
				scan.gone = append(scan.gone, name)
			}
		case info.IsDir():
			scan.dirs = append(scan.dirs, name)
			scanTree(root, name, &budget, &scan)
		case isMarkdownName(name):
			scan.files[name] = readLinkedFile(filepath.Join(root, filepath.FromSlash(name)))
		}
	}
	return scan
}

// noteFolderScanState serializes the reads of one root: one in flight, and what
// arrived meanwhile is read next.
type noteFolderScanState struct {
	running bool
	full    bool
	names   []string
}

// probeNoteFolder asks for a root to be read again, in full or for some paths.
func (s *State) probeNoteFolder(root string, names []string, full bool) {
	if s.noteScans == nil {
		s.noteScans = map[string]*noteFolderScanState{}
	}
	st := s.noteScans[root]
	if st == nil {
		st = &noteFolderScanState{}
		s.noteScans[root] = st
	}
	st.full = st.full || full
	if !st.full {
		for _, name := range names {
			if !slices.Contains(st.names, name) {
				st.names = append(st.names, name)
			}
		}
		if len(st.names) > linkedFolderPending {
			st.full = true
		}
	}
	if st.running {
		return
	}
	s.runNoteFolderProbe(root, st)
}

func (s *State) runNoteFolderProbe(root string, st *noteFolderScanState) {
	if !st.full && len(st.names) == 0 {
		return
	}
	names, full := st.names, st.full
	st.names, st.full, st.running = nil, false, true
	done := func(owner *State, scan folderScan) {
		st.running = false
		owner.noteFolderAdopt(root, scan, full)
		if owner.noteScans[root] == st {
			owner.runNoteFolderProbe(root, st)
		}
	}
	if s.ai.inbox == nil {
		done(s, probeLinkedFolder(root, names, full))
		return
	}
	invalidate := s.options.Invalidate
	go func() {
		scan := probeLinkedFolder(root, names, full)
		s.aiPostWithInvalidator(func(owner *State) { done(owner, scan) }, invalidate)
	}()
}

// noteFolderRoot is the linked root folder for a path, or nil.
func (s *State) noteFolderRoot(root string) *savedNoteFolder {
	for i := range s.notes.folders {
		if l := s.notes.folders[i].Link; l != nil && l.Root && l.Path == root {
			return &s.notes.folders[i]
		}
	}
	return nil
}

// ensureNoteFolder is the folder mirroring the directory at rel below root,
// made with the folders above it when it is new.
func (s *State) ensureNoteFolder(root, rel string) string {
	rootFolder := s.noteFolderRoot(root)
	if rootFolder == nil {
		return ""
	}
	if rel == "" || rel == "." {
		return rootFolder.ID
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	for _, f := range s.notes.folders {
		if f.Link != nil && !f.Link.Root && f.Link.Path == abs {
			return f.ID
		}
	}
	parent := s.ensureNoteFolder(root, path.Dir(rel))
	f := savedNoteFolder{ID: noteID(), Name: path.Base(rel), Parent: parent, Order: len(s.notes.folders), Link: &noteFolderLink{Path: abs}}
	s.notes.folders = append(s.notes.folders, f)
	return f.ID
}

// noteFolderAdopt applies what a read of a linked root found: it is the one
// place the directory's files reach the notes.
func (s *State) noteFolderAdopt(root string, scan folderScan, full bool) {
	rootFolder := s.noteFolderRoot(root)
	if rootFolder == nil {
		return
	}
	defer s.invalidateNoteViews()
	members := map[string]string{} // absolute path -> note id
	for _, n := range s.notes.list {
		if n.Link != nil && n.Link.Tree == root {
			members[n.Link.Path] = n.ID
		}
	}
	abs := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	if scan.rootMissing {
		for abspath, id := range members {
			s.noteLinkAdopt(id, abspath, linkedFile{missing: true, err: fs.ErrNotExist})
		}
		return
	}
	if scan.err != nil && len(scan.files) == 0 && full {
		s.notes.error = s.noteLinkMessage(scan.err)
		return
	}
	hidden := map[string]bool{}
	for _, rel := range rootFolder.Link.Hidden {
		hidden[rel] = true
	}
	for _, dir := range scan.dirs {
		s.ensureNoteFolder(root, dir)
	}

	// Members whose files are gone. Rename detection below then finds, among
	// files that appeared, one that holds a missing member's text.
	isGone := func(rel string) bool {
		if scan.full && !scan.truncated {
			_, found := scan.files[rel]
			return !found
		}
		return slices.ContainsFunc(scan.gone, func(g string) bool { return rel == g || strings.HasPrefix(rel, g+"/") })
	}
	for abspath, id := range members {
		rel, err := filepath.Rel(root, abspath)
		if err != nil {
			continue
		}
		if rel = filepath.ToSlash(rel); isGone(rel) {
			if _, present := scan.files[rel]; !present {
				s.noteLinkAdopt(id, abspath, linkedFile{missing: true, err: fs.ErrNotExist})
			}
		}
	}

	rels := make([]string, 0, len(scan.files))
	for rel := range scan.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	created := false
	skipped := scan.skipped
	for _, rel := range rels {
		if hidden[rel] {
			continue
		}
		file := scan.files[rel]
		abspath := abs(rel)
		if id, ok := members[abspath]; ok {
			s.noteLinkAdopt(id, abspath, file)
			continue
		}
		if file.err != nil {
			skipped++
			continue
		}
		if id := s.noteRenamedFrom(root, members, file.body); id != "" {
			// The same text under a new name: the note follows its file, keeping
			// what Motty holds about it.
			delete(members, s.noteLinkPath(id))
			i := s.noteIndex(id)
			s.notes.list[i].Link.Path = abspath
			s.notes.list[i].Folder = s.ensureNoteFolder(root, pathDir(rel))
			if d := s.noteDrafts()[id]; d != nil {
				d.draft.Link, d.draft.Folder = s.notes.list[i].Link, s.notes.list[i].Folder
			}
			members[abspath] = id
			st := s.noteLinkInfo(id)
			st.missing, st.problem = false, ""
			s.noteLinkAdopt(id, abspath, file)
			continue
		}
		now := s.options.WallNow().UTC().Format(time.RFC3339Nano)
		note := savedNote{Revision: noteID(), ID: noteID(), Title: strings.TrimSuffix(path.Base(rel), filepath.Ext(rel)), Body: file.body, Folder: s.ensureNoteFolder(root, pathDir(rel)), Created: now, Updated: now, Order: len(s.notes.list), Link: &noteLink{Path: abspath, Tree: root}}
		if len(mustNoteJSON(recordPayload{Data: mustNoteJSON(note)})) > configstore.MaxPayload {
			skipped++
			continue
		}
		s.notes.list = append(s.notes.list, note)
		members[abspath] = note.ID
		st := s.noteLinkInfo(note.ID)
		st.bodyHash, st.bom = noteHash(file.body), file.bom
		created = true
	}
	if created || len(scan.dirs) > 0 {
		s.save()
	}
	switch {
	case scan.truncated:
		s.notes.error = s.noteText("The folder has more Markdown files than Motty reads; the rest are left out.", "文件夹中的 Markdown 文件超过 Motty 的读取上限，其余文件未导入。")
	case skipped > 0 && full:
		s.notes.error = s.noteText("Some Markdown files were left out: not UTF-8, over 1 MiB or unreadable.", "部分 Markdown 文件未导入：不是 UTF-8、超过 1 MiB 或无法读取。")
	}
}

func pathDir(rel string) string {
	d := path.Dir(rel)
	if d == "." {
		return ""
	}
	return d
}

// noteRenamedFrom is the member of root whose file is missing and whose last
// text is body, or "". An empty text matches nothing: it says nothing about
// which file it was.
func (s *State) noteRenamedFrom(root string, members map[string]string, body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	want := noteHash(body)
	for _, id := range members {
		if st := s.noteLinks[id]; st != nil && st.missing && st.bodyHash == want {
			return id
		}
	}
	return ""
}

// linkFolder links a directory of Markdown files as a folder of notes.
func (s *State) linkFolder() {
	if !s.flushNoteDraft() {
		return
	}
	dir, err := s.options.OpenDirectory(app.OpenDirectoryOptions{Title: s.noteText("Link Markdown folder", "链接 Markdown 文件夹")})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		s.notes.error = err.Error()
		return
	}
	root, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		s.notes.error = err.Error()
		return
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("not a directory")
		}
		s.notes.error = err.Error()
		return
	}
	for _, f := range s.notes.folders {
		if f.Link == nil || !f.Link.Root {
			continue
		}
		if f.Link.Path == root || strings.HasPrefix(root, f.Link.Path+string(filepath.Separator)) || strings.HasPrefix(f.Link.Path, root+string(filepath.Separator)) {
			s.notes.error = s.noteText("This folder overlaps a folder that is already linked.", "该文件夹与已链接的文件夹重叠。")
			return
		}
	}
	folder := savedNoteFolder{ID: noteID(), Name: filepath.Base(root), Parent: s.notes.folder, Order: len(s.notes.folders), Link: &noteFolderLink{Path: root, Root: true}}
	s.notes.folders = append(s.notes.folders, folder)
	s.notes.folder = folder.ID
	s.noteOpenAncestors(folder.ID)
	s.syncNoteWatches()
	s.save()
	s.probeNoteFolder(root, nil, true)
}

// noteFolderUnlink turns a linked root folder, and everything below it, into
// ordinary library notes and folders that keep their text.
func (s *State) noteFolderUnlink(id string) {
	root := s.noteFolderRoot2(id)
	if root == "" {
		return
	}
	for i := range s.notes.list {
		n := &s.notes.list[i]
		if n.Link != nil && n.Link.Tree == root {
			n.Link = nil
			if d := s.noteDrafts()[n.ID]; d != nil {
				d.draft.Link = nil
			}
			delete(s.noteLinks, n.ID)
		}
	}
	for i := range s.notes.folders {
		if l := s.notes.folders[i].Link; l != nil && (l.Path == root || strings.HasPrefix(l.Path, root+string(filepath.Separator))) {
			s.notes.folders[i].Link = nil
		}
	}
	delete(s.noteScans, root)
	s.syncNoteWatches()
	s.save()
}

// noteFolderRoot2 is the directory of a folder that is a linked root, or "".
func (s *State) noteFolderRoot2(id string) string {
	if f, ok := s.noteFolder(id); ok && f.Link != nil && f.Link.Root {
		return f.Link.Path
	}
	return ""
}

// noteTreeRootOf is the linked root a folder lies in, or "".
func (s *State) noteTreeRootOf(folder string) string {
	seen := map[string]bool{}
	for folder != "" && !seen[folder] {
		seen[folder] = true
		f, ok := s.noteFolder(folder)
		if !ok || f.Link == nil {
			return ""
		}
		if f.Link.Root {
			return f.Link.Path
		}
		folder = f.Parent
	}
	return ""
}

// noteHideMembers records the files of notes the user is deleting from Motty, so
// reading the directory again does not bring them back. The files stay on disk.
func (s *State) noteHideMembers(ids map[string]bool) {
	for _, n := range s.notes.list {
		if !ids[n.ID] || n.Link == nil || n.Link.Tree == "" {
			continue
		}
		root := s.noteFolderRoot(n.Link.Tree)
		rel, err := filepath.Rel(n.Link.Tree, n.Link.Path)
		if root == nil || err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if !slices.Contains(root.Link.Hidden, rel) {
			root.Link.Hidden = append(root.Link.Hidden, rel)
		}
	}
}

// newLinkedNote makes a file for a note created in a linked folder, so the note
// is not a library note sitting inside a folder that is a directory.
func (s *State) newLinkedNote(note *savedNote) {
	root := s.noteTreeRootOf(note.Folder)
	f, _ := s.noteFolder(note.Folder)
	if root == "" || f.Link == nil {
		return
	}
	dir := f.Link.Path
	name := "Untitled.md"
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(dir, name)); errors.Is(err, fs.ErrNotExist) {
			break
		}
		name = "Untitled " + strconv.Itoa(i) + ".md"
	}
	file := filepath.Join(dir, name)
	if err := writeLinkedFile(file, "", false); err != nil {
		s.notes.error = s.noteText("Could not create the file: ", "无法创建文件：") + err.Error()
		return
	}
	note.Link = &noteLink{Path: file, Tree: root}
	s.noteLinkInfo(note.ID).bodyHash = noteHash("")
}

// noteConfirmTouchesFiles says the deletion being confirmed concerns notes or
// folders that mirror files, which a deletion here leaves alone.
func (s *State) noteConfirmTouchesFiles() bool {
	n := s.noteContent()
	switch n.confirm {
	case "note":
		return s.noteLinkPath(n.selected) != ""
	case "folder":
		for _, note := range s.notes.list {
			if note.Link != nil && s.noteFolderWithin(note.Folder, n.folder) {
				return true
			}
		}
		f, _ := s.noteFolder(n.folder)
		return f.Link != nil
	}
	return false
}
