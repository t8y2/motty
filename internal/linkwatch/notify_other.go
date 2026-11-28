//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows)

package linkwatch

import "errors"

// subscribe has no notification to offer here; the watcher reads on a timer.
func subscribe(<-chan struct{}) (<-chan struct{}, error) {
	return nil, errors.New("linkwatch: no change notification on this system")
}
