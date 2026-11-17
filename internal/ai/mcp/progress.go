package mcp

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"

	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
)

// rzszProgressCall reports whether this body is one rz/sz tool call whose
// client asked for progress and can read an event stream. Other calls stay a
// single JSON response.
func rzszProgressCall(accept string, body []byte) (token json.RawMessage, ok bool) {
	if !headerAcceptsEventStream(accept) {
		return nil, false
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed[0] == '[' {
		return nil, false
	}
	var req rpcRequest
	if json.Unmarshal([]byte(trimmed), &req) != nil || req.Method != "tools/call" || len(req.ID) == 0 || !validID(req.ID) {
		return nil, false
	}
	var params struct {
		Name string `json:"name"`
		Meta struct {
			ProgressToken json.RawMessage `json:"progressToken"`
		} `json:"_meta"`
	}
	if json.Unmarshal(req.Params, &params) != nil {
		return nil, false
	}
	if params.Name != tools.NameRZSZUpload && params.Name != tools.NameRZSZDownload {
		return nil, false
	}
	if !validProgressToken(params.Meta.ProgressToken) {
		return nil, false
	}
	return params.Meta.ProgressToken, true
}

func headerAcceptsEventStream(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		mt, _, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err == nil && mt == "text/event-stream" {
			return true
		}
	}
	return false
}

func validProgressToken(raw json.RawMessage) bool {
	if len(raw) == 0 || len(raw) > 256 || string(raw) == "null" {
		return false
	}
	if raw[0] == '"' {
		var text string
		return json.Unmarshal(raw, &text) == nil && text != ""
	}
	if raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9') {
		var number float64
		return json.Unmarshal(raw, &number) == nil
	}
	return false
}

// serveRZSZProgress answers one rz/sz call as an event stream: progress
// notifications while bytes move, then the tool result. Headers are sent
// before the call runs, so a client can see the first event before the file
// has finished. Writing the event does not run on the transfer's goroutine:
// a client that has stopped reading must not stall the transfer.
func (s *Server) serveRZSZProgress(w http.ResponseWriter, flusher http.Flusher, ctx context.Context, body []byte, token json.RawMessage) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sink := newSSESink(w, flusher)
	defer sink.stop()
	var mu sync.Mutex
	var last float64
	sent := false
	ctx = gate.WithProgress(ctx, func(report gate.ProgressReport) {
		mu.Lock()
		defer mu.Unlock()
		if sent && report.Progress <= last {
			return
		}
		sent, last = true, report.Progress
		sink.send(progressNotification(token, report))
	})
	out := s.handleBody(ctx, body)
	sink.stop()
	if ctx.Err() != nil || out == nil {
		return
	}
	writeSSE(w, out)
	flusher.Flush()
}

// sseSink writes the newest progress event from its own goroutine. send keeps
// only that newest event, so a stalled client cannot pile up or block the caller.
type sseSink struct {
	w       io.Writer
	flusher http.Flusher
	mu      sync.Mutex
	latest  []byte
	dirty   bool
	wake    chan struct{}
	stopCh  chan struct{}
	done    chan struct{}
	once    sync.Once
}

func newSSESink(w io.Writer, flusher http.Flusher) *sseSink {
	sink := &sseSink{
		w: w, flusher: flusher,
		wake:   make(chan struct{}, 1),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	go sink.loop()
	return sink
}

func (sink *sseSink) send(payload []byte) {
	sink.mu.Lock()
	sink.latest = append(sink.latest[:0], payload...)
	sink.dirty = true
	sink.mu.Unlock()
	select {
	case sink.wake <- struct{}{}:
	default:
	}
}

func (sink *sseSink) loop() {
	defer close(sink.done)
	for {
		select {
		case <-sink.stopCh:
			sink.flush()
			return
		case <-sink.wake:
			sink.flush()
		}
	}
}

func (sink *sseSink) flush() {
	sink.mu.Lock()
	if !sink.dirty {
		sink.mu.Unlock()
		return
	}
	payload := append([]byte(nil), sink.latest...)
	sink.dirty = false
	sink.mu.Unlock()
	writeSSE(sink.w, payload)
	sink.flusher.Flush()
}

func (sink *sseSink) stop() {
	sink.once.Do(func() { close(sink.stopCh) })
	<-sink.done
}

func progressNotification(token json.RawMessage, report gate.ProgressReport) []byte {
	params := struct {
		ProgressToken json.RawMessage `json:"progressToken"`
		Progress      float64         `json:"progress"`
		Total         *float64        `json:"total,omitempty"`
		Message       string          `json:"message,omitempty"`
	}{ProgressToken: token, Progress: report.Progress, Message: report.Message}
	if report.HasTotal {
		total := report.Total
		params.Total = &total
	}
	body, _ := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{JSONRPC: "2.0", Method: "notifications/progress", Params: params})
	return body
}

func writeSSE(w io.Writer, payload []byte) {
	io.WriteString(w, "event: message\ndata: ")
	w.Write(payload)
	io.WriteString(w, "\n\n")
}
