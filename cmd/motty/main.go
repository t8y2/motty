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
	"github.com/vivaui/viva/ui"
	"motty/internal/buildinfo"
	"motty/internal/datadir"
	"motty/internal/diag"
	"motty/internal/selfupdate"
	"motty/internal/vault"
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
	if len(os.Args) > 1 && os.Args[1] == "mcp-serve" {
		os.Exit(runMCPServe(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == selfupdate.Command {
		os.Exit(runApplyUpdate(os.Args[2:]))
	}
	args, background := launchArguments(os.Args[1:])
	// A second launch hands what it was asked to open to the copy that runs, and
	// exits. A headless test run is its own world and never meets the real one.
	if os.Getenv("VIVA_HEADLESS") != "" {
		if err := headlessClaim(datadir.Current().Dir); err != nil {
			fatal(err)
		}
	} else {
		primary, claim, err := claimLaunch(datadir.Current().Dir, handoffArguments(args, background), func(more []string) {
			windows.launch(more)
		})
		if err != nil {
			fatal(err)
		} else if !primary {
			explainSecondaryLaunch(os.Stderr)
			return
		} else {
			defer claim.Close()
		}
	}
	// Only the copy that owns the data directory writes its files.
	startLogging()
	defer stopLogging()
	// The first run unpacks the bundled community schemes into the data folder
	// for the user to keep or trim; a directory that already holds schemes is
	// left alone. The current location is empty for a copy in the usual place,
	// which is then the default one.
	seedDir := datadir.Current().Dir
	if seedDir == "" {
		if dir, err := defaultDataDir(); err == nil {
			seedDir = dir
		}
	}
	if seeded, err := workbench.SeedSchemesDir(seedDir); err != nil {
		log.Printf("motty: unpacking the community color schemes: %v", err)
	} else if seeded > 0 {
		log.Printf("motty: unpacked %d community color schemes", seeded)
	}
	if len(args) > 0 {
		windows.launch(args)
	}
	hub := workbench.NewAIHub(aiDatabasePath(datadir.Current().Dir))
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
		if background {
			// Started at login: the first window opens hidden, so the icon can
			// go up first and closing it still leaves the application running.
			status.show(application)
		}
		err := openWindow()
		reportStarted(err)
		if err != nil {
			return err
		}
		// A development copy is told apart on its Dock icon, where every window
		// shows it, rather than by renaming the product in the interface.
		if buildinfo.DockBadge != "" {
			application.SetDockBadge(buildinfo.DockBadge)
		}
		// Putting the icon in the status area costs about 60 ms, which would
		// otherwise be part of how long the window takes to appear.
		status.show(application)
		return nil
	})
	hub.Close()
	if err != nil {
		fatal(err)
	}
	log.Print("motty: exiting normally")
}

func mainWindowTitle() string { return buildinfo.ProductName }

// newTray makes the status-area list without an icon: the icon is added once
// the first window is up, because making it delays the window (see show).
func newTray(application *app.Application) *tray {
	return &tray{list: workbench.NewTray(func() { windows.quitWithDocuments(application.Quit) })}
}

// show puts the icon in the status area. Without one, a desktop with no
// StatusNotifier host, the application simply has no tray. It is safe to call
// more than once.
func (status *tray) show(application *app.Application) {
	if status == nil || status.item != nil {
		return
	}
	item, err := application.AddStatusItem(app.StatusItemConfig{
		Icon: workbench.TrayIcon(),
		// The system's menu, built when it opens. Where a desktop reports a
		// primary press as an activation instead (a Linux tray), it brings the
		// window back.
		Menu: status.list.Menu,
		OnClick: func(click app.StatusClick) {
			if click.Button == ui.PointerButtonPrimary {
				status.list.ShowWindow()
			}
		},
	})
	if err != nil {
		if !errors.Is(err, app.ErrNoStatusArea) {
			log.Printf("motty: no status icon: %v", err)
		}
		return
	}
	status.item = item
}

// hasStatusArea reports whether the application is in the status area, which is
// what lets it keep running when its last window closes.
func (status *tray) hasStatusArea() bool { return status != nil && status.item != nil }

// keystore is where the saved-passwords vault of the data directory keeps its
// key: the system's own store, which is nil here, except in a portable copy.
// That one travels between computers and must leave nothing behind on them, so
// its key is a file next to the vault, which goes where the data goes.
func keystore() vault.Keystore { return keystoreFor(datadir.Current().Dir) }

func keystoreFor(dir string) vault.Keystore {
	if location := datadir.Current(); location.Portable && dir == location.Dir {
		return vault.FileKeystore{Path: filepath.Join(dir, "vault.key")}
	}
	return nil
}

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

// refresh brings the icon's tooltip up to date with the sessions that are open.
// It runs on the event-loop thread.
func (status *tray) refresh() {
	if !status.hasStatusArea() {
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
	// with nothing in it just closes, and so does every window when the user
	// chose in Settings that closing quits.
	requestClose := func(allow func()) {
		if status.hasStatusArea() && state.StaysInTray() && (state.WantsToStay() || status.list.WindowCount() <= 1) {
			log.Print("motty: window hidden to the status area; the program keeps running")
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
		LogDir: logDirectory(), PreviousCrash: crashNoticeFor(place),
		RememberWindowFrame: place.rememberFrame,
		TearOff: func(origin ui.Point, adopt func(*workbench.State)) error {
			return openWorkbenchWindow(application, status, hub, newWindow, false, placement{origin: &origin, adopt: adopt, blank: true})
		},
		CloseWindow:        func() { requestClose(func() { mainWindow.Close() }) },
		KnownHostsPath:     knownHostsPath(datadir.Current().Dir),
		Keystore:           keystore(),
		PreferredLanguages: preferredLanguages,
		Raise:              func() { mainWindow.Show() },
		StartAtLogin:       loginStart(),
		StatusArea:         status.hasStatusArea,
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
	width = max(width, workbench.MinimumWindowWidth)
	height = max(height, workbench.MinimumWindowHeight)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: float32(width), Height: float32(height)})

	var nativeInput app.TextInput
	resolveInput := func(context app.TextInputContext) *app.TextInput {
		if context.Terminal {
			return state.TerminalInput(context)
		}
		if input := state.SFTPInput(context); input != nil {
			return input
		}
		if input := state.RemoteToolsInput(context); input != nil {
			return input
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
		nativeInput = app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
		return &nativeInput
	}

	var err error
	mainWindow, err = application.OpenWindow(app.Config{
		Title:          mainWindowTitle(),
		Icon:           workbench.AppIcon(),
		Tree:           workbench.Tree(),
		Width:          width,
		Height:         height,
		MinWidth:       workbench.MinimumWindowWidth,
		MinHeight:      workbench.MinimumWindowHeight,
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
			log.Print("motty: window closed")
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
		if background && status.hasStatusArea() {
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
	dir := updatesDir(datadir.Current().Dir)
	if dir == "" {
		return nil
	}
	return selfupdate.New(dir)
}
