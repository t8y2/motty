package workbench

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/mcp"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
	"motty/internal/ai/tools"
)

// AIHub serves the assistant's tools to external agents (MCP clients such as
// Claude Code) for every window of the application. It is off until the user
// turns it on in Settings, listens only on the loopback interface, and every
// call goes through the same gate as the in-app assistant.
//
// Add, Remove and Reconfigure are for the event-loop thread; the server's
// requests arrive on other goroutines and reach a window only by posting to its
// inbox.
type AIHub struct {
	path string // the AI database

	mu      sync.Mutex
	windows []*hubWindow
	nextID  int
	server  *mcp.Server
	done    chan struct{}
	addr    string
	token   string
	mode    gate.Mode
}

type hubWindow struct {
	id    int
	state *State
	show  func() // brings the window forward, for a question that needs the user
}

// NewAIHub returns a hub whose settings live in the AI database at path (empty
// means ai.db next to the default state file). It opens nothing until
// Reconfigure finds the database exists.
func NewAIHub(path string) *AIHub {
	if path == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			dir = os.TempDir()
		}
		path = filepath.Join(dir, "motty", "ai.db")
	}
	return &AIHub{path: path}
}

// Add lists a window's sessions to external agents. show brings the window
// forward when a tool call needs the user's answer.
func (hub *AIHub) Add(state *State, show func()) {
	hub.mu.Lock()
	hub.nextID++
	hub.windows = append(hub.windows, &hubWindow{id: hub.nextID, state: state, show: show})
	addr, token := hub.addr, hub.token
	hub.mu.Unlock()
	state.setMCPEndpoint(addr, token)
}

// Remove stops listing a window.
func (hub *AIHub) Remove(state *State) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for index, window := range hub.windows {
		if window.state == state {
			hub.windows = slices.Delete(hub.windows, index, index+1)
			return
		}
	}
}

// Address is where the server listens, or "" while it is off.
func (hub *AIHub) Address() string {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return hub.addr
}

// Reconfigure reads the settings and starts, restarts or stops the server to
// match. Call it at startup and whenever an assistant setting changed.
func (hub *AIHub) Reconfigure() error {
	enabled, mode, token, port, err := hub.readSettings()
	if err != nil {
		return err
	}
	defer hub.publishEndpoint()
	hub.mu.Lock()
	running := hub.server != nil
	same := running && hub.token == token && hub.mode == mode
	hub.mu.Unlock()
	if !enabled {
		hub.stop()
		return nil
	}
	if same {
		return nil
	}
	hub.stop()
	return hub.start(token, mode, port)
}

// readSettings opens the database only if it already exists, so a user who
// never used the assistant pays nothing.
func (hub *AIHub) readSettings() (enabled bool, mode gate.Mode, token string, port int, err error) {
	if _, statErr := os.Stat(hub.path); statErr != nil {
		return false, gate.ModeObserve, "", 0, nil
	}
	db, err := store.Open(hub.path)
	if err != nil {
		return false, gate.ModeObserve, "", 0, err
	}
	defer db.Close()
	read := func(key string) string {
		value, _, _ := db.Setting(key)
		return value
	}
	enabled = read("ai.mcp.enabled") == "on"
	mode = mcpModeFromSetting(read("ai.mcp.mode"))
	port, _ = strconv.Atoi(read("ai.mcp.port"))
	token = read("ai.mcp.token")
	if enabled && len(token) < mcp.MinTokenLen {
		token = mcp.GenerateToken()
		if err := db.SetSetting("ai.mcp.token", token); err != nil {
			return false, mode, "", 0, err
		}
	}
	return enabled, mode, token, port, nil
}

func mcpModeFromSetting(value string) gate.Mode {
	switch value {
	case "ask":
		return gate.ModeAsk
	case "allow-all":
		return gate.ModeAllowAll
	default:
		return gate.ModeObserve
	}
}

func (hub *AIHub) start(token string, mode gate.Mode, port int) error {
	server, err := mcp.New(mcp.Config{
		Token: token, Gate: hub.gate, ServerName: "motty", Version: "1",
	})
	if err != nil {
		return err
	}
	listener, err := mcp.Listen(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil && port != 0 {
		listener, err = mcp.Listen("127.0.0.1:0") // the remembered port is taken
	}
	if err != nil {
		return err
	}
	address := "http://" + listener.Addr().String() + mcp.Path
	done := make(chan struct{})
	hub.mu.Lock()
	hub.server, hub.done, hub.addr, hub.token, hub.mode = server, done, address, token, mode
	hub.mu.Unlock()
	if db, openErr := store.Open(hub.path); openErr == nil {
		_ = db.SetSetting("ai.mcp.port", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
		_ = db.Close()
	}
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	return nil
}

func (hub *AIHub) stop() {
	hub.mu.Lock()
	server, done := hub.server, hub.done
	hub.server, hub.done, hub.addr, hub.token = nil, nil, "", ""
	hub.mu.Unlock()
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	<-done
}

// Close stops the server.
func (hub *AIHub) Close() {
	hub.stop()
	hub.publishEndpoint()
}

func (hub *AIHub) publishEndpoint() {
	hub.mu.Lock()
	addr, token := hub.addr, hub.token
	windows := append([]*hubWindow(nil), hub.windows...)
	hub.mu.Unlock()
	for _, window := range windows {
		window.state.setMCPEndpoint(addr, token)
	}
}

// gate is the Gate for the next request: the hub's backend over all windows, an
// external caller's limits, and questions shown in a window.
func (hub *AIHub) gate() gate.Gate {
	hub.mu.Lock()
	mode := hub.mode
	hub.mu.Unlock()
	return gate.Gate{
		Backend:  hubBackend{hub},
		Policy:   risk.Policy{Enabled: true, AutoUpTo: risk.Low},
		Mode:     mode,
		Approver: hub.approve,
	}
}

// approve shows a question in the window the call is about.
func (hub *AIHub) approve(ctx context.Context, request gate.Request) (bool, string, error) {
	window, _, err := hub.resolve(ctx, request.Session)
	if err != nil {
		return false, "", err
	}
	if window.show != nil {
		window.state.aiPost(func(*State) { window.show() })
	}
	return window.state.aiApprover(ctx, request)
}

// handle names a session across windows: "w2:5" is session 5 of window 2.
func hubHandle(window int, local string) string { return fmt.Sprintf("w%d:%s", window, local) }

// resolve finds the window and window-local handle a hub handle refers to. ""
// is the window with the active ready session, else the first window.
func (hub *AIHub) resolve(ctx context.Context, handle string) (*hubWindow, string, error) {
	hub.mu.Lock()
	windows := append([]*hubWindow(nil), hub.windows...)
	hub.mu.Unlock()
	if len(windows) == 0 {
		return nil, "", errNoSession
	}
	if handle == "" {
		if window := hub.readyWindow(ctx, windows); window != nil {
			return window, "", nil
		}
		return windows[0], "", nil
	}
	rest, ok := strings.CutPrefix(handle, "w")
	prefix, local, found := strings.Cut(rest, ":")
	if !ok || !found {
		return nil, "", errNoSession
	}
	id, err := strconv.Atoi(prefix)
	if err != nil {
		return nil, "", errNoSession
	}
	for _, window := range windows {
		if window.id == id {
			return window, local, nil
		}
	}
	return nil, "", errNoSession
}

// readyWindow is the window whose active session has an open shell, or else
// the first with any ready session.
func (hub *AIHub) readyWindow(ctx context.Context, windows []*hubWindow) *hubWindow {
	var fallback *hubWindow
	for _, window := range windows {
		sessions, err := window.state.ai.backend.Sessions(ctx)
		if err != nil {
			continue
		}
		for _, session := range sessions {
			if session.Ready && session.Active {
				return window
			}
			if session.Ready && fallback == nil {
				fallback = window
			}
		}
	}
	return fallback
}

// hubBackend is gate.Backend over every window.
type hubBackend struct{ hub *AIHub }

var _ gate.Backend = hubBackend{}

func (b hubBackend) Sessions(ctx context.Context) ([]gate.SessionInfo, error) {
	b.hub.mu.Lock()
	windows := append([]*hubWindow(nil), b.hub.windows...)
	b.hub.mu.Unlock()
	var all []gate.SessionInfo
	for _, window := range windows {
		sessions, err := window.state.ai.backend.Sessions(ctx)
		if err != nil {
			continue
		}
		for _, session := range sessions {
			session.ID = hubHandle(window.id, session.ID)
			all = append(all, session)
		}
	}
	return all, nil
}

// target resolves a handle to a window's backend and the local handle.
func (b hubBackend) target(ctx context.Context, handle string) (*aiBackend, string, error) {
	window, local, err := b.hub.resolve(ctx, handle)
	if err != nil {
		return nil, "", err
	}
	return window.state.ai.backend, local, nil
}

func (b hubBackend) ReadTerminal(ctx context.Context, args tools.TerminalReadArgs) (string, error) {
	backend, local, err := b.target(ctx, args.Session)
	if err != nil {
		return "", err
	}
	args.Session = local
	return backend.ReadTerminal(ctx, args)
}

func (b hubBackend) Exec(ctx context.Context, args tools.TerminalExecArgs) (gate.ExecResult, error) {
	backend, local, err := b.target(ctx, args.Session)
	if err != nil {
		return gate.ExecResult{}, err
	}
	args.Session = local
	return backend.Exec(ctx, args)
}

func (b hubBackend) ListDir(ctx context.Context, args tools.SFTPListArgs) ([]gate.FileEntry, error) {
	backend, local, err := b.target(ctx, args.Session)
	if err != nil {
		return nil, err
	}
	args.Session = local
	return backend.ListDir(ctx, args)
}

func (b hubBackend) ReadFile(ctx context.Context, args tools.SFTPReadArgs) (string, bool, error) {
	backend, local, err := b.target(ctx, args.Session)
	if err != nil {
		return "", false, err
	}
	args.Session = local
	return backend.ReadFile(ctx, args)
}

func (b hubBackend) WriteFile(ctx context.Context, args tools.SFTPWriteArgs) error {
	backend, local, err := b.target(ctx, args.Session)
	if err != nil {
		return err
	}
	args.Session = local
	return backend.WriteFile(ctx, args)
}

func (b hubBackend) Remove(ctx context.Context, args tools.SFTPRemoveArgs) error {
	backend, local, err := b.target(ctx, args.Session)
	if err != nil {
		return err
	}
	args.Session = local
	return backend.Remove(ctx, args)
}
