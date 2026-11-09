//go:build darwin || windows || linux

package workbench

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"testing"
)

func TestNotesSidebarNativeMouseActivatesAssociationSwitch(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.activeSection = sectionTerminal
	s.Click(hitNotesPanel)
	var c gfx.Canvas
	driver, err := app.NewDriver(app.Config{Title: "notes sidebar mouse", Width: 1200, Height: 800, Tree: Tree(), Paint: s.Paint, OnPointer: s.Pointer, OnKey: s.Key}, &c)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	hit, ok := hitByID(&c, hitNoteSideLinked)
	if !ok {
		t.Fatal("switch missing")
	}
	if err := driver.Click(hit.Bounds.X+hit.Bounds.Width-16, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !s.noteSidebarState().content.sideAll {
		t.Fatal("native mouse click did not toggle association filter")
	}
	if err := driver.Click(hit.Bounds.X+4, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if s.noteSidebarState().content.sideAll {
		t.Fatal("caption click did not toggle association filter back")
	}
}
