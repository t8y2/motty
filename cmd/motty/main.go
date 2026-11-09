//go:build darwin || windows || linux

package main

import (
	"errors"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
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
	if len(os.Args) > 1 && os.Args[1] == selfupdate.DebCommand {
		os.Exit(runInstallDeb(os.Args[2:]))
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
	if len(args) > 0 {
		windows.launch(args)
	}
	hub := workbench.NewAIHub(aiDatabasePath(datadir.Current().Dir))
	err := app.RunApplication(func(application *app.Application) error {
		application.OnQuitRequest(windows.quitWithDocuments)
		status := newTray(application)
		// The icon the desktop shows stands for the whole program, so the
		// windows report the work they are doing together, through one bar.
		// A fraction below zero is the end of the work.
		windowGroup.SetTransferProgress(func(_ int, fraction float32) {
			application.SetProgress(fraction)
		})
		firstWindow := true
		// openWindowAt opens the window filed under id. A new window takes the
		// next number, which is what the New Window command and a torn-off tab
		// do; a window restored at start keeps the number its record is under.
		var openWindowAt func(int) error
		openWindowAt = func(id int) error {
			// A window restored at start is numbered by its record; the counter
			// follows it, so the next new window takes a number no record has.
			windowIDs = max(windowIDs, id)
			// Only the first window starts out of sight and carries the crash
			// report. Every window opened at start keeps its own place.
			hidden := background
			background = false
			first := firstWindow
			firstWindow = false
			return openWorkbenchWindow(application, status, hub, func() error {
				return openWindowAt(nextWindowID())
			}, hidden, placement{windowID: id, rememberFrame: first})
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
		// Every window the last run left open comes back with its own tabs and
		// its own place. A launch that was asked to open something, and one at
		// login, open a single window: autostart must not put a screenful of
		// hidden windows up.
		restored := 1
		if !background && len(args) == 0 {
			count, err := workbench.RestoreWindowCount(stateDatabasePath())
			if err != nil {
				log.Printf("motty: windows of the last run: %v", err)
			} else {
				restored = count
			}
		}
		err := openWindowAt(1)
		reportStarted(err)
		if err != nil {
			return err
		}
		for id := 2; id <= restored; id++ {
			if openErr := openWindowAt(id); openErr != nil {
				log.Printf("motty: restore window %d: %v", id, openErr)
			}
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
	// The icon is named from the start: the shell shows this text as soon as the
	// pointer rests on it, which can be before the first window wake updates it.
	tooltip, _ := status.list.Tooltip()
	item, err := application.AddStatusItem(app.StatusItemConfig{
		Icon:    workbench.TrayIcon(),
		Tooltip: tooltip,
		// The system's menu, built when it opens. Where a desktop reports a
		// primary press as an activation instead (a Linux tray), it brings the
		// window back.
		Menu: status.list.Menu,
		OnClick: func(click app.StatusClick) {
			if click.Button == gfx.PointerButtonPrimary {
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

// stateDatabasePath is the configuration store, which sits beside known_hosts in
// the data directory. It is opened by the windows and, at start, read once by
// the launcher to see how many windows the last run left.
func stateDatabasePath() string {
	dir := datadir.Current().Dir
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.db")
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
	origin *gfx.Point
	adopt  func(*workbench.State)
	blank  bool
	// windowID is which window of this run it is. Zero takes the next number,
	// which is what a torn-off window and the New Window command want; a window
	// restored at start is given the number its record is filed under.
	windowID int
	// rememberFrame makes this window keep its own place and size. Every window
	// opened at start does; a torn-off one belongs to one session and does not.
	rememberFrame bool
}

// windowIDs numbers the windows of this run. The first is 1, and each window
// keeps its tabs and its place under that number, so two windows never write
// each other's. Windows are made on the event-loop thread, so a plain counter
// is enough.
var windowIDs int

// nextWindowID is the number for a window that was not given one.
func nextWindowID() int {
	windowIDs++
	return windowIDs
}

func openWorkbenchWindow(application *app.Application, status *tray, hub *workbench.AIHub, newWindow func() error, background bool, place placement) error {
	importSource, sourceErr := developmentImportSource(datadir.Current().Dir)
	if sourceErr != nil {
		return sourceErr
	}
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
			// A window that disappears into the status area is otherwise
			// indistinguishable from one that ended the program, so the first
			// time this happens the user is told the sessions are still there.
			state.NoteHiddenToTray()
			return
		}
		if state.ConfirmClose(allow) {
			invalidate()
			return
		}
		allow()
	}
	state = workbench.New(workbench.Options{
		DevelopmentSource: importSource,
		Invalidate:        invalidate, NewWindow: newWindow,
		OpenContentWindow: application.OpenContentWindow,
		Group:             windowGroup, Blank: place.blank, Log: recentLog,
		LogDir: logDirectory(), PreviousCrash: crashNoticeFor(place),
		WindowID:            place.windowID,
		RememberWindowFrame: place.rememberFrame,
		TearOff: func(origin gfx.Point, adopt func(*workbench.State)) error {
			return openWorkbenchWindow(application, status, hub, newWindow, false, placement{origin: &origin, adopt: adopt, blank: true})
		},
		OpenDragPreview: func(size gfx.Size, origin gfx.Point, paint func(*gfx.Canvas, gfx.Size)) (workbench.DragPreview, error) {
			preview, err := application.OpenDragPreview(app.DragPreviewConfig{
				Width: int(math.Ceil(float64(size.Width))), Height: int(math.Ceil(float64(size.Height))), Origin: origin, Paint: paint,
			})
			if err != nil {
				return nil, err
			}
			return preview, nil
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
			origin = &gfx.Point{X: frame.X, Y: frame.Y}
		}
	}
	window := state.WindowColor()
	width = max(width, workbench.MinimumWindowWidth)
	height = max(height, workbench.MinimumWindowHeight)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: float32(width), Height: float32(height)})

	var nativeInput app.TextInput
	resolveInput := func(context app.TextInputContext) *app.TextInput {
		if context.Terminal {
			return state.TerminalInput(context)
		}
		if input := state.SFTPInput(context); input != nil {
			return input
		}
		if input := state.SFTPTabInput(context); input != nil {
			return input
		}
		if input := state.RemoteToolsInput(context); input != nil {
			return input
		}
		if input := state.TerminalFindInput(context); input != nil {
			return input
		}
		if input := state.TerminalWordInput(context); input != nil {
			return input
		}
		// The assistant's message box is multi-line and takes Enter itself.
		if input := state.AIPanelInput(context); input != nil {
			return input
		}
		if input := state.NotesInput(context); input != nil {
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
		OnFullscreen:  state.SetWindowFullscreen,
		OnClosed: func() {
			log.Print("motty: window closed")
			windows.remove(state)
			hub.Remove(state)
			quitting := windows.isQuitting()
			remaining := windows.count()
			state.Close()
			// A window the user closed while other windows are still up is one
			// they are done with, so its record goes and the next launch does not
			// reopen it. A quit, and the last window, keep every record: that is
			// what the next launch reopens.
			if !quitting && remaining > 0 {
				state.DropWindowRecord()
			}
			if status != nil {
				status.list.Remove(state)
				status.refresh()
			}
		},
	}, &canvas)
	if err == nil {
		state.SetWindow(mainWindow.Frame, func() bool { return !mainWindow.Hidden() && !mainWindow.Closed() })
		state.SetShortcutCapture(mainWindow.CaptureSystemShortcuts)
		state.SetFullscreenControl(mainWindow.SetFullscreen)
		hub.Add(state, mainWindow.Show)
		windows.add(state)
		// The desktop sessions forward the local clipboard; watching it means
		// a change reaches them without the pointer moving first. It hangs on
		// the window's messages, so it starts once the window exists; where
		// the platform cannot watch, the pointer path in the workbench remains.
		if watchErr := app.OnClipboardChange(windowGroup.ClipboardChanged); watchErr != nil && !errors.Is(watchErr, app.ErrUnsupported) {
			log.Print("motty: the clipboard cannot be watched: ", watchErr)
		}
		if background && status.hasStatusArea() {
			// Started at login: sit in the status area until the user wants it.
			mainWindow.Hide()
		}
		if status != nil {
			status.list.Add(state, mainWindow)
			// The icon can be up before this window exists (a start at login), so
			// its tooltip is named now instead of at the next wake.
			status.refresh()
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
