// Package mcp exposes the assistant's tools to external agents (Claude Code,
// Codex, Cursor...) as a Model Context Protocol server over the Streamable HTTP
// transport, and offers a stdio bridge for clients that only speak stdio.
//
// The server knows nothing about the workbench: the host supplies a function
// returning the current gate.Gate, so every request sees the live backend,
// policy, mode and approver. Every tool call goes through that Gate, which is
// the only place approval rules live.
package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
)

// Path is the single endpoint the server answers on.
const Path = "/mcp"

// MinTokenLen is the shortest accepted bearer token.
const MinTokenLen = 24

// Defaults for Config.
const (
	DefaultMaxBody     int64 = 1 << 20
	DefaultCallTimeout       = 5 * time.Minute
	maxBatch                 = 16
)

// Protocol versions this server speaks, newest first.
var supportedVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

const instructions = "Tools to inspect and operate the terminal sessions the user has open in Motty. " +
	"Call list_sessions first to get a session id. list_hosts lists saved SSH hosts explicitly opened to agents, " +
	"including hosts without a session; its host ids are not terminal session ids. Commands and file changes may need the user's approval " +
	"in the Motty window; such a call waits for the answer and is refused if the user declines."

// Config configures a Server.
type Config struct {
	// Token is the bearer token clients must present (at least 24 characters).
	Token string
	// Gate supplies the gate for each tool call.
	Gate func() gate.Gate
	// Tools nil preserves the public server's default catalog. An explicit
	// empty slice exposes no tools. Dispatch is used by request-scoped agents.
	Tools    []tools.Spec
	Dispatch func(context.Context, string, []byte) gate.Result
	// ServerName and Version appear in the initialize reply.
	ServerName, Version string
	// Instructions is the usage note in the initialize reply; empty keeps the
	// one for the sessions of open windows.
	Instructions string
	// MaxBody limits a request body; default 1 MiB.
	MaxBody int64
	// CallTimeout limits one tools/call; default 5 minutes.
	CallTimeout time.Duration
}

// Server is the MCP HTTP server.
type Server struct {
	tokenHash [sha256.Size]byte
	gate      func() gate.Gate
	name, ver string
	guidance  string
	maxBody   int64
	timeout   time.Duration
	toolsList json.RawMessage
	allowed   map[string]bool
	dispatch  func(context.Context, string, []byte) gate.Result

	base   context.Context
	cancel context.CancelFunc

	mu       sync.Mutex // guards http and closed: Serve and Shutdown may race
	http     *http.Server
	closed   bool
	sessions map[string]time.Time
}

// New validates cfg and builds a Server.
func New(cfg Config) (*Server, error) {
	if len(cfg.Token) < MinTokenLen {
		return nil, fmt.Errorf("mcp: token must be at least %d characters", MinTokenLen)
	}
	for i := 0; i < len(cfg.Token); i++ {
		if c := cfg.Token[i]; c <= ' ' || c >= 0x7f {
			return nil, errors.New("mcp: token must be printable ASCII without spaces")
		}
	}
	if cfg.Gate == nil && cfg.Dispatch == nil {
		return nil, errors.New("mcp: Gate provider is required")
	}
	s := &Server{
		tokenHash: sha256.Sum256([]byte(cfg.Token)),
		gate:      cfg.Gate,
		name:      cfg.ServerName,
		ver:       cfg.Version,
		maxBody:   cfg.MaxBody,
		timeout:   cfg.CallTimeout,
		dispatch:  cfg.Dispatch,
		allowed:   make(map[string]bool),
		sessions:  make(map[string]time.Time),
	}
	s.guidance = cfg.Instructions
	if s.guidance == "" {
		s.guidance = instructions
	}
	if s.name == "" {
		s.name = "motty"
	}
	if s.ver == "" {
		s.ver = "0"
	}
	if s.maxBody <= 0 {
		s.maxBody = DefaultMaxBody
	}
	if s.timeout <= 0 {
		s.timeout = DefaultCallTimeout
	}
	specs := cfg.Tools
	if specs == nil {
		specs = tools.All()
	}
	for _, spec := range specs {
		if spec.Name == "" || s.allowed[spec.Name] {
			return nil, errors.New("mcp: invalid scoped tool catalog")
		}
		s.allowed[spec.Name] = true
	}
	list, err := buildToolsListFor(specs)
	if err != nil {
		return nil, err
	}
	s.toolsList = list
	s.base, s.cancel = context.WithCancel(context.Background())
	return s, nil
}

// GenerateToken returns a fresh random token: 32 bytes, base64url.
func GenerateToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("mcp: no randomness: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// Listen opens a TCP listener on a loopback address only. The host must be a
// loopback IP or "localhost"; the port may be 0.
func Listen(addr string) (net.Listener, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("mcp: bad listen address: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("mcp: refusing to listen on non-loopback address %q", addr)
	}
	return net.Listen("tcp", net.JoinHostPort(ip.String(), port))
}

// Serve accepts connections on ln until Shutdown. It returns nil after a
// clean shutdown.
func (s *Server) Serve(ln net.Listener) error {
	server := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return s.base },
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	s.http = server
	s.mu.Unlock()
	err := server.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown cancels in-flight tool calls and stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.cancel()
	s.mu.Lock()
	s.closed = true
	server := s.http
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

// Handler returns the HTTP handler (also usable with httptest).
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != Path {
		http.NotFound(w, r)
		return
	}
	// DNS-rebinding protection first: a page in the user's browser must not be
	// able to reach this server even if it somehow learned the token.
	if !loopbackHost(r.Host) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !loopbackOrigin(origin) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	if !s.authorized(r.Header.Get("Authorization")) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="motty-mcp"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !supported(v) {
		http.Error(w, "unsupported MCP-Protocol-Version", http.StatusBadRequest)
		return
	}
	if r.ContentLength > s.maxBody {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "cannot read request body", http.StatusBadRequest)
		}
		return
	}
	ctx := r.Context()
	if id := r.Header.Get("Mcp-Session-Id"); id != "" {
		s.mu.Lock()
		expires, ok := s.sessions[id]
		if ok && time.Now().Before(expires) {
			s.sessions[id] = time.Now().Add(30 * time.Minute)
		} else {
			ok = false
			delete(s.sessions, id)
		}
		s.mu.Unlock()
		if !ok {
			http.Error(w, "MCP session not found", http.StatusNotFound)
			return
		}
		ctx = gate.WithOutputOwner(ctx, id)
	} else {
		var request rpcRequest
		if json.Unmarshal(body, &request) == nil && request.JSONRPC == "2.0" && request.Method == "initialize" && len(request.ID) > 0 && validID(request.ID) {
			var random [24]byte
			if _, err := rand.Read(random[:]); err != nil {
				http.Error(w, "cannot create MCP session", http.StatusInternalServerError)
				return
			}
			id := base64.RawURLEncoding.EncodeToString(random[:])
			s.mu.Lock()
			for key, expires := range s.sessions {
				if time.Now().After(expires) {
					delete(s.sessions, key)
				}
			}
			if len(s.sessions) >= 128 {
				s.mu.Unlock()
				http.Error(w, "too many MCP sessions", http.StatusServiceUnavailable)
				return
			}
			s.sessions[id] = time.Now().Add(30 * time.Minute)
			s.mu.Unlock()
			w.Header().Set("Mcp-Session-Id", id)
			ctx = gate.WithOutputOwner(ctx, id)
		}
	}
	out := s.handleBody(ctx, body)
	if r.Context().Err() != nil {
		return // the client left; nobody to answer
	}
	if out == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(out)
}

// authorized compares the presented bearer token with the configured one in
// constant time. Both sides are hashed first so the comparison also hides the
// token's length.
func (s *Server) authorized(header string) bool {
	const prefix = "bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	got := sha256.Sum256([]byte(strings.TrimSpace(header[len(prefix):])))
	return subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) == 1
}

func supported(v string) bool {
	for _, s := range supportedVersions {
		if s == v {
			return true
		}
	}
	return false
}

func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false // includes the opaque "null" origin
	}
	return loopbackHost(u.Host)
}

// JSON-RPC plumbing.

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

var nullID = json.RawMessage("null")

func errorResponse(id json.RawMessage, code int, msg string) []byte {
	if len(id) == 0 {
		id = nullID
	}
	b, _ := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
	return b
}

func resultResponse(id json.RawMessage, result json.RawMessage) []byte {
	b, _ := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
	return b
}

// handleBody answers one POST body: a message or a batch. nil means there is
// nothing to send back (202).
func (s *Server) handleBody(ctx context.Context, body []byte) []byte {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return errorResponse(nil, codeParse, "empty body")
	}
	if trimmed[0] != '[' {
		return s.handleMessage(ctx, []byte(trimmed))
	}
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
		return errorResponse(nil, codeParse, "parse error")
	}
	if len(items) == 0 || len(items) > maxBatch {
		return errorResponse(nil, codeInvalidRequest, fmt.Sprintf("a batch holds 1 to %d messages", maxBatch))
	}
	var out []byte
	n := 0
	for _, item := range items {
		resp := s.handleMessage(ctx, item)
		if resp == nil {
			continue
		}
		if n == 0 {
			out = append(out, '[')
		} else {
			out = append(out, ',')
		}
		out = append(out, resp...)
		n++
	}
	if n == 0 {
		return nil
	}
	return append(out, ']')
}

func (s *Server) handleMessage(ctx context.Context, raw []byte) []byte {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		var syn *json.SyntaxError
		if errors.As(err, &syn) || len(raw) == 0 {
			return errorResponse(nil, codeParse, "parse error")
		}
		return errorResponse(nil, codeInvalidRequest, "invalid request")
	}
	hasID := len(req.ID) > 0
	if hasID && !validID(req.ID) {
		return errorResponse(nil, codeInvalidRequest, "id must be a string or a number")
	}
	// A response from the client (to a request we never send) is ignored.
	if req.Method == "" && (len(req.Result) > 0 || len(req.Error) > 0) {
		return nil
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return errorResponse(req.ID, codeInvalidRequest, "invalid request")
	}
	if !hasID {
		return nil // notification: acknowledged by the transport, never answered
	}
	switch req.Method {
	case "initialize":
		return resultResponse(req.ID, s.initialize(req.Params))
	case "ping":
		return resultResponse(req.ID, json.RawMessage(`{}`))
	case "tools/list":
		return resultResponse(req.ID, s.toolsList)
	case "tools/call":
		result, perr := s.callTool(ctx, req.Params)
		if perr != nil {
			return errorResponse(req.ID, perr.Code, perr.Message)
		}
		return resultResponse(req.ID, result)
	}
	return errorResponse(req.ID, codeMethodNotFound, "method not found: "+req.Method)
}

func validID(id json.RawMessage) bool {
	switch id[0] {
	case '"', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

func (s *Server) initialize(params json.RawMessage) json.RawMessage {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)
	version := supportedVersions[0]
	if supported(p.ProtocolVersion) {
		version = p.ProtocolVersion
	}
	b, _ := json.Marshal(struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		ServerInfo      map[string]any `json:"serverInfo"`
		Instructions    string         `json:"instructions"`
	}{
		ProtocolVersion: version,
		Capabilities:    map[string]any{"tools": map[string]any{"listChanged": false}},
		ServerInfo:      map[string]any{"name": s.name, "version": s.ver},
		Instructions:    s.guidance,
	})
	return b
}

func buildToolsList() (json.RawMessage, error) {
	return buildToolsListFor(tools.All())
}

func buildToolsListFor(specs []tools.Spec) (json.RawMessage, error) {
	type annotations struct {
		ReadOnlyHint    bool  `json:"readOnlyHint"`
		DestructiveHint *bool `json:"destructiveHint,omitempty"`
	}
	type entry struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
		Annotations annotations     `json:"annotations"`
	}
	yes, no := true, false
	list := make([]entry, 0, len(specs))
	for _, sp := range specs {
		a := annotations{ReadOnlyHint: sp.Kind == tools.Read, DestructiveHint: &yes}
		if sp.Kind == tools.Read {
			a.DestructiveHint = &no
		}
		list = append(list, entry{sp.Name, sp.Description, sp.Schema, a})
	}
	return json.Marshal(struct {
		Tools []entry `json:"tools"`
	}{list})
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	Content           []textContent   `json:"content"`
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
}

func toolResult(text string, isErr bool) json.RawMessage {
	b, _ := json.Marshal(callResult{Content: []textContent{{"text", text}}, IsError: isErr})
	return b
}

// callTool runs one tools/call through the Gate. Only malformed requests are
// protocol errors; everything the tool does is reported in the result.
func (s *Server) callTool(ctx context.Context, params json.RawMessage) (json.RawMessage, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(params) == 0 || json.Unmarshal(params, &p) != nil {
		return nil, &rpcError{codeInvalidParams, "params must be an object with name and arguments"}
	}
	if p.Name == "" {
		return nil, &rpcError{codeInvalidParams, "name is required"}
	}
	if !s.allowed[p.Name] {
		return nil, &rpcError{codeInvalidParams, "unknown tool: " + p.Name}
	}
	args := strings.TrimSpace(string(p.Arguments))
	if args != "" && args != "null" && args[0] != '{' {
		return nil, &rpcError{codeInvalidParams, "arguments must be an object"}
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	// Shutdown cancels every call, including ones served through Handler alone.
	defer context.AfterFunc(s.base, cancel)()

	done := make(chan gate.Result, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- gate.Result{Text: "internal error while running the tool", IsError: true}
			}
		}()
		if s.dispatch != nil {
			done <- s.dispatch(ctx, p.Name, []byte(args))
			return
		}
		g := s.gate()
		if g.Backend == nil {
			done <- gate.Result{Text: "the assistant backend is not available", IsError: true}
			return
		}
		done <- g.Run(ctx, p.Name, []byte(args))
	}()
	select {
	case res := <-done:
		encoded, err := json.Marshal(callResult{Content: []textContent{{"text", res.Text}}, IsError: res.IsError, StructuredContent: res.Structured})
		if err != nil {
			return toolResult("cannot encode tool result: "+err.Error(), true), nil
		}
		return encoded, nil
	case <-ctx.Done():
		// The goroutine ends when the backend honours ctx; it never blocks on done.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return toolResult(fmt.Sprintf("the tool call timed out after %s", s.timeout), true), nil
		}
		return toolResult("the tool call was cancelled", true), nil
	}
}
