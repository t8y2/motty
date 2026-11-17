package workbench

import (
	"runtime"
	"time"
)

// waitBudget is how long a test waits for work that runs off the window's
// thread to land. It is generous on purpose: these waits report a program the
// test cannot see, never a performance budget, so a slow machine should not
// turn them into failures. Windows is the slow case — starting a shell on a
// hosted runner's cold ConPTY, or settling an SFTP listing behind a worker,
// costs far more there than on the developer machine that set the old bound.
// The race detector slows every goroutine handoff, so race builds get extra
// room on the same bound; release builds keep the original budget.
func waitBudget() time.Duration {
	if runtime.GOOS == "windows" {
		return 30 * time.Second
	}
	if raceDetector {
		return 10 * time.Second
	}
	return 5 * time.Second
}
