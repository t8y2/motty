//go:build darwin || linux

package main

import "context"

// watchParent has nothing to watch here: these systems close the pipe when the
// MCP client holding it exits, so stdin reaches EOF and the server ends itself.
func watchParent(cancel context.CancelFunc) (stop func()) {
	return func() {}
}
