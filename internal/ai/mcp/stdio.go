package mcp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
)

// ServeStdio serves cfg over the stdio transport: newline-delimited JSON-RPC
// requests on in, replies on out, until in ends. There is no listener and no
// port; the requests go to the same handler the HTTP server uses, in this
// process, with a token made up for the occasion. It returns nil once in has
// ended and the pending replies are written, or the context's error if ctx ends
// first, and it cancels any tool call still running when it returns.
func ServeStdio(ctx context.Context, in io.Reader, out io.Writer, cfg Config) error {
	if cfg.Token == "" {
		cfg.Token = GenerateToken()
	}
	server, err := New(cfg)
	if err != nil {
		return err
	}
	defer server.Shutdown(context.Background())
	client := &http.Client{Transport: inProcess{server.Handler()}}
	return Bridge(ctx, in, out, "http://127.0.0.1"+Path, cfg.Token, client)
}

// inProcess is an http.RoundTripper that hands each request to a handler in the
// same process and returns what it wrote.
type inProcess struct{ handler http.Handler }

func (transport inProcess) RoundTrip(request *http.Request) (*http.Response, error) {
	recorded := &recordedResponse{header: http.Header{}, status: http.StatusOK}
	transport.handler.ServeHTTP(recorded, request)
	return &http.Response{
		Status:        strconv.Itoa(recorded.status) + " " + http.StatusText(recorded.status),
		StatusCode:    recorded.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        recorded.header,
		Body:          io.NopCloser(bytes.NewReader(recorded.body.Bytes())),
		ContentLength: int64(recorded.body.Len()),
		Request:       request,
	}, nil
}

// recordedResponse is the http.ResponseWriter inProcess gives the handler.
type recordedResponse struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (response *recordedResponse) Header() http.Header { return response.header }

func (response *recordedResponse) WriteHeader(status int) {
	if !response.wrote {
		response.status, response.wrote = status, true
	}
}

func (response *recordedResponse) Write(data []byte) (int, error) {
	response.wrote = true
	return response.body.Write(data)
}
