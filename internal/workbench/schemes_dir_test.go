package workbench

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/vivaui/viva/ui"
)

const (
	darkSchemeSample = `! A test scheme
#define bg #1e1f29
*.foreground:  #f8f8f2
*.background:  bg
*.cursorColor: #bbbbbb
*.color0:      #000000
*.color1:      #ff5555
*.color2:      #50fa7b
*.color3:      #f1fa8c
*.color4:      #bd93f9
*.color5:      #ff79c6
*.color6:      #8be9fd
*.color7:      #f8f8f2
*.color8:      #6272a4
*.color9:      #ff6e6e
*.color10:     #69ff94
*.color11:     #ffffa5
*.color12:     #d6acff
*.color13:     #ff92df
*.color14:     #a4ffff
*.color15:     #ffffff
`
	lightSchemeSample = `*.foreground:  #657b83
*.background:  #fdf6e3
*.color0:      #073642
*.color1:      #dc322f
*.color2:      #859900
*.color3:      #b58900
*.color4:      #268bd2
*.color5:      #d33682
*.color6:      #2aa198
*.color7:      #eee8d5
*.color8:      #586e75
*.color9:      #cb4b16
*.color10:     #6b8100
*.color11:     #9a7300
*.color12:     #1f77b8
*.color13:     #b02a6e
*.color14:     #1f8a82
*.color15:     #93a1a1
`
)

func TestParsingAnXResourcesScheme(t *testing.T) {
	scheme, err := parseXResources([]byte(darkSchemeSample))
	if err != nil {
		t.Fatal(err)
	}
	if scheme.background != [3]uint8{0x1e, 0x1f, 0x29} {
		t.Errorf("background = %v", scheme.background)
	}
	if scheme.foreground != [3]uint8{0xf8, 0xf8, 0xf2} {
		t.Errorf("foreground = %v", scheme.foreground)
	}
	if scheme.cursor != [3]uint8{0xbb, 0xbb, 0xbb} {
		t.Errorf("cursor = %v", scheme.cursor)
	}
	if scheme.ansi[1] != [3]uint8{0xff, 0x55, 0x55} || scheme.ansi[15] != [3]uint8{0xff, 0xff, 0xff} {
		t.Errorf("ansi = %v", scheme.ansi)
	}
}

func TestASchemeWithoutACursorColorUsesItsForeground(t *testing.T) {
	scheme, err := parseXResources([]byte(lightSchemeSample))
	if err != nil {
		t.Fatal(err)
	}
	if scheme.cursor != scheme.foreground {
		t.Errorf("cursor = %v, foreground %v", scheme.cursor, scheme.foreground)
	}
}

func TestAnIncompleteSchemeIsRefused(t *testing.T) {
	for name, text := range map[string]string{
		"no colors":     "*.foreground: #ffffff\n*.background: #000000\n",
		"no background": "*.foreground: #ffffff\n*.color0: #000000\n",
		"no foreground": "*.background: #000000\n*.color0: #000000\n",
		"a short color": "*.foreground: #ffffff\n*.background: #000000\n*.color7: #f\n",
		"a named color": "*.foreground: red\n*.background: #000000\n*.color0: #000000\n",
	} {
		if _, err := parseXResources([]byte(text)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestHexColorsAtEveryWidthParse(t *testing.T) {
	for value, want := range map[string][3]uint8{
		"#000":          {0, 0, 0},
		"#fff":          {255, 255, 255},
		"#f80":          {255, 136, 0},
		"#1e1f29":       {0x1e, 0x1f, 0x29},
		"#fff000":       {255, 240, 0},
		"#ffff0000ffff": {255, 0, 255},
	} {
		got, err := parseHexColor(value)
		if err != nil || got != want {
			t.Errorf("%s = %v, %v; want %v", value, got, err, want)
		}
	}
}

// TestEveryBundledSchemeParses keeps the collection the release unpacks honest:
// a file that stops parsing fails here instead of going missing on a user.
func TestEveryBundledSchemeParses(t *testing.T) {
	entries, err := fs.ReadDir(bundledSchemes, "schemes")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 150 {
		t.Fatalf("only %d schemes are bundled", len(entries))
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		data, err := fs.ReadFile(bundledSchemes, "schemes/"+entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseXResources(data); err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
		}
		name := schemeFileName(entry.Name())
		if name == "" || seen[name] {
			t.Errorf("scheme %q is empty or duplicated", name)
		}
		seen[name] = true
	}
}

func TestTheSchemesFolderJoinsTheBuiltins(t *testing.T) {
	_, options := newWorld(t)
	dir := filepath.Join(filepath.Dir(options.StatePath), "schemes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"Night": darkSchemeSample, "Day": lightSchemeSample} {
		if err := os.WriteFile(filepath.Join(dir, name+".xresources"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := newTestState(t, options)
	if len(state.dirSchemes) != 2 || state.dirSchemes[0].name != "Day" || state.dirSchemes[1].name != "Night" {
		t.Fatalf("schemes = %q", state.dirSchemes)
	}

	// The first card after the built-ins is Day, and choosing it wears it.
	state.Click(hitSettings)
	state.Click(hitSettingsSchemeList.At(schemeCount))
	if state.activeScheme() != "dir:Day" {
		t.Fatalf("scheme = %q", state.activeScheme())
	}
	if state.termPal.terminal == state.pal.terminal {
		t.Error("the terminal did not change")
	}

	restarted := newTestState(t, options)
	if restarted.activeScheme() != "dir:Day" {
		t.Fatalf("restored scheme = %q", restarted.activeScheme())
	}

	// A file that is gone leaves the choice to fall back to Default.
	if err := os.Remove(filepath.Join(dir, "Day.xresources")); err != nil {
		t.Fatal(err)
	}
	gone := newTestState(t, options)
	if gone.activeScheme() != defaultSchemeID {
		t.Fatalf("scheme without its file = %q", gone.activeScheme())
	}
}

func TestTheBundledSchemesSeedOnce(t *testing.T) {
	dir := t.TempDir()
	seeded, err := SeedSchemesDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "schemes"))
	if err != nil {
		t.Fatal(err)
	}
	bundled, err := fs.ReadDir(bundledSchemes, "schemes")
	if err != nil {
		t.Fatal(err)
	}
	if seeded != len(bundled) || len(entries) != len(bundled) {
		t.Fatalf("%d unpacked (%d files), %d bundled", seeded, len(entries), len(bundled))
	}

	// From then on the directory is the user's: what they remove stays removed.
	if err := os.Remove(filepath.Join(dir, "schemes", "Dracula.xresources")); err != nil {
		t.Fatal(err)
	}
	if seeded, err := SeedSchemesDir(dir); err != nil || seeded != 0 {
		t.Fatalf("seeding again unpacked %d schemes, %v", seeded, err)
	}
	entries, err = os.ReadDir(filepath.Join(dir, "schemes"))
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(entries, func(entry os.DirEntry) bool { return entry.Name() == "Dracula.xresources" }) {
		t.Error("seeding again brought the removed scheme back")
	}
}

// TestAnEmptySchemesFolderIsFilled covers a folder made by hand before the
// first run: it holds nothing of the user's, so the collection still unpacks.
func TestAnEmptySchemesFolderIsFilled(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "schemes"), 0o755); err != nil {
		t.Fatal(err)
	}
	seeded, err := SeedSchemesDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	bundled, err := fs.ReadDir(bundledSchemes, "schemes")
	if err != nil {
		t.Fatal(err)
	}
	if seeded != len(bundled) {
		t.Fatalf("%d unpacked, %d bundled", seeded, len(bundled))
	}
	entries, err := os.ReadDir(filepath.Join(dir, "schemes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(bundled) {
		t.Fatalf("%d files unpacked, %d bundled", len(entries), len(bundled))
	}
}

// TestTheSettingsPageWearsTheWholeCollection unpacks the bundled collection and
// opens the settings on it: about two hundred cards declared in one frame is
// the section's real working weight.
func TestTheSettingsPageWearsTheWholeCollection(t *testing.T) {
	_, options := newWorld(t)
	if _, err := SeedSchemesDir(filepath.Dir(options.StatePath)); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	bundled, err := fs.ReadDir(bundledSchemes, "schemes")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.dirSchemes) != len(bundled) {
		t.Fatalf("%d schemes loaded, %d bundled", len(state.dirSchemes), len(bundled))
	}
	dark, light := state.dirSchemeIndices()
	if len(dark)+len(light) != len(bundled) {
		t.Fatalf("the groups cover %d of %d", len(dark)+len(light), len(bundled))
	}
	state.Click(hitSettings)
	state.Click(hitSettingsSchemeList.At(schemeCount + 40))
	if state.activeScheme() != state.dirSchemes[40].id {
		t.Fatalf("scheme = %q", state.activeScheme())
	}
	canvas := &ui.Canvas{}
	canvas.BeginFrame()
	state.Paint(canvas, ui.Size{Width: 1400, Height: 900})
	canvas.EndFrame()
}

// TestASchemeGroupShowsItsFirstFewAndSpreadsTheRest keeps the long groups
// short: a group starts folded, and its control spreads and folds it.
func TestASchemeGroupShowsItsFirstFewAndSpreadsTheRest(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for index := 0; index < schemeGroupPreview+5; index++ {
		state.dirSchemes = append(state.dirSchemes, colorScheme{
			id:         fmt.Sprintf("dir:D%02d", index),
			name:       fmt.Sprintf("D%02d", index),
			background: [3]uint8{20, 20, 20},
		})
	}
	state.dirSchemeIndex = map[string]int{}
	for index := range state.dirSchemes {
		state.dirSchemeIndex[state.dirSchemes[index].id] = index
	}
	dark, _ := state.dirSchemeIndices()
	if len(dark) != schemeGroupPreview+5 {
		t.Fatalf("dark group = %d cards", len(dark))
	}
	if state.schemesExpandedDark {
		t.Fatal("a group starts folded")
	}
	state.Click(hitSchemeShowAllDark)
	if !state.schemesExpandedDark {
		t.Fatal("the control did not spread the group")
	}
	state.Click(hitSchemeShowAllDark)
	if state.schemesExpandedDark {
		t.Fatal("the control did not fold the group")
	}

	// The section paints in both fold states.
	state.Click(hitSettings)
	canvas := &ui.Canvas{}
	canvas.BeginFrame()
	state.Paint(canvas, ui.Size{Width: 1400, Height: 900})
	canvas.EndFrame()
	state.Click(hitSchemeShowAllDark)
	canvas.BeginFrame()
	state.Paint(canvas, ui.Size{Width: 1400, Height: 900})
	canvas.EndFrame()
}

func TestTheSchemeSearchFiltersAndGroupsTheCards(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.dirSchemes = []colorScheme{
		{id: "dir:Solarized Light", name: "Solarized Light", background: [3]uint8{0xfd, 0xf6, 0xe3}},
		{id: "dir:Gruvbox Dark", name: "Gruvbox Dark", background: [3]uint8{0x28, 0x28, 0x28}},
	}
	base := len(colorSchemes) + 1
	dark, light := state.dirSchemeIndices()
	if !slices.Equal(dark, []int{base + 1}) || !slices.Equal(light, []int{base}) {
		t.Errorf("groups = dark %v, light %v", dark, light)
	}

	matches := state.schemesMatching("solar")
	want := []int{item(schemeCard(t, "solarized-dark")), item(schemeCard(t, "solarized-light")), base} // the built-ins, then the directory's
	if !slices.Equal(matches, want) {
		t.Errorf("matches = %v, want %v", matches, want)
	}
	if matches := state.schemesMatching("no such scheme"); len(matches) != 0 {
		t.Errorf("matches = %v", matches)
	}
}
