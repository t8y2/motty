//go:build !windows

package workbench

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
)

func nativeSFTPLocalPlaces() []sftpLocalPlace {
	home, _ := os.UserHomeDir()
	var data []byte
	if runtime.GOOS == "linux" && home != "" {
		config := os.Getenv("XDG_CONFIG_HOME")
		if !filepath.IsAbs(config) {
			config = filepath.Join(home, ".config")
		}
		if file, err := os.Open(filepath.Join(config, "user-dirs.dirs")); err == nil {
			data, _ = io.ReadAll(io.LimitReader(file, 64<<10))
			file.Close()
		}
	}
	return unixSFTPLocalPlaces(home, runtime.GOOS, data)
}
