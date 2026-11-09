package workbench

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

// schemaFixture is a minimal valid X resources scheme; the user directory
// accepts the same form as the embedded collection.
const schemeFixture = `#define bg #1a1b26
*.background: bg
*.foreground: #c0caf5
*.cursorColor: #7aa2f7
*.color0: #15161e
*.color1: #f7768e
*.color2: #9ece6a
*.color3: #e0af68
*.color4: #7aa2f7
*.color5: #bb9af7
*.color6: #7dcfff
*.color7: #a9b1d6
*.color8: #414868
*.color9: #ff899d
*.color10: #b0e07c
*.color11: #f0bd7a
*.color12: #8db3ff
*.color13: #caabff
*.color14: #8fdbff
*.color15: #c0caf5
`

func writeScheme(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The collection ships in the binary. Every file there must parse, and opening
// a fresh profile must not unpack anything into the user's folder.
func TestCommunitySchemesAreEmbeddedAndNotSeeded(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)

	files := 0
	err := fs.WalkDir(bundledSchemes, "schemes", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), schemeFileExt) {
			return nil
		}
		files++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 || len(communitySchemes) != files {
		t.Fatalf("embedded schemes: %d files, %d parsed", files, len(communitySchemes))
	}
	for _, scheme := range communitySchemes {
		if !strings.HasPrefix(scheme.id, communitySchemePrefix) || scheme.name == "" {
			t.Errorf("bad embedded scheme id %q name %q", scheme.id, scheme.name)
		}
	}
	if _, err := os.Stat(state.schemesDir()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a fresh profile has a schemes directory: %v", err)
	}
	if len(state.dirSchemes) != 0 {
		t.Errorf("a fresh profile loaded %d user schemes", len(state.dirSchemes))
	}
}

// The user folder only adds the user's own files, after the embedded
// collection, and cannot shadow a built-in or community name.
func TestUserSchemesJoinTheListWithoutShadowing(t *testing.T) {
	options := testOptions(t)
	dir := filepath.Join(filepath.Dir(options.StatePath), "schemes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeScheme(t, dir, "Mine.xresources", schemeFixture)
	writeScheme(t, dir, "Dracula.xresources", schemeFixture)
	writeScheme(t, dir, "Rose Pine Moon.xresources", schemeFixture)
	writeScheme(t, dir, "broken.xresources", "! no colors here\n")

	state := newTestState(t, options)
	if len(state.dirSchemes) != 1 {
		t.Fatalf("user schemes = %d, want only Mine", len(state.dirSchemes))
	}
	user := state.dirSchemes[0]
	if user.name != "Mine" || user.id != dirSchemePrefix+"Mine" {
		t.Fatalf("user scheme = %q %q", user.id, user.name)
	}
	base := schemeCount + len(communitySchemes)
	if got, name, _ := state.schemeAt(base); got != user.id || name != "Mine" {
		t.Errorf("first user card = %q %q", got, name)
	}
	if got := state.schemeTotal(); got != base+1 {
		t.Errorf("scheme total = %d, want %d", got, base+1)
	}
	if !state.setTerminalScheme(user.id) {
		t.Fatal("a user scheme cannot be selected")
	}
	if state.termPal.terminal != gfx.RGB(0x1a, 0x1b, 0x26) {
		t.Errorf("the user scheme did not reach the terminal: %v", state.termPal.terminal)
	}
}

// The folded section is exactly two rows of four cards, and one of them is the
// curated community pick rather than a third built-in row.
func TestSchemeQuickPicksAreTwoRows(t *testing.T) {
	state := newTestState(t, testOptions(t))
	picks := state.schemeQuickPicks()
	if len(picks) != schemeColumns*2 {
		t.Fatalf("quick picks = %d, want %d", len(picks), schemeColumns*2)
	}
	if id, _, _ := state.schemeAt(picks[0]); id != defaultSchemeID {
		t.Errorf("first pick = %q, want Default", id)
	}
	found := false
	for _, index := range picks {
		if id, _, _ := state.schemeAt(index); id == communitySchemePrefix+"Rose Pine Moon" {
			found = true
		}
	}
	if !found {
		t.Error("the curated community pick is not in the quick picks")
	}
}

// Opening the browser must not make the settings page taller: the complete
// collection lives in a drawer over it.
func TestSchemeDrawerOverlaysThePage(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)

	var folded gfx.Canvas
	state.Paint(&folded, gfx.Size{Width: 1400, Height: 900})
	// The scheme section is at the bottom of the terminal category; scroll to
	// it the way someone looking for it would.
	state.settingsScroll = max(state.settingsContent-900, 0)
	folded = gfx.Canvas{}
	state.Paint(&folded, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&folded, hitSchemeBrowseAll); !ok {
		t.Fatalf("the folded section has no control for the complete collection (section %d category %d, %d hits)",
			state.activeSection, state.settingsCategory, len(folded.Hits()))
	}
	height := state.settingsContent

	state.Click(hitSchemeBrowseAll)
	if !state.schemeBrowserOpen {
		t.Fatal("the browse control did not open the drawer")
	}
	var open gfx.Canvas
	state.Paint(&open, gfx.Size{Width: 1400, Height: 900})
	if state.settingsContent != height {
		t.Errorf("the drawer grew the page from %v to %v", height, state.settingsContent)
	}
	if _, ok := hitByID(&open, hitSchemeDrawerClose); !ok {
		t.Error("the open drawer has no close control")
	}
	if _, ok := hitByID(&open, hitSchemeSearch); !ok {
		t.Error("the open drawer has no search box")
	}
	if state.schemeDrawerBody.Width <= 0 || state.schemeDrawerBody.Height <= 0 {
		t.Errorf("the drawer body is empty: %+v", state.schemeDrawerBody)
	}

	state.schemeFilter = *text.NewEditor("rose")
	var filtered gfx.Canvas
	state.Paint(&filtered, gfx.Size{Width: 1400, Height: 900})
	if state.schemeDrawerFilter != "rose" {
		t.Errorf("drawer filter = %q", state.schemeDrawerFilter)
	}
	matches := state.schemesMatching("rose")
	if len(matches) == 0 {
		t.Error("no scheme matches rose")
	}
	for _, index := range matches {
		if _, name, _ := state.schemeAt(index); !strings.Contains(strings.ToLower(name), "rose") {
			t.Errorf("%q matched rose", name)
		}
	}

	state.Click(hitSchemeDrawerClose)
	if state.schemeBrowserOpen {
		t.Error("the close control did not close the drawer")
	}
}
