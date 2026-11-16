//go:build windows

package workbench

import (
	"os"
	"syscall"
)

func localFileHidden(_ string, info os.FileInfo) bool {
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attributes == nil {
		return false
	}
	return attributes.FileAttributes&(syscall.FILE_ATTRIBUTE_HIDDEN|syscall.FILE_ATTRIBUTE_SYSTEM) != 0
}
