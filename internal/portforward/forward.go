// Package portforward runs explicitly requested TCP forwards over an
// authenticated SSH connection.
package portforward

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
)

// DialFunc opens one stream to the configured destination.
type DialFunc func(context.Context, string, string) (net.Conn, error)

// ListenFunc opens a listener through an authenticated transport.
type ListenFunc func(string, string) (net.Listener, error)

// Mode selects how accepted local connections choose their remote destination.
type Mode uint8

const (
	ModeLocal Mode = iota
	ModeSOCKS5
	ModeHTTPConnect
	ModeRemote
)

// Config describes one TCP listener and how it selects destinations.
type Config struct {
	ListenAddress string
	Destination   string
	Mode          Mode
	MaxStreams    int
	Listen        ListenFunc
	Dial          DialFunc
	Notify        func()
}

// Status is a consistent snapshot of a running forward.
type Status struct {
	Version       uint64
	ListenAddress string
	Active        int
	Accepted      uint64
	Uploaded      uint64
	Downloaded    uint64
	LastError     error
	Closed        bool
}

// Forward owns one loopback listener and the streams accepted from it.
type Forward struct {
	listener    net.Listener
	destination string
	mode        Mode
	dial        DialFunc
	notify      func()
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	closeOnce   sync.Once
	wait        sync.WaitGroup
	streams     chan struct{}

	mu     sync.Mutex
	status Status
	peers  map[*peer]struct{}
}

type peer struct {
	local  net.Conn
	remote net.Conn
}

type copyResult struct {
	bytes int64
	err   error
}

type closeWriter interface {
	CloseWrite() error
}

// Start binds a loopback TCP listener and begins forwarding accepted streams.
// ListenAddress must contain a literal loopback IP, never a wildcard or a host
// name, so an accidental configuration cannot expose a service. ModeRemote
// binds through Listen; every other mode binds on the local machine.
func Start(config Config) (*Forward, error) {
	if config.Dial == nil {
		return nil, errors.New("portforward: no remote dialer")
	}
	if err := validateAddress(config.ListenAddress, true); err != nil {
		return nil, fmt.Errorf("portforward: listen address: %w", err)
	}
	switch config.Mode {
	case ModeLocal, ModeRemote:
		if err := validateAddress(config.Destination, false); err != nil {
			return nil, fmt.Errorf("portforward: destination: %w", err)
		}
		if config.Mode == ModeRemote && config.Listen == nil {
			return nil, errors.New("portforward: no remote listener")
		}
	case ModeSOCKS5, ModeHTTPConnect:
		if config.Destination != "" {
			return nil, errors.New("portforward: proxy destination must be selected by the client")
		}
	default:
		return nil, fmt.Errorf("portforward: unsupported mode %d", config.Mode)
	}
	listen := net.Listen
	if config.Mode == ModeRemote {
		listen = config.Listen
	}
	listener, err := listen("tcp", config.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("portforward: listen on %s: %w", config.ListenAddress, err)
	}
	var streams chan struct{}
	if config.MaxStreams > 0 {
		streams = make(chan struct{}, config.MaxStreams)
	}
	ctx, cancel := context.WithCancel(context.Background())
	forward := &Forward{
		listener: listener, destination: config.Destination, mode: config.Mode, dial: config.Dial, notify: config.Notify,
		ctx: ctx, cancel: cancel, done: make(chan struct{}), peers: make(map[*peer]struct{}), streams: streams,
	}
	forward.status = Status{Version: 1, ListenAddress: listener.Addr().String()}
	forward.wait.Add(1)
	go forward.accept()
	return forward, nil
}

// Status returns the current listener and transfer counters.
func (forward *Forward) Status() Status {
	forward.mu.Lock()
	defer forward.mu.Unlock()
	return forward.status
}

// Done is closed after Close has stopped the listener and every active stream.
func (forward *Forward) Done() <-chan struct{} { return forward.done }

// Close stops accepting and closes every active local and remote stream. It is
// safe to call more than once.
func (forward *Forward) Close() {
	forward.closeOnce.Do(func() {
		forward.cancel()
		forward.mu.Lock()
		forward.status.Closed = true
		forward.status.Version++
		peers := make([]peer, 0, len(forward.peers))
		for connection := range forward.peers {
			peers = append(peers, *connection)
		}
		forward.mu.Unlock()
		listenerClosed := make(chan struct{})
		go func() {
			_ = forward.listener.Close()
			close(listenerClosed)
		}()
		// Close every local socket before waiting for any SSH close reply.
		for _, connection := range peers {
			local := connection.local
			if forward.mode == ModeRemote {
				local = connection.remote
			}
			if local != nil {
				_ = local.Close()
			}
		}
		for _, connection := range peers {
			remote := connection.remote
			if forward.mode == ModeRemote {
				remote = connection.local
			}
			if remote != nil {
				_ = remote.Close()
			}
		}
		<-listenerClosed
		forward.wait.Wait()
		close(forward.done)
		forward.wake()
	})
}

// CloseAsync frees local listeners immediately. Remote listener cancellation
// also runs in the background because SSH may wait for the server's reply.
func (forward *Forward) CloseAsync() {
	forward.cancel()
	if forward.mode != ModeRemote {
		_ = forward.listener.Close()
	}
	go forward.Close()
}

func (forward *Forward) accept() {
	defer forward.wait.Done()
	for {
		local, err := forward.listener.Accept()
		if err != nil {
			forward.acceptError(err)
			return
		}
		connection := &peer{local: local}
		forward.mu.Lock()
		if forward.status.Closed {
			forward.mu.Unlock()
			_ = local.Close()
			return
		}
		if forward.streams != nil {
			select {
			case forward.streams <- struct{}{}:
			default:
				forward.mu.Unlock()
				_ = local.Close()
				continue
			}
		}
		forward.peers[connection] = struct{}{}
		forward.status.Active++
		forward.status.Accepted++
		forward.status.Version++
		forward.wait.Add(1)
		forward.mu.Unlock()
		forward.wake()
		go forward.serve(connection)
	}
}

func (forward *Forward) serve(connection *peer) {
	defer forward.wait.Done()
	if forward.streams != nil {
		defer func() { <-forward.streams }()
	}
	destination := forward.destination
	localReader := io.Reader(connection.local)
	switch forward.mode {
	case ModeSOCKS5:
		var err error
		destination, err = readSOCKS5Request(connection.local, connection.local)
		if err != nil {
			_ = connection.local.Close()
			forward.finish(connection, 0, 0, err)
			return
		}
	case ModeHTTPConnect:
		reader := bufio.NewReaderSize(connection.local, httpConnectBufferSize)
		var err error
		destination, err = readHTTPConnectRequest(reader, connection.local)
		if err != nil {
			_ = connection.local.Close()
			forward.finish(connection, 0, 0, err)
			return
		}
		localReader = reader
	}
	remote, err := forward.dial(forward.ctx, "tcp", destination)
	if err != nil {
		if forward.ctx.Err() == nil {
			switch forward.mode {
			case ModeSOCKS5:
				_ = writeSOCKS5Reply(connection.local, socks5ReplyForError(err))
			case ModeHTTPConnect:
				_ = writeHTTPConnectReply(connection.local, httpConnectReplyForError(err))
			}
		}
		_ = connection.local.Close()
		forward.finish(connection, 0, 0, err)
		return
	}
	forward.mu.Lock()
	if forward.status.Closed {
		forward.mu.Unlock()
		_ = connection.local.Close()
		_ = remote.Close()
		forward.finish(connection, 0, 0, nil)
		return
	}
	connection.remote = remote
	forward.mu.Unlock()
	var replyErr error
	switch forward.mode {
	case ModeSOCKS5:
		replyErr = writeSOCKS5Reply(connection.local, socks5ReplySucceeded)
	case ModeHTTPConnect:
		replyErr = writeHTTPConnectReply(connection.local, httpConnectReplySuccess)
	}
	if replyErr != nil {
		_ = connection.local.Close()
		_ = remote.Close()
		forward.finish(connection, 0, 0, replyErr)
		return
	}
	forward.mu.Lock()
	clearedError := forward.status.LastError != nil
	if clearedError {
		forward.status.LastError = nil
		forward.status.Version++
	}
	forward.mu.Unlock()
	if clearedError {
		forward.wake()
	}

	uploaded := make(chan copyResult, 1)
	go func() {
		count, copyErr := io.Copy(remote, localReader)
		if writer, ok := remote.(closeWriter); ok {
			_ = writer.CloseWrite()
		}
		uploaded <- copyResult{bytes: count, err: copyErr}
	}()
	downloaded, downloadErr := io.Copy(connection.local, remote)
	_ = connection.local.Close()
	_ = remote.Close()
	upload := <-uploaded
	if downloadErr == nil {
		downloadErr = upload.err
	}
	uploadedBytes, downloadedBytes := uint64(upload.bytes), uint64(downloaded)
	if forward.mode == ModeRemote {
		uploadedBytes, downloadedBytes = downloadedBytes, uploadedBytes
	}
	forward.finish(connection, uploadedBytes, downloadedBytes, downloadErr)
}

func (forward *Forward) finish(connection *peer, uploaded, downloaded uint64, err error) {
	forward.mu.Lock()
	if _, ok := forward.peers[connection]; ok {
		delete(forward.peers, connection)
		forward.status.Active--
	}
	forward.status.Uploaded += uploaded
	forward.status.Downloaded += downloaded
	if err != nil && !forward.status.Closed {
		forward.status.LastError = err
	}
	forward.status.Version++
	forward.mu.Unlock()
	forward.wake()
}

func (forward *Forward) acceptError(err error) {
	forward.mu.Lock()
	if !forward.status.Closed && !errors.Is(err, net.ErrClosed) {
		forward.status.LastError = err
		forward.status.Version++
		forward.mu.Unlock()
		forward.wake()
		return
	}
	forward.mu.Unlock()
}

func (forward *Forward) wake() {
	if forward.notify != nil {
		forward.notify()
	}
}

func validateAddress(address string, loopback bool) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return errors.New("expected host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 || !loopback && port == 0 {
		return errors.New("port must be 1-65535")
	}
	if !loopback {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("listener must use a literal loopback address")
	}
	return nil
}
