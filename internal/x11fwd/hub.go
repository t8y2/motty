package x11fwd

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// MaxChannels bounds the X11 channels open at once on one connection: a
// remote program that opens windows in a loop must not exhaust descriptors.
const MaxChannels = 32

// setupTimeout is how long a new channel may take to send its setup packet.
const setupTimeout = 10 * time.Second

// Hub serves the "x11" channels the server opens on one SSH connection. The
// protocol does not say which session a channel belongs to, so every session
// that asked for forwarding registers a Grant and the cookie in the channel's
// setup packet picks the one it is for.
type Hub struct {
	slots chan struct{}

	mu     sync.Mutex
	grants []*Grant
}

// NewHub returns a hub with no sessions.
func NewHub() *Hub { return &Hub{slots: make(chan struct{}, MaxChannels)} }

// Grant is one session's forwarding: a fake cookie, valid until released, and
// the real display it stands for.
type Grant struct {
	hub    *Hub
	cookie [16]byte
	local  Local
}

// Register creates a grant for the display in env. It fails when there is no
// display to forward to or nothing is listening on it.
func (hub *Hub) Register(env Environ) (*Grant, error) {
	local, err := Resolve(env)
	if err != nil {
		return nil, err
	}
	if err := local.Display.Probe(); err != nil {
		return nil, err
	}
	return hub.registerLocal(local)
}

func (hub *Hub) registerLocal(local Local) (*Grant, error) {
	grant := &Grant{hub: hub, local: local}
	if _, err := rand.Read(grant.cookie[:]); err != nil {
		return nil, err
	}
	hub.mu.Lock()
	hub.grants = append(hub.grants, grant)
	hub.mu.Unlock()
	return grant, nil
}

// Cookie is the fake cookie as the x11-req request carries it: hex text.
func (grant *Grant) Cookie() string { return hex.EncodeToString(grant.cookie[:]) }

// Protocol is the authorization protocol of the request.
func (grant *Grant) Protocol() string { return cookieProtocol }

// Screen is the screen number of the request.
func (grant *Grant) Screen() uint32 { return uint32(grant.local.Display.Screen) }

// Release makes the fake cookie worthless. Channels already open continue.
func (grant *Grant) Release() {
	hub := grant.hub
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for index, other := range hub.grants {
		if other == grant {
			hub.grants = append(hub.grants[:index:index], hub.grants[index+1:]...)
			return
		}
	}
}

// match finds the grant whose fake cookie the setup presents. Every grant is
// compared, in constant time, whatever the outcome.
func (hub *Hub) match(setup Setup) *Grant {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	var found *Grant
	nameOK := setup.AuthName == cookieProtocol
	for _, grant := range hub.grants {
		if subtle.ConstantTimeCompare(setup.AuthData, grant.cookie[:]) == 1 && nameOK {
			found = grant
		}
	}
	return found
}

// Serve accepts the channels of ch until it closes.
func (hub *Hub) Serve(channels <-chan ssh.NewChannel) {
	for request := range channels {
		select {
		case hub.slots <- struct{}{}:
			go func() {
				defer func() { <-hub.slots }()
				hub.handle(request)
			}()
		default:
			request.Reject(ssh.ResourceShortage, "too many X11 channels")
		}
	}
}

func (hub *Hub) handle(request ssh.NewChannel) {
	channel, requests, err := request.Accept()
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	defer channel.Close()
	timer := time.AfterFunc(setupTimeout, func() { channel.Close() })
	setup, err := ReadSetup(channel)
	if !timer.Stop() || err != nil {
		return
	}
	grant := hub.match(setup)
	if grant == nil {
		return
	}
	server, err := grant.local.Display.Dial()
	if err != nil {
		return
	}
	defer server.Close()
	setup.AuthName, setup.AuthData = grant.local.Auth.Name, grant.local.Auth.Data
	if _, err := server.Write(setup.Marshal()); err != nil {
		return
	}
	pipe(channel, server)
}

type halfCloser interface{ CloseWrite() error }

// pipe copies both ways until each direction has ended.
func pipe(channel ssh.Channel, server net.Conn) {
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		io.Copy(server, channel)
		if half, ok := server.(halfCloser); ok {
			half.CloseWrite()
		}
	}()
	io.Copy(channel, server)
	channel.CloseWrite()
	wait.Wait()
}
