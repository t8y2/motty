//go:build darwin || windows || linux

package main

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/singleinstance"
	"github.com/vivaui/viva/ui"
	"motty/internal/diag"
	"motty/internal/selfupdate"
	"motty/internal/workbench"
)

// tray is the application's status-area presence: an icon, and the menu that
// lists every session. It is nil on a desktop that has no status area, and the
// windows then close as they always did.
type tray struct {
	list *workbench.Tray
	item *app.StatusItem
}

// recentLog keeps what the program logged, for the diagnostics report.
var recentLog = diag.NewLog(400, 64<<10)

func main() {
	// AppKit requires the process's main thread for NSApplication, windows and
	// status items. Pin the main goroutine before any setup that can yield; by
	// the time app.RunApplication locks later, the scheduler may already have
	// moved main to another OS thread.
	runtime.LockOSThread()
	log.SetOutput(io.MultiWriter(os.Stderr, recentLog))
	if len(os.Args) > 1 && os.Args[1] == "mcp-bridge" {
		os.Exit(runMCPBridge(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == selfupdate.Command {
		os.Exit(runApplyUpdate(os.Args[2:]))
	}
	args, background := launchArguments(os.Args[1:])
	// A second launch hands what it was asked to open to the copy that runs, and
	// exits. A headless test run is its own world and never meets the real one.
	if os.Getenv("VIVA_HEADLESS") == "" {
		claim, primary, err := singleinstance.SingleInstance(instanceID(os.Getenv(dataDirVariable)), args, func(more []string) {
			windows.launch(more)
		})
		if err != nil {
			log.Printf("motty: single instance: %v", err)
		} else if !primary {
			return
		} else {
			defer claim.Close()
		}
	}
	if len(args) > 0 {
		windows.launch(args)
	}
	hub := workbench.NewAIHub(aiDatabasePath(os.Getenv(dataDirVariable)))
	err := app.RunApplication(func(application *app.Application) error {
		application.OnQuitRequest(windows.quitWithDocuments)
		status := newTray(application)
		var openWindow func() error
		firstWindow := true
		openWindow = func() error {
			// Only the first window starts out of sight, and only it remembers
			// where the last first window was.
			hidden := background
			background = false
			remember := firstWindow
			firstWindow = false
			return openWorkbenchWindow(application, status, hub, openWindow, hidden, placement{rememberFrame: remember})
		}
		if err := hub.Reconfigure(); err != nil {
			log.Printf("motty: external agent access: %v", err)
		}
		// macOS hands links to the running program; elsewhere they are arguments.
		application.OnOpenURL(func(url string) { windows.launch([]string{url}) })
		return openWindow()
	})
	hub.Close()
	if err != nil {
		log.Fatal(err)
	}
}

// newTray puts the icon in the status area. Without one, a desktop with no
// StatusNotifier host, the application simply has no tray.
func newTray(application *app.Application) *tray {
	list := workbench.NewTray(func() { windows.quitWithDocuments(application.Quit) })
	item, err := application.AddStatusItem(app.StatusItemConfig{
		Icon: workbench.TrayIcon(),
		// The system's menu, built when it opens. Where a desktop reports a
		// primary press as an activation instead (a Linux tray), it brings the
		// window back.
		Menu: list.Menu,
		OnClick: func(click app.StatusClick) {
			if click.Button == ui.PointerButtonPrimary {
				list.ShowWindow()
			}
		},
	})
	if err != nil {
		if !errors.Is(err, app.ErrNoStatusArea) {
			log.Printf("motty: no status icon: %v", err)
		}
		return nil
	}
	return &tray{list: list, item: item}
}

// refresh brings the icon's tooltip up to date with the sessions that are open.
// It runs on the event-loop thread.
// dataDirVariable names the environment variable that moves everything the
// application keeps (hosts, settings, known hosts) to another directory, so a
// build under test never touches the user's own data.
const dataDirVariable = "MOTTY_DATA_DIR"

// languageVariable names the environment variable that stands in for the
// system's language ("en", "zh-CN"), so a script or a test run gets the same
// texts on every computer. A language the user chose in Settings still wins.
const languageVariable = "MOTTY_LANG"

// preferredLanguages is the language list the application starts from.
func preferredLanguages() []string {
	if tag := os.Getenv(languageVariable); tag != "" {
		return []string{tag}
	}
	return app.PreferredLanguages()
}

// knownHostsPath is where the known-hosts file lives for a data directory; the
// state file sits next to it. Empty means the platform's usual place.
func knownHostsPath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "known_hosts")
}

// aiDatabasePath is the assistant's database for a data directory; empty means
// the platform's usual place (next to the known-hosts file).
func aiDatabasePath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "ai.db")
}

func (status *tray) refresh() {
	if status == nil {
		return
	}
	if text, changed := status.list.Tooltip(); changed {
		status.item.SetTooltip(text)
	}
}

// windowGroup is every workbench window, so that a tab can move between them.
var windowGroup = workbench.NewGroup()

// placement says how a window is opened when it is not the usual new window: a
// torn-off tab opens one where the pointer let go, empty but for the tab.
type placement struct {
	origin *ui.Point
	adopt  func(*workbench.State)
	blank  bool
	// rememberFrame makes this window the one whose place and size are saved
	// and restored. The first window of a process is that window.
	rememberFrame bool
}

func openWorkbenchWindow(application *app.Application, status *tray, hub *workbench.AIHub, newWindow func() error, background bool, place placement) error {
	// Connection goroutines wake the window through this; it costs nothing
	// until a connection reports progress.
	var invalidator app.Invalidator
	invalidate := invalidator.Invalidate
	var state *workbench.State
	var mainWindow *app.Window
	// The close button and Command+W end up here. With a tray, a window that
	// has sessions hides (the connections go on, and the icon brings the window
	// back), and so does the last window, so that closing it leaves the
	// application in the status area instead of quitting it. Any other window
	// with nothing in it just closes.
	requestClose := func(allow func()) {
		if status != nil && (state.WantsToStay() || status.list.WindowCount() <= 1) {
			mainWindow.Hide()
			return
		}
		if state.ConfirmClose(allow) {
			invalidate()
			return
		}
		allow()
	}
	state = workbench.New(workbench.Options{
		Invalidate: invalidate, NewWindow: newWindow,
		OpenContentWindow: application.OpenContentWindow,
		Group:             windowGroup, Blank: place.blank, Log: recentLog,
		RememberWindowFrame: place.rememberFrame,
		TearOff: func(origin ui.Point, adopt func(*workbench.State)) error {
			return openWorkbenchWindow(application, status, hub, newWindow, false, placement{origin: &origin, adopt: adopt, blank: true})
		},
		CloseWindow:        func() { requestClose(func() { mainWindow.Close() }) },
		KnownHostsPath:     knownHostsPath(os.Getenv(dataDirVariable)),
		PreferredLanguages: preferredLanguages,
		Raise:              func() { mainWindow.Show() },
		StartAtLogin:       loginStart(),
		Updater:            updater(),
		Quit:               func() { windows.quitWithDocuments(application.Quit) },
		AISettingsChanged: func() {
			if err := hub.Reconfigure(); err != nil {
				log.Printf("motty: external agent access: %v", err)
			}
		},
	})
	if place.adopt != nil {
		place.adopt(state)
	}
	width, height := 1400, 900
	origin := place.origin
	if place.rememberFrame {
		if frame, ok := state.RestoredWindowFrame(); ok {
			width, height = int(frame.Width), int(frame.Height)
			origin = &ui.Point{X: frame.X, Y: frame.Y}
		}
	}
	window := state.WindowColor()
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: float32(width), Height: float32(height)})

	var nativeInput app.TextInput
	resolveInput := func(context app.TextInputContext) *app.TextInput {
		if context.Terminal {
			return state.TerminalInput(context)
		}
		if input := state.TerminalFindInput(context); input != nil {
			return input
		}
		// The assistant's message box is multi-line and takes Enter itself.
		if input := state.AIPanelInput(context); input != nil {
			return input
		}
		editor := state.EditorFor(context.ID)
		if editor == nil {
			return nil
		}
		// Bounds, font size and color come from the field the component painted.
		nativeInput = app.TextInput{Editor: editor}
		return &nativeInput
	}

	var err error
	mainWindow, err = application.OpenWindow(app.Config{
		Title:          "Motty",
		Icon:           workbench.AppIcon(),
		Tree:           workbench.Tree(),
		Width:          width,
		Height:         height,
		Origin:         origin,
		WindowMode:     app.WindowCustomTitlebar,
		TitlebarHeight: workbench.TitlebarHeight,
		WindowControls: state.WindowControlsStyle,
		TextInput:      resolveInput,
		Invalidator:    &invalidator,
		OnWake: func() {
			state.Wake()
			status.refresh()
		},
		OnCloseRequest: requestClose,
		ClearColor: app.Color{
			Red:   float64(window.Red),
			Green: float64(window.Green),
			Blue:  float64(window.Blue),
			Alpha: float64(window.Alpha),
		},
		Paint:         state.Paint,
		OnPointer:     state.Pointer,
		OnKey:         state.Key,
		OnShortcut:    state.Shortcut,
		OnFileDrop:    state.FileDrop,
		OnScroll:      state.Scroll,
		OnResize:      state.Resize,
		OnFrameChange: state.NoteWindowFrame,
		OnMinimize:    state.SetWindowMinimized,
		OnClosed: func() {
			windows.remove(state)
			hub.Remove(state)
			state.Close()
			if status != nil {
				status.list.Remove(state)
				status.refresh()
			}
		},
	}, &canvas)
	if err == nil {
		state.SetWindow(mainWindow.Frame, func() bool { return !mainWindow.Hidden() && !mainWindow.Closed() })
		hub.Add(state, mainWindow.Show)
		windows.add(state)
		if background && status != nil {
			// Started at login: sit in the status area until the user wants it.
			mainWindow.Hide()
		}
		if status != nil {
			status.list.Add(state, mainWindow)
		}
		// Wake once now that connections can reach the window: this is when
		// restored tabs are connected (see workbench.State.Wake).
		invalidator.Invalidate()
	}
	return err
}

// updater replaces the program with a newer release, or is nil when there is
// nowhere to keep downloads.
func updater() workbench.Updater {
	dir := updatesDir(os.Getenv(dataDirVariable))
	if dir == "" {
		return nil
	}
	return selfupdate.New(dir)
}
