package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"
)

// MaxLine is the longest line the bridge forwards.
const MaxLine = 1 << 20

const (
	maxBridgeReply    = 8 << 20
	maxBridgeInFlight = 32
	codeTransport     = -32000
)

// Bridge connects a stdio MCP client to the HTTP server: each newline-delimited
// JSON-RPC line from in is POSTed to endpoint with the bearer token, and each
// response body is written to out as one compact line (nothing for 202).
// An rz/sz call that reports progress comes back as an event stream; each
// event is written as its own line as soon as it arrives, so a client sees
// bytes move before the transfer finishes. Lines are handled concurrently so
// a call waiting for the user's approval does not block pings; writes to out
// are serialised. It returns nil on EOF after pending replies are written, or
// ctx's error if ctx ends first.
func Bridge(ctx context.Context, in io.Reader, out io.Writer, endpoint, token string, client *http.Client) error {
	if client == nil {
		client = http.DefaultClient
	}
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, maxBridgeInFlight)
		session bridgeSession
	)
	write := func(b []byte) {
		mu.Lock()
		defer mu.Unlock()
		out.Write(append(b, '\n'))
	}
	br := bufio.NewReaderSize(in, 64<<10)
	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		defer close(lines)
		for {
			line, tooLong, err := readLine(br)
			if tooLong {
				write(errorResponse(nil, codeInvalidRequest, fmt.Sprintf("line longer than %d bytes", MaxLine)))
			} else if len(line) > 0 {
				select {
				case lines <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				readErr <- err
				return
			}
		}
	}()
loop:
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				break loop
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				return ctx.Err()
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				forward(ctx, client, endpoint, token, line, &session, write)
			}()
		}
	}
	wg.Wait()
	select {
	case err := <-readErr:
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	default:
	}
	return ctx.Err()
}

// readLine returns the next non-oversized line without its newline. tooLong
// means the line was discarded.
func readLine(br *bufio.Reader) (line []byte, tooLong bool, err error) {
	var buf []byte
	for {
		part, isPrefix, e := br.ReadLine()
		if e != nil {
			if len(buf) > 0 && !tooLong {
				return bytes.TrimSpace(buf), false, e
			}
			return nil, tooLong, e
		}
		if !tooLong {
			if len(buf)+len(part) > MaxLine {
				tooLong, buf = true, nil
			} else {
				buf = append(buf, part...)
			}
		}
		if !isPrefix {
			if tooLong {
				return nil, true, nil
			}
			return bytes.TrimSpace(buf), false, nil
		}
	}
}

type bridgeSession struct {
	mu sync.Mutex
	id string
}

// forward posts one line and writes every JSON-RPC message it produced.
func forward(ctx context.Context, client *http.Client, endpoint, token string, line []byte, session *bridgeSession, write func([]byte)) {
	id := lineID(line)
	fail := func(msg string) {
		if id == nil {
			return // a notification gets no reply, not even an error
		}
		write(errorResponse(id, codeTransport, msg))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(line))
	if err != nil {
		fail("bad endpoint")
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	session.mu.Lock()
	if session.id != "" {
		req.Header.Set("Mcp-Session-Id", session.id)
	}
	session.mu.Unlock()
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		fail("cannot reach Motty: " + errText(err))
		return
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Mcp-Session-Id"); got != "" {
		session.mu.Lock()
		session.id = got
		session.mu.Unlock()
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil && mt == "text/event-stream" {
		if resp.StatusCode != http.StatusOK {
			fail(fmt.Sprintf("Motty answered HTTP %d", resp.StatusCode))
			return
		}
		streamEvents(resp.Body, id, write)
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBridgeReply+1))
	if err != nil {
		fail("reading the reply failed")
		return
	}
	if resp.StatusCode == http.StatusAccepted {
		return
	}
	if resp.StatusCode != http.StatusOK {
		fail(fmt.Sprintf("Motty answered HTTP %d", resp.StatusCode))
		return
	}
	if len(body) > maxBridgeReply {
		fail("reply too large")
		return
	}
	var compact bytes.Buffer
	if json.Compact(&compact, body) != nil {
		fail("Motty sent a reply that is not JSON")
		return
	}
	write(compact.Bytes())
}

// streamEvents writes each SSE event as one JSON line as soon as it arrives.
// A stream that ends without the tool result still answers the request.
func streamEvents(body io.Reader, id json.RawMessage, write func([]byte)) {
	sawResponse := false
	err := readSSE(body, func(payload []byte) error {
		if len(payload) > maxBridgeReply {
			return errors.New("reply too large")
		}
		var compact bytes.Buffer
		if json.Compact(&compact, payload) != nil {
			return errors.New("Motty sent a reply that is not JSON")
		}
		if lineID(compact.Bytes()) != nil {
			sawResponse = true
		}
		write(compact.Bytes())
		return nil
	})
	if sawResponse || id == nil {
		return
	}
	msg := "the progress stream ended before a result"
	if err != nil {
		msg = err.Error()
	}
	write(errorResponse(id, codeTransport, msg))
}

// readSSE calls emit with each event's data payload.
func readSSE(r io.Reader, emit func([]byte) error) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var data []byte
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		payload := data
		data = nil
		return emit(payload)
	}
	for {
		line, err := br.ReadSlice('\n')
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			switch {
			case len(line) == 0:
				if err2 := flush(); err2 != nil {
					return err2
				}
			case bytes.HasPrefix(line, []byte("data:")):
				part := line[len("data:"):]
				if len(part) > 0 && part[0] == ' ' {
					part = part[1:]
				}
				if len(data)+len(part) > maxBridgeReply {
					return errors.New("reply too large")
				}
				data = append(data, part...)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return flush()
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				return errors.New("reply too large")
			}
			return err
		}
	}
}

// lineID extracts a request id; nil for notifications and unparsable lines.
func lineID(line []byte) json.RawMessage {
	var m struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(line, &m) != nil || len(m.ID) == 0 || !validID(m.ID) {
		return nil
	}
	return m.ID
}

// errText drops the URL a net/http error carries, so nothing sensitive is echoed.
func errText(err error) string {
	var ue interface{ Unwrap() error }
	if errors.As(err, &ue) {
		if inner := ue.Unwrap(); inner != nil {
			return inner.Error()
		}
	}
	return err.Error()
}
