package workbench

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
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

// The settings search uses this anchor to reveal the scheme group wherever
// it sits in the terminal category.
func scrollToSchemeSettings(state *State) {
	state.settings.jump = state.messages.Tools.SchemeGroup
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
}

// Opening the browser must not make the settings page taller: the complete
// collection lives in a drawer over it.
func TestSchemeDrawerOverlaysThePage(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)

	scrollToSchemeSettings(state)
	var folded gfx.Canvas
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
	if _, ok := hitByID(&open, hitSchemeSearch); !ok {
		t.Error("the open drawer has no search box")
	}
	if _, ok := drawerCloseHit(&open); !ok {
		t.Error("the open drawer has no close control")
	}
	if state.scheme.body.Width <= 0 || state.scheme.body.Height <= 0 {
		t.Errorf("the drawer body is empty: %+v", state.scheme.body)
	}

	state.schemeFilter = *text.NewEditor("rose")
	var filtered gfx.Canvas
	state.Paint(&filtered, gfx.Size{Width: 1400, Height: 900})
	if state.scheme.filter != "rose" {
		t.Errorf("drawer filter = %q", state.scheme.filter)
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

	// The drawer's own close control dismisses it in the paint's event pass, as a
	// press beside it does: the page reads the click the drawer's Dialog reports.
	closeHit, ok := drawerCloseHit(&filtered)
	if !ok {
		t.Fatal("the drawer lost its close control")
	}
	clickSettingsChoiceHit(t, state, closeHit)
	if state.schemeBrowserOpen {
		t.Error("the close control did not close the drawer")
	}
}

// drawerCloseHit finds the Drawer's own close button, which the component names.
func drawerCloseHit(canvas *gfx.Canvas) (gfx.Hit, bool) {
	for _, hit := range canvas.Hits() {
		if hit.Label == "Close drawer" {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// drawerHit finds a card of the drawer by id and reports whether it lies in the
// drawer body: the wheel test scrolls the drawer, not the settings page behind
// it.
func drawerHit(snapshot app.Snapshot, body gfx.Rect, id gfx.ID) (gfx.Hit, bool) {
	for _, hit := range snapshot.Hits {
		if hit.ID == id && hit.Bounds.X >= body.X && hit.Bounds.X < body.X+body.Width &&
			hit.Bounds.Y >= body.Y && hit.Bounds.Y < body.Y+body.Height {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// The drawer is a Drawer of the settings page: its browse control opens it in a
// real event pass, clicking its search field gives the platform its editor,
// typing filters the collection, and a card is chosen by a real press.
func TestSchemeDrawerPageTakesPointerAndTyping(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionSettings
	state.selectSettingsCategory(settingsTerminal)
	scrollToSchemeSettings(state)
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "schemes", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	// Press the folded section's browse control in a real event pass.
	browse, ok := hitByIDInSnapshot(view.Snapshot(), hitSchemeBrowseAll)
	if !ok {
		t.Fatalf("the folded section has no browse control (section %d category %d)",
			state.activeSection, state.settingsCategory)
	}
	if err := view.Click(browse.Bounds.X+browse.Bounds.Width/2, browse.Bounds.Y+browse.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if !state.schemeBrowserOpen {
		t.Fatal("the browse control did not open the drawer")
	}

	// The wheel over the drawer scrolls its page and leaves the settings page
	// behind it where it was.
	body := state.scheme.body
	scrollIndex, ok := state.schemeIndexByID("solarized-dark")
	if !ok {
		t.Fatal("the built-in Solarized Dark card is missing")
	}
	before, ok := drawerHit(view.Snapshot(), body, schemeDrawerHit(scrollIndex))
	if !ok {
		t.Fatal("the drawer's built-in cards have no hits")
	}
	settingsScroll := state.settingsScroll
	if err := view.Driver().Scroll(gfx.ScrollEvent{X: body.X + body.Width/2, Y: body.Y + body.Height/2, DeltaY: 120, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(30 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if state.settingsScroll != settingsScroll {
		t.Fatalf("the wheel moved the settings page from %v to %v behind the drawer", settingsScroll, state.settingsScroll)
	}
	after, ok := drawerHit(view.Snapshot(), body, schemeDrawerHit(scrollIndex))
	if !ok || after.Bounds.Y >= before.Bounds.Y {
		t.Fatalf("the drawer page did not scroll to reveal content below: %v -> %v (present %t)", before.Bounds.Y, after.Bounds.Y, ok)
	}

	search, ok := hitByIDInSnapshot(view.Snapshot(), hitSchemeSearch)
	if !ok {
		t.Fatal("the drawer page has no search field")
	}
	if err := view.Click(search.Bounds.X+search.Bounds.Width/2, search.Bounds.Y+search.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(20 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if focused := view.Snapshot().Focused; focused != hitSchemeSearch {
		t.Fatalf("focus after pressing the drawer's search field = %v, want %v", focused, hitSchemeSearch)
	}
	if err := view.Type("rose"); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := state.schemeFilter.String(); got != "rose" {
		t.Fatalf("the search editor holds %q, want rose", got)
	}
	if got := state.scheme.filter; got != "rose" {
		t.Fatalf("the page filtered by %q, want rose", got)
	}

	matches := state.schemesMatching("rose")
	if len(matches) == 0 {
		t.Fatal("no scheme matches rose")
	}
	index := matches[0]
	snapshot := view.Snapshot()
	var card gfx.Hit
	found := false
	for _, hit := range snapshot.Hits {
		if hit.ID == schemeDrawerHit(index) && hit.Bounds.X >= state.scheme.body.X && hit.Bounds.X < state.scheme.body.X+state.scheme.body.Width &&
			hit.Bounds.Y >= state.scheme.body.Y && hit.Bounds.Y < state.scheme.body.Y+state.scheme.body.Height {
			card, found = hit, true
			break
		}
	}
	if !found {
		t.Fatalf("the filtered card %d has no hit in the drawer body", index)
	}
	if err := view.Click(card.Bounds.X+card.Bounds.Width/2, card.Bounds.Y+card.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if id, _, _ := state.schemeAt(index); state.activeScheme() != id {
		t.Fatalf("the chosen scheme = %q, want %q", state.activeScheme(), id)
	}
}
