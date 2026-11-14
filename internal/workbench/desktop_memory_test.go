package workbench

import (
	"testing"

	"motty/internal/memlimit"
)

// TestDesktopPicturesSetTheHeapLimit checks the wiring: an open picture sets the
// process limit from its size, a resize moves it, and closing the last one
// removes it.
func TestDesktopPicturesSetTheHeapLimit(t *testing.T) {
	// The registry is process-wide, and a test earlier in the package may have
	// left a picture in it: another window's picture legitimately sets a floor.
	// Give this test an empty registry and put the others back afterwards.
	desktopPictures.Lock()
	previous := desktopPictures.views
	desktopPictures.views = make(map[*desktopView]memlimit.Size)
	desktopPictures.Unlock()
	t.Cleanup(func() {
		desktopPictures.Lock()
		desktopPictures.views = previous
		desktopPictures.Unlock()
		applyDesktopMemoryLimit()
	})
	memlimit.Apply(0)
	view := &desktopView{}
	trackDesktopPicture(view)
	defer untrackDesktopPicture(view)

	if got := memlimit.Current(); got != 0 {
		t.Fatalf("new picture of unknown size set a %d byte limit", got)
	}

	noteDesktopSize(view, 1920, 1080)
	if got, want := memlimit.Current(), memlimit.ForDesktops([]memlimit.Size{{Width: 1920, Height: 1080}}); got != want {
		t.Fatalf("1080p limit = %d, want %d", got, want)
	}

	noteDesktopSize(view, 5120, 2880)
	if got, want := memlimit.Current(), memlimit.ForDesktops([]memlimit.Size{{Width: 5120, Height: 2880}}); got != want {
		t.Fatalf("5K limit = %d, want %d", got, want)
	}

	untrackDesktopPicture(view)
	if got := memlimit.Current(); got != 0 {
		t.Fatalf("closing the last picture left a %d byte limit", got)
	}
	// The deferred untrack is idempotent.
}
