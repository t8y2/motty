package workbench

import (
	"testing"
	"time"

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
		for {
			desktopSleep.mu.Lock()
			windows := desktopSleep.windows
			desktopSleep.mu.Unlock()
			if windows == 0 {
				break
			}
			holdDesktopSleep(false)
		}
		waitSleepClaim(t, desktopSleep)
	})

	holdDesktopSleep(true)
	holdDesktopSleep(true)
	waitSleepClaim(t, desktopSleep)
	reasons := driver.AwakeReasons()
	if len(reasons) != 1 || reasons[0] != desktopSleepReason {
		t.Fatalf("AwakeReasons = %v while two windows show a desktop", reasons)
	}
	holdDesktopSleep(false)
	waitSleepClaim(t, desktopSleep)
	if reasons := driver.AwakeReasons(); len(reasons) != 1 {
		t.Fatalf("AwakeReasons = %v after one window stopped", reasons)
	}
	holdDesktopSleep(false)
	waitSleepClaim(t, desktopSleep)
	if reasons := driver.AwakeReasons(); len(reasons) != 0 {
		t.Fatalf("AwakeReasons = %v after the last window stopped", reasons)
	}
}

// TestDesktopSleepRequestDoesNotBlockTheUI checks that making a remote desktop
// visible never waits for the power service: that happens on the tab switch,
// which runs on the goroutine that paints. A service that takes seconds to
// answer must leave the window free to keep painting, and a claim let go while
// it answers has to be released as soon as the answer arrives.
func TestDesktopSleepRequestDoesNotBlockTheUI(t *testing.T) {
	started, allow, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
	previous := desktopSleep
	desktopSleep = &asyncSleepClaim{reason: desktopSleepReason, prevent: func(reason string) (func(), error) {
		if reason != desktopSleepReason {
			t.Errorf("sleep reason = %q, want %q", reason, desktopSleepReason)
		}
		close(started)
		<-allow
		return func() { close(released) }, nil
	}}
	t.Cleanup(func() {
		for {
			desktopSleep.mu.Lock()
			windows := desktopSleep.windows
			desktopSleep.mu.Unlock()
			if windows == 0 {
				break
			}
			holdDesktopSleep(false)
		}
		waitSleepClaim(t, desktopSleep)
		desktopSleep = previous
	})

	holdDesktopSleep(true)
	select {
	case <-started:
	case <-time.After(time.Second):
		close(allow)
		t.Fatal("the sleep request never reached the system")
	}
	// The caller keeps painting while the system has not answered yet.
	holdDesktopSleep(false)
	close(allow)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("a desktop made invisible while the system answered stayed awake")
	}
}
