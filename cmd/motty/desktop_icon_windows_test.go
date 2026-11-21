package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/vivaui/viva/app"
	win32 "golang.org/x/sys/windows"
	"motty/internal/workbench"
)

func TestDesktopIconPersistsEveryWindowsSizeAndChangesResource(t *testing.T) {
	dir := t.TempDir()
	icon := workbench.AppIcon()
	first, err := writeDesktopIcon(dir, icon)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if count := binary.LittleEndian.Uint16(data[4:6]); count != 14 {
		t.Fatalf("ICO has %d resolutions, want 14", count)
	}
	info, _ := os.Stat(first)
	if same, err := writeDesktopIcon(dir, icon); err != nil || same != first {
		t.Fatalf("reuse: %s, %v", same, err)
	}
	after, _ := os.Stat(first)
	if !after.ModTime().Equal(info.ModTime()) {
		t.Fatal("unchanged icon was rewritten")
	}
	icon.RGBA = bytes.Clone(icon.RGBA)
	icon.RGBA[0] ^= 255
	second, err := writeDesktopIcon(dir, icon)
	if err != nil || second == first {
		t.Fatalf("changed artwork reused shell cache key: %s %v", second, err)
	}
	if _, err := writeDesktopIcon(dir, app.WindowIcon{Size: 512}); err == nil {
		t.Fatal("invalid pixels accepted")
	}
}

func TestDesktopShortcutsChangeOnlyOwnedTargetsAndPreserveLaunch(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	status, _, _ := iconCoInit.Call(0, 2)
	if int32(status) < 0 {
		t.Fatalf("COM %#x", status)
	}
	defer iconCoUninit.Call()
	dir := t.TempDir()
	executable := filepath.Join(dir, "motty.exe")
	other := filepath.Join(dir, "unrelated.exe")
	for _, path := range []string{executable, other} {
		if err := os.WriteFile(path, []byte("not executed"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const args = `--background "路径 with spaces"`
	owned := filepath.Join(dir, "Renamed by user.lnk")
	unrelated := filepath.Join(dir, "Motty-looking but unrelated.lnk")
	createTestIconShortcut(t, owned, executable, args, dir)
	createTestIconShortcut(t, unrelated, other, "--other", dir)
	unrelatedBefore, _ := os.ReadFile(unrelated)
	resource, err := writeDesktopIcon(dir, workbench.AppIcon())
	if err != nil {
		t.Fatal(err)
	}
	if err := updateDesktopShortcuts(executable, resource, "dev.motty.test", []shortcutRoot{{dir, false}}); err != nil {
		t.Fatal(err)
	}
	unrelatedAfter, _ := os.ReadFile(unrelated)
	if !bytes.Equal(unrelatedBefore, unrelatedAfter) {
		t.Fatal("modified an unrelated shortcut")
	}
	link, persist := loadTestIconShortcut(t, owned)
	defer iconCOM(link, 2)
	defer iconCOM(persist, 2)
	var text [32768]uint16
	var index int32
	if err := iconCOM(link, 16, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)), uintptr(unsafe.Pointer(&index))); err != nil {
		t.Fatal(err)
	}
	if got := win32.UTF16ToString(text[:]); !sameIconTestFile(got, resource) || index != 0 {
		t.Fatalf("shortcut icon = %q,%d, want %s,0", got, index, resource)
	}
	for method, want := range map[int]string{3: executable, 8: dir, 10: args} {
		text = [32768]uint16{}
		var err error
		if method == 3 {
			err = iconCOM(link, method, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)), 0, 4)
		} else {
			err = iconCOM(link, method, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
		}
		got := win32.UTF16ToString(text[:])
		matches := got == want
		if method == 3 || method == 8 {
			matches = sameIconTestFile(got, want)
		}
		if err != nil || !matches {
			t.Fatalf("launch field %d = %q, want %q (%v)", method, win32.UTF16ToString(text[:]), want, err)
		}
	}
	store, err := iconQuery(link, &iidIconProperties)
	if err != nil {
		t.Fatal(err)
	}
	key := struct {
		Format win32.GUID
		ID     uint32
	}{
		win32.GUID{Data1: 0x9f4c2855, Data2: 0x9f79, Data3: 0x4b39, Data4: [8]byte{0xa8, 0xd0, 0xe1, 0xd4, 0x2d, 0xe1, 0xd5, 0xf3}}, 5,
	}
	var value struct {
		Kind     uint16
		Reserved [3]uint16
		Text     *uint16
		Tail     uintptr
	}
	if err := iconCOM(store, 5, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&value))); err != nil {
		t.Fatal(err)
	}
	if value.Kind != 31 || win32.UTF16ToString(unsafe.Slice(value.Text, len("dev.motty.test")+1)) != "dev.motty.test" {
		t.Fatal("shortcut AppID does not match window grouping")
	}
	iconOle.NewProc("PropVariantClear").Call(uintptr(unsafe.Pointer(&value)))
	iconCOM(store, 2)
	// A second change must replace the location, rather than keep the first
	// path merely because this link already carries an explicit AppID.
	secondIcon := workbench.AppIcon()
	secondIcon.RGBA = bytes.Clone(secondIcon.RGBA)
	for i := 0; i < len(secondIcon.RGBA); i += 4 {
		secondIcon.RGBA[i+1], secondIcon.RGBA[i+2] = secondIcon.RGBA[i+2], secondIcon.RGBA[i+1]
	}
	secondResource, err := writeDesktopIcon(dir, secondIcon)
	if err != nil {
		t.Fatal(err)
	}
	if err := updateDesktopShortcuts(executable, secondResource, "dev.motty.test", []shortcutRoot{{dir, false}}); err != nil {
		t.Fatal(err)
	}
	secondLink, secondPersist := loadTestIconShortcut(t, owned)
	defer iconCOM(secondLink, 2)
	defer iconCOM(secondPersist, 2)
	if err := iconCOM(secondLink, 16, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)), uintptr(unsafe.Pointer(&index))); err != nil {
		t.Fatal(err)
	}
	if got := win32.UTF16ToString(text[:]); !sameIconTestFile(got, secondResource) || sameIconTestFile(secondResource, resource) {
		t.Fatalf("second icon change was not persisted: %s", got)
	}
}

func sameIconTestFile(a, b string) bool {
	left, err := os.Stat(a)
	if err != nil {
		return false
	}
	right, err := os.Stat(b)
	return err == nil && os.SameFile(left, right)
}

func createTestIconShortcut(t *testing.T, path, target, args, dir string) {
	t.Helper()
	var link uintptr
	status, _, _ := iconCoCreate.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, 1, uintptr(unsafe.Pointer(&iidShellLink)), uintptr(unsafe.Pointer(&link)))
	if int32(status) < 0 {
		t.Fatalf("create link %#x", status)
	}
	defer iconCOM(link, 2)
	for method, value := range map[int]string{20: target, 11: args, 9: dir} {
		encoded, err := win32.UTF16PtrFromString(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := iconCOM(link, method, uintptr(unsafe.Pointer(encoded))); err != nil {
			t.Fatal(err)
		}
	}
	persist, err := iconQuery(link, &iidPersistFile)
	if err != nil {
		t.Fatal(err)
	}
	defer iconCOM(persist, 2)
	encoded, _ := win32.UTF16PtrFromString(path)
	if err := iconCOM(persist, 6, uintptr(unsafe.Pointer(encoded)), 1); err != nil {
		t.Fatal(err)
	}
}

func loadTestIconShortcut(t *testing.T, path string) (uintptr, uintptr) {
	t.Helper()
	var link uintptr
	status, _, _ := iconCoCreate.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, 1, uintptr(unsafe.Pointer(&iidShellLink)), uintptr(unsafe.Pointer(&link)))
	if int32(status) < 0 {
		t.Fatalf("create link %#x", status)
	}
	persist, err := iconQuery(link, &iidPersistFile)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := win32.UTF16PtrFromString(path)
	if err := iconCOM(persist, 5, uintptr(unsafe.Pointer(encoded)), 0); err != nil {
		t.Fatal(err)
	}
	return link, persist
}
