//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows

package linkwatch

import (
	"testing"
	"time"
)

// The system's own notification opens here, so a running watcher waits to be
// told rather than reading on a timer, and stopping it lets the source go.
func TestTheSystemNotificationOpensAndCloses(t *testing.T) {
	done := make(chan struct{})
	signals, err := subscribe(done)
	if err != nil {
		t.Fatalf("no change notification on this system: %v", err)
	}
	close(done)
	if isWindows {
		// IP Helper has no socket to close: the channel stays, and the
		// registration is what stopping cancels.
		return
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-signals:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("the source did not end after stopping")
		}
	}
}
