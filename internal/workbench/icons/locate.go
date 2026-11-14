package icons

import "github.com/vivaui/viva/gfx"

// locate-fixed is a compiled vector icon.
var LocateFixed = gfx.Icon{
	ViewBox: 24.0,
	Stroke:  2.0,
	Cap:     gfx.CapRound,
	Join:    gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(2.0, 12.0),
		gfx.LineTo(5.0, 12.0),
		gfx.MoveTo(19.0, 12.0),
		gfx.LineTo(22.0, 12.0),
		gfx.MoveTo(12.0, 2.0),
		gfx.LineTo(12.0, 5.0),
		gfx.MoveTo(12.0, 19.0),
		gfx.LineTo(12.0, 22.0),
		gfx.MoveTo(19.0, 12.0),
		gfx.CubicTo(19.0, 15.866, 15.866, 19.0, 12.0, 19.0),
		gfx.CubicTo(8.13401, 19.0, 5.0, 15.866, 5.0, 12.0),
		gfx.CubicTo(5.0, 8.13401, 8.13401, 5.0, 12.0, 5.0),
		gfx.CubicTo(15.866, 5.0, 19.0, 8.13401, 19.0, 12.0),
		gfx.ClosePath(),
		gfx.MoveTo(13.0, 12.0),
		gfx.CubicTo(13.0, 12.5523, 12.5523, 13.0, 12.0, 13.0),
		gfx.CubicTo(11.4477, 13.0, 11.0, 12.5523, 11.0, 12.0),
		gfx.CubicTo(11.0, 11.4477, 11.4477, 11.0, 12.0, 11.0),
		gfx.CubicTo(12.5523, 11.0, 13.0, 11.4477, 13.0, 12.0),
		gfx.ClosePath(),
	},
}
