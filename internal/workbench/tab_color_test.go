package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	layoutcore "motty/internal/workspace"
)

func TestTabColorChangesOnlyTargetAndPersists(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	openHost(state)
	target := state.deck.ActiveWorkspace()
	hostColor := "#a855f7"
	state.sessions[0].host.Color = hostColor
	openEmptyTab(state)
	active := state.deck.ActiveWorkspace()
	rightClickTab(state, hitTabList.At(int(target)))
	state.Click(hitTabColors.At(4))
	if state.tabMenu.open || state.deck.ActiveWorkspace() != active || state.sessions[0].host.Color != hostColor {
		t.Fatal("changing color switched tabs, changed the connection, or left the menu open")
	}
	assertColor := func(state *State, workspace layoutcore.WorkspaceID, want string) {
		t.Helper()
		index, ok := state.workspaceTab(workspace)
		if !ok || state.tabColor(state.workspaceTabs[index]) != want {
			t.Fatalf("workspace %d color: want %q, tabs=%+v", workspace, want, state.workspaceTabs)
		}
	}
	assertColor(state, target, tabColors[4])
	assertColor(state, active, "")
	assertColor(newTestState(t, options), target, tabColors[4])
	// No colour has to override the connection's colour, or the user could not
	// clear a tab that inherited a colour.
	rightClickTab(state, hitTabList.At(int(target)))
	state.Click(hitTabColors.At(0))
	assertColor(state, target, "")
	assertColor(newTestState(t, options), target, "")
	if index, _ := state.workspaceTab(target); state.workspaceTabs[index].Color != tabColorNone {
		t.Fatal("no-color override was not stored explicitly")
	}
}

func TestTabMenuColorChoicesAreLastAndStayInsideWindow(t *testing.T) {
	state, tabs := fourTabs(t)
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 600, Height: 360}} {
		state.Paint(&gfx.Canvas{}, size)
		pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitTabList.At(int(tabs[0])), X: size.Width - 1, Y: size.Height - 1, Viewport: size})
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		lastRow, _ := menuHit(&canvas, state, hitTabMenuCloseRight)
		for index := range tabColors {
			hit, ok := hitByID(&canvas, hitTabColors.At(index))
			if !ok || hit.Role != gfx.RoleRadio || hit.Bounds.Y <= lastRow.Bounds.Y ||
				hit.Bounds.X < 0 || hit.Bounds.Y < 0 || hit.Bounds.X+hit.Bounds.Width > size.Width || hit.Bounds.Y+hit.Bounds.Height > size.Height {
				t.Fatalf("invalid footer swatch %d at size %+v: %+v, found=%v", index, size, hit, ok)
			}
		}
		state.tabMenu.open = false
	}
}

// The colour row must not decide how wide the menu is. The widest row sets the
// width, exactly as it did before the row existed; seven swatches then shrink to
// fit inside it instead of pushing it wider.
func TestTabColorSwatchesFitTheMenuTheySitIn(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		state, tabs := fourTabs(t)
		state.setLanguage(language)
		size := gfx.Size{Width: 1400, Height: 900}
		state.Paint(&gfx.Canvas{}, size)
		pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitTabList.At(int(tabs[0])), X: size.Width - 1, Y: size.Height - 1, Viewport: size})
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		menu := tabMenuBounds(&canvas)
		for index := range tabColors {
			hit, ok := hitByID(&canvas, hitTabColors.At(index))
			if !ok {
				t.Fatalf("%v: swatch %d is missing", language, index)
			}
			if hit.Bounds.X < menu.X || hit.Bounds.X+hit.Bounds.Width > menu.X+menu.Width {
				t.Fatalf("%v: swatch %d spans %.1f..%.1f outside the menu %.1f..%.1f",
					language, index, hit.Bounds.X, hit.Bounds.X+hit.Bounds.Width, menu.X, menu.X+menu.Width)
			}
			if hit.Bounds.Width < 8 {
				t.Fatalf("%v: swatch %d is %.1fpx wide, too small to click", language, index, hit.Bounds.Width)
			}
		}
		// The colour row is a content row: it has no width of its own to widen the menu with.
		state.tabMenu.open = false
	}
}

// widestTabMenuRow is the width the tab menu's labels alone ask for, measured the
// way menuRows.size does for the rows of a menu with no session.
func (state *State) widestTabMenuRow(canvas *gfx.Canvas) float32 {
	theme := state.contextMenuTheme()
	m := state.messages
	var keys [6]string
	closeKeys := state.shortcuts.chords.Chord(shortcutCloseTab).Keycaps(keys[:0])
	items := []components.ContextMenuItem{
		{Label: m.MenuRenameTab, Icon: lucideIcons[iconPencil]},
		{Label: m.MenuLockTab, Icon: lucideIcons[iconLock]},
		{Label: m.MenuDuplicateTab, Icon: lucideIcons[iconCopy]},
		{Label: m.EditHostTitle, Icon: lucideIcons[iconPencil]},
		{Label: m.MenuMoveTabToNewWindow, Icon: lucideIcons[iconExternal]},
		{Label: m.CopyAddressAndPort, Icon: lucideIcons[iconCopy]},
		{Label: m.CopyConnectionName, Icon: lucideIcons[iconCopy]},
		{Label: m.LabelCloseTab, Icon: lucideIcons[iconClose], Keys: closeKeys},
		{Label: m.MenuCloseOtherTabs, Icon: lucideIcons[iconCloseOthers]},
		{Label: m.MenuCloseTabsToRight, Icon: lucideIcons[iconCloseRight]},
	}
	var widest float32
	for _, item := range items {
		widest = max(widest, components.ContextMenuItemWidth(canvas, theme, item))
	}
	return widest
}

func TestTabColorSwatchLayoutShrinksToTheRow(t *testing.T) {
	// A 166px menu leaves this much for the swatches once the row inset is taken.
	const room = float32(166 - 2*tabColorPaletteInset)
	size, gap := tabColorSwatchLayout(room)
	width := float32(len(tabColors))*size + float32(len(tabColors)-1)*gap
	if width > room+0.01 {
		t.Fatalf("swatches need %.2fpx in a %.2fpx row", width, room)
	}
	if size > tabColorSwatchSize {
		t.Fatalf("swatch %.2fpx exceeds the %.0fpx cap", size, tabColorSwatchSize)
	}
	// Seven swatches must fill most of a 166px menu rather than shrink to dots:
	// the row is what the colour row is for.
	if size < 18 {
		t.Fatalf("swatch %.2fpx is too small for a 166px menu", size)
	}
	// A roomy menu keeps the cap; a cramped one tightens the gaps before the
	// discs get too small to hit.
	if size, gap := tabColorSwatchLayout(1000); size != tabColorSwatchSize || gap != tabColorSwatchGap {
		t.Fatalf("roomy row: got %.1f/%.1f", size, gap)
	}
	if size, gap := tabColorSwatchLayout(40); gap != 2 || size < 12 {
		t.Fatalf("cramped row: got %.1f/%.1f", size, gap)
	}
}

// nearlySame compares two painted lengths through the half-pixel a snapped edge
// may move.
func nearlySame(left, right float32) bool {
	diff := left - right
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.51
}

// Moving the pointer over the colour row must not move keyboard focus: the menu
// row keeps the current item, and the swatches stay reachable by Tab and the
// arrow keys.
func TestTabColorSwatchesKeepTheirHoverFeedback(t *testing.T) {
	state, tabs := fourTabs(t)
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&gfx.Canvas{}, size)
	pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitTabList.At(int(tabs[0])), X: size.Width - 1, Y: size.Height - 1, Viewport: size})

	var canvas gfx.Canvas
	paint := func(hovered, focused gfx.ID) gfx.ID {
		t.Helper()
		canvas.Reset()
		canvas.SetInteraction(hovered, gfx.ID{}, focused, false)
		if _, swatch := hovered.In(hitTabColors); swatch {
			canvas.SetHoveredRole(gfx.RoleRadio)
		}
		canvas.BeginFrame()
		state.Paint(&canvas, size)
		canvas.EndFrame()
		return canvas.FocusRequest()
	}
	// The first frame only establishes the hover state; the second is the frame where
	// the pointer crosses onto the swatch.
	paint(gfx.ID{}, gfx.ID{})
	if request := paint(hitTabColors.At(2), gfx.ID{}); !request.IsZero() {
		t.Fatalf("crossing onto a swatch requested focus %v", request)
	}
	if request := paint(hitTabColors.At(2), gfx.ID{}); !request.IsZero() {
		t.Fatalf("a pointer resting on a swatch requested focus %v", request)
	}
	// On hover the border becomes Ring, which is visible even without focus.
	swatch, ok := hitByID(&canvas, hitTabColors.At(2))
	if !ok {
		t.Fatal("the hovered swatch is missing")
	}
	side := min(swatch.Bounds.Width, swatch.Bounds.Height)
	border := gfx.Rect{X: swatch.Bounds.X + (swatch.Bounds.Width-side)/2, Y: swatch.Bounds.Y + (swatch.Bounds.Height-side)/2, Width: side, Height: side}
	ring := state.Theme().Ring
	found := false
	for _, rectangle := range canvas.Rectangles() {
		b := rectangle.Bounds
		if rectangle.Color == ring && nearlySame(b.X, border.X) && nearlySame(b.Y, border.Y) &&
			nearlySame(b.Width, border.Width) && nearlySame(b.Height, border.Height) {
			found = true
		}
	}
	if !found {
		t.Fatal("the hovered swatch lost its hover ring")
	}
}

// The colour has to be visible: a tab's fill mixes the chosen colour into the
// tab's own fill, and the mix amount is that colour's share of the result. At too
// low an amount all six saturated colours mix into the same grey, and users think
// the colour's opacity was turned down. This test guards both ends: the fill is far
// enough from the grey base (the colour is visible), and the ink is far enough from
// the fill (the name is still readable).
func TestColoredTabsPaintTheirColourAtFullStrength(t *testing.T) {
	type scheme struct {
		name string
		pal  *palette
		base map[string]gfx.Color
	}
	schemes := []scheme{
		{"dark", &darkPalette, map[string]gfx.Color{"idle": darkPalette.tab, "hover": darkPalette.tabHover, "active": darkPalette.tabActive}},
		{"light", &lightPalette, map[string]gfx.Color{"idle": lightPalette.tab, "hover": lightPalette.tabHover, "active": lightPalette.tabActive}},
	}
	// The minimum chroma (the largest channel spread across 0..255) and the minimum
	// distance from the grey base that each state requires. The old .08/.12/.18
	// amounts reached only 4/7/14 of chroma, far short of these lines; the lines also
	// keep a margin so a single byte of rounding cannot trip them.
	floors := map[string]struct{ chroma, fromBase float32 }{
		"idle":   {26, 33},
		"hover":  {38, 46},
		"active": {50, 54},
	}
	for _, s := range schemes {
		for state, base := range s.base {
			floor := floors[state]
			active, hovered := state == "active", state == "hover"
			for _, color := range tabColors[1:] {
				fill := tabFill(s.pal, base, color, active, hovered)
				fill8 := rgb8(fill)
				chroma := float32(max(fill8[0], fill8[1], fill8[2]) - min(fill8[0], fill8[1], fill8[2]))
				if chroma < floor.chroma {
					t.Errorf("%s %s %s: fill %v has chroma %.0f, want at least %.0f",
						s.name, state, color, fill8, chroma, floor.chroma)
				}
				base8 := rgb8(base)
				var away float64
				for i := range 3 {
					away += float64(fill8[i]-base8[i]) * float64(fill8[i]-base8[i])
				}
				if away = math.Sqrt(away); away < float64(floor.fromBase) {
					t.Errorf("%s %s %s: fill %v is only %.0f from the grey %v, want at least %.0f",
						s.name, state, color, fill8, away, base8, floor.fromBase)
				}
				// The name uses the body ink, so it must stay readable even over the deepest fill.
				if got := contrastRatio(rgb8(tabInk(s.pal, color, s.pal.muted)), fill8); got < 4.5 {
					t.Errorf("%s %s %s: name contrast %.2f on fill %v is below 4.5", s.name, state, color, got, fill8)
				}
			}
		}
	}
}

// The connection-colour palette holds a very bright yellow: at the full amount it
// pushes a tab's name down to 3.7:1. The amount then steps down once, the name is
// readable again, and the tab is still that yellow rather than grey.
func TestTabFillKeepsBrightColoursReadable(t *testing.T) {
	for name, pal := range map[string]*palette{"dark": &darkPalette, "light": &lightPalette} {
		for _, color := range hostColors[1:] {
			for _, state := range []struct {
				active, hovered bool
				base            gfx.Color
			}{{false, false, pal.tab}, {false, true, pal.tabHover}, {true, false, pal.tabActive}} {
				fill := tabFill(pal, state.base, color, state.active, state.hovered)
				if got := contrastRatio(rgb8(pal.text), rgb8(fill)); got < tabColorInkFloor {
					t.Errorf("%s %s %v: name contrast %.2f on %v is below %.1f",
						name, color, state, got, rgb8(fill), tabColorInkFloor)
				}
				if fill == state.base {
					t.Errorf("%s %s %v: the tab lost its colour instead of softening it", name, color, state)
				}
			}
		}
	}
}

// A tab without a colour stays as it was: the ink is still the quiet rest-state
// tone, and the number is still a shade lighter than the name.
func TestTabInkOnlyStrengthensForColouredTabs(t *testing.T) {
	pal := &darkPalette
	if got := tabInk(pal, "", pal.muted); got != pal.muted {
		t.Fatalf("a plain tab took the strong ink")
	}
	if got := tabInk(pal, "#df302a", pal.muted); got != pal.text {
		t.Fatalf("a coloured tab kept the muted ink")
	}
	if got := tabNumberInk(pal.text, ""); got != tabNumberColor(pal.text) {
		t.Fatalf("a plain tab's number lost its dimming")
	}
	if got := tabNumberInk(pal.text, "#df302a"); got != pal.text {
		t.Fatalf("a coloured tab's number dimmed into the colour")
	}
}

// A painted tab must really carry its colour, not just remember it in
// barTab.background. This path threads the swatch menu, persistence and painting
// together: after picking a colour, the tab rectangle's fill must be exactly that
// colour's mix, not the palette grey.
func TestColoredTabIsPaintedWithItsColour(t *testing.T) {
	size := gfx.Size{Width: 1400, Height: 900}
	for _, appearance := range []appearance{appearanceDark, appearanceLight} {
		state, tabs := fourTabs(t)
		state.setAppearance(appearance)
		target, color := tabs[1], tabColors[4]
		// Take the user's route: right-click the tab, then click the swatch.
		state.Paint(&gfx.Canvas{}, size)
		rightClickTab(state, hitTabList.At(int(target)))
		state.Paint(&gfx.Canvas{}, size)
		state.Click(hitTabColors.At(4))
		if got := state.tabColor(state.workspaceTabs[1]); got != color {
			t.Fatalf("%v: the menu stored %q, want %q", appearance, got, color)
		}
		state.Paint(&gfx.Canvas{}, size)
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		hit, ok := hitByID(&canvas, hitTabList.At(int(target)))
		if !ok {
			t.Fatal("the coloured tab is missing")
		}
		active := state.deck.ActiveWorkspace() == target
		want := tabFill(state.pal, state.pal.tab, color, active, false)
		found := false
		for _, rectangle := range canvas.Rectangles() {
			b := rectangle.Bounds
			if rectangle.Color == want && b.X > hit.Bounds.X && b.X+b.Width < hit.Bounds.X+hit.Bounds.Width &&
				b.Y >= hit.Bounds.Y && b.Y+b.Height <= hit.Bounds.Y+hit.Bounds.Height {
				found = true
			}
		}
		if !found {
			t.Fatalf("%v: no rectangle inside the tab is painted %v (its colour %s)", appearance, rgb8(want), color)
		}
	}
}

func TestTabColorRowMovesFocusWithArrowKeys(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.tabMenu.open = true
	result, handled := state.tabMenuColorKey(gfx.KeyEvent{Key: gfx.KeyRight, Target: hitTabColors.At(0)})
	if !handled || result.Focus != hitTabColors.At(1) || !result.Repaint || !result.FocusVisible {
		t.Fatalf("right arrow = %+v handled=%v, want focus on the second swatch", result, handled)
	}
	if _, handled := state.tabMenuColorKey(gfx.KeyEvent{Key: gfx.KeyEscape, Target: hitTabColors.At(0)}); handled {
		t.Fatal("a non-arrow key moved the colour row's focus")
	}
	state.tabMenu.open = false
	if _, handled := state.tabMenuColorKey(gfx.KeyEvent{Key: gfx.KeyRight, Target: hitTabColors.At(0)}); handled {
		t.Fatal("a closed menu handled the colour row's arrow key")
	}
}

// tabMenuBounds is the span of the open tab menu's rows.
func tabMenuBounds(canvas *gfx.Canvas) gfx.Rect {
	var left, right float32 = 1e9, 0
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleMenuItem {
			left, right = min(left, hit.Bounds.X), max(right, hit.Bounds.X+hit.Bounds.Width)
		}
	}
	return gfx.Rect{X: left, Width: right - left}
}
