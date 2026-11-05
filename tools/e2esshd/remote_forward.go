package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"
)

type tcpipForwardRequest struct {
	BindAddress string
	BindPort    uint32
}

type forwardedTCPIPRequest struct {
	ConnectedAddress string
	ConnectedPort    uint32
	OriginAddress    string
	OriginPort       uint32
}

type remoteListener struct {
	listener net.Listener
	address  string
	host     string
	port     uint32
}

type remoteForwardSet struct {
	conn *ssh.ServerConn

	mu        sync.Mutex
	listeners map[string]*remoteListener
	closed    bool
}

func newRemoteForwardSet(conn *ssh.ServerConn) *remoteForwardSet {
	return &remoteForwardSet{conn: conn, listeners: make(map[string]*remoteListener)}
}

func (forwards *remoteForwardSet) Serve(requests <-chan *ssh.Request) {
	defer forwards.Close()
	for request := range requests {
		switch request.Type {
		case "tcpip-forward":
			listener, reply, err := openRemoteListener(request.Payload)
			if err != nil || !forwards.add(listener) {
				if listener != nil {
					_ = listener.listener.Close()
				}
				_ = request.Reply(false, nil)
				continue
			}
			log.Printf("remote-listen %s", listener.address)
			_ = request.Reply(true, reply)
			go forwards.serve(listener)
		case "cancel-tcpip-forward":
			address, ok := forwards.cancel(request.Payload)
			if ok {
				log.Printf("remote-cancel %s", address)
			}
			_ = request.Reply(ok, nil)
		default:
			_ = request.Reply(false, nil)
		}
	}
}

func (forwards *remoteForwardSet) Close() {
	forwards.mu.Lock()
	if forwards.closed {
		forwards.mu.Unlock()
		return
	}
	forwards.closed = true
	listeners := make([]net.Listener, 0, len(forwards.listeners))
	for _, listener := range forwards.listeners {
		listeners = append(listeners, listener.listener)
	}
	clear(forwards.listeners)
	forwards.mu.Unlock()
	for _, listener := range listeners {
		_ = listener.Close()
	}
}

func (forwards *remoteForwardSet) add(listener *remoteListener) bool {
	if listener == nil {
		return false
	}
	forwards.mu.Lock()
	defer forwards.mu.Unlock()
	if forwards.closed || forwards.listeners[listener.address] != nil {
		return false
	}
	forwards.listeners[listener.address] = listener
	return true
}

func (forwards *remoteForwardSet) cancel(payload []byte) (string, bool) {
	address, _, _, err := parseRemoteForwardRequest(payload, false)
	if err != nil {
		return "", false
	}
	forwards.mu.Lock()
	listener := forwards.listeners[address]
	delete(forwards.listeners, address)
	forwards.mu.Unlock()
	if listener == nil {
		return address, false
	}
	_ = listener.listener.Close()
	return address, true
}

func (forwards *remoteForwardSet) directTargets() []directTarget {
	forwards.mu.Lock()
	defer forwards.mu.Unlock()
	targets := make([]directTarget, 0, len(forwards.listeners))
	for _, listener := range forwards.listeners {
		targets = append(targets, directTarget{requested: listener.address, dial: listener.address})
	}
	return targets
}

func (forwards *remoteForwardSet) serve(listener *remoteListener) {
	for {
		incoming, err := listener.listener.Accept()
		if err != nil {
			return
		}
		go forwards.forward(listener, incoming)
	}
}

func (forwards *remoteForwardSet) forward(listener *remoteListener, incoming net.Conn) {
	originHost, originPortText, err := net.SplitHostPort(incoming.RemoteAddr().String())
	if err != nil {
		_ = incoming.Close()
		return
	}
	originPort, err := strconv.ParseUint(originPortText, 10, 32)
	if err != nil {
		_ = incoming.Close()
		return
	}
	payload := ssh.Marshal(forwardedTCPIPRequest{
		ConnectedAddress: listener.host,
		ConnectedPort:    listener.port,
		OriginAddress:    originHost,
		OriginPort:       uint32(originPort),
	})
	channel, requests, err := forwards.conn.OpenChannel("forwarded-tcpip", payload)
	if err != nil {
		_ = incoming.Close()
		return
	}
	log.Printf("remote-forward %s <- %s", listener.address, incoming.RemoteAddr())
	serveDirect(channel, incoming, requests)
}

func openRemoteListener(payload []byte) (*remoteListener, []byte, error) {
	_, host, requestedPort, err := parseRemoteForwardRequest(payload, true)
	if err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(int(requestedPort))))
	if err != nil {
		return nil, nil, err
	}
	_, actualPortText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		return nil, nil, err
	}
	actualPort, err := strconv.ParseUint(actualPortText, 10, 32)
	if err != nil {
		_ = listener.Close()
		return nil, nil, err
	}
	address := net.JoinHostPort(host, strconv.FormatUint(actualPort, 10))
	remote := &remoteListener{listener: listener, address: address, host: host, port: uint32(actualPort)}
	if requestedPort == 0 {
		return remote, ssh.Marshal(struct{ Port uint32 }{remote.port}), nil
	}
	return remote, nil, nil
}

func parseRemoteForwardRequest(payload []byte, allowZero bool) (string, string, uint32, error) {
	var request tcpipForwardRequest
	if err := ssh.Unmarshal(payload, &request); err != nil {
		return "", "", 0, errors.New("invalid tcpip-forward request")
	}
	ip := net.ParseIP(request.BindAddress)
	if ip == nil || !ip.IsLoopback() {
		return "", "", 0, fmt.Errorf("remote bind %q is not a literal loopback address", request.BindAddress)
	}
	if request.BindPort > 65535 || request.BindPort == 0 && !allowZero {
		return "", "", 0, errors.New("remote bind port must be 1-65535")
	}
	address := net.JoinHostPort(request.BindAddress, strconv.Itoa(int(request.BindPort)))
	return address, request.BindAddress, request.BindPort, nil
}
