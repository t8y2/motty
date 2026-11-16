//go:build !windows

package workbench

import (
	"os"
	"strings"
)

func localFileHidden(name string, _ os.FileInfo) bool {
	return strings.HasPrefix(name, ".")
}
