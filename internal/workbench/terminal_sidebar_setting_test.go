package workbench

import (
	"encoding/json"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	layoutcore "motty/internal/workspace"
	"reflect"
	"testing"
)

func TestTerminalSidebarMigrationAndExplicitOff(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want []string
	}{
		{`{"sftp_panel_auto_open":true}`, []string{"sftp"}},
		{`{"terminal_sidebars":[],"sftp_panel_auto_open":true}`, []string{}},
		{`{"terminal_sidebars":["off","sftp"],"sftp_panel_auto_open":true}`, []string{}},
		{`{"terminal_sidebars":["notes","notes","bad","ai","sftp"]}`, []string{"notes", "ai"}},
	} {
		var saved savedState
		if err := json.Unmarshal([]byte(tt.raw), &saved); err != nil {
			t.Fatal(err)
		}
		s := &State{}
		s.restoreTerminalSidebars(saved.TerminalSidebars, saved.SFTPPanelAutoOpen)
		if !reflect.DeepEqual(s.terminalSidebars, tt.want) {
			t.Fatalf("%s: %v", tt.raw, s.terminalSidebars)
		}
		saved.TerminalSidebars = s.savedTerminalSidebars()
		bytes, err := json.Marshal(saved)
		if err != nil {
			t.Fatal(err)
		}
		var round savedState
		if err := json.Unmarshal(bytes, &round); err != nil {
			t.Fatal(err)
		}
		if round.TerminalSidebars == nil {
			t.Fatal("explicit Off lost during persistence")
		}
	}
}
func TestTerminalSidebarChoicesAndLegacySwitch(t *testing.T) {
	s := &State{}
	for _, key := range []string{"ai", "notes", "sftp"} {
		s.toggleTerminalSidebar(key)
	}
	if !reflect.DeepEqual(s.terminalSidebars, []string{"notes", "sftp"}) || !s.sftpPanelAutoOpen {
		t.Fatal(s.terminalSidebars)
	}
	s.toggleTerminalSidebar("off")
	if len(s.terminalSidebars) != 0 || s.sftpPanelAutoOpen {
		t.Fatal("Off must clear all defaults")
	}
	s.toggleTerminalSidebar("sftp")
	s.toggleTerminalSidebar("sftp")
	if len(s.terminalSidebars) != 0 || s.sftpPanelAutoOpen {
		t.Fatal("legacy switch did not toggle membership")
	}
}
func TestTerminalSidebarInitializationAndIsolation(t *testing.T) {
	s := newTestState(t, Options{})
	s.viewport = gfx.Size{Width: 800, Height: 700}
	s.ai.open = true
	old, _, _ := s.deck.Create("old")
	fresh, _, _ := s.deck.Create("new")
	s.terminalSidebars = []string{"ai", "monitor"}
	s.initializeTerminalSidebars(fresh)
	c, _ := s.deck.Context(fresh)
	if !c.MonitorOpen || s.startupAIVisibility[fresh] {
		t.Fatal("narrow window must not open two drawers")
	}
	if !s.ai.open {
		t.Fatal("new defaults changed global AI visibility")
	}
	if _, exists := s.startupAIVisibility[old]; exists {
		t.Fatal("existing workspace overwritten")
	}
	s.terminalSidebars = []string{"notes", "sftp"}
	s.initializeTerminalSidebars(fresh)
	if !s.notesPanelOpenFor(fresh) || s.notesPanelOpenFor(old) {
		t.Fatal("notes are not workspace scoped")
	}
	// Once a workspace has a context, the actual initialization entry point is
	// a no-op, even after the user edits defaults or reconnects a pane.
	c.Kind = layoutcore.ContextSFTP
	c.Open = true
	_ = s.deck.SetContext(fresh, c)
	s.terminalSidebars = []string{}
	s.initializeSFTPPanel(fresh, false)
	after, _ := s.deck.Context(fresh)
	if after != c || !s.notesPanelOpenFor(fresh) {
		t.Fatal("defaults reapplied to an existing workspace")
	}
}
func TestTerminalSidebarSettingsPicker(t *testing.T) {
	s := newTestState(t, Options{})
	s.Click(hitSettings)
	s.selectSettingsCategory(settingsFiles)
	var filesCanvas gfx.Canvas
	s.Paint(&filesCanvas, gfx.Size{Width: 1500, Height: 1200})
	if _, ok := hitByID(&filesCanvas, tree.ID(sftpHits, "settings-panel-auto-open")); ok {
		t.Fatal("legacy auto-open switch remains visible")
	}
	s.selectSettingsCategory(settingsTerminal)
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		s.setLanguage(language)
		var canvas gfx.Canvas
		s.Paint(&canvas, gfx.Size{Width: 1500, Height: 1200})
		if _, ok := hitByID(&canvas, hitTerminalSidebars); !ok {
			t.Fatal("missing terminal sidebar select")
		}
		s.Click(hitTerminalSidebars)
		s.Paint(&canvas, gfx.Size{Width: 1500, Height: 1200})
		for i := range terminalSidebarKeys {
			if _, ok := hitByID(&canvas, hitTerminalSidebarItems.At(i)); !ok {
				t.Fatalf("missing choice %d", i)
			}
		}
		s.Click(hitTerminalSidebarItems.At(1))
		if !s.terminalSidebarPicker.open {
			t.Fatal("multiselect closed after selection")
		}
		s.Click(hitTerminalSidebarItems.At(0))
		s.Click(hitTerminalSidebarsDismiss)
	}
}
