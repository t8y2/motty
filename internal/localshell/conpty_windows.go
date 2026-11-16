//go:build windows

package localshell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pseudoConsole is one ConPTY implementation: the inbox one exported by
// kernel32, or the bundled modern one from conpty.dll. A console is resized and
// closed with the implementation that created it.
type pseudoConsole struct {
	create func(size windows.Coord, in, out windows.Handle, flags uint32, console *windows.Handle) error
	resize func(console windows.Handle, size windows.Coord) error
	close  func(console windows.Handle)
}

func systemPseudoConsole() pseudoConsole {
	return pseudoConsole{
		create: func(size windows.Coord, in, out windows.Handle, flags uint32, console *windows.Handle) error {
			return windows.CreatePseudoConsole(size, in, out, flags, console)
		},
		resize: func(console windows.Handle, size windows.Coord) error {
			return windows.ResizePseudoConsole(console, size)
		},
		close: windows.ClosePseudoConsole,
	}
}

var (
	bundledOnce sync.Once
	bundled     *pseudoConsole
	// conptyDirectoryOverride is where a test finds the shipped conpty tree; in
	// the application it is empty and the tree sits beside the executable.
	conptyDirectoryOverride string
)

// bundledConPTY loads conpty.dll from the arch tree beside the executable once.
// A missing or unusable DLL is not an error here: the caller falls back to the
// inbox ConPTY, which works everywhere but re-renders the program's output.
func bundledConPTY() *pseudoConsole {
	bundledOnce.Do(func() {
		path, err := bundledConPTYPath()
		if err != nil {
			return
		}
		console, err := loadPseudoConsole(path)
		if err != nil {
			return
		}
		bundled = &console
	})
	return bundled
}

func bundledConPTYPath() (string, error) {
	arch := ""
	switch runtime.GOARCH {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "arm64"
	default:
		return "", fmt.Errorf("localshell: no bundled ConPTY for %s", runtime.GOARCH)
	}
	root := conptyDirectoryOverride
	if root == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", err
		}
		root = filepath.Join(filepath.Dir(executable), "conpty")
	}
	return filepath.Join(root, arch, "conpty.dll"), nil
}

// loadPseudoConsole loads the modern ConPTY and returns its entry points. The
// module stays loaded for the life of the process; unloading it is not a
// meaningful saving and would invalidate consoles still using it.
func loadPseudoConsole(path string) (pseudoConsole, error) {
	dll, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return pseudoConsole{}, err
	}
	procedure := func(name string) (uintptr, error) {
		address, err := windows.GetProcAddress(dll, name)
		if err != nil {
			return 0, fmt.Errorf("localshell: %s: %w", name, err)
		}
		return address, nil
	}
	create, err := procedure("CreatePseudoConsole")
	if err != nil {
		return pseudoConsole{}, err
	}
	resize, err := procedure("ResizePseudoConsole")
	if err != nil {
		return pseudoConsole{}, err
	}
	closeConsole, err := procedure("ClosePseudoConsole")
	if err != nil {
		return pseudoConsole{}, err
	}
	return pseudoConsole{
		create: func(size windows.Coord, in, out windows.Handle, flags uint32, console *windows.Handle) error {
			r, _, _ := syscall.SyscallN(create, packedCoord(size), uintptr(in), uintptr(out), uintptr(flags), uintptr(unsafe.Pointer(console)))
			if int32(uint32(r)) < 0 {
				return fmt.Errorf("conpty.dll: CreatePseudoConsole: HRESULT 0x%08x", uint32(r))
			}
			return nil
		},
		resize: func(console windows.Handle, size windows.Coord) error {
			r, _, _ := syscall.SyscallN(resize, uintptr(console), packedCoord(size))
			if int32(uint32(r)) < 0 {
				return fmt.Errorf("conpty.dll: ResizePseudoConsole: HRESULT 0x%08x", uint32(r))
			}
			return nil
		},
		close: func(console windows.Handle) {
			syscall.SyscallN(closeConsole, uintptr(console))
		},
	}, nil
}

// createPseudoConsole uses the bundled ConPTY and falls back to the inbox one
// if the DLL cannot create a console, so a broken install still opens a
// terminal (without the modern forwarding).
func createPseudoConsole(size windows.Coord, in, out windows.Handle) (windows.Handle, pseudoConsole, error) {
	var console windows.Handle
	var bundledErr error
	if modern := bundledConPTY(); modern != nil {
		if err := modern.create(size, in, out, 0, &console); err == nil {
			return console, *modern, nil
		} else {
			bundledErr = err
		}
	}
	system := systemPseudoConsole()
	if err := system.create(size, in, out, 0, &console); err != nil {
		if bundledErr != nil {
			return 0, pseudoConsole{}, errors.Join(bundledErr, err)
		}
		return 0, pseudoConsole{}, err
	}
	return console, system, nil
}

func packedCoord(size windows.Coord) uintptr {
	return uintptr(uint32(uint16(size.X)) | uint32(uint16(size.Y))<<16)
}
