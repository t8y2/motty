//go:build race

package workbench

// raceDetector reports whether this test binary was built with -race. The
// detector changes allocation behaviour and its counts are not comparable with
// a release run, so allocation budgets are measured without it.
const raceDetector = true
