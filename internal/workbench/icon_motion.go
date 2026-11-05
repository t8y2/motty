package workbench

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/vivaui/viva/ui"
)

// A sidebar row and a settings entry play a short motion of their icon when they
// are chosen: the glyph turns, rocks, sinks or breathes, and comes back to
// exactly the icon that was there. The timings and the curves start from Otty's,
// from the stylesheet of its settings page, so the two applications move alike.
//
// What a motion does follows what the glyph stands for, down to the contours the
// glyph is drawn from. A lock's shackle lifts while its body stays, a keyboard's
// keys press one after another, the halves of a forwarding arrow trade places:
// those are separate contours in the source, so ui.IconTransform.Parts moves them
// relative to the glyph instead of the whole glyph at once. The rest turn, sway
// or breathe as one shape, and the ones that settle use a spring rather than a
// curve so they arrive with a little weight.
//
// One click is one scalar of progress: the canvas runs the clock (ui.Animate)
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
	// iconAxisTrim writes part of one contour on, measured along the contour.
	// It is read by a part track; a whole-glyph track cannot trim one contour.
	iconAxisTrim
	// iconAxisFade hides one contour while it travels, so a pulse can leave the
	// drawing at its outer edge without a detached line lingering there.
	iconAxisFade
)

// iconMotionStop is one keyframe of an axis: the value it holds at milliseconds
// into the motion, and the curve that carries it to the next stop. The last stop
// of an axis holds to the end, so a motion that comes back to the still icon
// needs no keyframe after the one that returns it.
type iconMotionStop struct {
	at    float32 // milliseconds
	value float32
	ease  ui.Curve
}

// iconMotionTrack is one axis of a motion.
type iconMotionTrack struct {
	axis  iconAxis
	stops []iconMotionStop
}

// iconPartMotion is one contour of a glyph moving on its own while the glyph
// plays its motion; contour numbering is ui.Icon.Contours' drawing order.
type iconPartMotion struct {
	contour int
	// anchor is the point of the glyph the part turns and stretches about, in the
	// icon's view-box units; nil is the part's own centre. A page turns over the
	// book's spine, so its anchor is the middle of the glyph.
	anchor *ui.Point
	tracks []iconMotionTrack
}

// iconMotion is what choosing one row plays: how long it lasts, the axes that
// move the whole glyph, and the contours that move on their own.
type iconMotion struct {
	duration time.Duration
	tracks   []iconMotionTrack
	parts    []iconPartMotion
	// clipToBox keeps a part that travels beyond the icon's view box from
	// painting over the control beside it.
	clipToBox bool
}

// iconHoverStretch widens every icon gesture without changing its shape. The
// dwell is deliberately unchanged: waiting longer before an icon answers makes
// it feel late, while a slightly longer pass makes the gesture legible at 14 to
// 18px. Hover and click read the same stretched timeline, so one drawing never
// moves at two speeds.
const iconHoverStretch = 1.18

// at returns the transform the motion paints at progress through it. A motion
// with parts allocates them: they belong to the frame being built, and two rows
// can be in motion at once, so they cannot share a buffer.
func (motion iconMotion) at(progress float32) ui.IconTransform {
	elapsed := progress * float32(motion.duration/time.Millisecond)
	transform := readPose(motion.tracks, elapsed).transform()
	transform.ClipToBox = motion.clipToBox
	if len(motion.parts) > 0 {
		transform.Parts = make([]ui.IconPart, len(motion.parts))
		for index, part := range motion.parts {
			own := readPose(part.tracks, elapsed)
			movement := ui.IconPart{
				Contour:   part.contour,
				Rotate:    own.turn,
				Scale:     own.scale,
				ScaleX:    own.stretch,
				Translate: ui.Point{X: own.across, Y: own.down},
				Anchor:    part.anchor,
				Fade:      own.fade,
			}
			if own.trimmed {
				movement.Trim = ui.IconTrim{Active: true, From: 0, To: own.trim}
			}
			transform.Parts[index] = movement
		}
	}
	return transform
}

// span is the time the motion takes once the shared hover stretch is applied.
func (motion iconMotion) span() time.Duration {
	return time.Duration(float64(motion.duration) * iconHoverStretch)
}

// pose is what one set of axes holds at one instant: the four values a motion
// writes, before they become a transform.
type pose struct {
	turn, scale, across, down float32
	reveal, stretch           float32
	trim                      float32
	fade                      float32
	trimmed                   bool
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
		case iconAxisTrim:
			values.trim, values.trimmed = value, true
		case iconAxisFade:
			values.fade = value
		}
	}
	return values
}

// transform turns the values into the transform the painter takes.
func (values pose) transform() ui.IconTransform {
	return ui.IconTransform{
		Rotate: values.turn,
		Scale:  values.scale,
		Offset: ui.Point{X: values.across, Y: values.down},
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
		// Interpolate without ui.Lerp: it clamps progress to [0, 1], and a spring
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
// control points ui.CubicBezier takes.
var (
	easeTurn    = ui.CubicBezier(0.2, 0, 0.3, 1)   // turning into place
	easeSqueeze = ui.CubicBezier(0.33, 0, 0.67, 1) // the middle of a squash
	easeRockIn  = ui.CubicBezier(0.25, 0, 0.95, 1) // swinging out
	easeRockOut = ui.CubicBezier(1, 0, 0.95, 1)    // and back past it
	easeSettle  = ui.CubicBezier(1, 0, 0.75, 1)    // a small overshoot returning
	easeSink    = ui.CubicBezier(0.05, 0, 0, 1)    // opening and dropping
	// A part that comes back lands instead of stopping: the spring is what makes
	// a key feel pressed and a shackle feel like it closed.
	easeLand = ui.Spring(0.68)
)

// iconSpine is the middle of a glyph's view box, where an open book is drawn
// from: the line a page turns over.
var iconSpine = ui.Point{X: 12, Y: 12}

// iconClockFace is the centre of the clock inside the session-log glyph.
var iconClockFace = ui.Point{X: 8, Y: 16}

// iconStickyFold is where the sticky note's folded corner meets its top edge.
var iconStickyFold = ui.Point{X: 15, Y: 3}

// iconFolderHinge is where the folder's lip meets the body on the left of the
// mouth. The lip turns about this point; the pocket stays as the fixed frame.
var iconFolderHinge = ui.Point{X: 6, Y: 14}

// iconBroadcastSource is the centre of the aerial. Every wave expands from this
// point, rather than growing around the centre of its own bounds.
var iconBroadcastSource = ui.Point{X: 12, Y: 9}

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

	// iconMotionFold turns the right page of a ledger toward its spine. The spine
	// and left page stay put; only the page carrying the motion narrows, holds as
	// if being read, then opens again with a small overshoot.
	iconMotionFold = iconMotion{
		duration: 920 * time.Millisecond,
		parts: []iconPartMotion{
			{contour: 2, anchor: &iconSpine, tracks: []iconMotionTrack{
				{axis: iconAxisStretch, stops: []iconMotionStop{
					{at: 0, value: 1, ease: easeSqueeze},
					{at: 70, value: 0.96, ease: easeSink},
					{at: 300, value: 0.62, ease: easeLand},
					{at: 520, value: 0.62, ease: easeRockOut},
					{at: 760, value: 1.03, ease: easeLand},
					{at: 920, value: 1},
				}},
			}},
		},
	}

	// iconMotionClock advances the hands of the session-log clock through four
	// quarter turns. Each step is followed by a real hold, so the icon reads as
	// recorded time passing rather than as a drawing being rotated.
	iconMotionClock = iconMotion{
		duration: 900 * time.Millisecond,
		parts: []iconPartMotion{
			{contour: 3, anchor: &iconClockFace, tracks: []iconMotionTrack{
				{axis: iconAxisTurn, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeTurn},
					{at: 120, value: turn(90), ease: easeTurn},
					{at: 240, value: turn(90), ease: easeTurn},
					{at: 360, value: turn(180), ease: easeTurn},
					{at: 480, value: turn(180), ease: easeTurn},
					{at: 600, value: turn(270), ease: easeTurn},
					{at: 720, value: turn(270), ease: easeTurn},
					{at: 840, value: turn(360)},
					// The full turn and zero are the same picture. Reset on a
					// millisecond so the final frame is exactly the still glyph.
					{at: 841, value: 0},
				}},
			}},
		},
	}

	// iconMotionPeel lifts the sticky note's folded corner, holds it open for a
	// beat, then lets it fall back onto the page.
	iconMotionPeel = iconMotion{
		duration: 860 * time.Millisecond,
		parts: []iconPartMotion{
			{contour: 1, anchor: &iconStickyFold, tracks: []iconMotionTrack{
				{axis: iconAxisTurn, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeSqueeze},
					{at: 90, value: turn(2), ease: easeSink},
					{at: 280, value: turn(-18), ease: easeLand},
					{at: 480, value: turn(-18), ease: easeRockOut},
					{at: 700, value: turn(2), ease: easeLand},
					{at: 860, value: 0},
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

	// iconMotionColumns pulls the split seam toward the new pane, holds it there
	// long enough to read as a second column, then lets the frame close again.
	// Only the divider moves; the rounded frame is the fixed reference.
	iconMotionColumns = iconMotion{
		duration: 900 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 80, value: -0.5, ease: easeSink},
				{at: 300, value: 3, ease: easeLand},
				{at: 500, value: 3, ease: easeRockOut},
				{at: 760, value: -0.35, ease: easeLand},
				{at: 900, value: 0},
			}}),
		},
	}

	// iconMotionFolderOpen swings only the upper lip around the point where it
	// meets the pocket. The body holds still; the lip opens, waits as if a file
	// is being taken out, then drops back with one small overshoot.
	iconMotionFolderOpen = iconMotion{
		duration: 940 * time.Millisecond,
		parts: []iconPartMotion{
			{
				contour: 0,
				anchor:  &iconFolderHinge,
				tracks: []iconMotionTrack{
					{axis: iconAxisTurn, stops: []iconMotionStop{
						{at: 0, value: 0, ease: easeSink},
						{at: 80, value: turn(1.6), ease: easeSqueeze},
						{at: 320, value: turn(-13), ease: easeRockOut},
						{at: 520, value: turn(-13), ease: easeRockIn},
						{at: 760, value: turn(2.5), ease: easeLand},
						{at: 940, value: 0},
					}},
				},
			},
		},
	}

	// iconMotionSwap trades the arrows of a transfer mark without turning the
	// frame around them.
	iconMotionSwap = iconMotion{
		duration: 880 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 1.4, ease: easeLand},
				{at: 880, value: 0},
			}}),
			part(1, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 1.4, ease: easeLand},
				{at: 880, value: 0},
			}}),
			part(2, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -1.4, ease: easeLand},
				{at: 880, value: 0},
			}}),
			part(3, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -1.4, ease: easeLand},
				{at: 880, value: 0},
			}}),
		},
	}

	// iconMotionInspect sweeps a lens across what it is examining and turns it
	// slightly toward the thing underneath.
	iconMotionInspect = iconMotion{
		duration: 820 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -1.1, ease: easeLand},
				{at: 820, value: 0},
			}},
			{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: turn(-7), ease: easeLand},
				{at: 820, value: 0},
			}},
		},
	}

	// iconMotionRays lets a sun's rays travel outward from the still disc.
	iconMotionRays = iconMotion{
		duration: 860 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -1.1, ease: easeLand},
				{at: 860, value: 0},
			}}),
			part(2, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 1.1, ease: easeLand},
				{at: 860, value: 0},
			}}),
			part(3,
				iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeSqueeze},
					{at: 220, value: -0.6, ease: easeLand},
					{at: 860, value: 0},
				}},
				iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeSqueeze},
					{at: 220, value: -0.6, ease: easeLand},
					{at: 860, value: 0},
				}},
			),
			part(4,
				iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeSqueeze},
					{at: 220, value: 0.6, ease: easeLand},
					{at: 860, value: 0},
				}},
				iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeSqueeze},
					{at: 220, value: 0.6, ease: easeLand},
					{at: 860, value: 0},
				}},
			),
			part(5, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -1.1, ease: easeLand},
				{at: 860, value: 0},
			}}),
			part(6, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 1.1, ease: easeLand},
				{at: 860, value: 0},
			}}),
			part(7,
				iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeSqueeze},
					{at: 220, value: 0.6, ease: easeLand},
					{at: 860, value: 0},
				}},
				iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeSqueeze},
					{at: 220, value: -0.6, ease: easeLand},
					{at: 860, value: 0},
				}},
			),
			part(8,
				iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeSqueeze},
					{at: 220, value: -0.6, ease: easeLand},
					{at: 860, value: 0},
				}},
				iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeSqueeze},
					{at: 220, value: 0.6, ease: easeLand},
					{at: 860, value: 0},
				}},
			),
		},
	}

	// iconMotionCrescent rocks the moon on its curve instead of rotating it like
	// a generic switch.
	iconMotionCrescent = iconMotion{
		duration: 820 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: turn(9), ease: easeLand},
				{at: 820, value: 0},
			}},
		},
	}

	// iconMotionCross separates and recloses the two strokes of a close mark.
	iconMotionCross = iconMotion{
		duration: 840 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: turn(-7), ease: easeLand},
				{at: 840, value: 0},
			}}),
			part(1, iconMotionTrack{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: turn(7), ease: easeLand},
				{at: 840, value: 0},
			}}),
		},
	}

	// iconMotionBroadcast emits two waves from the aerial. The inner pair leaves
	// first, the outer pair follows a beat later, and each fades before it can
	// become a detached line at the edge of the box. The tower and its base stay
	// still; the waves return invisibly at rest.
	iconMotionBroadcast = iconMotion{
		duration:  820 * time.Millisecond,
		clipToBox: true,
		parts: []iconPartMotion{
			broadcastArc(1, 0),
			broadcastArc(3, 0),
			broadcastArc(0, 80),
			broadcastArc(4, 80),
		},
	}

	// iconMotionDivider nudges the horizontal divider of a row layout down and
	// seats it back in the frame.
	iconMotionDivider = iconMotion{
		duration: 840 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 1.2, ease: easeLand},
				{at: 840, value: 0},
			}}),
		},
	}

	// iconMotionRecord pulses the dot inside a record ring.
	iconMotionRecord = iconMotion{
		duration: 820 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 220, value: 1.4, ease: easeLand},
				{at: 820, value: 1},
			}}),
		},
	}

	// iconMotionRecordStop compresses the square that ends a recording.
	iconMotionRecordStop = iconMotion{
		duration: 820 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 220, value: 0.8, ease: easeLand},
				{at: 820, value: 1},
			}}),
		},
	}

	// iconMotionFilm advances the sprocket rows of a film strip one frame and
	// lets them settle back.
	iconMotionFilm = iconMotion{
		duration: 860 * time.Millisecond,
		parts: []iconPartMotion{
			part(2, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 0.9, ease: easeLand},
				{at: 860, value: 0},
			}}),
			part(3, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -0.9, ease: easeLand},
				{at: 860, value: 0},
			}}),
			part(4, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 0.9, ease: easeLand},
				{at: 860, value: 0},
			}}),
			part(6, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 0.9, ease: easeLand},
				{at: 860, value: 0},
			}}),
			part(7, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -0.9, ease: easeLand},
				{at: 860, value: 0},
			}}),
		},
	}

	// iconMotionSave lowers an arrow into its tray, and iconMotionExport and
	// iconMotionUpload move theirs in the other direction.
	iconMotionSave = iconMotion{
		duration: 840 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 1.2, ease: easeLand},
				{at: 840, value: 0},
			}}),
			part(2, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 1.2, ease: easeLand},
				{at: 840, value: 0},
			}}),
		},
	}

	iconMotionExport = iconMotion{
		duration: 840 * time.Millisecond,
		parts: []iconPartMotion{
			part(2, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 1.1, ease: easeLand},
				{at: 840, value: 0},
			}}),
			part(3, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 1.1, ease: easeLand},
				{at: 840, value: 0},
			}}),
		},
	}

	iconMotionUpload = iconMotion{
		duration: 840 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -1.1, ease: easeLand},
				{at: 840, value: 0},
			}}),
			part(2, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -1.1, ease: easeLand},
				{at: 840, value: 0},
			}}),
		},
	}

	// iconMotionFolderUp carries a folder's arrow out of the folder.
	iconMotionFolderUp = iconMotion{
		duration: 840 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -1.0, ease: easeLand},
				{at: 840, value: 0},
			}}),
			part(2, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -1.0, ease: easeLand},
				{at: 840, value: 0},
			}}),
		},
	}

	// iconMotionFolderPlus opens the plus inside a folder without moving the
	// folder itself.
	iconMotionFolderPlus = iconMotion{
		duration: 840 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 220, value: 1.28, ease: easeLand},
				{at: 840, value: 1},
			}}),
			part(1, iconMotionTrack{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 220, value: 1.28, ease: easeLand},
				{at: 840, value: 1},
			}}),
		},
	}

	// iconMotionFileTransfer exchanges the arrows of the file-transfer mark.
	iconMotionFileTransfer = iconMotion{
		duration: 860 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -0.9, ease: easeLand},
				{at: 860, value: 0},
			}}),
			part(2, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: 0.9, ease: easeLand},
				{at: 860, value: 0},
			}}),
		},
	}

	// iconMotionBrackets opens the frame around one workload pulse, holds it
	// open, then closes it again. The translation is in this glyph's 1024-unit
	// view box: at 16px, 78 units is about 1.2px per side.
	iconMotionBrackets = iconMotion{
		duration: 900 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 70, value: -14, ease: easeSink},
				{at: 250, value: 78, ease: easeRockOut},
				{at: 520, value: 78, ease: easeRockIn},
				{at: 780, value: -12, ease: easeLand},
				{at: 900, value: 0},
			}}),
			part(1, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 70, value: 14, ease: easeSink},
				{at: 250, value: -78, ease: easeRockOut},
				{at: 520, value: -78, ease: easeRockIn},
				{at: 780, value: 12, ease: easeLand},
				{at: 900, value: 0},
			}}),
			part(2, iconMotionTrack{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 70, value: 0.96, ease: easeSink},
				{at: 270, value: 1.16, ease: easeLand},
				{at: 520, value: 1.16, ease: easeRockOut},
				{at: 780, value: 0.99, ease: easeLand},
				{at: 900, value: 1},
			}}),
		},
	}

	// iconMotionRefresh indexes a driven turn clockwise and returns.
	iconMotionRefresh = iconMotion{
		duration: 820 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: turn(11), ease: easeLand},
				{at: 820, value: 0},
			}},
		},
	}

	// iconMotionRise and iconMotionFall make an arrow travel the way it points,
	// then settle back onto its baseline.
	iconMotionRise = iconMotion{
		duration: 820 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 240, value: -1.3, ease: easeLand},
				{at: 820, value: 0},
			}},
		},
	}

	iconMotionFall = iconMotion{
		duration: 820 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 240, value: 1.3, ease: easeLand},
				{at: 820, value: 0},
			}},
		},
	}

	// iconMotionPlus opens the two arms of a plus away from their crossing and
	// closes them on their original drawing.
	iconMotionPlus = iconMotion{
		duration: 840 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 230, value: 1.22, ease: easeLand},
				{at: 840, value: 1},
			}}),
			part(1, iconMotionTrack{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 230, value: 1.22, ease: easeLand},
				{at: 840, value: 1},
			}}),
		},
	}

	// iconMotionDots lets the dots of an ellipsis gather attention in sequence
	// without moving the control.
	iconMotionDots = iconMotion{
		duration: 840 * time.Millisecond,
		parts: []iconPartMotion{
			dotPulse(0, 0),
			dotPulse(1, 70),
			dotPulse(2, 140),
		},
	}

	// iconMotionClip flexes a paperclip as if it were being opened over a sheet.
	iconMotionClip = iconMotion{
		duration: 840 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 230, value: turn(-8), ease: easeLand},
				{at: 840, value: 0},
			}},
			{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 230, value: 1.08, ease: easeLand},
				{at: 840, value: 1},
			}},
		},
	}

	// iconMotionInquire rocks the question mark of a help mark while its dot
	// acknowledges the pointer.
	iconMotionInquire = iconMotion{
		duration: 860 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 230, value: turn(8), ease: easeLand},
				{at: 860, value: 0},
			}}),
			dotPulse(2, 120),
		},
	}

	// iconMotionDraw tilts a pencil onto the surface and lays its nib back down.
	// The body and nib line move together, so it reads as one drawing stroke.
	iconMotionDraw = iconMotion{
		duration: 960 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 260, value: turn(-8), ease: easeLand},
				{at: 960, value: 0},
			}},
			{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 260, value: 0.8, ease: easeLand},
				{at: 960, value: 0},
			}},
		},
	}

	// iconMotionLid lifts a bin's lid clear of its body and closes it again.
	iconMotionLid = iconMotion{
		duration: 960 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 260, value: -2.2, ease: easeLand},
				{at: 960, value: 0},
			}}),
		},
	}

	// iconMotionStar makes a star swell once, the way its outline lights up when
	// it becomes a favourite. Star and StarSolid share it so the two states move
	// alike even though their drawings differ.
	iconMotionStar = iconMotion{
		duration: 920 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 260, value: 1.16, ease: easeLand},
				{at: 920, value: 1},
			}},
		},
	}

	// iconMotionPlay nudges a play triangle in the direction it points.
	iconMotionPlay = iconMotion{
		duration: 920 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 250, value: 1.2, ease: easeLand},
				{at: 920, value: 0},
			}},
		},
	}

	// iconMotionPause lets the two bars fall inward toward each other and settle
	// apart again: the drawing of a pause starting.
	iconMotionPause = iconMotion{
		duration: 920 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 250, value: -0.7, ease: easeLand},
				{at: 920, value: 0},
			}}),
			part(1, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 250, value: 0.7, ease: easeLand},
				{at: 920, value: 0},
			}}),
		},
	}

	// iconMotionRewind turns a history clock's hand back, then lets it settle on
	// the recorded time.
	iconMotionRewind = iconMotion{
		duration: 960 * time.Millisecond,
		parts: []iconPartMotion{
			part(2, iconMotionTrack{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 250, value: turn(16), ease: easeLand},
				{at: 960, value: 0},
			}}),
			part(1, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 250, value: -0.5, ease: easeLand},
				{at: 960, value: 0},
			}}),
		},
	}

	// iconMotionCopy slides the page being copied off the page underneath.
	iconMotionCopy = iconMotion{
		duration: 940 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 250, value: 1.2, ease: easeLand},
				{at: 940, value: 0},
			}}, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 250, value: 1.2, ease: easeLand},
				{at: 940, value: 0},
			}}),
		},
	}

	// The chevrons lean the way they point and come back onto their centre.
	iconMotionNudgeRight = iconMotion{
		duration: 880 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 240, value: 1.2, ease: easeLand},
				{at: 880, value: 0},
			}},
		},
	}

	iconMotionNudgeLeft = iconMotion{
		duration: 880 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 240, value: -1.2, ease: easeLand},
				{at: 880, value: 0},
			}},
		},
	}

	iconMotionNudgeDown = iconMotion{
		duration: 880 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 240, value: 1.2, ease: easeLand},
				{at: 880, value: 0},
			}},
		},
	}

	// iconMotionSliders moves the three knobs along their own tracks, each by the
	// distance its track has room for.
	iconMotionSliders = iconMotion{
		duration: 980 * time.Millisecond,
		parts: []iconPartMotion{
			part(6, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 260, value: -2.2, ease: easeLand},
				{at: 980, value: 0},
			}}),
			part(7, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 40, value: 0, ease: easeSqueeze},
				{at: 300, value: 2.2, ease: easeLand},
				{at: 980, value: 0},
			}}),
			part(8, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 80, value: 0, ease: easeSqueeze},
				{at: 340, value: -2.6, ease: easeLand},
				{at: 980, value: 0},
			}}),
		},
	}

	// iconMotionEye looks left and right inside the still outline.
	iconMotionEye = iconMotion{
		duration: 940 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 220, value: -1.1, ease: easeLand},
				{at: 520, value: 1.1, ease: easeLand},
				{at: 940, value: 0},
			}}),
		},
	}

	// iconMotionEyeOff draws the slash a little farther across, as if closing the
	// view, and lets it return.
	iconMotionEyeOff = iconMotion{
		duration: 940 * time.Millisecond,
		parts: []iconPartMotion{
			part(3,
				iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeSqueeze},
					{at: 250, value: 0.8, ease: easeLand},
					{at: 940, value: 0},
				}},
				iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
					{at: 0, value: 0, ease: easeSqueeze},
					{at: 250, value: 0.8, ease: easeLand},
					{at: 940, value: 0},
				}},
			),
		},
	}

	// iconMotionPicture lets the sun in a photograph brighten while the mountains
	// settle by a pixel.
	iconMotionPicture = iconMotion{
		duration: 960 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisScale, stops: []iconMotionStop{
				{at: 0, value: 1, ease: easeSqueeze},
				{at: 250, value: 1.25, ease: easeLand},
				{at: 960, value: 1},
			}}),
			part(2, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 250, value: 0.8, ease: easeLand},
				{at: 960, value: 0},
			}}),
		},
	}

	// iconMotionProfiles fans three saved places apart and closes them into a
	// single column again: the glyph is a stack of cards, so the stack is what
	// moves.
	iconMotionProfiles = iconMotion{
		duration: 900 * time.Millisecond,
		parts: []iconPartMotion{
			part(0, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 260, value: -1.2, ease: easeLand},
				{at: 900, value: 0},
			}}),
			part(1, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 30, value: 0, ease: easeSqueeze},
				{at: 290, value: -1.2, ease: easeLand},
				{at: 900, value: 0},
			}}),
			part(2, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 60, value: 0, ease: easeSqueeze},
				{at: 320, value: 1.2, ease: easeLand},
				{at: 900, value: 0},
			}}),
		},
	}

	// iconMotionSync turns the two halves of a browse-sync mark in opposite
	// directions, the way a pair of folders trade their contents.
	iconMotionSync = iconMotion{
		duration: 920 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 260, value: -0.8, ease: easeLand},
				{at: 920, value: 0},
			}}),
			part(2, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 60, value: 0, ease: easeSqueeze},
				{at: 320, value: 0.8, ease: easeLand},
				{at: 920, value: 0},
			}}),
		},
	}

	// iconMotionDrive tilts a drive's platter and settles it back level.
	iconMotionDrive = iconMotion{
		duration: 880 * time.Millisecond,
		tracks: []iconMotionTrack{
			{axis: iconAxisTurn, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 240, value: turn(-3), ease: easeLand},
				{at: 880, value: 0},
			}},
		},
	}

	// iconMotionCompose lifts the nib of a square pen away from its page and
	// brings it back down, the gesture of opening an editor on a file.
	iconMotionCompose = iconMotion{
		duration: 900 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisAcross, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 240, value: 0.9, ease: easeLand},
				{at: 900, value: 0},
			}}, iconMotionTrack{axis: iconAxisDown, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 240, value: -0.9, ease: easeLand},
				{at: 900, value: 0},
			}}),
		},
	}

	// iconMotionConfirm draws a check inside its still ring and leaves it there:
	// the completed check is the answer a re-trust action gives. It must not
	// retrace to an empty ring.
	iconMotionConfirm = iconMotion{
		duration: 900 * time.Millisecond,
		parts: []iconPartMotion{
			part(1, iconMotionTrack{axis: iconAxisTrim, stops: []iconMotionStop{
				{at: 0, value: 0, ease: easeSqueeze},
				{at: 260, value: 1, ease: easeLand},
				{at: 620, value: 1},
			}}),
		},
	}
)

// broadcastArc spreads one pair of waves from the aerial and hides it before
// the expanded contour reaches the icon's edge. The arc is reset while faded,
// then fades back in at rest so the next hover starts from the same picture.
func broadcastArc(contour int, delay float32) iconPartMotion {
	return iconPartMotion{
		contour: contour,
		anchor:  &iconBroadcastSource,
		tracks: []iconMotionTrack{
			{axis: iconAxisScale, stops: []iconMotionStop{
				{at: delay, value: 1, ease: easeSqueeze},
				{at: delay + 20, value: 1},
				{at: delay + 350, value: 1.30, ease: easeLand},
				{at: delay + 390, value: 1.34},
				{at: delay + 440, value: 1},
				{at: 820, value: 1},
			}},
			{axis: iconAxisFade, stops: []iconMotionStop{
				{at: delay, value: 0},
				{at: delay + 120, value: 0},
				{at: delay + 350, value: 0.72},
				{at: delay + 390, value: 1},
				{at: delay + 440, value: 1},
				{at: delay + 600, value: 1},
				{at: delay + 700, value: 0},
				{at: 820, value: 0},
			}},
		},
	}
}

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

// dotPulse enlarges one dot and lets it settle, start milliseconds into the
// motion.
func dotPulse(contour int, start float32) iconPartMotion {
	return part(contour, iconMotionTrack{axis: iconAxisScale, stops: []iconMotionStop{
		{at: start, value: 1, ease: easeSqueeze},
		{at: start + 150, value: 1.45, ease: easeLand},
		{at: start + 390, value: 1},
	}})
}

// iconGlyphGroup names one actual drawing, not one screen or action. Every kind
// that paints the same drawing belongs to the same group and therefore plays
// the same gesture: Settings in the sidebar, the window bar and settings rows;
// Terminal in the local-terminal row and every compact terminal action; the
// globe in Proxies and the local proxy.
type iconGlyphGroup uint8

const (
	iconGlyphSettings iconGlyphGroup = iota
	iconGlyphTerminal
	iconGlyphForward
	iconGlyphFolderOpen
	iconGlyphArchive
	iconGlyphLink
	iconGlyphBook
	iconGlyphActivity
	iconGlyphLock
	iconGlyphKey
	iconGlyphGlobe
	iconGlyphServer
	iconGlyphKeyboard
	iconGlyphBot
	iconGlyphPlug
	iconGlyphColumns
	iconGlyphNote
	iconGlyphInfo
	iconGlyphBug
	iconGlyphFileClock
	iconGlyphGrid
	iconGlyphTransfer
	iconGlyphSearch
	iconGlyphSun
	iconGlyphMoon
	iconGlyphClose
	iconGlyphBroadcast
	iconGlyphDivider
	iconGlyphRecord
	iconGlyphStop
	iconGlyphFilm
	iconGlyphDownload
	iconGlyphFileDown
	iconGlyphFileTransfer
	iconGlyphProcess
	iconGlyphUpload
	iconGlyphFolderUp
	iconGlyphFolderPlus
	iconGlyphRefresh
	iconGlyphArrowUp
	iconGlyphArrowDown
	iconGlyphPlus
	iconGlyphEllipsis
	iconGlyphPaperclip
	iconGlyphHelp
	iconGlyphPencil
	iconGlyphTrash
	iconGlyphStar
	iconGlyphStarSolid
	iconGlyphPlay
	iconGlyphPause
	iconGlyphHistory
	iconGlyphCopy
	iconGlyphChevronRight
	iconGlyphChevronLeft
	iconGlyphChevronDown
	iconGlyphSliders
	iconGlyphEye
	iconGlyphEyeOff
	iconGlyphImage
	iconGlyphProfiles
	iconGlyphSync
	iconGlyphDrive
	iconGlyphCompose
	iconGlyphConfirm
)

// iconGlyphs is one motion per drawing. Aliases share an entry: Settings has one
// gesture, not one for each place it is painted.
var iconGlyphs = map[iconGlyphGroup]iconMotion{
	iconGlyphSettings:     iconMotionTurn,
	iconGlyphTerminal:     iconMotionCursor,
	iconGlyphForward:      iconMotionExchange,
	iconGlyphFolderOpen:   iconMotionFolderOpen,
	iconGlyphArchive:      iconMotionUnlid,
	iconGlyphLink:         iconMotionRock,
	iconGlyphBook:         iconMotionFold,
	iconGlyphActivity:     iconMotionTrace,
	iconGlyphLock:         iconMotionUnlock,
	iconGlyphKey:          iconMotionKey,
	iconGlyphGlobe:        iconMotionWorld,
	iconGlyphServer:       iconMotionRack,
	iconGlyphKeyboard:     iconMotionType,
	iconGlyphBot:          iconMotionBlink,
	iconGlyphPlug:         iconMotionPlug,
	iconGlyphColumns:      iconMotionColumns,
	iconGlyphNote:         iconMotionPeel,
	iconGlyphInfo:         iconMotionSquash,
	iconGlyphBug:          iconMotionSquash,
	iconGlyphFileClock:    iconMotionClock,
	iconGlyphGrid:         iconMotionSquash,
	iconGlyphTransfer:     iconMotionSwap,
	iconGlyphSearch:       iconMotionInspect,
	iconGlyphSun:          iconMotionRays,
	iconGlyphMoon:         iconMotionCrescent,
	iconGlyphClose:        iconMotionCross,
	iconGlyphBroadcast:    iconMotionBroadcast,
	iconGlyphDivider:      iconMotionDivider,
	iconGlyphRecord:       iconMotionRecord,
	iconGlyphStop:         iconMotionRecordStop,
	iconGlyphFilm:         iconMotionFilm,
	iconGlyphDownload:     iconMotionSave,
	iconGlyphFileDown:     iconMotionExport,
	iconGlyphFileTransfer: iconMotionFileTransfer,
	iconGlyphProcess:      iconMotionBrackets,
	iconGlyphUpload:       iconMotionUpload,
	iconGlyphFolderUp:     iconMotionFolderUp,
	iconGlyphFolderPlus:   iconMotionFolderPlus,
	iconGlyphRefresh:      iconMotionRefresh,
	iconGlyphArrowUp:      iconMotionRise,
	iconGlyphArrowDown:    iconMotionFall,
	iconGlyphPlus:         iconMotionPlus,
	iconGlyphEllipsis:     iconMotionDots,
	iconGlyphPaperclip:    iconMotionClip,
	iconGlyphHelp:         iconMotionInquire,
	iconGlyphPencil:       iconMotionDraw,
	iconGlyphTrash:        iconMotionLid,
	iconGlyphStar:         iconMotionStar,
	iconGlyphStarSolid:    iconMotionStar,
	iconGlyphPlay:         iconMotionPlay,
	iconGlyphPause:        iconMotionPause,
	iconGlyphHistory:      iconMotionRewind,
	iconGlyphCopy:         iconMotionCopy,
	iconGlyphChevronRight: iconMotionNudgeRight,
	iconGlyphChevronLeft:  iconMotionNudgeLeft,
	iconGlyphChevronDown:  iconMotionNudgeDown,
	iconGlyphSliders:      iconMotionSliders,
	iconGlyphEye:          iconMotionEye,
	iconGlyphEyeOff:       iconMotionEyeOff,
	iconGlyphImage:        iconMotionPicture,
	iconGlyphProfiles:     iconMotionProfiles,
	iconGlyphSync:         iconMotionSync,
	iconGlyphDrive:        iconMotionDrive,
	iconGlyphCompose:      iconMotionCompose,
	iconGlyphConfirm:      iconMotionConfirm,
}

// iconGlyphMembers declares every kind by the drawing it paints. A kind absent
// here has no gesture, which is how static badges and illustrations stay still.
var iconGlyphMembers = map[iconGlyphGroup][]uint8{
	iconGlyphSettings: {
		iconSettings,
	},
	iconGlyphTerminal: {
		iconTerminal,
	},
	iconGlyphForward: {
		iconForward,
	},
	iconGlyphFolderOpen: {
		iconFolderOpen,
	},
	iconGlyphArchive: {
		iconArchive,
	},
	iconGlyphLink: {
		iconLink,
	},
	iconGlyphBook: {
		iconKnown,
	},
	iconGlyphActivity: {
		iconActivity,
	},
	iconGlyphLock: {
		iconLock,
	},
	iconGlyphKey: {
		iconKey,
	},
	iconGlyphGlobe: {
		iconGlobe,
	},
	iconGlyphServer: {
		iconHosts,
	},
	iconGlyphKeyboard: {
		iconKeyboard,
	},
	iconGlyphBot: {
		iconBot,
	},
	iconGlyphPlug: {
		iconPlug,
	},
	iconGlyphColumns: {
		iconColumns2,
	},
	iconGlyphNote: {
		iconNote,
	},
	iconGlyphInfo: {
		iconInfo,
	},
	iconGlyphBug: {
		iconBug,
	},
	iconGlyphFileClock: {
		iconFileClock,
	},
	iconGlyphGrid: {
		iconGrid2x2,
		iconHome,
	},
	iconGlyphTransfer: {
		iconTransfer,
	},
	iconGlyphSearch: {
		iconSearch,
	},
	iconGlyphSun: {
		iconSun,
	},
	iconGlyphMoon: {
		iconMoon,
	},
	iconGlyphClose: {
		iconClose,
	},
	iconGlyphBroadcast: {
		iconRadio,
	},
	iconGlyphDivider: {
		iconRows2,
	},
	iconGlyphRecord: {
		iconRecord,
	},
	iconGlyphStop: {
		iconStop,
		iconRecordStop,
	},
	iconGlyphFilm: {
		iconFilm,
	},
	iconGlyphDownload: {
		iconDownload,
		iconSave,
	},
	iconGlyphFileDown: {
		iconFileDown,
	},
	iconGlyphFileTransfer: {
		iconSFTPTransfer,
	},
	iconGlyphProcess: {
		iconProcess,
	},
	iconGlyphUpload: {
		iconUpload,
	},
	iconGlyphFolderUp: {
		iconFolderUp,
	},
	iconGlyphFolderPlus: {
		iconFolderPlus,
	},
	iconGlyphRefresh: {
		iconRefresh,
	},
	iconGlyphArrowUp: {
		iconArrowUp,
	},
	iconGlyphArrowDown: {
		iconArrowDown,
	},
	iconGlyphPlus: {
		iconPlus,
	},
	iconGlyphEllipsis: {
		iconEllipsis,
	},
	iconGlyphPaperclip: {
		iconPaperclip,
	},
	iconGlyphHelp: {
		iconHelp,
	},
	iconGlyphPencil: {
		iconPencil,
	},
	iconGlyphTrash: {
		iconTrash,
	},
	iconGlyphStar: {
		iconStar,
	},
	iconGlyphStarSolid: {
		iconStarSolid,
	},
	iconGlyphPlay: {
		iconPlay,
	},
	iconGlyphPause: {
		iconPause,
	},
	iconGlyphHistory: {
		iconHistory,
	},
	iconGlyphCopy: {
		iconCopy,
	},
	iconGlyphChevronRight: {
		iconChevronRight,
	},
	iconGlyphChevronLeft: {
		iconChevronLeft,
	},
	iconGlyphChevronDown: {
		iconChevronDown,
	},
	iconGlyphSliders: {
		iconSliders,
	},
	iconGlyphEye: {
		iconEye,
	},
	iconGlyphEyeOff: {
		iconEyeOff,
	},
	iconGlyphImage: {
		iconImage,
	},
	iconGlyphProfiles: {
		iconSFTPProfiles,
	},
	iconGlyphSync: {
		iconSFTPBrowseSync,
	},
	iconGlyphDrive: {
		iconHardDrive,
	},
	iconGlyphCompose: {
		iconExternal,
	},
	iconGlyphConfirm: {
		iconCircleCheck,
	},
}

var iconGlyphByKind = func() map[uint8]iconGlyphGroup {
	resolved := make(map[uint8]iconGlyphGroup, len(lucideIcons))
	for group, kinds := range iconGlyphMembers {
		for _, kind := range kinds {
			if first, ok := resolved[kind]; ok && first != group {
				panic(fmt.Sprintf("icon kind %d belongs to glyph groups %d and %d", kind, first, group))
			}
			resolved[kind] = group
		}
	}
	return resolved
}()

// iconDrawingKey identifies a glyph without allocating on the paint path.
// Compiled icons share immutable command and layer storage across their kinds,
// so that storage plus its length is the drawing's identity.
type iconDrawingKey struct {
	viewBox, viewHeight, stroke float32
	cap                         ui.StrokeCap
	join                        ui.StrokeJoin
	commands                    *ui.PathCommand
	layers                      *ui.IconLayer
	commandCount, layerCount    int
}

func iconDrawingKeyOf(icon ui.Icon) iconDrawingKey {
	key := iconDrawingKey{
		viewBox: icon.ViewBox, viewHeight: icon.ViewHeight, stroke: icon.Stroke,
		cap: icon.Cap, join: icon.Join, commandCount: len(icon.Commands), layerCount: len(icon.Layers),
	}
	if len(icon.Commands) > 0 {
		key.commands = &icon.Commands[0]
	}
	if len(icon.Layers) > 0 {
		key.layers = &icon.Layers[0]
	}
	return key
}

// iconDrawingContent spells the drawing for a copy that does not share the
// compiled storage. It is only needed on a storage-index miss.
func iconDrawingContent(icon ui.Icon) string {
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

// iconGlyphFor resolves a kind through the drawing group that owns it.
func iconGlyphFor(kind uint8) (iconGlyphGroup, bool) {
	group, ok := iconGlyphByKind[kind]
	return group, ok
}

// iconGlyphKinds returns the kinds in a group.
func iconGlyphKinds(group iconGlyphGroup) []uint8 {
	return iconGlyphMembers[group]
}

// iconGlyphByDrawing resolves the exact drawing a control holds to its glyph
// group. Building it once keeps a paint pass from comparing every registered
// icon, and means Viva components and raw drawing share the same decision as a
// Motty icon kind.
var iconGlyphByDrawing, iconGlyphByContent = func() (map[iconDrawingKey]iconGlyphGroup, map[string]iconGlyphGroup) {
	byStorage := make(map[iconDrawingKey]iconGlyphGroup, len(lucideIcons))
	byContent := make(map[string]iconGlyphGroup, len(lucideIcons))
	for kind := range lucideIcons {
		group, ok := iconGlyphFor(uint8(kind))
		if !ok {
			continue
		}
		icon := lucideIcons[kind]
		storageKey, contentKey := iconDrawingKeyOf(icon), iconDrawingContent(icon)
		if first, exists := byStorage[storageKey]; exists && first != group {
			panic(fmt.Sprintf("the same drawing storage belongs to glyph groups %d and %d", first, group))
		}
		if first, exists := byContent[contentKey]; exists && first != group {
			panic(fmt.Sprintf("the same drawing belongs to glyph groups %d and %d", first, group))
		}
		byStorage[storageKey], byContent[contentKey] = group, group
	}
	return byStorage, byContent
}()

// iconMotionForDrawing resolves an externally held drawing through the same
// glyph groups as the compiled icon kinds. A drawing absent from the groups has
// no gesture.
func iconMotionForDrawing(icon ui.Icon) (iconMotion, bool) {
	group, ok := iconGlyphForDrawing(icon)
	if !ok {
		return iconMotion{}, false
	}
	motion, ok := iconGlyphs[group]
	return motion, ok
}

// iconGlyphForDrawing resolves the drawing group of an externally held icon.
func iconGlyphForDrawing(icon ui.Icon) (iconGlyphGroup, bool) {
	if group, ok := iconGlyphByDrawing[iconDrawingKeyOf(icon)]; ok {
		return group, true
	}
	group, ok := iconGlyphByContent[iconDrawingContent(icon)]
	return group, ok
}

// iconMotionFor is the motion a glyph plays, whichever control owns it.
func iconMotionFor(icon uint8) (iconMotion, bool) {
	group, ok := iconGlyphFor(icon)
	if !ok {
		return iconMotion{}, false
	}
	motion, ok := iconGlyphs[group]
	return motion, ok
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
func (state *State) pressIconMotion(id ui.ID) {
	if !playsIconMotion(id) {
		return
	}
	if state.iconMotions == nil {
		state.iconMotions = make(map[ui.ID]iconMotionRun, 2)
	}
	state.iconMotions[id] = iconMotionPending
}

// playsIconMotion reports whether choosing id animates an icon: the rows of the
// sidebar and the entries of the settings list are the two places that draw one.
// A row added to either place needs its id here, and its icon in iconMotionFor.
func playsIconMotion(id ui.ID) bool {
	if id.Scope() == hitSettingsCategoryList || devPlaysIconMotion(id) {
		return true
	}
	switch id {
	case hitHosts, hitWorkspaces, hitDashboard, hitSessionLogs, hitKeychain, hitProxies, hitPortForwarding,
		hitKnownHosts, hitSnippets, hitLocalTerminal, hitSettings:
		return true
	}
	return false
}

// iconMotion returns the transform to draw the row's icon with on this frame,
// advancing the motion the click recorded. A row that is not in motion gets the
// zero transform, which paints the still icon on the cached path.
func (state *State) iconMotion(canvas *ui.Canvas, id ui.ID, icon uint8) ui.IconTransform {
	run, running := state.iconMotions[id]
	if !running {
		return ui.IconTransform{}
	}
	motion, ok := iconMotionFor(icon)
	if !ok {
		// A row whose glyph has no motion, such as one added after this table,
		// paints still; it must not wait for a frame that would never come.
		delete(state.iconMotions, id)
		return ui.IconTransform{}
	}
	if run == iconMotionPending {
		// A slot starts settled at the value it is first asked for, so the clock
		// has to be pinned to zero before the motion asks for one; otherwise the
		// first frame of the motion would already be its end.
		canvas.Animate(id, iconMotionSlot, 0, ui.Motion{})
		state.iconMotions[id] = iconMotionPlaying
	}
	progress := canvas.Animate(id, iconMotionSlot, 1, ui.Motion{Duration: motion.span(), Easing: ui.EaseLinear})
	if progress >= 1 {
		// Over, or never started: reduced motion, a still preview and a canvas
		// without a frame clock all arrive here with 1.
		delete(state.iconMotions, id)
		return ui.IconTransform{}
	}
	return motion.at(progress)
}
