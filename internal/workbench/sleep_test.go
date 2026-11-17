package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/sftpclient"
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

func TestClosingTransferWindowsReleasesTheirSleepRequest(t *testing.T) {
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
	first := newTestState(t, testOptions(t))
	second := newTestState(t, testOptions(t))
	for _, state := range []*State{first, second} {
		state.transfers.entries = []transferEntry{{snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferRunning}}}
		state.describeTransferSummary()
	}
	if reasons := driver.AwakeReasons(); len(reasons) != 1 || reasons[0] != transferSleepReason {
		t.Fatalf("AwakeReasons = %v while two windows transfer", reasons)
	}
	first.Close()
	first.Close()
	if reasons := driver.AwakeReasons(); len(reasons) != 1 || reasons[0] != transferSleepReason {
		t.Fatalf("AwakeReasons = %v while the second window still transfers", reasons)
	}
	second.Close()
	if reasons := driver.AwakeReasons(); len(reasons) != 0 {
		t.Fatalf("AwakeReasons = %v after both transfer windows close", reasons)
	}
}
