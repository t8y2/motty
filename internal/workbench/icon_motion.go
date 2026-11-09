package workbench

import (
	"math"
	"time"

	"github.com/vivaui/viva/gfx"
)

// A sidebar row and a settings entry play a short motion of their icon when they
// are chosen: the glyph turns, rocks, sinks or breathes, and comes back to
// exactly the icon that was there. The timings and the curves start from Otty's,
// from the stylesheet of its settings page, so the two applications move alike.
//
// What a motion does follows what the glyph stands for, down to the contours the
// glyph is drawn from. A lock's shackle lifts while its body stays, a keyboard's
// keys press one after another, the halves of a forwarding arrow trade places:
// those are separate contours in the source, so gfx.IconTransform.Parts moves them
// relative to the glyph instead of the whole glyph at once. The rest turn, sway
// or breathe as one shape, and the ones that settle use a spring rather than a
// curve so they arrive with a little weight.
//
// One click is one scalar of progress: the canvas runs the clock (gfx.Animate)
// and hands back linear progress, which the motion maps onto a transform with
// its own keyframes. Progress 1 is always the still icon, so a motion ends on
// the picture the row draws anyway; that is also what a reduced-motion session
// and a window coming back from being hidden paint. Only the terminal starts one
// line down, which is the line it scrolls up.

// iconAxis is one value of the transform over the length of a motion.
type iconAxis uint8

const (
	iconAxisTurn iconAxis = iota
	iconAxisScale
	iconAxisAcross
	iconAxisDown
	// iconAxisReveal writes the glyph on rather than moving it, and iconAxisStretch
	// narrows it across, which is how a turning page and a spinning meridian
	// project onto the plane.
	iconAxisReveal
	iconAxisStretch
)

// iconMotionStop is one keyframe of an axis: the value it holds at milliseconds
// into the motion, and the curve that carries it to the next stop. The last stop
// of an axis holds to the end, so a motion that comes back to the still icon
// needs no keyframe after the one that returns it.
type iconMotionStop struct {
	at    float32 // milliseconds
	value float32
	ease  gfx.Curve
}

// iconMotionTrack is one axis of a motion.
type iconMotionTrack struct {
	axis  iconAxis
	stops []iconMotionStop
}

// iconPartMotion is one contour of a glyph moving on its own while the glyph
// plays its motion; contour numbering is gfx.Icon.Contours' drawing order.
type iconPartMotion struct {
	contour int
	// anchor is the point of the glyph the part turns and stretches about, in the
	// icon's view-box units; nil is the part's own centre. A page turns over the
	// book's spine, so its anchor is the middle of the glyph.
	anchor *gfx.Point
	tracks []iconMotionTrack
}

// iconMotion is what choosing one row plays: how long it lasts, the axes that
// move the whole glyph, and the contours that move on their own.
type iconMotion struct {
	duration time.Duration
	tracks   []iconMotionTrack
	parts    []iconPartMotion
}

// at returns the transform the motion paints at progress through it. A motion
// with parts allocates them: they belong to the frame being built, and two rows
// can be in motion at once, so they cannot share a buffer.
func (motion iconMotion) at(progress float32) gfx.IconTransform {
	elapsed := progress * float32(motion.duration/time.Millisecond)
	transform := readPose(motion.tracks, elapsed).transform()
	if len(motion.parts) > 0 {
		transform.Parts = make([]gfx.IconPart, len(motion.parts))
		for index, part := range motion.parts {
			own := readPose(part.tracks, elapsed)
			transform.Parts[index] = gfx.IconPart{
				Contour: part.contour,
				Rotate:  own.turn,
				Scale:   own.scale,
				ScaleX:  own.stretch,
				Offset:  gfx.Point{X: own.across, Y: own.down},
				Anchor:  part.anchor,
			}
		}
	}
	return transform
}

// pose is what one set of axes holds at one instant: the four values a motion
// writes, before they become a transform.
type pose struct {
	turn, scale, across, down float32
	reveal, stretch           float32
}

// readPose reads one set of axes at elapsed milliseconds into the motion.
func readPose(tracks []iconMotionTrack, elapsed float32) (values pose) {
	for _, track := range tracks {
		value := track.at(elapsed)
		switch track.axis {
		case iconAxisTurn:
			values.turn = value
		case iconAxisScale:
			values.scale = value
		case iconAxisAcross:
			values.across = value
		case iconAxisDown:
			values.down = value
		case iconAxisReveal:
			values.reveal = value
		case iconAxisStretch:
			values.stretch = value
		}
	}
	return values
}

// transform turns the values into the transform the painter takes.
func (values pose) transform() gfx.IconTransform {
	return gfx.IconTransform{
		Rotate: values.turn,
		Scale:  values.scale,
		Offset: gfx.Point{X: values.across, Y: values.down},
		Reveal: values.reveal,
	}
}

// at returns the value of one axis at elapsed milliseconds into the motion.
func (track iconMotionTrack) at(elapsed float32) float32 {
	if len(track.stops) == 0 {
		return 0
	}
	from := track.stops[0]
	if elapsed <= from.at {
		return from.value
	}
	for _, to := range track.stops[1:] {
		if elapsed > to.at {
			from = to
			continue
		}
		span := to.at - from.at
		if span <= 0 {
			return to.value
		}
		progress := (elapsed - from.at) / span
		if from.ease != nil {
			progress = from.ease(progress)
		}
		// Interpolate without gfx.Lerp: it clamps progress to [0, 1], and a spring
		// passes the end value on purpose, which is the whole of its bounce.
		return from.value + (to.value-from.value)*progress
	}
	return from.value
}

// turn converts degrees to the radians a transform rotates in.
func turn(degrees float32) float32 { return degrees * math.Pi / 180 }

// part builds the motion of one contour out of the same axes the glyph uses.
func part(contour int, tracks ...iconMotionTrack) iconPartMotion {
	return iconPartMotion{contour: contour, tracks: tracks}
}

// The curves Otty's settings stylesheet declares for these motions, as the CSS
// control points gfx.CubicBezier takes.
var (
	easeTurn    = gfx.CubicBezier(0.2, 0, 0.3, 1)   // turning into place
	easeSqueeze = gfx.CubicBezier(0.33, 0, 0.67, 1) // the middle of a squash
	easeRockIn  = gfx.CubicBezier(0.25, 0, 0.95, 1) // swinging out
	easeRockOut = gfx.CubicBezier(1, 0, 0.95, 1)    // and back past it
	easeSettle  = gfx.CubicBezier(1, 0, 0.75, 1)    // a small overshoot returning
	easeSink    = gfx.CubicBezier(0.05, 0, 0, 1)    // opening and dropping
	// A part that comes back lands instead of stopping: the spring is what makes
	// a key feel pressed and a shackle feel like it closed.
	easeLand = gfx.Spring(0.68)
)

// iconSpine is the middle of a glyph's view box, where an open book is drawn
// from: the line a page turns over.
var iconSpine = gfx.Point{X: 12, Y: 12}

var (
	// iconMotionTurn turns a gear half way round, slowly, while it settles. Half a
	// turn is the still picture for a gear, which is why the motion can end where
	// it started without being seen to snap.
	iconMotionTurn = iconMotion{
		duration: 1333 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeTurn},
				{at: 1333, value: math.Pi},
			}},
			{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 360, value: 0.86, ease: easeSqueeze},
				{at: 760, value: 1},
			}},
		},
	}

	// iconMotionSquash settles a glyph that has nothing to gather: a breath in and
	// out that arrives with weight rather than stopping.
	iconMotionSquash = iconMotion{
		duration: 760 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 320, value: 0.84, ease: easeLand},
				{at: 760, value: 1},
			}},
		},
	}

	// iconMotionRock swings a connection's glyph left, back past upright and home:
	// the shape of a connector working its way in.
	iconMotionRock = iconMotion{
		duration: 1833 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeRockIn},
				{at: 460, value: turn(-26), ease: easeRockOut},
				{at: 1600, value: turn(16), ease: easeSettle},
				{at: 1833, value: 0},
			}},
		},
	}

	// iconMotionExchange trades the halves of a forwarding arrow places: the top
	// line runs right, the bottom one runs left, and both come back. The glyph
	// stands for traffic going both ways, so the motion is traffic going both
	// ways; contour 0 and 1 are the upper head and shaft, 2 and 3 the lower pair.
	iconMotionExchange = iconMotion{
		duration: 900 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 210, value: 2, ease: easeLand},
				{at: 900, value: 0},
			}}),
			part(1, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 210, value: 2, ease: easeLand},
				{at: 900, value: 0},
			}}),
			part(2, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 100, value: 0, ease: easeSqueeze},
				{at: 310, value: -2, ease: easeLand},
				{at: 900, value: 0},
			}}),
			part(3, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 100, value: 0, ease: easeSqueeze},
				{at: 310, value: -2, ease: easeLand},
				{at: 900, value: 0},
			}}),
		},
	}

	// iconMotionUnlock lifts a lock's shackle clear of the body and drops it back
	// onto the lock: contour 0 is the body, which stays where it is, and contour 1
	// the shackle.
	iconMotionUnlock = iconMotion{
		duration: 1000 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSettle},
				{at: 280, value: -2.5, ease: easeLand},
				{at: 1000, value: 0},
			}}),
		},
	}

	// iconMotionType presses a keyboard's keys from the left edge to the right,
	// one after another. The dots are contours 0 to 7 and the outline is 8; the
	// order is by the left edge of each shape, so the wide space bar, contour 6,
	// presses second rather than last.
	iconMotionType = iconMotion{
		duration: 1000 * time.Millisecond,
		parts: []iconPartMotion{
			keyPress(5, 0), keyPress(6, 70), keyPress(7, 140), keyPress(0, 210),
			keyPress(1, 280), keyPress(2, 350), keyPress(3, 420), keyPress(4, 490),
		},
	}

	// iconMotionBlink nods a robot's antenna and blinks its eyes: contour 0 is the
	// antenna, 4 and 5 are the eyes, and the head keeps still.
	iconMotionBlink = iconMotion{
		duration: 1100 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeRockIn},
				{at: 280, value: turn(-32), ease: easeLand},
				{at: 1100, value: 0},
			}}),
			eyeBlink(4, 450),
			eyeBlink(5, 450),
		},
	}

	// iconMotionUnlid lifts an archive box's lid and closes it again; the box and
	// its handle stay. Contour 0 is the lid.
	iconMotionUnlid = iconMotion{
		duration: 1000 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSettle},
				{at: 300, value: -2.4, ease: easeLand},
				{at: 1000, value: 0},
			}}),
		},
	}

	// iconMotionRack separates a server's plates and lets them close: the two
	// plates are contours 0 and 1, the lights 2 and 3, which stay lit where they
	// are.
	iconMotionRack = iconMotion{
		duration: 900 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSettle},
				{at: 270, value: -1.6, ease: easeLand},
				{at: 900, value: 0},
			}}),
			part(1, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSettle},
				{at: 270, value: 1.6, ease: easeLand},
				{at: 900, value: 0},
			}}),
		},
	}

	// iconMotionPlug pushes a plug's prongs into the socket: contours 1 and 3 are
	// the prongs and 2 is the body behind them.
	iconMotionPlug = iconMotion{
		duration: 860 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSettle},
				{at: 240, value: 1.6, ease: easeLand},
				{at: 860, value: 0},
			}}),
			part(3, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSettle},
				{at: 240, value: 1.6, ease: easeLand},
				{at: 860, value: 0},
			}}),
		},
	}

	// iconMotionCursor steps a terminal's prompt mark to the right while its
	// cursor blinks: contour 0 is the prompt mark, contour 1 the cursor.
	iconMotionCursor = iconMotion{
		duration: 700 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 230, value: 1.8, ease: easeLand},
				{at: 700, value: 0},
			}}),
			part(1, iconMotionTrack{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 250, value: 1, ease: easeSqueeze},
				{at: 400, value: 0.2, ease: easeLand},
				{at: 700, value: 1},
			}}),
		},
	}

	// iconMotionWorld turns a globe about its own axis. A sphere turning toward the
	// viewer shows its meridian narrowing to the axis and opening again mirrored on
	// the far side, so contour 1 — the meridian, between the outline (0) and the
	// equator (2) — is stretched across, through zero, to the other side and back:
	// one revolution of the world. The outline and the equator do not move, which
	// is what makes it read as a sphere turning rather than a drawing being
	// squashed.
	//
	// The stops stop short of zero on either side, so the meridian is never painted
	// exactly edge on, where a stretch of zero would read as a full width.
	iconMotionWorld = iconMotion{
		duration: 1100 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisStretch, stops: []iconMotionStop{
				{at: 0, value: 1},
				{at: 275, value: 0.02},
				{at: 550, value: -1},
				{at: 825, value: -0.02},
				{at: 1100, value: 1},
			}}),
		},
	}

	// iconMotionTrace writes a monitor's line on. The trace runs from the right of
	// the glyph to the left, and the reveal follows it, so the row draws the line
	// it stands for the way the screen it names would, then stands still as the
	// whole glyph.
	iconMotionTrace = iconMotion{
		duration: 700 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisReveal, stops: []iconMotionStop{
				// A sliver from the first frame, so the row is never empty.
				{at: 0, value: 0.04},
				{at: 620, value: 1},
			}},
		},
	}

	// iconMotionFold closes a ledger half way on its spine and opens it again: known
	// hosts is a book, and a book is the one glyph here that hinges. The outline is
	// a single contour that wraps both pages, and the spine is the line the two
	// halves meet on, so stretching the outline across about the spine folds the
	// pages toward it while the spine itself stays where it is drawn.
	iconMotionFold = iconMotion{
		duration: 960 * time.Millisecond,
		parts: []iconPartMotion{
			{contour: 1, anchor: &iconSpine, tracks: []iconMotionTrack{
				{axis: iconAxisStretch, stops: []iconMotionStop{
					{at: 0, value: 1, ease: easeSqueeze},
					// Half closed: the pages meet in the middle of the book, still wide
					// enough to read as a book rather than as the row going out.
					{at: 300, value: 0.5},
					// It is held half closed for a moment, the way it is while it is read.
					{at: 520, value: 0.5, ease: easeLand},
					{at: 960, value: 1},
				}},
			}},
		},
	}

	// iconMotionKey turns a key in its lock.
	iconMotionKey = iconMotion{
		duration: 1000 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeRockIn},
				{at: 300, value: turn(-26), ease: easeLand},
				{at: 1000, value: 0},
			}},
		},
	}

	// iconMotionSink opens a folder and lets it drop into place: down, held while
	// the contents settle, then back.
	iconMotionSink = iconMotion{
		duration: 1250 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSink},
				{at: 210, value: 2.6},
				{at: 1060, value: 2.6, ease: easeRockOut},
				{at: 1250, value: 0},
			}},
		},
	}
)

// keyPress is one key of the keyboard glyph going down and coming back up, start
// milliseconds into the motion.
func keyPress(contour int, start float32) iconPartMotion {
	return part(contour, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
		{at: start, value: 0, ease: easeSqueeze},
		{at: start + 140, value: 1.3, ease: easeLand},
		{at: start + 420, value: 0},
	}})
}

// eyeBlink is one eye of the robot glyph closing and opening, start milliseconds
// into the motion. The eye is a dot, so closing it is shrinking it to a speck.
func eyeBlink(contour int, start float32) iconPartMotion {
	return part(contour, iconMotionTrack{axis: iconAxisScale, stops: []iconMotionStop{
		{at: start, value: 1, ease: easeSqueeze},
		{at: start + 140, value: 0.15, ease: easeLand},
		{at: start + 400, value: 1},
	}})
}

// iconMotionFor is the motion choosing a row with this icon plays. Two rows can
// share an icon — Settings in the sidebar and the general entries of the settings
// list — and they play the same motion. A motion with parts names contours of one
// particular glyph, so an icon that gains or loses a contour when it is
// regenerated has to keep the parts table honest: the test draws every part's
// contour from the icon itself.
func iconMotionFor(icon uint8) (iconMotion, bool) {
	switch icon {
	case iconSettings:
		return iconMotionTurn, true
	case iconTerminal:
		return iconMotionCursor, true
	case iconForward:
		return iconMotionExchange, true
	case iconFolderOpen:
		return iconMotionSink, true
	case iconArchive:
		return iconMotionUnlid, true
	case iconLink:
		return iconMotionRock, true
	case iconKnown:
		return iconMotionFold, true
	case iconActivity:
		return iconMotionTrace, true
	case iconLock:
		return iconMotionUnlock, true
	case iconKey:
		return iconMotionKey, true
	case iconGlobe:
		return iconMotionWorld, true
	case iconHosts:
		return iconMotionRack, true
	case iconKeyboard:
		return iconMotionType, true
	case iconBot:
		return iconMotionBlink, true
	case iconPlug:
		return iconMotionPlug, true
	case iconColumns2, iconNote, iconInfo, iconBug, iconFileClock:
		return iconMotionSquash, true
	}
	return iconMotion{}, false
}

// iconMotionRun is where one row is in the motion a click started.
type iconMotionRun uint8

const (
	// iconMotionPending is a click the painter has not seen yet: the next frame
	// pins the motion's clock to zero, which is what makes a second click replay
	// it from the beginning instead of continuing.
	iconMotionPending iconMotionRun = iota + 1
	// iconMotionPlaying is a motion in flight; every frame asks for the next.
	iconMotionPlaying
)

// iconMotionSlot is the animation slot these motions use. Only the navigation
// rows animate on their own hit identities, so the slot is free.
const iconMotionSlot uint8 = 0

// pressIconMotion records that the row id was chosen, so the motion its icon
// plays starts on the next frame. A click on the row already on show replays the
// motion: the row answers every click, not only the ones that change the page.
func (state *State) pressIconMotion(id gfx.ID) {
	if !playsIconMotion(id) {
		return
	}
	if state.iconMotions == nil {
		state.iconMotions = make(map[gfx.ID]iconMotionRun, 2)
	}
	state.iconMotions[id] = iconMotionPending
}

// playsIconMotion reports whether choosing id animates an icon: the rows of the
// sidebar and the entries of the settings list are the two places that draw one.
// A row added to either place needs its id here, and its icon in iconMotionFor.
func playsIconMotion(id gfx.ID) bool {
	if id.Scope() == hitSettingsCategoryList || devPlaysIconMotion(id) {
		return true
	}
	switch id {
	case hitHosts, hitWorkspaces, hitDashboard, hitSessionLogs, hitKeychain, hitProxies, hitPortForwarding,
		hitKnownHosts, hitSnippets, hitNotes, hitLocalTerminal, hitSettings:
		return true
	}
	return false
}

// iconMotion returns the transform to draw the row's icon with on this frame,
// advancing the motion the click recorded. A row that is not in motion gets the
// zero transform, which paints the still icon on the cached path.
func (state *State) iconMotion(canvas *gfx.Canvas, id gfx.ID, icon uint8) gfx.IconTransform {
	run, running := state.iconMotions[id]
	if !running {
		return gfx.IconTransform{}
	}
	motion, ok := iconMotionFor(icon)
	if !ok {
		// A row whose glyph has no motion, such as one added after this table,
		// paints still; it must not wait for a frame that would never come.
		delete(state.iconMotions, id)
		return gfx.IconTransform{}
	}
	if run == iconMotionPending {
		// A slot starts settled at the value it is first asked for, so the clock
		// has to be pinned to zero before the motion asks for one; otherwise the
		// first frame of the motion would already be its end.
		canvas.Animate(id, iconMotionSlot, 0, gfx.Motion{})
		state.iconMotions[id] = iconMotionPlaying
	}
	progress := canvas.Animate(id, iconMotionSlot, 1, gfx.Motion{Duration: motion.duration, Easing: gfx.EaseLinear})
	if progress >= 1 {
		// Over, or never started: reduced motion, a still preview and a canvas
		// without a frame clock all arrive here with 1.
		delete(state.iconMotions, id)
		return gfx.IconTransform{}
	}
	return motion.at(progress)
}
