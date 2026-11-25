//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
)

func openNotesRenameSession(t *testing.T, title string, chinese bool) (*State, *headless.Session) {
	t.Helper()
	options := freshOptions(t)
	if chinese {
		options.PreferredLanguages = func() []string { return []string{"zh-CN"} }
	}
	s := newTestState(t, options)
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	s.notes.title.Insert(title)
	s.notes.body.Insert("keep this body")
	s.save()
	config := s.WindowConfig(app.Config{
		Title: "Notes rename", Width: 1232, Height: 700, Tree: Tree(),
		Paint: s.Paint, OnPointer: s.Pointer, OnKey: s.Key, OnShortcut: s.Shortcut, OnScroll: s.Scroll, OnResize: s.Resize,
		TextInput: func(c app.TextInputContext) *app.TextInput {
			if editor := s.EditorFor(c.ID); editor != nil {
				return &app.TextInput{Editor: editor}
			}
			return nil
		},
	})
	v, err := headless.Open(config, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	return s, v
}

func clickNotesRename(t *testing.T, s *State, v *headless.Session) {
	t.Helper()
	for _, label := range []string{s.noteText("More actions", "更多操作"), s.noteText("Rename", "重命名")} {
		if err := v.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := hitByIDSnapshot(v.Snapshot().Hits, hitNoteInline); !ok {
		t.Fatal("Rename opened no visible name field")
	}
	if v.Snapshot().Focused != hitNoteInline {
		t.Fatal("Rename did not focus the name field")
	}
}

func TestNotesRenameExistingNoteUsesFocusedField(t *testing.T) {
	for _, chinese := range []bool{false, true} {
		for _, title := range []string{"", "Original title"} {
			t.Run(noteRenameCaseName(chinese, title), func(t *testing.T) {
				s, v := openNotesRenameSession(t, title, chinese)
				id := s.notes.selected
				clickNotesRename(t, s, v)
				name := s.noteText("Renamed note", "改名后的笔记")
				// Typing immediately replaces the selected old name, without a
				// click or Ctrl+A that could hide a missing focus/selection.
				if err := v.Type(name); err != nil {
					t.Fatal(err)
				}
				if err := v.Keys("Enter"); err != nil {
					t.Fatal(err)
				}
				if s.notes.view.inlineID != "" || s.notes.error != "" || s.notes.title.String() != name || s.notes.body.String() != "keep this body" {
					t.Fatalf("rename left title=%q body=%q inline=%q error=%q", s.notes.title.String(), s.notes.body.String(), s.notes.view.inlineID, s.notes.error)
				}
				if err := v.ClickLabel(s.messages.NavHosts); err != nil {
					t.Fatal(err)
				}
				if s.activeSection != sectionHosts {
					t.Fatal("renaming a note blocked navigation")
				}
				reopened := newTestState(t, s.options)
				if at := reopened.noteIndex(id); at < 0 || reopened.notes.list[at].Title != name || reopened.notes.list[at].Body != "keep this body" {
					t.Fatal("the renamed note was not persisted with its original body")
				}
			})
		}
	}
}

func noteRenameCaseName(chinese bool, title string) string {
	language := "English"
	if chinese {
		language = "Chinese"
	}
	if title == "" {
		return language + "/untitled"
	}
	return language + "/titled"
}

func TestNotesRenameEmptyNameCanBeCorrectedOrCanceled(t *testing.T) {
	for _, action := range []string{"Enter", "Tab", "outside", "Escape"} {
		t.Run(action, func(t *testing.T) {
			s, v := openNotesRenameSession(t, "Original title", true)
			clickNotesRename(t, s, v)
			if err := v.Keys("Backspace"); err != nil {
				t.Fatal(err)
			}
			if err := v.Keys("Enter"); err != nil {
				t.Fatal(err)
			}
			if s.notes.error != s.noteText("Name cannot be empty.", "名称不能为空。") || s.notes.title.String() != "Original title" {
				t.Fatal("an empty name did not retain the original title and show validation")
			}
			if _, ok := hitByIDSnapshot(v.Snapshot().Hits, hitNoteInline); !ok || v.Snapshot().Focused != hitNoteInline {
				t.Fatal("the invalid name field cannot be corrected")
			}
			want := "Original title"
			if action != "Escape" {
				want = "修正后的标题"
				if err := v.Type(want); err != nil {
					t.Fatal(err)
				}
			}
			if action == "outside" {
				if err := v.ClickLabel(s.noteText("Title", "标题")); err != nil {
					t.Fatal(err)
				}
			} else if err := v.Keys(action); err != nil {
				t.Fatal(err)
			}
			if s.notes.view.inlineID != "" || s.notes.error != "" || s.notes.title.String() != want {
				t.Fatalf("%s left title=%q inline=%q error=%q", action, s.notes.title.String(), s.notes.view.inlineID, s.notes.error)
			}
			if err := v.ClickLabel(s.messages.NavHosts); err != nil {
				t.Fatal(err)
			}
			if s.activeSection != sectionHosts {
				t.Fatal("leaving name editing blocked navigation")
			}
		})
	}
}

func TestNotesRenameFolderUsesFocusedField(t *testing.T) {
	s, v := openNotesRenameSession(t, "Original title", true)
	if err := v.ClickLabel(s.noteText("New folder", "新建文件夹")); err != nil {
		t.Fatal(err)
	}
	if err := v.Type("原目录"); err != nil {
		t.Fatal(err)
	}
	if err := v.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	if len(s.notes.folders) != 1 {
		t.Fatal("typing into the new folder's name field did not create it")
	}
	id := s.notes.folders[0].ID
	clickNotesRename(t, s, v)
	if err := v.Type("改名后的目录"); err != nil {
		t.Fatal(err)
	}
	if err := v.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	reopened := newTestState(t, s.options)
	folder, ok := reopened.noteFolder(id)
	if !ok || folder.Name != "改名后的目录" || s.notes.title.String() != "Original title" || s.notes.body.String() != "keep this body" {
		t.Fatal("renaming a folder did not persist its name or changed the note")
	}
}

func TestNotesCreateNestedFolderUsesFocusedField(t *testing.T) {
	s, v := openNotesRenameSession(t, "Original title", false)
	addFolder := s.noteText("New folder", "新建文件夹")
	createFolder := func(name string) {
		t.Helper()
		if err := v.ClickLabel(addFolder); err != nil {
			t.Fatal(err)
		}
		if v.Snapshot().Focused != hitNoteInline {
			t.Fatal("creating a folder did not focus the name field")
		}
		if err := v.Type(name); err != nil {
			t.Fatal(err)
		}
		if err := v.Keys("Enter"); err != nil {
			t.Fatal(err)
		}
	}
	createFolder("Parent folder")
	if len(s.notes.folders) != 1 {
		t.Fatal("the parent folder was not created")
	}
	parentID := s.notes.folders[0].ID
	if err := v.ClickLabel("Parent folder"); err != nil {
		t.Fatal(err)
	}
	createFolder("Child folder")
	childID := s.notes.folder

	reopened := newTestState(t, s.options)
	child, ok := reopened.noteFolder(childID)
	if !ok || child.Name != "Child folder" || child.Parent != parentID {
		t.Fatalf("the child folder was not saved under %q: %+v", parentID, child)
	}
}
