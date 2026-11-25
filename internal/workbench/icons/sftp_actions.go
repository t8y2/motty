package icons

import "github.com/vivaui/viva/gfx"

var (
	// sftpFolderFullCommands is the lowered open folder outline for SftpUp.
	sftpFolderFullCommands = []gfx.PathCommand{
		gfx.MoveTo(2.5, 19.0),
		gfx.LineTo(2.5, 11.0),
		gfx.CubicTo(2.5, 10.0, 3.3, 9.3, 4.3, 9.3),
		gfx.LineTo(7.0, 9.3),
		gfx.CubicTo(7.7, 9.3, 8.3, 9.6, 8.7, 10.2),
		gfx.LineTo(9.6, 11.4),
		gfx.CubicTo(10.0, 11.9, 10.6, 12.2, 11.2, 12.2),
		gfx.LineTo(18.5, 12.2),
		gfx.CubicTo(19.5, 12.2, 20.3, 12.9, 20.5, 13.8),
		gfx.LineTo(20.5, 14.5),
		gfx.MoveTo(4.5, 21.0),
		gfx.LineTo(18.0, 21.0),
		gfx.CubicTo(18.9, 21.0, 19.6, 20.3, 19.8, 19.4),
		gfx.LineTo(21.0, 15.2),
		gfx.CubicTo(21.2, 14.3, 20.6, 13.6, 19.7, 13.6),
		gfx.LineTo(8.8, 13.6),
		gfx.CubicTo(8.0, 13.6, 7.3, 14.1, 6.9, 14.8),
		gfx.LineTo(3.7, 19.7),
		gfx.CubicTo(3.2, 20.5, 3.7, 21.0, 4.5, 21.0),
		gfx.ClosePath(),
	}

	// sftpCurvedReturnArrowCommands represents the clean return arrow for SftpUp.
	sftpCurvedReturnArrowCommands = []gfx.PathCommand{
		gfx.MoveTo(18.5, 13.5),
		gfx.LineTo(18.5, 7.8),
		gfx.CubicTo(18.5, 5.0, 16.5, 4.0, 13.5, 4.0),
		gfx.LineTo(6.5, 4.0),
		gfx.MoveTo(9.5, 1.5),
		gfx.LineTo(6.0, 4.0),
		gfx.LineTo(9.5, 6.5),
	}

	// sftpFileBaseCommands has the document outline with folded dog-ear and 3 left-aligned lines,
	// leaving >= 3.0 units of clear gap around the bottom-right action glyph.
	sftpFileBaseCommands = []gfx.PathCommand{
		gfx.MoveTo(14.0, 2.0),
		gfx.LineTo(5.0, 2.0),
		gfx.CubicTo(3.89543, 2.0, 3.0, 2.89543, 3.0, 4.0),
		gfx.LineTo(3.0, 20.0),
		gfx.CubicTo(3.0, 21.1046, 3.89543, 22.0, 5.0, 22.0),
		gfx.LineTo(12.0, 22.0),
		gfx.MoveTo(18.5, 10.0),
		gfx.LineTo(18.5, 6.5),
		gfx.LineTo(14.0, 2.0),
		gfx.MoveTo(14.0, 2.0),
		gfx.LineTo(14.0, 5.5),
		gfx.CubicTo(14.0, 6.05, 14.45, 6.5, 15.0, 6.5),
		gfx.LineTo(18.5, 6.5),
		gfx.MoveTo(6.5, 9.5), gfx.LineTo(13.0, 9.5),
		gfx.MoveTo(6.5, 13.0), gfx.LineTo(11.5, 13.0),
		gfx.MoveTo(6.5, 16.5), gfx.LineTo(11.0, 16.5),
	}

	// sftpFolderClearedCommands has the open folder outline with bottom-right cleared
	// so the action glyph never touches the folder lines.
	sftpFolderClearedCommands = []gfx.PathCommand{
		gfx.MoveTo(2.0, 18.0),
		gfx.LineTo(2.0, 5.5),
		gfx.CubicTo(2.0, 4.39543, 2.89543, 3.5, 4.0, 3.5),
		gfx.LineTo(7.5, 3.5),
		gfx.CubicTo(8.15, 3.5, 8.75, 3.8, 9.1, 4.35),
		gfx.LineTo(10.0, 5.65),
		gfx.CubicTo(10.35, 6.2, 10.95, 6.5, 11.6, 6.5),
		gfx.LineTo(17.0, 6.5),
		gfx.CubicTo(18.1046, 6.5, 19.0, 7.39543, 19.0, 8.5),
		gfx.LineTo(19.0, 10.0),
		gfx.MoveTo(5.5, 14.5),
		gfx.LineTo(7.0, 11.5),
		gfx.CubicTo(7.35, 10.85, 8.0, 10.5, 8.75, 10.5),
		gfx.LineTo(13.5, 10.5),
		gfx.MoveTo(12.0, 20.5),
		gfx.LineTo(4.0, 20.5),
		gfx.CubicTo(2.89543, 20.5, 2.0, 19.6046, 2.0, 18.5),
	}

	// sftpUpArrowCommands represents the compact, detached upward arrow at the bottom right.
	sftpUpArrowCommands = []gfx.PathCommand{
		gfx.MoveTo(18.5, 13.5),
		gfx.LineTo(18.5, 21.5),
		gfx.MoveTo(15.0, 17.0),
		gfx.LineTo(18.5, 13.5),
		gfx.LineTo(22.0, 17.0),
	}

	// sftpPlusCommands represents the standalone, detached plus sign at the bottom right (no outer circle).
	sftpPlusCommands = []gfx.PathCommand{
		gfx.MoveTo(15.0, 17.5),
		gfx.LineTo(22.0, 17.5),
		gfx.MoveTo(18.5, 14.0),
		gfx.LineTo(18.5, 21.0),
	}
)

// SftpUp is the parent directory icon: an open folder with an arching return arrow pointing left.
var SftpUp = gfx.Icon{
	ViewBox: 24.0, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: append(append([]gfx.PathCommand{}, sftpFolderFullCommands...), sftpCurvedReturnArrowCommands...),
}

// SftpUpload is the upload file icon: a document with a detached upward action arrow.
var SftpUpload = gfx.Icon{
	ViewBox: 24.0, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: append(append([]gfx.PathCommand{}, sftpFileBaseCommands...), sftpUpArrowCommands...),
}

// SftpUploadFolder is the upload folder icon: an open folder with a detached upward action arrow.
var SftpUploadFolder = gfx.Icon{
	ViewBox: 24.0, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: append(append([]gfx.PathCommand{}, sftpFolderClearedCommands...), sftpUpArrowCommands...),
}

// SftpNewFile is the new file icon: a document with a detached plus sign at the bottom right.
var SftpNewFile = gfx.Icon{
	ViewBox: 24.0, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: append(append([]gfx.PathCommand{}, sftpFileBaseCommands...), sftpPlusCommands...),
}

// SftpNewFolder is the new directory icon: an open folder with a detached plus sign at the bottom right.
var SftpNewFolder = gfx.Icon{
	ViewBox: 24.0, Stroke: 1.6, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: append(append([]gfx.PathCommand{}, sftpFolderClearedCommands...), sftpPlusCommands...),
}
