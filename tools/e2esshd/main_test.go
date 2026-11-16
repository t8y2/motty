package main

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestAllowedDirectTargetIsRestrictedToEchoFixture(t *testing.T) {
	allowed := directTarget{requested: "echo.gop.test:45678", dial: "127.0.0.1:45678"}
	payload := ssh.Marshal(directTCPIPRequest{
		DestinationHost: "echo.gop.test",
		DestinationPort: 45678,
		OriginHost:      "127.0.0.1",
		OriginPort:      54321,
	})
	if target, err := allowedDirectTarget(payload, allowed); err != nil || target != allowed {
		t.Fatalf("target=%q err=%v", target, err)
	}

	for _, request := range []directTCPIPRequest{
		{DestinationHost: "echo.gop.test", DestinationPort: 22},
		{DestinationHost: "example.com", DestinationPort: 45678},
	} {
		if target, err := allowedDirectTarget(ssh.Marshal(request), allowed); err == nil {
			t.Fatalf("unexpected target %+v for %+v", target, request)
		}
	}
	if target, err := allowedDirectTarget([]byte{1, 2, 3}, allowed); err == nil {
		t.Fatalf("malformed payload accepted as %+v", target)
	}
}

func TestAllowedDirectTargetAcceptsOnlyDeclaredLoopbackEndpoints(t *testing.T) {
	first := directTarget{requested: "127.0.0.1:45678", dial: "127.0.0.1:45678"}
	second := directTarget{requested: "127.0.0.1:45679", dial: "127.0.0.1:45679"}
	payload := ssh.Marshal(directTCPIPRequest{DestinationHost: "127.0.0.1", DestinationPort: 45679})
	if target, err := allowedDirectTarget(payload, first, second); err != nil || target != second {
		t.Fatalf("target=%q err=%v", target, err)
	}
	for _, target := range []string{first.requested, second.requested, "[::1]:45680"} {
		if err := validateLoopbackTarget(target); err != nil {
			t.Fatalf("loopback target %q: %v", target, err)
		}
	}
	for _, target := range []string{"example.com:22", "192.0.2.1:22", "missing-port"} {
		if err := validateLoopbackTarget(target); err == nil {
			t.Fatalf("unsafe target %q accepted", target)
		}
	}
}

func TestRemoteForwardListenersAreDirectTargets(t *testing.T) {
	forwards := &remoteForwardSet{listeners: map[string]*remoteListener{
		"127.0.0.1:45681": {address: "127.0.0.1:45681"},
	}}
	targets := forwards.directTargets()
	if len(targets) != 1 || targets[0].requested != "127.0.0.1:45681" || targets[0].dial != targets[0].requested {
		t.Fatalf("remote forward direct targets = %+v", targets)
	}
}

func TestEchoServerUppercasesBytes(t *testing.T) {
	listener, err := startEchoServer()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := io.WriteString(connection, "forward me"); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len("FORWARD ME"))
	if _, err := io.ReadFull(connection, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "FORWARD ME" {
		t.Fatalf("echo = %q", buffer)
	}
}

func TestStatsFixtureAcceptsOnlyTheResourceProtocol(t *testing.T) {
	if !isStatsCommand("# motty-stats-v2\ncollect") {
		t.Fatal("the resource monitor command was refused")
	}
	for _, command := range []string{"uname -a", "", "gop-stats-v2", "# motty-stats-v1"} {
		if isStatsCommand(command) {
			t.Fatalf("arbitrary command %q accepted", command)
		}
	}
}

func TestRemoteForwardRequestRequiresLiteralLoopback(t *testing.T) {
	for _, request := range []tcpipForwardRequest{
		{BindAddress: "127.0.0.1", BindPort: 8080},
		{BindAddress: "::1", BindPort: 8080},
		{BindAddress: "127.0.0.1", BindPort: 0},
	} {
		if _, _, _, err := parseRemoteForwardRequest(ssh.Marshal(request), true); err != nil {
			t.Fatalf("safe remote bind %+v: %v", request, err)
		}
	}
	for _, request := range []tcpipForwardRequest{
		{BindAddress: "0.0.0.0", BindPort: 8080},
		{BindAddress: "::", BindPort: 8080},
		{BindAddress: "localhost", BindPort: 8080},
		{BindAddress: "192.0.2.1", BindPort: 8080},
		{BindAddress: "127.0.0.1", BindPort: 65536},
	} {
		if _, _, _, err := parseRemoteForwardRequest(ssh.Marshal(request), true); err == nil {
			t.Fatalf("unsafe remote bind accepted: %+v", request)
		}
	}
	if _, _, _, err := parseRemoteForwardRequest(ssh.Marshal(tcpipForwardRequest{BindAddress: "127.0.0.1"}), false); err == nil {
		t.Fatal("zero port accepted for cancel")
	}
	if _, _, _, err := parseRemoteForwardRequest([]byte{1, 2, 3}, true); err == nil {
		t.Fatal("malformed remote forward payload accepted")
	}
}

func TestRemoteForwardListenerReturnsAssignedPortAndCancels(t *testing.T) {
	listener, reply, err := openRemoteListener(ssh.Marshal(tcpipForwardRequest{BindAddress: "127.0.0.1"}))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.listener.Close()
	var assigned struct{ Port uint32 }
	if err := ssh.Unmarshal(reply, &assigned); err != nil {
		t.Fatal(err)
	}
	if assigned.Port == 0 || assigned.Port != listener.port || listener.address != net.JoinHostPort("127.0.0.1", fmt.Sprint(assigned.Port)) {
		t.Fatalf("listener=%+v assigned=%d", listener, assigned.Port)
	}

	forwards := newRemoteForwardSet(nil)
	if !forwards.add(listener) {
		t.Fatal("remote listener was not registered")
	}
	payload := ssh.Marshal(tcpipForwardRequest{BindAddress: "127.0.0.1", BindPort: assigned.Port})
	if address, ok := forwards.cancel(payload); !ok || address != listener.address {
		t.Fatalf("cancel address=%q ok=%v", address, ok)
	}
	if connection, err := net.DialTimeout("tcp", listener.address, 100*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("canceled remote listener still accepts connections")
	}
	if _, ok := forwards.cancel(payload); ok {
		t.Fatal("duplicate cancel succeeded")
	}
}
