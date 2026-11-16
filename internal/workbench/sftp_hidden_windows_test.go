//go:build windows

package workbench

import (
	"syscall"
	"testing"
)

func markLocalHidden(t *testing.T, path string) {
	t.Helper()
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := syscall.GetFileAttributes(pointer)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.SetFileAttributes(pointer, attributes|syscall.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Fatal(err)
	}
}

type hiddenWindowsInfo struct {
	workbenchFileInfo
	attributes syscall.Win32FileAttributeData
}

func (info hiddenWindowsInfo) Sys() any { return &info.attributes }

func TestSFTPHiddenLocalPredicateUsesWindowsAttributes(t *testing.T) {
	for _, attributes := range []uint32{syscall.FILE_ATTRIBUTE_HIDDEN, syscall.FILE_ATTRIBUTE_SYSTEM} {
		info := hiddenWindowsInfo{attributes: syscall.Win32FileAttributeData{FileAttributes: attributes}}
		if !localFileHidden("visible-name.txt", info) {
			t.Fatalf("attributes %#x were not hidden", attributes)
		}
	}
	if localFileHidden(".dot-name.txt", hiddenWindowsInfo{}) {
		t.Fatal("a Windows dot-name without Hidden/System attributes was hidden")
	}
}
