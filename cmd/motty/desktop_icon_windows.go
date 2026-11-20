package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/tools/winpkg"
	win32 "golang.org/x/sys/windows"
	"motty/internal/buildinfo"
	"motty/internal/datadir"
)

var desktopIcons struct {
	sync.Mutex
	paths map[[32]byte]string
	last  string
}

func desktopWindowClass() string {
	id, err := claimInstanceID(datadir.Current().Dir)
	if err != nil {
		log.Printf("motty: desktop identity: %v", err)
		return applicationID
	}
	return id
}

// The shell needs a file that outlives the process. Content-addressed resources
// prevent Explorer from reusing a cached green icon when the choice changes.
// Neither the installed executable nor the installer's icon is rewritten.
func desktopWindowIcon(icon app.WindowIcon) app.WindowIcon {
	if os.Getenv("VIVA_HEADLESS") != "" {
		return icon
	}
	desktopIcons.Lock()
	defer desktopIcons.Unlock()
	digest := sha256.Sum256(icon.RGBA)
	path := desktopIcons.paths[digest]
	if path == "" {
		dir := datadir.Current().Dir
		if dir == "" {
			var err error
			dir, err = defaultDataDir()
			if err != nil {
				log.Printf("motty: desktop icon directory: %v", err)
				return icon
			}
		}
		var err error
		path, err = writeDesktopIcon(filepath.Join(dir, "icons"), icon)
		if err != nil {
			log.Printf("motty: desktop icon resource: %v", err)
			return icon
		}
		if desktopIcons.paths == nil {
			desktopIcons.paths = make(map[[32]byte]string)
		}
		desktopIcons.paths[digest] = path
	}
	icon.ResourcePath = path
	// Profiles and portable/dev copies must not take over the installed copy's
	// shortcuts. Their own window/taskbar still receives its resource and AppID.
	if desktopIcons.last != path && os.Getenv(datadir.Variable) == "" &&
		!datadir.Current().Portable && !buildinfo.Development {
		executable, err := os.Executable()
		if err == nil {
			err = updateDesktopShortcuts(executable, path, desktopWindowClass(), desktopShortcutRoots())
		}
		if err != nil {
			log.Printf("motty: desktop shortcuts: %v", err)
		} else {
			desktopIcons.last = path
		}
	}
	return icon
}

func writeDesktopIcon(dir string, icon app.WindowIcon) (string, error) {
	if icon.Size < 1 || icon.Size > 1024 || len(icon.RGBA) != icon.Size*icon.Size*4 {
		return "", fmt.Errorf("invalid desktop icon pixels")
	}
	source := &image.NRGBA{Pix: icon.RGBA, Stride: icon.Size * 4, Rect: image.Rect(0, 0, icon.Size, icon.Size)}
	data, _, err := winpkg.EncodeICO(source)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	path, err := filepath.Abs(filepath.Join(dir, fmt.Sprintf("logo-%x.ico", digest[:12])))
	if err != nil {
		return "", err
	}
	if previous, err := os.ReadFile(path); err == nil && bytes.Equal(previous, data) {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".logo-*.ico")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

type shortcutRoot struct {
	path      string
	recursive bool
}

func desktopShortcutRoots() []shortcutRoot {
	var roots []shortcutRoot
	for _, folder := range []*win32.KNOWNFOLDERID{
		win32.FOLDERID_Desktop, win32.FOLDERID_Programs, win32.FOLDERID_Startup,
	} {
		if path, err := win32.KnownFolderPath(folder, 0); err == nil {
			roots = append(roots, shortcutRoot{path, folder == win32.FOLDERID_Programs})
		}
	}
	if roaming, err := win32.KnownFolderPath(win32.FOLDERID_RoamingAppData, 0); err == nil {
		roots = append(roots, shortcutRoot{filepath.Join(roaming, "Microsoft", "Internet Explorer", "Quick Launch", "User Pinned", "TaskBar"), false})
	}
	return roots
}

var (
	iconOle           = win32.NewLazySystemDLL("ole32.dll")
	iconShell         = win32.NewLazySystemDLL("shell32.dll")
	iconCoInit        = iconOle.NewProc("CoInitializeEx")
	iconCoUninit      = iconOle.NewProc("CoUninitialize")
	iconCoCreate      = iconOle.NewProc("CoCreateInstance")
	iconNotify        = iconShell.NewProc("SHChangeNotify")
	clsidShellLink    = win32.GUID{Data1: 0x00021401, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLink      = win32.GUID{Data1: 0x000214f9, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile    = win32.GUID{Data1: 0x0000010b, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIconProperties = win32.GUID{Data1: 0x886d8eeb, Data2: 0x8cf2, Data3: 0x4446, Data4: [8]byte{0x8d, 0x02, 0xcd, 0xba, 0x1d, 0xbd, 0xcf, 0x99}}
)

//go:uintptrescapes
func iconCOM(object uintptr, method int, args ...uintptr) error {
	// The address came from COM, not a Go allocation. Preserve its pointer bits
	// without converting an integer into a Go heap pointer under checkptr.
	address := *(*unsafe.Pointer)(unsafe.Pointer(&object))
	table := *(*unsafe.Pointer)(address)
	function := *(*uintptr)(unsafe.Add(table, uintptr(method)*unsafe.Sizeof(object)))
	result, _, _ := syscall.SyscallN(function, append([]uintptr{object}, args...)...)
	if int32(result) < 0 {
		return fmt.Errorf("shell HRESULT %#x", uint32(result))
	}
	return nil
}

func iconQuery(object uintptr, iid *win32.GUID) (uintptr, error) {
	var result uintptr
	err := iconCOM(object, 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&result)))
	return result, err
}

// Use a dedicated COM apartment. No Resolve call: inspecting a link must never
// launch a target, contact a share, or show a shell dialog.
func updateDesktopShortcuts(executable, resource, id string, roots []shortcutRoot) error {
	result := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		status, _, _ := iconCoInit.Call(0, 2 /* COINIT_APARTMENTTHREADED */)
		if int32(status) < 0 {
			result <- fmt.Errorf("initialize shell COM: %#x", uint32(status))
			return
		}
		defer iconCoUninit.Call()
		var firstErr error
		for _, root := range roots {
			_ = filepath.WalkDir(root.path, func(path string, entry fs.DirEntry, err error) error {
				if err == nil && entry.IsDir() && path != root.path && !root.recursive {
					return filepath.SkipDir
				}
				if err != nil || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(path), ".lnk") {
					return nil
				}
				if err := updateDesktopShortcut(path, executable, resource, id); err != nil && firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", path, err)
				}
				return nil
			})
		}
		result <- firstErr
	}()
	return <-result
}

func updateDesktopShortcut(path, executable, resource, id string) error {
	var link uintptr
	status, _, _ := iconCoCreate.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, 1,
		uintptr(unsafe.Pointer(&iidShellLink)), uintptr(unsafe.Pointer(&link)))
	if int32(status) < 0 {
		return fmt.Errorf("create shell link: %#x", uint32(status))
	}
	defer iconCOM(link, 2)
	persist, err := iconQuery(link, &iidPersistFile)
	if err != nil {
		return err
	}
	defer iconCOM(persist, 2)
	encodedPath, err := win32.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := iconCOM(persist, 5 /* Load */, uintptr(unsafe.Pointer(encodedPath)), 2 /* STGM_READWRITE */); err != nil {
		return nil // Unreadable/unrelated shell links are not ours to repair.
	}
	var target [32768]uint16
	if err := iconCOM(link, 3 /* GetPath */, uintptr(unsafe.Pointer(&target[0])), uintptr(len(target)), 0, 4 /* SLGP_RAWPATH */); err != nil {
		return nil
	}
	targetPath := win32.UTF16ToString(target[:])
	// A shortcut to another volume cannot be this file. Avoid probing remote
	// targets and unrelated volumes while looking through the Start menu.
	if strings.HasPrefix(targetPath, `\\`) || !strings.EqualFold(filepath.VolumeName(targetPath), filepath.VolumeName(executable)) {
		return nil
	}
	info, err := os.Stat(targetPath)
	own, ownErr := os.Stat(executable)
	if err != nil || ownErr != nil || !os.SameFile(info, own) {
		return nil
	}
	encodedResource, err := win32.UTF16PtrFromString(resource)
	if err != nil {
		return err
	}
	if err := iconCOM(link, 17 /* SetIconLocation */, uintptr(unsafe.Pointer(encodedResource)), 0); err != nil {
		return err
	}
	store, err := iconQuery(link, &iidIconProperties)
	if err != nil {
		return err
	}
	defer iconCOM(store, 2)
	key := struct {
		Format win32.GUID
		ID     uint32
	}{
		win32.GUID{Data1: 0x9f4c2855, Data2: 0x9f79, Data3: 0x4b39, Data4: [8]byte{0xa8, 0xd0, 0xe1, 0xd4, 0x2d, 0xe1, 0xd5, 0xf3}}, 5,
	}
	encodedID, err := win32.UTF16PtrFromString(id)
	if err != nil {
		return err
	}
	value := struct {
		Kind     uint16
		Reserved [3]uint16
		Text     *uint16
		Tail     uintptr
	}{Kind: 31, Text: encodedID}
	if err := iconCOM(store, 6 /* SetValue */, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&value))); err != nil {
		return err
	}
	if err := iconCOM(store, 7 /* Commit */); err != nil {
		return err
	}
	if err := iconCOM(persist, 6 /* Save */, uintptr(unsafe.Pointer(encodedPath)), 1); err != nil {
		return err
	}
	iconNotify.Call(0x2000 /* SHCNE_UPDATEITEM */, 0x5 /* SHCNF_PATHW */, uintptr(unsafe.Pointer(encodedPath)), 0)
	return nil
}
