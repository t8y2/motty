// Package termsize is the size of a terminal: its grid in cells and its text
// area in device pixels.
//
// These are the four numbers a pty's window size holds — TIOCGWINSZ's rows,
// cols, xpixel and ypixel — and what an SSH channel's pty-req and window-change
// carry. A program that lays images out by the character cell, as the kitty
// graphics protocol does, computes the cell as the text area divided by the
// grid, so a transport that reports cells but invents the pixels (or the other
// way around) makes its pictures the wrong size. A transport whose protocol
// carries only cells, as mosh and telnet's NAWS do, leaves the pixel fields at
// zero rather than filling them with a guess.
package termsize

// Size is one terminal size, all numbers in their natural units: cells for the
// grid, device pixels for the text area. The zero Size says nothing is known.
type Size struct {
	Cols, Rows              int
	PixelWidth, PixelHeight int
}
