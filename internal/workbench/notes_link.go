package workbench

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/configstore"
)

// A linked note keeps its text in a Markdown file instead of the library. The
// file is the single source of truth: the note holds the last text seen in it so
// the note stays readable when the file cannot be reached, and a save writes the
// file. What the note and the file last agreed on is remembered as a hash, so
// the two can be told apart from a change made on either side since.

var (
	hitNoteLinkFile      = tree.ID(notesHits, "link-file")
	hitNoteLinkLoad      = tree.ID(notesHits, "link-load")
	hitNoteLinkOverwrite = tree.ID(notesHits, "link-overwrite")
	hitNoteLinkFork      = tree.ID(notesHits, "link-fork")
	hitNoteLinkLocate    = tree.ID(notesHits, "link-locate")
	hitNoteLinkUnlink    = tree.ID(notesHits, "link-unlink")
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

var (
	errLinkNotUTF8    = errors.New("Markdown must be UTF-8")
	errLinkTooLarge   = errors.New("the file exceeds 1 MiB")
	errLinkNotRegular = errors.New("not a regular file")
)

// noteLinkState is what one window knows about a linked note's file.
type noteLinkState struct {
	// bodyHash is the hash of the text the note and the file last agreed on: read
	// from the file, or written to it. A note whose text hashes differently has
	// edits the file does not have; a file that hashes differently has changes the
	// note does not have.
	bodyHash string
	// bom is whether the file starts with a byte order mark, which a save keeps.
	bom bool
	// conflict: the file changed while the note has edits of its own, so neither
	// side is written over the other until the user chooses.
	conflict bool
	// missing: the file or its directory cannot be found. The note keeps its last
	// text and is not written.
	missing bool
	// problem is why the file could not be read or written, for the user.
	problem string
	// reading is set while a background read is in flight; recheck asks for one
	// more when a change arrived during it.
	reading, recheck bool
}

// linkedFile is what reading a linked file found.
type linkedFile struct {
	body    string
	bom     bool
	missing bool
	err     error
}

// readLinkedFile reads a file a note is linked to, within the note size limit.
func readLinkedFile(path string) linkedFile {
	f, err := os.Open(path)
	if err != nil {
		return linkedFile{missing: isLinkMissing(err), err: err}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return linkedFile{err: err}
	}
	if !info.Mode().IsRegular() {
		return linkedFile{err: errLinkNotRegular}
	}
	raw, err := io.ReadAll(io.LimitReader(f, configstore.MaxPayload+1))
	if err != nil {
		return linkedFile{err: err}
	}
	if len(raw) > configstore.MaxPayload {
		return linkedFile{err: errLinkTooLarge}
	}
	if !utf8.Valid(raw) {
		return linkedFile{err: errLinkNotUTF8}
	}
	return linkedFile{body: string(bytes.TrimPrefix(raw, utf8BOM)), bom: bytes.HasPrefix(raw, utf8BOM)}
}

// isLinkMissing says the path does not exist, as opposed to existing and being
// unreadable: a missing directory or a file that moved is something to relocate,
// a permission error is not.
func isLinkMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// writeLinkedFile replaces the file with body: the text goes to a temporary file
// beside it, is flushed, and is renamed over the original, so a reader sees the
// old file or the new one and never half of one. The original's permissions are
// kept, and a link is followed so the file it points to is the one replaced.
func writeLinkedFile(path, body string, bom bool) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".motty-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	fail := func(err error) error {
		temp.Close()
		os.Remove(name)
		return err
	}
	if bom {
		if _, err := temp.Write(utf8BOM); err != nil {
			return fail(err)
		}
	}
	if _, err := io.WriteString(temp, body); err != nil {
		return fail(err)
	}
	if runtime.GOOS != "windows" {
		if err := temp.Chmod(mode); err != nil {
			return fail(err)
		}
	}
	if err := temp.Sync(); err != nil {
		return fail(err)
	}
	if err := temp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, target); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func (s *State) noteLinkInfo(id string) *noteLinkState {
	if s.noteLinks == nil {
		s.noteLinks = map[string]*noteLinkState{}
	}
	st := s.noteLinks[id]
	if st == nil {
		st = &noteLinkState{}
		s.noteLinks[id] = st
	}
	return st
}

// noteLinkPath is the file a note is linked to, or "" for a library note.
func (s *State) noteLinkPath(id string) string {
	if i := s.noteIndex(id); i >= 0 && s.notes.list[i].Link != nil {
		return s.notes.list[i].Link.Path
	}
	return ""
}

// noteMemoryBody is the text the user has in the note right now, including edits
// not yet moved from the editor into the note.
func (s *State) noteMemoryBody(id string) string {
	if d := s.noteDrafts()[id]; d != nil && s.noteIndex(id) >= 0 {
		return d.body.String()
	}
	if i := s.noteIndex(id); i >= 0 {
		return s.notes.list[i].Body
	}
	return ""
}

func (s *State) noteLinkMessage(err error) string {
	switch {
	case errors.Is(err, errLinkNotUTF8):
		return s.noteText("Markdown must be UTF-8.", "Markdown 文件必须使用 UTF-8 编码。")
	case errors.Is(err, errLinkTooLarge):
		return s.noteText("The file exceeds 1 MiB.", "文件超过 1 MiB。")
	case errors.Is(err, errLinkNotRegular):
		return s.noteText("The linked path is not a regular file.", "链接的路径不是普通文件。")
	}
	return err.Error()
}

// linkNotes links Markdown files as notes: the text stays in the file.
func (s *State) linkNotes() {
	if !s.flushNoteDraft() {
		return
	}
	paths, err := s.options.OpenFiles(app.OpenFileOptions{Title: s.noteText("Link Markdown file", "链接 Markdown 文件"), AllowMultiple: true})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		s.notes.error = err.Error()
		return
	}
	type fresh struct {
		note savedNote
		file linkedFile
	}
	var added []fresh
	var existing string
	for _, path := range paths {
		path = filepath.Clean(path)
		if !filepath.IsAbs(path) {
			if path, err = filepath.Abs(path); err != nil {
				s.notes.error = err.Error()
				return
			}
		}
		if id := s.noteLinkedTo(path); id != "" {
			if existing == "" {
				existing = id
			}
			continue
		}
		file := readLinkedFile(path)
		if file.err != nil {
			s.notes.error = s.noteLinkMessage(file.err)
			return
		}
		now := s.options.WallNow().UTC().Format(time.RFC3339Nano)
		note := savedNote{Revision: noteID(), ID: noteID(), Title: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Body: file.body, Folder: s.notes.folder, Created: now, Updated: now, Order: len(s.notes.list) + len(added), Link: &noteLink{Path: path}}
		if len(mustNoteJSON(recordPayload{Data: mustNoteJSON(note)})) > configstore.MaxPayload {
			s.notes.error = s.noteText("Linked note exceeds 1 MiB; no files linked.", "链接的笔记超过 1 MiB，未链接任何文件。")
			return
		}
		added = append(added, fresh{note, file})
	}
	for _, item := range added {
		s.notes.list = append(s.notes.list, item.note)
		s.noteLinkInfo(item.note.ID).bodyHash = noteHash(item.file.body)
		s.noteLinkInfo(item.note.ID).bom = item.file.bom
	}
	s.syncNoteWatches()
	s.save()
	switch {
	case len(added) > 0:
		s.selectNote(added[0].note.ID)
	case existing != "":
		s.selectNote(existing)
	}
}

// noteLinkedTo is the note already linked to path, or "".
func (s *State) noteLinkedTo(path string) string {
	info, infoErr := os.Stat(path)
	for _, n := range s.notes.list {
		if n.Link == nil {
			continue
		}
		if n.Link.Path == path {
			return n.ID
		}
		if other, err := os.Stat(n.Link.Path); infoErr == nil && err == nil && os.SameFile(info, other) {
			return n.ID
		}
	}
	return ""
}

// noteLinksRestored runs after the notes were read from the store: it starts
// following the files of linked notes this window has not seen yet, and lets go
// of the ones that are gone.
func (s *State) noteLinksRestored() {
	live := map[string]bool{}
	for i := range s.notes.list {
		n := &s.notes.list[i]
		if n.Link == nil {
			continue
		}
		if n.Link.Tree != "" && s.noteFolderRoot(n.Link.Tree) == nil {
			// Its linked folder is gone: it is a file linked on its own now.
			n.Link.Tree = ""
		}
		live[n.ID] = true
		if _, known := s.noteLinks[n.ID]; !known {
			s.noteLinkInfo(n.ID).bodyHash = noteHash(n.Body)
			if n.Link.Tree == "" {
				s.noteLinkRefresh(n.ID)
			}
		}
	}
	for _, f := range s.notes.folders {
		// Each linked folder is read once per run; what changes later arrives
		// through its watch.
		if f.Link != nil && f.Link.Root && s.noteScans[f.Link.Path] == nil {
			s.probeNoteFolder(f.Link.Path, nil, true)
		}
	}
	for id := range s.noteLinks {
		if !live[id] {
			delete(s.noteLinks, id)
		}
	}
	s.syncNoteWatches()
}

// noteLinkRefresh reads a linked note's file away from the window thread, which
// a stalled network drive would otherwise freeze, and applies what it found there.
func (s *State) noteLinkRefresh(id string) {
	path := s.noteLinkPath(id)
	if path == "" {
		return
	}
	st := s.noteLinkInfo(id)
	if st.reading {
		st.recheck = true
		return
	}
	st.reading = true
	if s.ai.inbox == nil {
		s.noteLinkAdopt(id, path, readLinkedFile(path))
		return
	}
	invalidate := s.options.Invalidate
	go func() {
		file := readLinkedFile(path)
		s.aiPostWithInvalidator(func(owner *State) { owner.noteLinkAdopt(id, path, file) }, invalidate)
	}()
}

// noteLinkAdopt applies what reading the file found. It is the one place a change
// to the file reaches the note: an untouched note takes it, a note with edits of
// its own is marked as in conflict, and a file that cannot be reached is noted.
func (s *State) noteLinkAdopt(id, path string, file linkedFile) {
	st := s.noteLinks[id]
	if st == nil {
		return
	}
	again := st.recheck
	st.reading, st.recheck = false, false
	if again {
		defer s.noteLinkRefresh(id)
	}
	if s.noteLinkPath(id) != path {
		return
	}
	defer s.invalidateNoteViews()
	switch {
	case file.missing:
		st.missing, st.problem = true, ""
		return
	case file.err != nil:
		st.missing, st.problem = false, s.noteLinkMessage(file.err)
		return
	}
	st.missing, st.problem = false, ""
	disk := noteHash(file.body)
	st.bom = file.bom
	if disk == st.bodyHash {
		// The file is what the note last had, which is also what a save of the
		// note's own just wrote: nothing to do, and a conflict that came from a
		// change since undone is over.
		st.conflict = false
		return
	}
	memory := s.noteMemoryBody(id)
	switch {
	case noteHash(memory) == st.bodyHash:
		s.noteLinkSetBody(id, file.body)
		st.bodyHash, st.conflict = disk, false
	case noteHash(memory) == disk:
		st.bodyHash, st.conflict = disk, false
	default:
		st.conflict = true
	}
}

// noteLinkSetBody makes body the text of a linked note, in the note and in the
// editors that show it, keeping selections where the text allows and the title
// and tags the user may be editing.
func (s *State) noteLinkSetBody(id, body string) {
	i := s.noteIndex(id)
	if i < 0 {
		return
	}
	next := s.notes.list[i]
	if previous := noteRevision(next); !slices.Contains(next.Ancestors, previous) {
		next.Ancestors = append(slices.Clone(next.Ancestors), previous)
	}
	next.Body, next.Revision = body, noteID()
	next.Updated = s.options.WallNow().UTC().Format(time.RFC3339Nano)
	s.notes.list[i] = next
	if d := s.noteDrafts()[id]; d != nil {
		replaceNoteEditorValue(&d.body, body)
		d.draft.Body, d.draft.Revision, d.draft.Ancestors, d.draft.Updated = next.Body, next.Revision, next.Ancestors, next.Updated
	}
	s.save()
}

// noteLinkWrite is the save of a linked note's text to its file. A file that
// cannot be written, or that changed meanwhile, leaves the text in the note and
// says so in the note's notice; the user is not held in the note.
func (s *State) noteLinkWrite(note savedNote) {
	st := s.noteLinkInfo(note.ID)
	if st.conflict || st.missing {
		return
	}
	disk := readLinkedFile(note.Link.Path)
	switch {
	case disk.missing:
		st.missing = true
		return
	case disk.err != nil:
		st.problem = s.noteLinkMessage(disk.err)
		return
	case noteHash(disk.body) != st.bodyHash:
		st.bom, st.conflict = disk.bom, true
		return
	}
	if err := writeLinkedFile(note.Link.Path, note.Body, st.bom); err != nil {
		st.problem = s.noteText("Could not write the file: ", "无法写入文件：") + err.Error()
		return
	}
	st.bodyHash, st.problem = noteHash(note.Body), ""
}

// noteLinkNotice is what a linked note needs the user to know, and can be asked.
type noteLinkNotice struct {
	text    string
	actions []noteLinkAction
}

type noteLinkAction struct {
	id    gfx.ID
	label string
}

func (s *State) noteLinkNotice(id string) (noteLinkNotice, bool) {
	path := s.noteLinkPath(id)
	st := s.noteLinks[id]
	if path == "" || st == nil {
		return noteLinkNotice{}, false
	}
	switch {
	case st.conflict:
		return noteLinkNotice{
			text: s.noteText("The file changed on disk while this note has unsaved edits.", "文件在磁盘上已更改，而此笔记含有未保存的编辑。"),
			actions: []noteLinkAction{
				{hitNoteLinkLoad, s.noteText("Load disk version", "载入磁盘版本")},
				{hitNoteLinkOverwrite, s.noteText("Keep mine and overwrite", "保留我的并覆盖")},
				{hitNoteLinkFork, s.noteText("Save mine as a note", "另存我的为笔记")},
			},
		}, true
	case st.missing:
		return noteLinkNotice{
			text: s.noteText("Source file is missing: ", "源文件已丢失：") + path,
			actions: []noteLinkAction{
				{hitNoteLinkLocate, s.noteText("Locate…", "重新定位…")},
				{hitNoteLinkUnlink, s.noteText("Convert to library note", "转为库内笔记")},
			},
		}, true
	case st.problem != "":
		return noteLinkNotice{
			text: st.problem,
			actions: []noteLinkAction{
				{hitNoteLinkOverwrite, s.noteText("Write to file", "写入文件")},
				{hitNoteLinkLoad, s.noteText("Load disk version", "载入磁盘版本")},
			},
		}, true
	}
	return noteLinkNotice{}, false
}

// paintNoteLinkNotice shows what a linked note needs, with the choices, and
// returns the height it used.
func (s *State) paintNoteLinkNotice(c *gfx.Canvas, x, y, width float32) float32 {
	n := s.noteContent()
	notice, ok := s.noteLinkNotice(n.selected)
	if !ok {
		return 0
	}
	used := float32(0)
	c.Label(notice.text, gfx.Rect{X: x, Y: y, Width: width, Height: 20}, gfx.TextStyle{Size: 12, Color: s.pal.danger})
	used += 24
	bx, by := x, y+used
	for _, action := range notice.actions {
		button := components.BeginButton(c, s.Theme(), action.id, gfx.Rect{X: bx, Y: by, Height: 28}).Small().SizeToFit(action.label)
		if bx > x && bx+button.BoundsWidth() > x+width {
			bx, by = x, by+32
			button = components.BeginButton(c, s.Theme(), action.id, gfx.Rect{X: bx, Y: by, Height: 28}).Small().SizeToFit(action.label)
		}
		button.Outline(action.label)
		bx += button.BoundsWidth() + 8
	}
	return by + 28 + 8 - y
}

// clickNoteLink handles the choices a linked note's notice offers.
func (s *State) clickNoteLink(id gfx.ID) bool {
	switch id {
	case hitNoteLinkLoad, hitNoteLinkOverwrite, hitNoteLinkFork, hitNoteLinkLocate, hitNoteLinkUnlink:
	default:
		return false
	}
	n := s.noteContent()
	note := n.selected
	if s.noteLinkPath(note) == "" {
		return true
	}
	if !s.flushNoteDraft() {
		return true
	}
	switch id {
	case hitNoteLinkLoad:
		s.noteLinkLoadDisk(note)
	case hitNoteLinkOverwrite:
		s.noteLinkOverwrite(note)
	case hitNoteLinkFork:
		s.noteLinkFork(note)
	case hitNoteLinkLocate:
		s.noteLinkLocate(note)
	case hitNoteLinkUnlink:
		s.noteLinkUnlink(note)
	}
	s.invalidateNoteViews()
	return true
}

// noteLinkLoadDisk replaces the note's text with the file's.
func (s *State) noteLinkLoadDisk(id string) {
	st := s.noteLinkInfo(id)
	file := readLinkedFile(s.noteLinkPath(id))
	switch {
	case file.missing:
		st.missing = true
	case file.err != nil:
		st.problem = s.noteLinkMessage(file.err)
	default:
		s.noteLinkSetBody(id, file.body)
		st.bodyHash, st.bom = noteHash(file.body), file.bom
		st.conflict, st.missing, st.problem = false, false, ""
	}
}

// noteLinkOverwrite writes the note's text over the file's.
func (s *State) noteLinkOverwrite(id string) {
	st := s.noteLinkInfo(id)
	i := s.noteIndex(id)
	if i < 0 {
		return
	}
	note := s.notes.list[i]
	if err := writeLinkedFile(note.Link.Path, note.Body, st.bom); err != nil {
		st.problem = s.noteText("Could not write the file: ", "无法写入文件：") + err.Error()
		return
	}
	st.bodyHash = noteHash(note.Body)
	st.conflict, st.missing, st.problem = false, false, ""
}

// noteLinkFork keeps the note's text as a note of the library, then takes the
// file's text for the linked one.
func (s *State) noteLinkFork(id string) {
	i := s.noteIndex(id)
	if i < 0 {
		return
	}
	now := s.options.WallNow().UTC().Format(time.RFC3339Nano)
	copyNote := s.notes.list[i]
	copyNote.ID, copyNote.Revision, copyNote.Ancestors, copyNote.Link = noteID(), noteID(), nil, nil
	copyNote.Title += s.noteText(" (my edits)", "（我的编辑）")
	copyNote.Created, copyNote.Updated, copyNote.Order = now, now, len(s.notes.list)
	s.notes.list = append(s.notes.list, copyNote)
	s.noteLinkLoadDisk(id)
	s.save()
}

// noteLinkLocate points a note whose file is gone at another file.
func (s *State) noteLinkLocate(id string) {
	paths, err := s.options.OpenFiles(app.OpenFileOptions{Title: s.noteText("Locate Markdown file", "重新定位 Markdown 文件")})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		s.notes.error = err.Error()
		return
	}
	if len(paths) == 0 {
		return
	}
	path, err := filepath.Abs(paths[0])
	if err != nil {
		s.notes.error = err.Error()
		return
	}
	i := s.noteIndex(id)
	if i < 0 {
		return
	}
	file := readLinkedFile(path)
	if file.err != nil {
		s.notes.error = s.noteLinkMessage(file.err)
		return
	}
	s.notes.list[i].Link = &noteLink{Path: path}
	if d := s.noteDrafts()[id]; d != nil {
		d.draft.Link = s.notes.list[i].Link
	}
	st := s.noteLinkInfo(id)
	st.missing, st.problem = false, ""
	s.noteLinkAdopt(id, path, file)
	s.syncNoteWatches()
	s.save()
}

// noteLinkUnlink turns a linked note into a library note that keeps its text.
func (s *State) noteLinkUnlink(id string) {
	i := s.noteIndex(id)
	if i < 0 {
		return
	}
	s.notes.list[i].Link = nil
	if d := s.noteDrafts()[id]; d != nil {
		d.draft.Link = nil
	}
	delete(s.noteLinks, id)
	s.syncNoteWatches()
	s.save()
}

// syncNoteWatches makes the directory watches the ones the linked notes need:
// a file is followed through its directory, because an editor that saves by
// replacing the file leaves a watch on the file itself pointing at the old one.
func (s *State) syncNoteWatches() {
	want := map[string]bool{}
	for _, n := range s.notes.list {
		if n.Link != nil && n.Link.Tree == "" {
			want[filepath.Dir(n.Link.Path)] = true
		}
	}
	roots := map[string]bool{}
	for _, f := range s.notes.folders {
		if f.Link != nil && f.Link.Root {
			roots[f.Link.Path] = true
		}
	}
	for root, watch := range s.noteTreeWatches {
		if !roots[root] {
			watch.Close()
			delete(s.noteTreeWatches, root)
		}
	}
	for root := range roots {
		if s.noteTreeWatches[root] != nil {
			continue
		}
		watch, err := app.WatchTree(root, noteTreeSkip, func() {
			if s.options.Invalidate != nil {
				s.options.Invalidate()
			}
		})
		if err != nil {
			// Not watchable (missing, too large, unsupported): it is read when the
			// notes are opened, and the watch is tried again then.
			if errors.Is(err, app.ErrWatchTooLarge) {
				s.notes.error = s.noteText("The folder is too large to watch; it is read when you open Notes.", "文件夹过大无法实时监听；打开笔记时会重新读取。")
			}
			continue
		}
		if s.noteTreeWatches == nil {
			s.noteTreeWatches = map[string]*app.DirectoryWatch{}
		}
		s.noteTreeWatches[root] = watch
	}
	for dir, watch := range s.noteWatches {
		if !want[dir] {
			watch.Close()
			delete(s.noteWatches, dir)
		}
	}
	invalidate := s.options.Invalidate
	wake := func() {
		if invalidate != nil {
			invalidate()
		}
	}
	for dir := range want {
		if s.noteWatches[dir] != nil {
			continue
		}
		watch, err := app.WatchDirectory(dir, wake)
		if err != nil {
			// The directory is not there or cannot be watched: what its notes say is
			// read from the files, and the watch is tried again when notes are used.
			for _, n := range s.notes.list {
				if n.Link != nil && n.Link.Tree == "" && filepath.Dir(n.Link.Path) == dir {
					s.noteLinkRefresh(n.ID)
				}
			}
			continue
		}
		if s.noteWatches == nil {
			s.noteWatches = map[string]*app.DirectoryWatch{}
		}
		s.noteWatches[dir] = watch
	}
}

// noteLinksRecheck looks again at what could not be followed: directories that
// could not be watched, and files that were missing or unreadable.
func (s *State) noteLinksRecheck() {
	s.syncNoteWatches()
	for id, st := range s.noteLinks {
		if (st.missing || st.problem != "") && s.noteLinkTree(id) == "" {
			s.noteLinkRefresh(id)
		}
	}
	for _, f := range s.notes.folders {
		if f.Link != nil && f.Link.Root && s.noteTreeWatches[f.Link.Path] == nil {
			s.probeNoteFolder(f.Link.Path, nil, true)
		}
	}
}

// noteLinkTree is the linked folder a note's file belongs to, or "".
func (s *State) noteLinkTree(id string) string {
	if i := s.noteIndex(id); i >= 0 && s.notes.list[i].Link != nil {
		return s.notes.list[i].Link.Tree
	}
	return ""
}

// pumpNoteLinks reads what the directory watches gathered. Wake calls it.
func (s *State) pumpNoteLinks() {
	for dir, watch := range s.noteWatches {
		changes := watch.Changes(s.noteWatchNames)
		s.noteWatchNames = changes.Names[:0]
		if changes.Resync {
			// The system lost events or the directory itself changed: start the
			// watch again and look at every file in it.
			watch.Close()
			delete(s.noteWatches, dir)
			s.syncNoteWatches()
		}
		for _, n := range s.notes.list {
			if n.Link == nil || n.Link.Tree != "" || filepath.Dir(n.Link.Path) != dir {
				continue
			}
			if changes.Resync || slices.ContainsFunc(changes.Names, func(name string) bool { return sameFileName(name, filepath.Base(n.Link.Path)) }) {
				s.noteLinkRefresh(n.ID)
			}
		}
	}
	for root, watch := range s.noteTreeWatches {
		changes := watch.Changes(s.noteWatchNames)
		s.noteWatchNames = changes.Names[:0]
		switch {
		case changes.Resync:
			watch.Close()
			delete(s.noteTreeWatches, root)
			s.syncNoteWatches()
			s.probeNoteFolder(root, nil, true)
		case len(changes.Names) > 0:
			s.probeNoteFolder(root, slices.Clone(changes.Names), false)
		}
	}
}

// sameFileName compares names as the file system does for the file systems that
// ignore case; where they do not, an extra comparison costs one re-read.
func sameFileName(a, b string) bool {
	return a == b || (runtime.GOOS != "linux" && strings.EqualFold(a, b))
}

func (s *State) closeNoteWatches() {
	for root, watch := range s.noteTreeWatches {
		watch.Close()
		delete(s.noteTreeWatches, root)
	}
	for dir, watch := range s.noteWatches {
		watch.Close()
		delete(s.noteWatches, dir)
	}
}

// noteLinkStatus is the line shown with a linked note where a library note shows
// when it was updated.
func (s *State) noteLinkStatus(id string, width func(string) float32, room float32) (string, bool) {
	path := s.noteLinkPath(id)
	if path == "" {
		return "", false
	}
	full := s.noteText("Linked to ", "链接到 ") + path
	if width(full) <= room {
		return full, true
	}
	short := s.noteText("Linked: ", "链接：") + filepath.Base(path)
	if width(short) <= room {
		return short, true
	}
	return "", false
}
