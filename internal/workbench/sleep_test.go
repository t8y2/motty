package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
)

// TestRunningTransfersHoldOneSleepRequest checks the process-wide rule: two
// windows transferring share one system request, and the request is gone when
// the last transfer stops.
func TestRunningTransfersHoldOneSleepRequest(t *testing.T) {
	driver, err := app.NewDriver(app.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	t.Cleanup(func() {
		for transferSleep.windows > 0 {
			holdTransferSleep(false)
		}
	})

	holdTransferSleep(true)
	holdTransferSleep(true)
	reasons := driver.AwakeReasons()
	if len(reasons) != 1 || reasons[0] != transferSleepReason {
		t.Fatalf("AwakeReasons = %v while two windows transfer", reasons)
	}
	holdTransferSleep(false)
	if reasons := driver.AwakeReasons(); len(reasons) != 1 {
		t.Fatalf("AwakeReasons = %v after one window stopped", reasons)
	}
	holdTransferSleep(false)
	if reasons := driver.AwakeReasons(); len(reasons) != 0 {
		t.Fatalf("AwakeReasons = %v after the last window stopped", reasons)
	}
}
