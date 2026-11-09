package workbench

import (
	"math"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

// iconStill reports whether a transform paints the icon it would paint without
// one: no turn, no offset, the identity scale and reveal — which a motion's first
// keyframe spells either as zero or as one — and no contour moving or stretching
// on its own.
func iconStill(transform gfx.IconTransform) bool {
	if transform.Rotate != 0 || transform.Offset != (gfx.Point{}) {
		return false
	}
	if transform.Scale != 0 && transform.Scale != 1 {
		return false
	}
	if transform.Reveal != 0 && transform.Reveal != 1 {
		return false
	}
	for _, part := range transform.Parts {
		if part.Rotate != 0 || part.Offset != (gfx.Point{}) {
			return false
		}
		if part.Scale != 0 && part.Scale != 1 {
			return false
		}
		if part.ScaleX != 0 && part.ScaleX != 1 {
			return false
		}
	}
	return true
}

// sameMotion reports whether two motions play the same keyframes; motions hold
// slices, so they cannot be compared with ==.
func sameMotion(motion, want iconMotion) bool {
	if motion.duration != want.duration || len(motion.tracks) != len(want.tracks) || len(motion.parts) != len(want.parts) {
		return false
	}
	for index, part := range motion.parts {
		if part.contour != want.parts[index].contour || len(part.tracks) != len(want.parts[index].tracks) {
			return false
		}
	}
	return true
}

// iconFrame paints one frame at at and returns the transform the row's icon is
// drawn with, the way a paint pass asks for it.
func iconFrame(canvas *gfx.Canvas, state *State, id gfx.ID, icon uint8, at time.Duration) gfx.IconTransform {
	canvas.SetClock(func() time.Duration { return at })
	canvas.BeginFrame()
	transform := state.iconMotion(canvas, id, icon)
	canvas.EndFrame()
	return transform
}

func TestIconMotionPlaysOnceAndLeavesTheCanvasAtRest(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var canvas gfx.Canvas

	state.Click(hitSettings)
	if run, tracking := state.iconMotions[hitSettings]; !tracking || run != iconMotionPending {
		t.Fatalf("choosing Settings tracks %v %v, want a pending motion", tracking, run)
	}

	if got := iconFrame(&canvas, state, hitSettings, iconSettings, 0); !iconStill(got) {
		t.Fatalf("the frame of the click paints %+v, want the still gear", got)
	}

	// A third of a turn in, the gear is part way round and briefly smaller.
	mid := iconFrame(&canvas, state, hitSettings, iconSettings, 300*time.Millisecond)
	if mid.Rotate <= 0 || mid.Rotate >= float32(math.Pi) {
		t.Fatalf("300ms into the turn the gear is at %v radians, want a part turn", mid.Rotate)
	}
	// The squeeze bottoms out at 0.86 a third of the way in, so by 300ms it is
	// between that and the size it started at.
	if mid.Scale >= 1 || mid.Scale < 0.86 {
		t.Fatalf("300ms into the turn the gear is at scale %v, want it squeezed", mid.Scale)
	}
	if !canvas.Animating() {
		t.Fatal("a motion in flight does not ask for the next frame")
	}

	// The motion lasts as long as Otty's and then the row is a plain row again,
	// so a window left alone goes back to painting nothing.
	last := iconFrame(&canvas, state, hitSettings, iconSettings, iconMotionTurn.duration)
	if !iconStill(last) {
		t.Fatalf("the end of the motion paints %+v, want the still gear", last)
	}
	if _, tracking := state.iconMotions[hitSettings]; tracking {
		t.Fatal("a finished motion is still tracked")
	}
	if canvas.Animating() {
		t.Fatal("a finished motion still asks for frames")
	}
}

func TestIconMotionStartsOverWhenTheRowIsChosenAgain(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var canvas gfx.Canvas

	state.Click(hitPortForwarding)
	iconFrame(&canvas, state, hitPortForwarding, iconForward, 0)
	moved := iconFrame(&canvas, state, hitPortForwarding, iconForward, 300*time.Millisecond)
	if iconStill(moved) {
		t.Fatal("the arrow does not leave its place 300ms after the click")
	}

	// Choosing the row again restarts the motion from its first keyframe instead
	// of leaving it part way through.
	state.Click(hitPortForwarding)
	if got := iconFrame(&canvas, state, hitPortForwarding, iconForward, 300*time.Millisecond); !iconStill(got) {
		t.Fatalf("choosing the row again paints %+v, want the motion to start over", got)
	}
	if run := state.iconMotions[hitPortForwarding]; run != iconMotionPlaying {
		t.Fatalf("the replay is %v, want it playing", run)
	}
}

func TestIconMotionNeedsAChoiceAndAFrameClock(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var canvas gfx.Canvas

	if got := state.iconMotion(&canvas, hitSettings, iconSettings); !iconStill(got) {
		t.Fatalf("a row that was never chosen paints %+v", got)
	}

	// A hit that did not come from the control itself records nothing: the icon
	// of a row reached through the command palette stays still.
	state.click(hitSettings, false)
	if len(state.iconMotions) != 0 {
		t.Fatalf("a hit that is not the row records %d motions", len(state.iconMotions))
	}

	// Without a frame clock there is nothing to animate: a static preview and a
	// headless run paint the still icon and keep no state for it.
	state.Click(hitSettings)
	if got := state.iconMotion(&canvas, hitSettings, iconSettings); !iconStill(got) {
		t.Fatalf("a canvas without a clock paints %+v, want the still gear", got)
	}
	if len(state.iconMotions) != 0 {
		t.Fatal("a canvas without a clock keeps a motion waiting")
	}

	// The same goes for reduced motion.
	state.Click(hitSettings)
	canvas.SetReduceMotion(true)
	if got := iconFrame(&canvas, state, hitSettings, iconSettings, 0); !iconStill(got) {
		t.Fatalf("reduced motion paints %+v, want the still gear", got)
	}
	if len(state.iconMotions) != 0 {
		t.Fatal("reduced motion keeps a motion waiting")
	}
}

func TestIconMotionTrackHoldsBetweenKeyframes(t *testing.T) {
	track := iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
		{at: 0, value: 10},
		{at: 100, value: 20},
		{at: 200, value: 20},
	}}
	for _, c := range []struct{ elapsed, want float32 }{
		{-5, 10}, {0, 10}, {50, 15}, {100, 20}, {150, 20}, {200, 20}, {900, 20},
	} {
		if got := track.at(c.elapsed); math.Abs(float64(got-c.want)) > 1e-6 {
			t.Fatalf("%v ms into the track the axis is %v, want %v", c.elapsed, got, c.want)
		}
	}

	if got := (iconMotionTrack{}).at(50); got != 0 {
		t.Fatalf("an empty track is %v, want 0", got)
	}

	// A curve bends the way between two keyframes towards its own shape: an
	// ease-in curve is still below the straight line half way along.
	eased := iconMotionTrack{stops: []iconMotionStop{
		{at: 0, value: 0, ease: gfx.CubicBezier(0.4, 0, 1, 1)},
		{at: 100, value: 1},
	}}
	if got := eased.at(50); got >= 0.5 {
		t.Fatalf("an ease-in track is at %v half way, want less than half", got)
	}
}

// Every row the sidebar paints plays a motion. The rows are read from a real
// paint rather than copied here, so a row added to the sidebar fails this test
// until it is taught to move.
func TestEveryPaintedNavigationRowPlaysAMotion(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	rows := 0
	for _, hit := range canvas.Hits() {
		if hit.Role != gfx.RoleMenuItem || hit.Bounds.X >= navigationWidth {
			continue
		}
		rows++
		if !playsIconMotion(hit.ID) {
			t.Errorf("the sidebar row %q (%v) does not play a motion", hit.Label, hit.ID)
		}
	}
	if rows < 10 {
		t.Fatalf("the painted sidebar has %d rows, want the ten page rows", rows)
	}
	for entry := settingsGeneral; entry < settingsCategoryCount; entry++ {
		if _, ok := iconMotionFor(entry.icon()); !ok {
			t.Errorf("the %v settings entry has no motion", entry)
		}
	}
}

func TestIconMotionFollowsWhatTheGlyphStandsFor(t *testing.T) {
	for _, c := range []struct {
		what  string
		icons []uint8
		want  iconMotion
	}{
		{"turn", []uint8{iconSettings}, iconMotionTurn},
		{"cursor", []uint8{iconTerminal}, iconMotionCursor},
		{"exchange", []uint8{iconForward}, iconMotionExchange},
		{"sink", []uint8{iconFolderOpen}, iconMotionSink},
		{"unlid", []uint8{iconArchive}, iconMotionUnlid},
		{"rock", []uint8{iconLink}, iconMotionRock},
		{"unlock", []uint8{iconLock}, iconMotionUnlock},
		{"key", []uint8{iconKey}, iconMotionKey},
		{"world", []uint8{iconGlobe}, iconMotionWorld},
		{"trace", []uint8{iconActivity}, iconMotionTrace},
		{"fold", []uint8{iconKnown}, iconMotionFold},
		{"rack", []uint8{iconHosts}, iconMotionRack},
		{"type", []uint8{iconKeyboard}, iconMotionType},
		{"blink", []uint8{iconBot}, iconMotionBlink},
		{"plug", []uint8{iconPlug}, iconMotionPlug},
		{"squash", []uint8{iconColumns2, iconNote, iconInfo, iconBug}, iconMotionSquash},
	} {
		for _, icon := range c.icons {
			got, ok := iconMotionFor(icon)
			if !ok || !sameMotion(got, c.want) {
				t.Errorf("icon %d plays %v with %d parts (%v), want the %s motion", icon, got.duration, len(got.parts), ok, c.what)
			}
		}
	}
}

// A part names a contour of the glyph it moves. The contours come from the
// generated icon, so an icon that is regenerated with a different number of
// contours fails here rather than painting a part that is not there.
func TestEveryMotionPartNamesAContourOfItsGlyph(t *testing.T) {
	for icon := range lucideIcons {
		motion, ok := iconMotionFor(uint8(icon))
		if !ok || len(motion.parts) == 0 {
			continue
		}
		count := len(lucideIcons[icon].Contours())
		seen := make(map[int]bool, len(motion.parts))
		for _, part := range motion.parts {
			if part.contour < 0 || part.contour >= count {
				t.Errorf("icon %d moves contour %d of a glyph drawn from %d", icon, part.contour, count)
			}
			if seen[part.contour] {
				t.Errorf("icon %d moves contour %d twice", icon, part.contour)
			}
			seen[part.contour] = true
			if len(part.tracks) == 0 {
				t.Errorf("icon %d moves contour %d with no keyframes", icon, part.contour)
			}
		}
	}
}

// A part motion moves the contour it names and leaves the glyph's own transform
// alone unless it also asks for one.
func TestIconMotionPartsMoveOnlyTheirContour(t *testing.T) {
	// A third of the way into the keyboard's motion the first key is down, and the
	// glyph itself has not moved.
	motion := iconMotionType
	transform := motion.at(0.2)
	if transform.Rotate != 0 || transform.Offset != (gfx.Point{}) || transform.Scale != 0 {
		t.Fatalf("the keyboard's motion moves the whole glyph: %+v", transform)
	}
	if len(transform.Parts) != len(motion.parts) {
		t.Fatalf("the motion paints %d parts, want %d", len(transform.Parts), len(motion.parts))
	}
	pressed := false
	for _, part := range transform.Parts {
		if part.Offset.Y > 0.05 {
			pressed = true
		}
	}
	if !pressed {
		t.Fatal("no key is down a fifth of the way into the keyboard's motion")
	}
	if !iconStill(motion.at(1)) {
		t.Fatalf("the end of the keyboard's motion paints %+v, want the still keyboard", motion.at(1))
	}
}

func TestPaintingANavRowAdvancesItsMotion(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	at := time.Duration(0)
	canvas.SetClock(func() time.Duration { return at })
	paint := func() {
		canvas.BeginFrame()
		state.Paint(&canvas, size)
		canvas.EndFrame()
	}

	// A window with nothing happening paints no further frames.
	paint()
	paint()
	if canvas.Animating() {
		t.Fatal("a window at rest asks for frames")
	}

	// The sidebar row draws the motion the click recorded.
	state.Click(hitKeychain)
	at = time.Millisecond
	paint()
	if run := state.iconMotions[hitKeychain]; run != iconMotionPlaying {
		t.Fatalf("painting the sidebar leaves the keychain %v, want it playing", run)
	}

	// So does an entry of the settings list.
	terminal := hitSettingsCategoryList.At(int(settingsTerminal))
	state.Click(hitSettings)
	state.Click(terminal)
	paint()
	if run := state.iconMotions[terminal]; run != iconMotionPlaying {
		t.Fatalf("painting the settings list leaves the entry %v, want it playing", run)
	}

	// Every motion of those clicks is over once the longest of them is.
	at = iconMotionRock.duration + time.Second
	paint()
	paint()
	if len(state.iconMotions) != 0 {
		t.Fatalf("%d motions outlived their click", len(state.iconMotions))
	}
	if canvas.Animating() {
		t.Fatal("a window whose motions are over still asks for frames")
	}
}

// The three glyphs that name a screen, a network and a ledger move the part of
// themselves that says so: a trace is written on, a meridian turns the globe
// over, a page turns over the book's spine.
func TestTheNamedGlyphsMoveThePartThatSaysWhatTheyAre(t *testing.T) {
	// The monitor's trace is written from its right end to its left: a fifth of
	// the way in it has written a little, and it is never written backwards.
	trace := iconMotionTrace
	first := trace.at(0.1)
	last := trace.at(0.6)
	if first.Reveal <= 0 || first.Reveal >= 1 {
		t.Fatalf("the trace starts at reveal %v, want part of the line written", first.Reveal)
	}
	if last.Reveal <= first.Reveal {
		t.Fatalf("the trace is at reveal %v after %v, want it to have written more", last.Reveal, first.Reveal)
	}
	if !iconStill(trace.at(1)) {
		t.Fatalf("the end of the trace paints %+v, want the whole glyph", trace.at(1))
	}

	// The globe's meridian is stretched across and through zero: a quarter of a
	// revolution narrows it, half of one has it mirrored on the far side.
	world := iconMotionWorld.at(0.25)
	if len(world.Parts) != 1 || world.Parts[0].Contour != 1 {
		t.Fatalf("the globe moves parts %+v, want its meridian", world.Parts)
	}
	if world.Parts[0].ScaleX >= 0.1 {
		t.Fatalf("a quarter of the way round the meridian is at %v across, want it narrow", world.Parts[0].ScaleX)
	}
	if half := iconMotionWorld.at(0.5).Parts[0].ScaleX; half > -0.9 {
		t.Fatalf("half way round the meridian is at %v across, want it mirrored", half)
	}

	// The ledger folds on its spine: the outline shrinks across about the middle of
	// the glyph, where the spine is drawn, and the spine contour is left alone.
	fold := iconMotionFold.at(0.45)
	if len(fold.Parts) != 1 || fold.Parts[0].Contour != 1 {
		t.Fatalf("the ledger moves parts %+v, want its pages", fold.Parts)
	}
	if fold.Parts[0].Anchor == nil || *fold.Parts[0].Anchor != iconSpine {
		t.Fatalf("the pages fold about %v, want the spine at %v", fold.Parts[0].Anchor, iconSpine)
	}
	if fold.Parts[0].ScaleX > 0.55 {
		t.Fatalf("the ledger is %v across at its half closed point, want it folded to about half", fold.Parts[0].ScaleX)
	}
	if !iconStill(iconMotionFold.at(1)) {
		t.Fatalf("the end of the fold paints %+v, want the open ledger", iconMotionFold.at(1))
	}
}
