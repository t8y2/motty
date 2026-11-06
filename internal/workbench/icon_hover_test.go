package workbench

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/ai/llm"
)

// hoverFrame paints one frame at at with the pointer on hovered and returns the
// transform the toolbar's glyph is drawn with.
func hoverFrame(canvas *ui.Canvas, state *State, id ui.ID, icon uint8, at time.Duration, hovered bool, disabled bool) ui.IconTransform {
	canvas.SetClock(func() time.Duration { return at })
	if hovered {
		canvas.SetInteraction(id, ui.ID{}, ui.ID{}, false)
	} else {
		canvas.SetInteraction(ui.ID{}, ui.ID{}, ui.ID{}, false)
	}
	canvas.BeginFrame()
	transform := state.iconHover(canvas, id, icon, disabled)
	canvas.EndFrame()
	return transform
}

func TestToolbarHoverWaitsForDwellThenPlaysOnce(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var canvas ui.Canvas
	at := time.Duration(0)
	canvas.SetClock(func() time.Duration { return at })
	canvas.SetInteraction(hitWindowSettings, ui.ID{}, ui.ID{}, false)

	canvas.BeginFrame()
	if got := state.iconHover(&canvas, hitWindowSettings, iconSettings, false); !iconStill(got) {
		t.Fatalf("before the dwell the gear paints %+v, want it still", got)
	}
	canvas.EndFrame()
	if _, ok := canvas.NextFrame(); !ok {
		t.Fatal("the dwell did not ask for the frame that starts the gesture")
	}

	at = iconHoverDwell
	canvas.SetInteraction(hitWindowSettings, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	if got := state.iconHover(&canvas, hitWindowSettings, iconSettings, false); !iconStill(got) {
		t.Fatalf("the first frame of the gesture paints %+v, want the still gear", got)
	}
	canvas.EndFrame()

	// The gear is part way through the same turn as its click gesture, and the
	// glyph is asking for the next frame.
	at = iconHoverDwell + 150*time.Millisecond
	canvas.SetInteraction(hitWindowSettings, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	mid := state.iconHover(&canvas, hitWindowSettings, iconSettings, false)
	canvas.EndFrame()
	motion, _ := iconMotionFor(iconSettings)
	if iconStill(mid) || mid.Rotate <= 0 || mid.Rotate >= float32(math.Pi) {
		t.Fatalf("part way the gear paints %+v, want the shared turn", mid)
	}
	if !canvas.Animating() {
		t.Fatal("the gesture in flight does not ask for the next frame")
	}

	at = iconHoverDwell + motion.span()
	canvas.SetInteraction(hitWindowSettings, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	last := state.iconHover(&canvas, hitWindowSettings, iconSettings, false)
	canvas.EndFrame()
	if !iconStill(last) {
		t.Fatalf("the end of the gesture paints %+v, want the still gear", last)
	}
	if canvas.Animating() {
		t.Fatal("a finished gesture still asks for frames")
	}
}

func TestToolbarHoverFinishesAfterThePointerLeaves(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var canvas ui.Canvas
	at := time.Duration(0)
	canvas.SetClock(func() time.Duration { return at })
	canvas.SetInteraction(hitCommandPalette, ui.ID{}, ui.ID{}, false)

	canvas.BeginFrame()
	state.iconHover(&canvas, hitCommandPalette, iconSearch, false)
	canvas.EndFrame()
	at = iconHoverDwell
	canvas.SetInteraction(hitCommandPalette, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	state.iconHover(&canvas, hitCommandPalette, iconSearch, false)
	canvas.EndFrame()

	// The pointer leaves after the gesture has started. The lens keeps moving
	// until it has finished, instead of snapping back from part way.
	at = iconHoverDwell + 150*time.Millisecond
	canvas.SetInteraction(ui.ID{}, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	moving := state.iconHover(&canvas, hitCommandPalette, iconSearch, false)
	canvas.EndFrame()
	if iconStill(moving) {
		t.Fatal("the gesture stopped as soon as the pointer left")
	}

	motion, _ := iconMotionFor(iconSearch)
	at = iconHoverDwell + motion.span()
	canvas.SetInteraction(ui.ID{}, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	last := state.iconHover(&canvas, hitCommandPalette, iconSearch, false)
	canvas.EndFrame()
	if !iconStill(last) {
		t.Fatalf("the gesture left the glyph at %+v, want it still", last)
	}
}

func TestToolbarHoverReplaysOnlyAfterLeaving(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var canvas ui.Canvas
	at := time.Duration(0)
	canvas.SetClock(func() time.Duration { return at })
	canvas.SetInteraction(hitTheme, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	state.iconHover(&canvas, hitTheme, iconMoon, false)
	canvas.EndFrame()
	at = iconHoverDwell
	canvas.SetInteraction(hitTheme, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	state.iconHover(&canvas, hitTheme, iconMoon, false)
	canvas.EndFrame()
	motion, _ := iconMotionFor(iconMoon)
	at = iconHoverDwell + motion.span()
	canvas.SetInteraction(hitTheme, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	state.iconHover(&canvas, hitTheme, iconMoon, false)
	canvas.EndFrame()

	// A pointer that stays put does not loop the gesture.
	at += time.Second
	canvas.SetInteraction(hitTheme, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	if got := state.iconHover(&canvas, hitTheme, iconMoon, false); !iconStill(got) {
		t.Fatalf("a held pointer replays %+v, want the still moon", got)
	}
	canvas.EndFrame()

	// Leaving and returning arms it again.
	at += 10 * time.Millisecond
	canvas.SetInteraction(ui.ID{}, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	state.iconHover(&canvas, hitTheme, iconMoon, false)
	canvas.EndFrame()
	at += 10 * time.Millisecond
	canvas.SetInteraction(hitTheme, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	state.iconHover(&canvas, hitTheme, iconMoon, false)
	canvas.EndFrame()
	at += iconHoverDwell
	canvas.SetInteraction(hitTheme, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	state.iconHover(&canvas, hitTheme, iconMoon, false)
	canvas.EndFrame()
	at += motion.span() / 2
	canvas.SetInteraction(hitTheme, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	if got := state.iconHover(&canvas, hitTheme, iconMoon, false); iconStill(got) {
		t.Fatal("the second hover did not start the gesture again")
	}
	canvas.EndFrame()
}

func TestToolbarHoverIsStillForDisabledControlsAndReducedMotion(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var canvas ui.Canvas
	at := time.Duration(0)
	canvas.SetClock(func() time.Duration { return at })

	canvas.SetInteraction(hitWindowSettings, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	if got := state.iconHover(&canvas, hitWindowSettings, iconSettings, true); !iconStill(got) {
		t.Fatalf("a disabled control paints %+v, want it still", got)
	}
	canvas.EndFrame()
	if _, ok := canvas.NextFrame(); ok {
		t.Fatal("a disabled control asked for an animation frame")
	}

	state = newTestState(t, freshOptions(t))
	canvas = ui.Canvas{}
	canvas.SetClock(func() time.Duration { return at })
	canvas.SetReduceMotion(true)
	canvas.SetInteraction(hitWindowSettings, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	if got := state.iconHover(&canvas, hitWindowSettings, iconSettings, false); !iconStill(got) {
		t.Fatalf("reduced motion paints %+v, want it still", got)
	}
	canvas.EndFrame()
	if canvas.Animating() {
		t.Fatal("reduced motion asked for an animation")
	}
}

// A reveal control keeps one hit identity while its drawing changes between
// Eye and EyeOff. The two drawings have different gestures, so changing the
// drawing must start a fresh timeline rather than continuing the old one.
func TestIconHoverRestartsWhenTheDrawingChanges(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var canvas ui.Canvas
	at := time.Duration(0)
	id := ui.Item(4242)
	canvas.SetClock(func() time.Duration { return at })
	canvas.SetInteraction(id, ui.ID{}, ui.ID{}, false)

	paint := func(icon uint8, hovered bool) ui.IconTransform {
		t.Helper()
		canvas.SetInteraction(hoveredID(hovered, id), ui.ID{}, ui.ID{}, false)
		canvas.BeginFrame()
		got := state.iconHover(&canvas, id, icon, false)
		canvas.EndFrame()
		return got
	}
	paint(iconEye, true)
	at = iconHoverDwell
	paint(iconEye, true)
	at = iconHoverDwell + 300*time.Millisecond
	eye := paint(iconEye, true)
	if iconStill(eye) {
		t.Fatal("the first drawing never started")
	}

	eyeOff := paint(iconEyeOff, true)
	if !iconStill(eyeOff) {
		t.Fatalf("the new drawing continued the old timeline with %+v", eyeOff)
	}
	hover := state.iconHovers[id]
	if hover.group != iconGlyphEyeOff {
		t.Fatalf("the control holds group %d, want EyeOff", hover.group)
	}

	at += iconHoverDwell
	if got := paint(iconEyeOff, true); !iconStill(got) {
		t.Fatalf("the new drawing started before its dwell with %+v", got)
	}
	at += 150 * time.Millisecond
	if got := paint(iconEyeOff, true); iconStill(got) {
		t.Fatal("the new drawing did not start its own gesture")
	}
}

func hoveredID(hovered bool, id ui.ID) ui.ID {
	if hovered {
		return id
	}
	return ui.ID{}
}

func TestIconHoverAndClickShareOneGesture(t *testing.T) {
	for group, motion := range iconGlyphs {
		kinds := iconGlyphKinds(group)
		if len(kinds) == 0 {
			t.Errorf("glyph group %d has a gesture but no icon", group)
			continue
		}
		for _, icon := range kinds {
			got, ok := iconMotionFor(icon)
			if !ok || !sameMotion(got, motion) {
				t.Errorf("icon %d has a gesture that differs from its shared glyph group %d", icon, group)
			}
		}
	}
}

func TestHoverAndClickReadTheSameTimeline(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var canvas ui.Canvas
	at := time.Duration(0)
	canvas.SetClock(func() time.Duration { return at })

	state.Click(hitSettings)
	iconFrame(&canvas, state, hitSettings, iconSettings, 0)
	clickMid := iconFrame(&canvas, state, hitSettings, iconSettings, 300*time.Millisecond)
	if iconStill(clickMid) {
		t.Fatal("the click gesture did not start")
	}

	state = newTestState(t, freshOptions(t))
	canvas = ui.Canvas{}
	canvas.SetClock(func() time.Duration { return at })
	canvas.SetInteraction(hitWindowSettings, ui.ID{}, ui.ID{}, false)
	at = 0
	canvas.BeginFrame()
	state.iconHover(&canvas, hitWindowSettings, iconSettings, false)
	canvas.EndFrame()
	at = iconHoverDwell
	canvas.SetInteraction(hitWindowSettings, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	state.iconHover(&canvas, hitWindowSettings, iconSettings, false)
	canvas.EndFrame()
	at = iconHoverDwell + 300*time.Millisecond
	canvas.SetInteraction(hitWindowSettings, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	hoverMid := state.iconHover(&canvas, hitWindowSettings, iconSettings, false)
	canvas.EndFrame()

	if math.Abs(float64(clickMid.Rotate-hoverMid.Rotate)) > 1e-5 ||
		math.Abs(float64(clickMid.Scale-hoverMid.Scale)) > 1e-5 {
		t.Fatalf("300ms in, click paints %+v and hover paints %+v; want one timeline", clickMid, hoverMid)
	}
}

// A glyph can be declared under more than one kind, as Home and Grid are. The
// drawing is what the user recognises, so every kind that names the same
// contours must either play the same gesture or both stay still. Skipping the
// unregistered kinds would let a new place for an already-animated drawing
// silently opt out.
func TestIconsWithTheSameDrawingShareOneGesture(t *testing.T) {
	type registration struct {
		group iconGlyphGroup
		moves bool
	}
	seen := make(map[string]registration, len(lucideIcons))
	for kind, icon := range lucideIcons {
		group, registered := iconGlyphFor(uint8(kind))
		motion, hasMotion := iconMotionFor(uint8(kind))
		if registered != hasMotion {
			t.Fatalf("icon %d resolves to group %d (%v) but has a motion (%v)", kind, group, registered, hasMotion)
		}
		_ = motion
		key := drawingSignature(icon)
		if first, ok := seen[key]; ok {
			if first.moves != registered || first.moves && first.group != group {
				t.Fatalf("icons %d and a previous kind draw the same glyph but resolve differently: group %d registered=%v and group %d registered=%v",
					kind, first.group, first.moves, group, registered)
			}
		}
		seen[key] = registration{group: group, moves: registered}
	}
}

// A drawing held by a component has no icon kind at that call site. It must
// resolve through the same drawing index, so the same contours cannot animate
// differently just because a different painter owns the control.
func TestDrawingLookupUsesTheSharedGesture(t *testing.T) {
	copy := lucideIcons[iconSettings]
	copy.Commands = append([]ui.PathCommand(nil), copy.Commands...)
	copy.Layers = append([]ui.IconLayer(nil), copy.Layers...)
	if got, ok := iconMotionForDrawing(copy); !ok || !sameMotion(got, iconMotionTurn) {
		t.Fatalf("an independently stored copy of the settings drawing resolves to %v (%v), want the shared turn", got.duration, ok)
	}
	for group, motion := range iconGlyphs {
		for _, kind := range iconGlyphKinds(group) {
			got, ok := iconMotionForDrawing(lucideIcons[kind])
			if !ok || !sameMotion(got, motion) {
				t.Errorf("drawing for kind %d resolves to %v (%v), want group %d", kind, got.duration, ok, group)
			}
		}
	}
	if _, ok := iconMotionForDrawing(ui.Icon{ViewBox: 24, Commands: []ui.PathCommand{ui.MoveTo(1, 1), ui.LineTo(2, 2)}}); ok {
		t.Fatal("an unregistered drawing resolved to a gesture")
	}
}

func TestIconMotionRegistryHasOneEntryPerDrawing(t *testing.T) {
	seen := make(map[string]iconGlyphGroup, len(iconGlyphs))
	for group := range iconGlyphs {
		for _, kind := range iconGlyphKinds(group) {
			key := drawingSignature(lucideIcons[kind])
			if first, ok := seen[key]; ok && first != group {
				t.Errorf("glyph groups %d and %d register the same drawing", first, group)
			}
			seen[key] = group
		}
	}
}

// A brand mark names a project, not an action. It stays still everywhere, just
// as it does beside an About page row: a head tilt on GitHub is decoration that
// the same drawing would perform on any identity mark.
func TestBrandMarksStayStill(t *testing.T) {
	for _, icon := range []uint8{iconGitHub, iconQQ, iconWeChat, iconDiscord} {
		if _, ok := iconMotionFor(icon); ok {
			t.Errorf("brand icon %d has a gesture", icon)
		}
	}
}

// A motion belongs to an action, not to every drawing of an object. File-type
// marks, distribution badges and status lights describe content or identity;
// they stay still even when the same drawing appears beside a button that does
// move. This pins the current boundary so adding one to the registry is a
// deliberate product decision rather than a side effect of sharing a drawing.
func TestContentAndStatusMarksStayStill(t *testing.T) {
	for _, icon := range []uint8{iconFile, iconAlert, iconCheck, iconClock, iconWrench, iconSparkles} {
		if _, ok := iconMotionFor(icon); ok {
			t.Errorf("content or status icon %d has a gesture", icon)
		}
	}
}

// drawingSignature is the test's content comparison. Production resolves a
// drawing by its immutable compiled storage; this spelling makes two separately
// allocated copies with the same contours fail the alias test.
func drawingSignature(icon ui.Icon) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%.3f/%.3f/%.3f/%d/%d/%d/", icon.ViewBox, icon.ViewHeight, icon.Stroke, icon.Cap, icon.Join, len(icon.Commands))
	for _, command := range icon.Commands {
		fmt.Fprintf(&builder, "%d,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f;", command.Kind,
			command.Point.X, command.Point.Y, command.Control1.X, command.Control1.Y, command.Control2.X, command.Control2.Y)
	}
	fmt.Fprintf(&builder, "/%d/", len(icon.Layers))
	for _, layer := range icon.Layers {
		fmt.Fprintf(&builder, "%d,%.3f,%.3f,%.3f,%.3f,%d;", layer.FillRule,
			layer.Fill.Red, layer.Fill.Green, layer.Fill.Blue, layer.Fill.Alpha, len(layer.Commands))
		if gradient := layer.Gradient; gradient != nil {
			fmt.Fprintf(&builder, "gradient:%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f;",
				gradient.Start.X, gradient.Start.Y, gradient.End.X, gradient.End.Y,
				gradient.From.Red, gradient.From.Green, gradient.From.Blue, gradient.From.Alpha,
				gradient.To.Red, gradient.To.Green, gradient.To.Blue, gradient.To.Alpha)
		}
		for _, command := range layer.Commands {
			fmt.Fprintf(&builder, "%d,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f;", command.Kind,
				command.Point.X, command.Point.Y, command.Control1.X, command.Control1.Y, command.Control2.X, command.Control2.Y)
		}
	}
	return builder.String()
}

func TestEveryIconGlyphBelongsToExactlyOneGroup(t *testing.T) {
	for kind := range lucideIcons {
		group, ok := iconGlyphFor(uint8(kind))
		if !ok {
			continue
		}
		count := 0
		for _, member := range iconGlyphKinds(group) {
			if member == uint8(kind) {
				count++
			}
		}
		if count != 1 {
			t.Errorf("icon %d is declared %d times in group %d", kind, count, group)
		}
	}
}

func TestHomeAndGridUseTheGestureOfTheDrawingTheyShare(t *testing.T) {
	want, ok := iconMotionFor(iconGrid2x2)
	if !ok {
		t.Fatal("the grid has no gesture")
	}
	got, ok := iconMotionFor(iconHome)
	if !ok || !sameMotion(got, want) || !sameMotion(want, iconMotionSquash) {
		t.Fatalf("Home plays %+v registered=%v, want the grid's squash", got, ok)
	}
}

func TestIconHoverReachesEveryIconButtonWithAGesture(t *testing.T) {
	for icon := range iconGlyphByKind {
		id := ui.Item(int(icon) + 1)
		state := newTestState(t, freshOptions(t))
		var canvas ui.Canvas
		at := time.Duration(0)
		canvas.SetClock(func() time.Duration { return at })
		canvas.SetInteraction(id, ui.ID{}, ui.ID{}, false)
		canvas.BeginFrame()
		state.iconHover(&canvas, id, uint8(icon), false)
		canvas.EndFrame()
		at = iconHoverDwell
		canvas.SetInteraction(id, ui.ID{}, ui.ID{}, false)
		canvas.BeginFrame()
		state.iconHover(&canvas, id, uint8(icon), false)
		canvas.EndFrame()
		at = iconHoverDwell + 150*time.Millisecond
		canvas.SetInteraction(id, ui.ID{}, ui.ID{}, false)
		canvas.BeginFrame()
		got := state.iconHover(&canvas, id, uint8(icon), false)
		canvas.EndFrame()
		if iconStill(got) {
			t.Errorf("icon %d does not animate on an ordinary icon button", icon)
		}
	}
}

func TestSFTPHeaderHoverReachesItsPaintButtons(t *testing.T) {
	paintAt := func(state *State, canvas *ui.Canvas, hovered ui.ID, at time.Duration) {
		t.Helper()
		canvas.SetClock(func() time.Duration { return at })
		canvas.SetInteraction(hovered, ui.ID{}, ui.ID{}, false)
		canvas.BeginFrame()
		state.Paint(canvas, ui.Size{Width: 1400, Height: 900})
		canvas.EndFrame()
	}

	t.Run("side panel", func(t *testing.T) {
		state, _, _, _ := openTestSFTP(t)
		var canvas ui.Canvas
		hovered := contextID(hitSFTPRefresh)
		paintAt(state, &canvas, hovered, 0)
		paintAt(state, &canvas, hovered, iconHoverDwell+150*time.Millisecond)
		// BeginEmbedded hands the panel its own identities, so its refresh button
		// records the gesture under the unmounted ID.
		if _, ok := state.iconHovers[hitSFTPRefresh]; !ok {
			t.Fatal("the side panel's refresh button did not receive a hover gesture")
		}
	})

	t.Run("file tab", func(t *testing.T) {
		state, _, w, _, _ := openSFTPTabFixture(t)
		var canvas ui.Canvas
		mounted := tree.MountID(sftpWindowMount(w), sftpWindowRefresh[1])
		paintAt(state, &canvas, mounted, 0)
		paintAt(state, &canvas, mounted, iconHoverDwell+150*time.Millisecond)
		if _, ok := state.iconHovers[sftpWindowRefresh[1]]; !ok {
			t.Fatal("the file manager's refresh button did not receive a hover gesture")
		}
	})
}

func TestSFTPWideEntryPointsReachTheirHoverGestures(t *testing.T) {
	paintAt := func(state *State, canvas *ui.Canvas, hovered ui.ID, at time.Duration) {
		t.Helper()
		canvas.SetClock(func() time.Duration { return at })
		canvas.SetInteraction(hovered, ui.ID{}, ui.ID{}, false)
		canvas.BeginFrame()
		state.Paint(canvas, ui.Size{Width: 1400, Height: 900})
		canvas.EndFrame()
	}

	t.Run("side panel pane toggle", func(t *testing.T) {
		state, _, _, _ := openTestSFTP(t)
		var canvas ui.Canvas
		hovered := contextID(hitSFTPPaneToggle)
		paintAt(state, &canvas, hovered, 0)
		paintAt(state, &canvas, hovered, iconHoverDwell+150*time.Millisecond)
		if _, ok := state.iconHovers[hitSFTPPaneToggle]; !ok {
			t.Fatal("the side panel's pane toggle did not receive a hover gesture")
		}
	})

	t.Run("file window location profiles", func(t *testing.T) {
		state, _, w, _, _ := openSFTPTabFixture(t)
		var canvas ui.Canvas
		mounted := tree.MountID(sftpWindowMount(w), hitSFTPLocationProfiles)
		paintAt(state, &canvas, mounted, 0)
		paintAt(state, &canvas, mounted, iconHoverDwell+150*time.Millisecond)
		if _, ok := state.iconHovers[hitSFTPLocationProfiles]; !ok {
			t.Fatal("the file window's saved-location button did not receive a hover gesture")
		}
	})

	t.Run("file window browse sync", func(t *testing.T) {
		state, _, w, _, _ := openSFTPTabFixture(t)
		var canvas ui.Canvas
		mounted := tree.MountID(sftpWindowMount(w), hitSFTPBrowseSync)
		paintAt(state, &canvas, mounted, 0)
		paintAt(state, &canvas, mounted, iconHoverDwell+150*time.Millisecond)
		if _, ok := state.iconHovers[hitSFTPBrowseSync]; !ok {
			t.Fatal("the file window's browse-sync button did not receive a hover gesture")
		}
	})

	t.Run("file window external action", func(t *testing.T) {
		state, _, w, _, _ := openSFTPTabFixture(t)
		index := rowIndexByName(t, w, false, "notes.txt")
		w.view.sftpFolder.selectRow(index, 0)
		w.selectionChanged(false)
		action := sftpWindowActionIndex(hitSFTPEditExternal)
		if action < 0 {
			t.Fatal("the external-editor action has no identity")
		}
		mounted := tree.MountID(sftpWindowMount(w), sftpWindowActions[1].At(action))
		var canvas ui.Canvas
		paintAt(state, &canvas, mounted, 0)
		paintAt(state, &canvas, mounted, iconHoverDwell+150*time.Millisecond)
		if _, ok := state.iconHovers[sftpWindowActions[1].At(action)]; !ok {
			t.Fatal("the file window's external-editor button did not receive a hover gesture")
		}
	})

	t.Run("known hosts retrust", func(t *testing.T) {
		options := freshOptions(t)
		writeKnownHostsFixture(t, options.KnownHostsPath)
		state := newTestState(t, options)
		state.Click(hitKnownHosts)
		index := state.knownHosts.visible[0]
		state.knownHosts.rows[index].selected = true
		state.knownHosts.selected = 1
		canvas, _ := narrowSection(t, state)
		if hit, ok := hitByID(canvas, hitKnownHostsRetrust); !ok || hit.Bounds.Width != toolbarIconButton {
			t.Fatalf("the narrow retrust button is %+v (present %v), want the icon-only form", hit, ok)
		}
		var hover ui.Canvas
		paint := func(at time.Duration) {
			hover.SetClock(func() time.Duration { return at })
			hover.SetInteraction(hitKnownHostsRetrust, ui.ID{}, ui.ID{}, false)
			hover.BeginFrame()
			state.Paint(&hover, ui.Size{Width: 1262, Height: 900})
			hover.EndFrame()
		}
		paint(0)
		paint(iconHoverDwell + 150*time.Millisecond)
		if _, ok := state.iconHovers[hitKnownHostsRetrust]; !ok {
			t.Fatal("the narrow retrust button did not receive a hover gesture")
		}
	})
}

func TestWindowIconsHoverThroughTheirOwnPaintPaths(t *testing.T) {
	t.Run("home", func(t *testing.T) {
		state, _, _, _ := liveSession(t)
		var canvas ui.Canvas
		paintAt := func(at time.Duration) {
			canvas.SetClock(func() time.Duration { return at })
			canvas.SetInteraction(hitHome, ui.ID{}, ui.ID{}, false)
			canvas.BeginFrame()
			state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
			canvas.EndFrame()
		}
		paintAt(0)
		paintAt(iconHoverDwell + 150*time.Millisecond)
		if _, ok := state.iconHovers[hitHome]; !ok {
			t.Fatal("the window bar's home button did not receive a hover gesture")
		}
	})

	t.Run("new tab", func(t *testing.T) {
		state, _, _, _ := liveSession(t)
		var canvas ui.Canvas
		paintAt := func(at time.Duration) {
			canvas.SetClock(func() time.Duration { return at })
			canvas.SetInteraction(hitNewTab, ui.ID{}, ui.ID{}, false)
			canvas.BeginFrame()
			state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
			canvas.EndFrame()
		}
		paintAt(0)
		paintAt(iconHoverDwell + 150*time.Millisecond)
		if _, ok := state.iconHovers[hitNewTab]; !ok {
			t.Fatal("the window bar's new-tab button did not receive a hover gesture")
		}
	})

	t.Run("tab close", func(t *testing.T) {
		state, _, _, _ := liveSession(t)
		closeID := hitTabCloseList.At(int(state.sessions[0].ID))
		var canvas ui.Canvas
		paintAt := func(at time.Duration) {
			canvas.SetClock(func() time.Duration { return at })
			canvas.SetInteraction(closeID, ui.ID{}, ui.ID{}, false)
			canvas.BeginFrame()
			state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
			canvas.EndFrame()
		}
		paintAt(0)
		paintAt(iconHoverDwell + 150*time.Millisecond)
		if _, ok := state.iconHovers[closeID]; !ok {
			t.Fatal("a tab close button did not receive a hover gesture")
		}
	})
}

func TestFindAIAttachAndHelpHoverThroughTheirOwnPaintPaths(t *testing.T) {
	t.Run("find", func(t *testing.T) {
		state, _, _, _ := liveSession(t)
		openFindForTest(t, state)
		var canvas ui.Canvas
		paintAt := func(at time.Duration) {
			canvas.SetClock(func() time.Duration { return at })
			canvas.SetInteraction(hitTerminalFindClose, ui.ID{}, ui.ID{}, false)
			canvas.BeginFrame()
			state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
			canvas.EndFrame()
		}
		paintAt(0)
		paintAt(iconHoverDwell + 150*time.Millisecond)
		if _, ok := state.iconHovers[hitTerminalFindClose]; !ok {
			t.Fatal("the find bar's close button did not receive a hover gesture")
		}
	})

	t.Run("AI attachment", func(t *testing.T) {
		state := openedPanel(t, &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("x") }})
		var canvas ui.Canvas
		paintAt := func(at time.Duration) {
			canvas.SetClock(func() time.Duration { return at })
			canvas.SetInteraction(hitAIAttach, ui.ID{}, ui.ID{}, false)
			canvas.BeginFrame()
			state.Paint(&canvas, panelWide)
			canvas.EndFrame()
		}
		paintAt(0)
		paintAt(iconHoverDwell + 150*time.Millisecond)
		if _, ok := state.iconHovers[hitAIAttach]; !ok {
			t.Fatal("the assistant's attachment button did not receive a hover gesture")
		}
	})

	t.Run("help", func(t *testing.T) {
		var canvas ui.Canvas
		state := newTestState(t, freshOptions(t))
		at := time.Duration(0)
		canvas.SetClock(func() time.Duration { return at })
		paint := func(at time.Duration) {
			canvas.SetClock(func() time.Duration { return at })
			canvas.SetInteraction(hitDialogIdentityHelp, ui.ID{}, ui.ID{}, false)
			canvas.BeginFrame()
			state.paintHelpIcon(&canvas, hitDialogIdentityHelp, "Hint", ui.Rect{X: 10, Y: 10, Width: 16, Height: 16})
			canvas.EndFrame()
		}
		paint(0)
		paint(iconHoverDwell + 150*time.Millisecond)
		if _, ok := state.iconHovers[hitDialogIdentityHelp]; !ok {
			t.Fatal("a help icon did not receive a hover gesture")
		}
	})
}

func TestToolbarHoverCoversEveryCustomizableTool(t *testing.T) {
	for _, catalog := range toolbarCatalog {
		for _, spec := range catalog {
			if spec.icon == iconGitHub {
				continue
			}
			if _, ok := iconMotionFor(spec.icon); !ok {
				t.Errorf("the tool %q has no hover gesture", spec.key)
			}
		}
	}
}

func TestToolbarHoverPartsNameContoursOfTheirGlyph(t *testing.T) {
	for _, catalog := range toolbarCatalog {
		for _, spec := range catalog {
			motion, ok := iconMotionFor(spec.icon)
			if !ok {
				continue
			}
			transform := motion.at(0.5)
			if len(transform.Parts) == 0 {
				continue
			}
			count := len(lucideIcons[spec.icon].Contours())
			seen := make(map[int]bool, len(transform.Parts))
			for _, part := range transform.Parts {
				if part.Contour < 0 || part.Contour >= count {
					t.Errorf("the tool %q moves contour %d of a glyph drawn from %d", spec.key, part.Contour, count)
				}
				if seen[part.Contour] {
					t.Errorf("the tool %q moves contour %d twice", spec.key, part.Contour)
				}
				seen[part.Contour] = true
			}
		}
	}
}

func TestToolbarHoverLeavesTheRestFrameAndTheHitRegionAlone(t *testing.T) {
	state, _, _, _ := liveSession(t)
	size := ui.Size{Width: 1400, Height: 900}
	sizeBefore := state.viewport

	rest := paintToolbarAt(state, size, ui.ID{}, 0)
	hover := paintToolbarAt(state, size, hitSFTPOpen, iconHoverDwell+150*time.Millisecond)
	restFiles, ok := hitByID(rest, hitSFTPOpen)
	if !ok {
		t.Fatal("the rest frame has no Files button")
	}
	hoverFiles, ok := hitByID(hover, hitSFTPOpen)
	if !ok {
		t.Fatal("the hover frame has no Files button")
	}
	if restFiles.Bounds != hoverFiles.Bounds {
		t.Fatalf("hover moved the Files hit from %+v to %+v", restFiles.Bounds, hoverFiles.Bounds)
	}
	if state.viewport != sizeBefore {
		t.Fatal("hovering a toolbar changed the layout size")
	}
}

// paintToolbarAt paints one window frame with an explicit clock and pointer.
func paintToolbarAt(state *State, size ui.Size, hovered ui.ID, at time.Duration) *ui.Canvas {
	var canvas ui.Canvas
	canvas.SetClock(func() time.Duration { return at })
	canvas.SetInteraction(hovered, ui.ID{}, ui.ID{}, false)
	canvas.BeginFrame()
	state.Paint(&canvas, size)
	canvas.EndFrame()
	return &canvas
}
