//go:build !windows

package workbench

import "os"

func readSessionLogSystemZone() ([]byte, error) {
	return os.ReadFile("/etc/localtime")
}
