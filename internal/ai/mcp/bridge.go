package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
// Lines are handled concurrently so a call waiting for the user's approval does
// not block pings; writes to out are serialised. It returns nil on EOF after
// pending replies are written, or ctx's error if ctx ends first.
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
				if resp := forward(ctx, client, endpoint, token, line, &session); resp != nil {
					write(resp)
				}
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

// forward posts one line and returns the line to print, or nil.
func forward(ctx context.Context, client *http.Client, endpoint, token string, line []byte, session *bridgeSession) []byte {
	id := lineID(line)
	fail := func(msg string) []byte {
		if id == nil {
			return nil // a notification gets no reply, not even an error
		}
		return errorResponse(id, codeTransport, msg)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(line))
	if err != nil {
		return fail("bad endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	session.mu.Lock()
	if session.id != "" {
		req.Header.Set("Mcp-Session-Id", session.id)
	}
	session.mu.Unlock()
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fail("cannot reach Motty: " + errText(err))
	}
	defer resp.Body.Close()
	if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
		session.mu.Lock()
		session.id = id
		session.mu.Unlock()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBridgeReply+1))
	if err != nil {
		return fail("reading the reply failed")
	}
	if resp.StatusCode == http.StatusAccepted {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Sprintf("Motty answered HTTP %d", resp.StatusCode))
	}
	if len(body) > maxBridgeReply {
		return fail("reply too large")
	}
	var compact bytes.Buffer
	if json.Compact(&compact, body) != nil {
		return fail("Motty sent a reply that is not JSON")
	}
	return compact.Bytes()
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
