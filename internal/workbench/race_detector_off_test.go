//go:build !race

package workbench

// raceDetector reports whether this test binary was built with -race.
const raceDetector = false
