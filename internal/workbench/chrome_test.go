package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

// windowKeyRoutes and its pointer and wheel peers deliver an event the way the
// window's Config does: the page's own elements first, then the application's
// handlers.
func windowKeyRoutes(state *State, event gfx.KeyEvent) gfx.EventResult {
	return state.WindowConfig(app.Config{OnKey: state.Key}).OnKey(event)
}

func windowPointerRoutes(state *State, event gfx.PointerEvent) gfx.EventResult {
	return state.WindowConfig(app.Config{OnPointer: state.Pointer}).OnPointer(event)
}

func windowScrollRoutes(state *State, event gfx.ScrollEvent) gfx.EventResult {
	return state.WindowConfig(app.Config{OnScroll: state.Scroll}).OnScroll(event)
}

func hitByID(canvas *gfx.Canvas, id gfx.ID) (gfx.Hit, bool) {
	for _, hit := range canvas.Hits() {
		if hit.ID == id {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// hitByLabel finds the first painted button with label: a page-owned dialog
// button is named by its place, not by an application identity. The first
// match is the dialog's, because the page declares dialogs before the section
// body is painted; a section can carry a button with the same label.
func hitByLabel(canvas *gfx.Canvas, label string) (gfx.Hit, bool) {
	return hitByLabelSnapshot(canvas.Hits(), label)
}

// hitByLabelSnapshot is hitByLabel over a snapshot's hits.
func hitByLabelSnapshot(hits []gfx.Hit, label string) (gfx.Hit, bool) {
	for _, hit := range hits {
		if hit.Label == label && hit.Role == gfx.RoleButton {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// The shell is declared by a page of its own, and that page keeps the hit
// identities the central dispatcher names by hand: the window bar above the
// navigation rail, the rail's rows under it, and the slot the sections paint
// into. A bar button never takes focus, so pressing one leaves the keyboard in
// the terminal it was over.
func TestShellPageKeepsBarAndNavigationHits(t *testing.T) {
	state := newTestState(t, testOptions(t))
	canvas, _ := paintedTexts(state)
	if state.chrome.windowPage == nil {
		t.Fatal("the shell is not declared by a page")
	}
	if state.chrome.probePage == nil {
		t.Fatal("the window bar was not measured by its page")
	}

	buttons := []struct {
		id    gfx.ID
		label string
	}{
		{hitHome, state.messages.NavHosts},
		{hitNewTab, state.messages.LabelNewTab},
		{hitWindowLocalTerminal, state.messages.LocalTerminal},
		{hitWindowSettings, state.messages.NavSettings},
	}
	for _, button := range buttons {
		hit := panelHit(t, canvas, button.id)
		if hit.Label != button.label {
			t.Errorf("%q reads %q", button.label, hit.Label)
		}
		if hit.Role != gfx.RoleButton {
			t.Errorf("%q has role %v, want a button", button.label, hit.Role)
		}
		if hit.Focusable {
			t.Errorf("%q takes focus; a bar button must leave the keyboard alone", button.label)
		}
		if hit.Bounds.Y < 0 || hit.Bounds.Y+hit.Bounds.Height > windowBarHeight {
			t.Errorf("%q is outside the window bar: %+v", button.label, hit.Bounds)
		}
	}

	rows := []struct {
		id       gfx.ID
		label    string
		selected bool
	}{
		{hitHosts, state.messages.NavHosts, true},
		{hitSettings, state.messages.NavSettings, false},
		{hitLocalTerminal, state.messages.LocalTerminal, false},
	}
	for _, row := range rows {
		hit := panelHit(t, canvas, row.id)
		if hit.Label != row.label {
			t.Errorf("%q reads %q", row.label, hit.Label)
		}
		if hit.Role != gfx.RoleMenuItem {
			t.Errorf("%q has role %v, want a menu item", row.label, hit.Role)
		}
		if hit.Selected != row.selected {
			t.Errorf("%q selected=%t, want %t", row.label, hit.Selected, row.selected)
		}
		if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > navigationWidth || hit.Bounds.Y < windowBarHeight {
			t.Errorf("%q is outside the navigation rail: %+v", row.label, hit.Bounds)
		}
	}

	slot := state.chrome.workspaceSlot.Rect()
	if slot.Width <= 0 || slot.Height <= 0 {
		t.Fatalf("the shell recorded no workspace slot: %+v", slot)
	}
	window := state.windowRect()
	if slot.X < navigationWidth {
		t.Errorf("the workspace slot overlaps the navigation rail: %+v", slot)
	}
	if slot.X+slot.Width > window.Width || slot.Y+slot.Height > window.Height {
		t.Errorf("the workspace slot spills out of the window: %+v in %+v", slot, window)
	}
}

func TestChromeHasOnlyTheControlsThatDoSomething(t *testing.T) {
	state := newTestState(t, testOptions(t))
	canvas, texts := paintedTexts(state)

	// "Session Logs" is a real page now, so its sidebar row is expected; a
	// substring check for "Logs" would fail on it, which is why it is gone from
	// this list of controls nothing answers to.
	for _, gone := range []string{"Vaults", "SFTP", "Scripts"} {
		if contains(texts, gone) {
			t.Errorf("%q is still painted", gone)
		}
	}
	// Every painted hit is one the state handles; none is a decoration waiting
	// for a feature.
	wanted := map[gfx.ID]bool{
		hitHosts: true, hitWorkspaces: true, hitKeychain: true, hitProxies: true, hitPortForwarding: true,
		hitKnownHosts: true, hitSnippets: true, hitNotes: true, hitSettings: true, hitWindowSettings: true, hitWindowLocalTerminal: true, hitSearch: true, hitConnect: true,
		hitAddHost: true, hitHostFilterAll: true, hitImportMenu: true, hitTheme: true, hitHome: true, hitNewTab: true,
		hitGitHub: true,
	}
	// A development build also declares the component gallery.
	for _, id := range devOnlyShellHits() {
		wanted[id] = true
	}
	for _, hit := range canvas.Hits() {
		// The window chrome is what the shell declares itself; everything else
		// (host rows, the palette, the assistant, the menus) is another place's.
		if hit.ID.Scope() != shellHits || hit.Block {
			continue
		}
		if !wanted[hit.ID] {
			t.Errorf("unexpected hit %d (%q)", hit.ID, hit.Label)
		}
		delete(wanted, hit.ID)
	}
	for id := range wanted {
		t.Errorf("hit %d is not painted", id)
	}
}

// The window bar has a GitHub button beside the other icon buttons: it names
// itself like the rest and opens the project's page in the browser.
func TestWindowBarGitHubButtonOpensTheRepository(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var opened []string
	state.options.OpenURL = func(url string) error { opened = append(opened, url); return nil }

	canvas, _ := paintedTexts(state)
	hit, ok := hitByID(canvas, hitGitHub)
	if !ok {
		t.Fatal("the window bar does not paint a GitHub button")
	}
	if hit.Label != state.messages.LabelGitHub || hit.Hint != state.messages.LabelGitHub {
		t.Fatalf("the button reads %q with hint %q", hit.Label, hit.Hint)
	}

	state.Click(hitGitHub)
	if len(opened) != 1 || opened[0] != aboutRepoURL {
		t.Fatalf("the button opened %v, want %q", opened, aboutRepoURL)
	}
}

func TestHostActionsAppearOnlyWhileSomethingIsChecked(t *testing.T) {
	state := newTestState(t, testOptions(t))
	actions := []gfx.ID{hitExportHosts, hitEditHost, hitCopyHost, hitMoveHostGroup, hitDeleteHost}
	canvas, _ := paintedTexts(state)
	for _, id := range actions {
		if _, ok := hitByID(canvas, id); ok {
			t.Errorf("action %d is painted with nothing checked", id)
		}
	}
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	canvas, _ = paintedTexts(state)
	for _, id := range actions {
		if _, ok := hitByID(canvas, id); !ok {
			t.Errorf("action %d is not painted with a host checked", id)
		}
	}
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitDeleteHost); ok {
		t.Error("the actions stayed after the last check was removed")
	}
}

// narrowSection paints the state with the assistant panel as wide as it goes,
// which leaves the section beside it the least room it promises: about 345px on
// a 1262-wide window, the narrowest a real window can put a section in.
func narrowSection(t *testing.T, state *State) (*gfx.Canvas, []string) {
	t.Helper()
	state.ai.open = true
	state.ai.panelWidth = 5000 // a width dragged on a wider window: it keeps what this one has
	return paintAt(state, gfx.Size{Width: 1262, Height: 900})
}

// TestTheHostsToolbarCondensesIntoIcons guards the toolbar's narrow form: a
// section squeezed by the assistant panel keeps its three actions as icons whose
// tooltips name them, rather than spilling the last one out of the card.
func TestTheHostsToolbarCondensesIntoIcons(t *testing.T) {
	state := newTestState(t, testOptions(t))
	m := state.messages
	buttons := []struct {
		id    gfx.ID
		label string
	}{{hitConnect, m.Connect}, {hitImportMenu, m.ImportMenu}, {hitAddHost, m.NewHost}}
	wide, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	for _, button := range buttons {
		if hit := panelHit(t, wide, button.id); hit.Hint != "" || hit.Bounds.Width <= toolbarIconButton {
			t.Errorf("%q is already condensed in a wide section: %+v", button.label, hit)
		}
	}
	canvas, _ := narrowSection(t, state)
	section := state.hostsBounds
	if section.Width > 360 {
		t.Fatalf("the fixture left the section %v wide, too wide to be narrow", section.Width)
	}
	for _, button := range buttons {
		hit := panelHit(t, canvas, button.id)
		if hit.Label != button.label || hit.Hint != button.label {
			t.Errorf("condensed %q keeps label %q and hint %q", button.label, hit.Label, hit.Hint)
		}
		if hit.Bounds.Width != toolbarIconButton {
			t.Errorf("condensed %q is %v wide, want %v", button.label, hit.Bounds.Width, toolbarIconButton)
		}
		if hit.Bounds.X+hit.Bounds.Width > section.X+section.Width {
			t.Errorf("condensed %q reaches out of the section: %+v", button.label, hit.Bounds)
		}
	}
	if _, ok := hitByID(canvas, hitSearch); !ok {
		t.Error("the condensed toolbar dropped the search box")
	}
}

// headerHit is the control with id nearest the top: a section paints some of its
// buttons twice, once in its header and once in its empty state.
func headerHit(t *testing.T, canvas *gfx.Canvas, id gfx.ID) gfx.Hit {
	t.Helper()
	var found gfx.Hit
	first := true
	for _, hit := range canvas.Hits() {
		if hit.ID == id && (first || hit.Bounds.Y < found.Bounds.Y) {
			found, first = hit, false
		}
	}
	if first {
		t.Fatalf("hit %d is not painted", id)
	}
	return found
}

// TestTheSectionToolbarsCondenseIntoIcons guards the same narrow form on the
// other section pages: their actions keep their icons and tooltips, and the
// search box keeps the width it works at.
func TestTheSectionToolbarsCondenseIntoIcons(t *testing.T) {
	sections := []struct {
		name string
		page uint8
		ids  []gfx.ID
	}{
		{"keys", sectionKeys, []gfx.ID{hitKeysAdd, hitKeysCopyPublic, hitKeysRemove}},
		{"proxies", sectionProxies, []gfx.ID{hitProxyAdd, hitProxyEdit, hitProxyStartStop, hitProxyRemove}},
		{"forwards", sectionForwards, []gfx.ID{hitForwardAdd, hitForwardEdit, hitForwardStartStop, hitForwardRemove}},
		{"known hosts", sectionKnownHosts, []gfx.ID{hitKnownHostsRefresh, hitKnownHostsRetrust, hitKnownHostsDelete}},
	}
	for _, section := range sections {
		state := newTestState(t, testOptions(t))
		state.activeSection = section.page
		wide, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
		for _, id := range section.ids {
			if hit := headerHit(t, wide, id); hit.Hint != "" || hit.Bounds.Width <= toolbarIconButton {
				t.Errorf("%s: %q is already condensed in a wide section: %+v", section.name, hit.Label, hit)
			}
		}
		canvas, _ := narrowSection(t, state)
		for _, id := range section.ids {
			hit := headerHit(t, canvas, id)
			if hit.Label == "" || hit.Hint != hit.Label || hit.Bounds.Width != toolbarIconButton {
				t.Errorf("%s: condensed %q keeps hint %q and width %v", section.name, hit.Label, hit.Hint, hit.Bounds.Width)
			}
		}
		if section.page == sectionKnownHosts {
			if hit := headerHit(t, canvas, hitKnownHostsSearch); hit.Bounds.Width < 100 {
				t.Errorf("the known-hosts search box collapsed to %v wide", hit.Bounds.Width)
			}
		}
		state.Close()
	}
}

// TestTheHostActionsCondenseIntoIcons guards the row of actions on a checked
// host: a narrow card keeps the five of them as icons whose tooltips name them,
// beside the count of what is checked and the view switch.
func TestTheHostActionsCondenseIntoIcons(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hostSelections = append(state.hostSelections, state.hosts[0].ID)
	state.selectedHostID = state.hosts[0].ID
	actions := []gfx.ID{hitExportHosts, hitEditHost, hitCopyHost, hitMoveHostGroup, hitDeleteHost}
	wide, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	for _, id := range actions {
		if hit := panelHit(t, wide, id); hit.Hint != "" || hit.Bounds.Width <= actionIconSize {
			t.Errorf("action %d is already condensed in a wide section: %+v", id, hit)
		}
	}
	canvas, _ := narrowSection(t, state)
	section := state.hostsBounds
	for _, id := range actions {
		hit := panelHit(t, canvas, id)
		if hit.Label == "" || hit.Hint != hit.Label || hit.Bounds.Width != actionIconSize {
			t.Errorf("condensed action %d keeps hint %q and width %v", id, hit.Hint, hit.Bounds.Width)
		}
		if hit.Bounds.X+hit.Bounds.Width > section.X+section.Width {
			t.Errorf("condensed action %d reaches out of the section: %+v", id, hit.Bounds)
		}
	}
}

// TestANarrowHostRowKeepsTheName guards the row itself: a row with no room for
// both the name and the status in words keeps the name and shows the light.
func TestANarrowHostRowKeepsTheName(t *testing.T) {
	state := newTestState(t, testOptions(t))
	_, wide := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if !hasText(wide, state.messages.HostStateIdle) {
		t.Fatal("a wide row does not say its status")
	}
	_, narrow := narrowSection(t, state)
	if hasText(narrow, state.messages.HostStateIdle) {
		t.Error("a narrow row still paints the status words beside the name")
	}
	if !hasText(narrow, "Production") {
		t.Errorf("a narrow row lost the host name: %q", narrow)
	}
}

func TestWholeCatalogExportIsInTheImportMenu(t *testing.T) {
	options := testOptions(t)
	options.SaveFile = func(app.SaveFileOptions) (string, error) { return "", app.ErrCanceled } // never a real dialog
	state := newTestState(t, options)
	state.Click(hitImportMenu)
	canvas, _ := paintedTexts(state)
	hit, ok := hitByID(canvas, hitExportHosts)
	if !ok || hit.Disabled {
		t.Fatalf("no usable export in the import menu: %+v, %v", hit, ok)
	}
	state.Click(hitExportHosts)
	if state.importMenu {
		t.Fatal("choosing it left the menu open")
	}
}

func TestThemeToggleSwitchesPaletteAndComponentTheme(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.appearance != appearanceDark || state.Theme().Background != darkPalette.workspace {
		t.Fatal("the default is not the dark palette")
	}
	darkClear := state.WindowColor()

	state.Click(hitTheme)
	if state.appearance != appearanceLight {
		t.Fatal("the toggle did not switch to light")
	}
	theme := state.Theme()
	if theme.Background != lightPalette.workspace || theme.Foreground != lightPalette.text ||
		theme.Primary != lightPalette.accent || theme.Border != lightPalette.line {
		t.Fatalf("the component theme did not follow the palette: %+v", theme)
	}
	if state.WindowColor() == darkClear {
		t.Fatal("the window color did not follow the theme")
	}
	canvas, _ := paintedTexts(state)
	theme2, _ := hitByID(canvas, hitTheme)
	if theme2.Label != state.messages.LabelThemeToDark {
		t.Fatalf("label after switching = %q", theme2.Label)
	}

	state.Click(hitTheme)
	if state.appearance != appearanceDark || state.Theme().Background != darkPalette.workspace {
		t.Fatal("the second toggle did not return to dark")
	}
}

// Text must stay readable on whatever it sits on. This is a contrast floor, not
// a design review: it catches a palette entry set to the wrong value.
func TestPalettesKeepTextReadable(t *testing.T) {
	luminance := func(c gfx.Color) float64 {
		channel := func(v float32) float64 {
			x := float64(v)
			if x <= 0.03928 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*channel(c.Red) + 0.7152*channel(c.Green) + 0.0722*channel(c.Blue)
	}
	ratio := func(a, b gfx.Color) float64 {
		la, lb := luminance(a), luminance(b)
		if la < lb {
			la, lb = lb, la
		}
		return (la + 0.05) / (lb + 0.05)
	}
	for name, pal := range map[string]*palette{"dark": &darkPalette, "light": &lightPalette} {
		for _, check := range []struct {
			what    string
			fg, bg  gfx.Color
			minimum float64
		}{
			{"text on workspace", pal.text, pal.workspace, 7},
			{"text on shell", pal.text, pal.shell, 7},
			{"text on surface", pal.text, pal.surface, 7},
			{"muted on workspace", pal.muted, pal.workspace, 4.5},
			{"muted on shell", pal.muted, pal.shell, 4.5},
			{"muted on surface", pal.muted, pal.surface, 4.5},
			{"faint on workspace", pal.faint, pal.workspace, 3},
			{"accent label on soft tint", pal.accent, pal.accentSoft, 3},
			{"label on accent button", pal.onAccent, pal.accent, 4.5},
			{"label on hot accent button", pal.onAccent, pal.accentHot, 4.5},
			{"success on workspace", pal.success, pal.workspace, 3},
			{"danger on workspace", pal.danger, pal.workspace, 3},
			{"text on selected row", pal.text, pal.surfaceHot, 7},
		} {
			if got := ratio(check.fg, check.bg); got < check.minimum {
				t.Errorf("%s: %s contrast %.2f is below %.1f", name, check.what, got, check.minimum)
			}
		}
	}
}

func TestDialogFieldsShowEachHintOnlyOnce(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	_, texts := paintedTexts(state)
	m := state.messages
	for _, placeholder := range []string{m.FieldLabelPlaceholder, m.FieldAddressPlaceholder, m.FieldUserPlaceholder, m.FieldGroupPlaceholder} {
		count := 0
		for _, value := range texts {
			if value == placeholder {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%q is painted %d times, want exactly once (as the placeholder)", placeholder, count)
		}
	}
}

func TestDialogButtonsFitTheirLabelsAndSitAtTheRightEdge(t *testing.T) {
	cases := []struct {
		name   string
		open   func(*State, *fakeConn)
		layout func(*State) []string
	}{
		{"new host", func(state *State, _ *fakeConn) { state.Click(hitAddHost) }, func(state *State) []string {
			return []string{state.messages.Cancel, state.messages.AddHost}
		}},
		{"host key", func(state *State, fake *fakeConn) {
			fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseHostKeyPrompt; s.HostKey = testHostKey })
			state.Wake()
		}, func(state *State) []string {
			m := state.messages
			return []string{m.HostKeyReject, m.HostKeyOnce, m.HostKeyTrust}
		}},
		{"password", func(state *State, fake *fakeConn) {
			fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 1 })
			state.Wake()
		}, func(state *State) []string {
			return []string{state.messages.Cancel, state.messages.Connect}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			world, options := newWorld(t)
			state := newTestState(t, options)
			fake := connect(t, state, world)
			test.open(state, fake)
			canvas, _ := paintedTexts(state)
			theme := state.Theme()
			layout := test.layout(state)

			var previousRight float32
			var last gfx.Hit
			for index, label := range layout {
				hit, ok := hitByLabel(canvas, label)
				if !ok {
					t.Fatalf("button %q is not painted", label)
				}
				fit := components.BeginButton(canvas, theme, gfx.ID{}, hit.Bounds).SizeToFit(hit.Label).BoundsWidth()
				if math.Abs(float64(hit.Bounds.Width-fit)) > 0.5 {
					t.Errorf("%q is %.1f wide, its label fits in %.1f", hit.Label, hit.Bounds.Width, fit)
				}
				if hit.Bounds.Width > canvas.TextWidth(hit.Label, theme.Metrics.FontSizeM)+40 {
					t.Errorf("%q has more than 20px of padding per side", hit.Label)
				}
				if index > 0 && math.Abs(float64(hit.Bounds.X-previousRight-theme.Metrics.GapItem)) > 0.5 {
					t.Errorf("gap before %q is %.1f, want %.1f", hit.Label, hit.Bounds.X-previousRight, theme.Metrics.GapItem)
				}
				previousRight = hit.Bounds.X + hit.Bounds.Width
				last = hit
			}
			// The last button ends where the dialog's content ends.
			var content gfx.Rect
			for _, hit := range canvas.Hits() {
				if hit.Block {
					content = hit.Bounds
				}
			}
			padding := theme.Metrics.PadSurface + theme.Metrics.GapItem
			if want := content.X + content.Width - padding; math.Abs(float64(last.Bounds.X+last.Bounds.Width-want)) > 0.5 {
				t.Errorf("last button ends at %.1f, the footer's right edge is %.1f", last.Bounds.X+last.Bounds.Width, want)
			}
		})
	}
}

func TestPaintDoesNotAllocateInEitherThemeOrWithADialogOpen(t *testing.T) {
	for _, appearance := range []appearance{appearanceDark, appearanceLight} {
		for _, open := range []struct {
			name string
			do   func(*State)
		}{
			{"hosts", func(*State) {}},
			{"new host dialog", func(state *State) { state.Click(hitAddHost) }},
			{"empty", func(state *State) { state.hosts = nil; state.selectedHostID = 0 }},
		} {
			state := newTestState(t, testOptions(t))
			state.setAppearance(appearance)
			open.do(state)
			var canvas gfx.Canvas
			size := gfx.Size{Width: 1400, Height: 900}
			state.Paint(&canvas, size)
			allocations := testing.AllocsPerRun(50, func() {
				canvas.Reset()
				state.Paint(&canvas, size)
			})
			if allocations != 0 {
				t.Errorf("%v / %s: paint allocated %v times", appearance, open.name, allocations)
			}
		}
	}
}

// Text must not change size when a field gains focus: the native editor takes
// over at the size the field declared, which is the size it painted. Both are
// the workbench's body size.
func TestFieldTextHasTheSameSizeFocusedAndUnfocused(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	state.editor.label.Insert("Production")
	canvas, _ := paintedTexts(state)

	var hit gfx.Hit
	for _, candidate := range canvas.Hits() {
		if candidate.ID == hitDialogLabel {
			hit = candidate
		}
	}
	text := hitText(canvas, &hit)
	// The field's value is painted at the native editor's text bounds. Another
	// control may show the same text (the host list can hold a host of the same
	// name), so pick the paint inside the field, not the last match.
	var painted gfx.Text
	for _, candidate := range canvas.Texts() {
		if candidate.Value == "Production" && candidate.Bounds.Y == text.Bounds.Y &&
			candidate.Bounds.X >= text.Bounds.X && candidate.Bounds.X < text.Bounds.X+text.Bounds.Width {
			painted = candidate
		}
	}
	if hit.ID.IsZero() || painted.Value == "" {
		t.Fatalf("hit=%+v painted=%+v", hit, painted)
	}
	if painted.Style.Size != metrics.FontSizeM || text.Size != painted.Style.Size {
		t.Fatalf("painted size %v, declared to the native editor %v, want both %v", painted.Style.Size, text.Size, metrics.FontSizeM)
	}
	if text.Bounds.X != painted.Bounds.X {
		t.Fatalf("native editor would start at x=%v, the painted text at %v", text.Bounds.X, painted.Bounds.X)
	}
}

// TestPageDeclaredControlsKeepTheirHitSemantics guards the controls that are now
// page elements: a button keeps its role, its accessible name and its hint, and
// a disabled one stays out of the Tab order they used to share.
func TestPageDeclaredControlsKeepTheirHitSemantics(t *testing.T) {
	state := newTestState(t, testOptions(t))
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})

	view, ok := canvasHit(canvas.Hits(), hitHostViewList)
	if !ok {
		t.Fatal("the hosts view switch is not a hit")
	}
	if view.Role != gfx.RoleButton || view.Label != state.messages.Tools.HostViewList ||
		view.Hint != state.messages.Tools.HostViewList || !view.Focusable || view.Disabled {
		t.Errorf("the view switch hit = %+v", view)
	}
	if view.Bounds.Width != 26 || view.Bounds.Height != 26 {
		t.Errorf("the view switch is %vx%v, want 26x26", view.Bounds.Width, view.Bounds.Height)
	}

	chip, ok := canvasHit(canvas.Hits(), hitHostFilterAll)
	if !ok {
		t.Fatal("the All chip is not a hit")
	}
	if chip.Role != gfx.RoleButton || chip.Label != state.messages.HostFilterAll || !chip.Selected {
		t.Errorf("the All chip hit = %+v", chip)
	}
	if want := state.filterChipWidth(canvas, state.messages.HostFilterAll, state.hostTotalDigits); chip.Bounds.Width != want {
		t.Errorf("the All chip is %v wide, its label needs %v", chip.Bounds.Width, want)
	}

	connect, ok := canvasHit(canvas.Hits(), hitConnect)
	if !ok || connect.Role != gfx.RoleButton || connect.Label != state.messages.Connect {
		t.Errorf("the Connect button hit = %+v", connect)
	}

	state.activeSection = sectionKnownHosts
	canvas, _ = paintAt(state, gfx.Size{Width: 1400, Height: 900})
	retrust, ok := canvasHit(canvas.Hits(), hitKnownHostsRetrust)
	if !ok {
		t.Fatal("the known-hosts Retrust button is not a hit")
	}
	if !retrust.Disabled || retrust.Focusable || retrust.Role != gfx.RoleButton {
		t.Errorf("a disabled Retrust hit = %+v", retrust)
	}

	state.activeSection = sectionSessionLogs
	canvas, _ = paintAt(state, gfx.Size{Width: 1400, Height: 900})
	filter, ok := canvasHit(canvas.Hits(), hitSessionLogsTime)
	if !ok {
		t.Fatal("the session-log time filter is not a hit")
	}
	if filter.Role != gfx.RoleButton || filter.Label != sessionLogFilterLabel(&state.messages.SessionLogs, sessionLogFilterTimeMenu) ||
		!filter.Focusable || filter.Expanded || !filter.Expandable {
		t.Errorf("the time filter hit = %+v", filter)
	}
}
