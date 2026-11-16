//go:build !windows && !darwin && !linux

package localshell

import "os"

func foregroundArgs(*os.File) ([]string, bool) { return nil, false }
