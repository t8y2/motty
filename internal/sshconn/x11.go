package sshconn

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"

	"motty/internal/x11fwd"
)

// ErrX11Refused means the server declined the x11-req: X11Forwarding is off in
// its sshd_config, or it has no xauth.
var ErrX11Refused = errors.New("the server refused X11 forwarding")

// x11Request is the payload of an "x11-req" session request (RFC 4254 6.3.1).
type x11Request struct {
	SingleConnection bool
	AuthProtocol     string
	AuthCookie       string
	Screen           uint32
}

// requestX11 asks the server to forward X11 for session, with a fake cookie
// that only this session knows. Without a local display or with a server that
// refuses, it returns the reason and the session carries on without X11.
func (conn *Conn) requestX11(session *ssh.Session, env x11fwd.Environ) (*x11fwd.Grant, error) {
	hub, err := conn.x11Hub()
	if err != nil {
		return nil, err
	}
	grant, err := hub.Register(env)
	if err != nil {
		return nil, err
	}
	ok, err := session.SendRequest("x11-req", true, ssh.Marshal(x11Request{
		AuthProtocol: grant.Protocol(),
		AuthCookie:   grant.Cookie(),
		Screen:       grant.Screen(),
	}))
	if err != nil || !ok {
		grant.Release()
		if err == nil {
			err = ErrX11Refused
		}
		return nil, err
	}
	return grant, nil
}

// x11Hub returns the hub serving x11 channels of the connection's current
// client, starting it on first use. All tabs sharing the client share it.
func (conn *Conn) x11Hub() (*x11fwd.Hub, error) {
	owner := conn
	if conn.owner != nil {
		owner = conn.owner
	}
	client := conn.Client()
	if client == nil {
		return nil, errors.New("not connected")
	}
	owner.x11mu.Lock()
	defer owner.x11mu.Unlock()
	if owner.x11 != nil && owner.x11client == client {
		return owner.x11, nil
	}
	channels := client.HandleChannelOpen("x11")
	if channels == nil {
		return nil, fmt.Errorf("x11 channels are already handled")
	}
	hub := x11fwd.NewHub()
	owner.x11, owner.x11client = hub, client
	go hub.Serve(channels)
	return hub, nil
}

// X11Err is why X11 forwarding is not active although it was asked for, or nil
// when it is active or was not asked for.
func (shell *Shell) X11Err() error { return shell.x11Err }

func (shell *Shell) releaseX11() {
	if shell.x11 != nil {
		shell.x11.Release()
	}
}
