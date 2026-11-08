package workbench

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"motty/internal/configstore"
)

func (s *State) importNotes() {
	if !s.flushNoteDraft() {
		return
	}
	paths, err := s.options.OpenFiles(app.OpenFileOptions{Title: s.noteText("Import Markdown", "导入 Markdown"), AllowMultiple: true})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		s.notes.error = err.Error()
		return
	}
	var imported []savedNote
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			s.notes.error = err.Error()
			return
		}
		body, err := io.ReadAll(io.LimitReader(f, configstore.MaxPayload+1))
		f.Close()
		if err != nil {
			s.notes.error = err.Error()
			return
		}
		if !utf8.Valid(body) {
			s.notes.error = s.noteText("Markdown must be UTF-8.", "Markdown 文件必须使用 UTF-8 编码。")
			return
		}
		now := s.options.WallNow().UTC().Format(time.RFC3339Nano)
		note := savedNote{Revision: noteID(), ID: noteID(), Title: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Body: strings.TrimPrefix(string(body), "\ufeff"), Folder: s.notes.folder, Created: now, Updated: now, Order: len(s.notes.list) + len(imported)}
		if len(mustNoteJSON(recordPayload{Data: mustNoteJSON(note)})) > configstore.MaxPayload {
			s.notes.error = s.noteText("Imported note exceeds 1 MiB; no files imported.", "导入笔记超过 1 MiB，未导入任何文件。")
			return
		}
		imported = append(imported, note)
	}
	s.notes.list = append(s.notes.list, imported...)
	s.save()
	if len(imported) > 0 {
		s.selectNote(imported[0].ID)
	}
}
func noteFilename(v string) string {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, v)
	v = strings.Trim(strings.TrimSpace(v), ". ")
	if v == "" {
		v = "Untitled"
	}
	if len(v) > 120 {
		v = strings.ToValidUTF8(v[:120], "")
	}
	return v
}
func (s *State) notesZIP() ([]byte, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	used := map[string]bool{}
	for _, note := range s.notes.list {
		var dirs []string
		seen := map[string]bool{}
		id := note.Folder
		for id != "" && !seen[id] {
			seen[id] = true
			next := ""
			for _, f := range s.notes.folders {
				if f.ID == id {
					dirs = append([]string{noteFilename(f.Name)}, dirs...)
					next = f.Parent
					break
				}
			}
			id = next
		}
		base := strings.Join(append(dirs, noteFilename(note.Title)), "/")
		name := base + ".md"
		for i := 2; used[strings.ToLower(name)]; i++ {
			name = fmt.Sprintf("%s (%d).md", base, i)
		}
		used[strings.ToLower(name)] = true
		w, err := z.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = io.WriteString(w, note.Body); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
func (s *State) exportNotes(all bool) {
	if !s.flushNoteDraft() {
		return
	}
	s.save()
	var data []byte
	var err error
	name := "notes.zip"
	if all {
		data, err = s.notesZIP()
	} else {
		i := s.noteIndex(s.notes.selected)
		if i < 0 {
			return
		}
		note := s.notes.list[i]
		name = noteFilename(note.Title) + ".md"
		data = []byte(note.Body)
	}
	if err != nil {
		s.notes.error = err.Error()
		return
	}
	path, err := s.options.SaveFile(app.SaveFileOptions{Title: s.noteText("Export notes", "导出笔记"), SuggestedName: name})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err == nil && path != "" {
		err = os.WriteFile(path, data, 0600)
	}
	if err != nil {
		s.notes.error = err.Error()
	}
}
