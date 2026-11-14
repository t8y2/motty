package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
)

// TestVisibleDesktopsHoldOneSleepRequest checks the process-wide rule for
// remote desktops: two windows share one request, and the request is gone when
// the last visible desktop closes.
func TestVisibleDesktopsHoldOneSleepRequest(t *testing.T) {
	driver, err := app.NewDriver(app.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	t.Cleanup(func() {
		for desktopSleep.windows > 0 {
			holdDesktopSleep(false)
		}
	})

	holdDesktopSleep(true)
	holdDesktopSleep(true)
	reasons := driver.AwakeReasons()
	if len(reasons) != 1 || reasons[0] != desktopSleepReason {
		t.Fatalf("AwakeReasons = %v while two windows show a desktop", reasons)
	}
	holdDesktopSleep(false)
	if reasons := driver.AwakeReasons(); len(reasons) != 1 {
		t.Fatalf("AwakeReasons = %v after one window stopped", reasons)
	}
	holdDesktopSleep(false)
	if reasons := driver.AwakeReasons(); len(reasons) != 0 {
		t.Fatalf("AwakeReasons = %v after the last window stopped", reasons)
	}
}
