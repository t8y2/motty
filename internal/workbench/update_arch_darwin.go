//go:build darwin

package workbench

import (
	"runtime"
	"syscall"
)

// updateArch is the architecture whose package an update should install. On an
// Apple silicon Mac running the Intel build through Rosetta that is arm64, not
// the architecture the running program reports: carrying the translation
// forward would leave every later update translated too.
func updateArch() string {
	if runningTranslated() {
		return "arm64"
	}
	return runtime.GOARCH
}

// runningTranslated is whether the system translates this process, which only
// an Intel build on Apple silicon is; Intel Macs have no such sysctl.
var runningTranslated = func() bool {
	translated, err := syscall.SysctlUint32("sysctl.proc_translated")
	return err == nil && translated == 1
}
