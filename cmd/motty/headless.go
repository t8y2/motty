//go:build vivahead && (darwin || windows || linux)

package main

// Built with -tags vivahead, a process started with VIVA_HEADLESS=1 serves the
// headless line protocol on standard input and output instead of opening a
// window (see github.com/vivaui/viva/headless). Release builds omit the tag and contain none of it.
import _ "github.com/vivaui/viva/headless/auto"
