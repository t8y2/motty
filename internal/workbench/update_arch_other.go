//go:build !darwin

package workbench

import "runtime"

// updateArch is the architecture whose package an update should install, which
// is always the one the program was built for.
func updateArch() string { return runtime.GOARCH }
