package telnet

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"motty/internal/shellkind"
)

// pair connects a Session to a raw peer the test plays the server on.
func pair(t *testing.T, config Config) (*Session, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	session := Start(client, config)
	t.Cleanup(func() { session.Close(); server.Close() })
	return session, server
}

func TestATelnetSessionIsADeviceConsole(t *testing.T) {
	session, _ := pair(t, Config{})
	if got := session.Kind(); got != shellkind.Console {
		t.Fatalf("Kind() = %v; want a device console", got)
	}
}

// readN reads exactly n bytes from the server side within a second.
func readN(t *testing.T, server net.Conn, n int) []byte {
	t.Helper()
	data := make([]byte, n)
	server.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(server, data); err != nil {
		t.Fatalf("reading %d bytes from the client: %v", n, err)
	}
	return data
}

func waitOutput(t *testing.T, session *Session, want string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		got = session.Drain(got)
		if strings.Contains(string(got), want) || (want == "" && len(got) > 0) {
			return string(got)
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("output %q never contained %q", got, want)
	return ""
}

func TestTheOptionsATerminalNeedsAreAcceptedAndTheRestRefused(t *testing.T) {
	session, server := pair(t, Config{Cols: 100, Rows: 30})
	// WILL ECHO, WILL SGA -> DO; DO SGA -> WILL; DO TTYPE -> WILL; DO NAWS -> WILL (+ the size);
	// DO LINEMODE -> WONT; WILL STATUS -> DONT; DO ECHO -> WONT (we do not echo).
	server.Write([]byte{iac, will, optEcho, iac, will, optSGA, iac, do, optSGA, iac, do, optTType, iac, do, optNAWS,
		iac, do, 34, iac, will, 5, iac, do, optEcho})
	want := []byte{iac, do, optEcho, iac, do, optSGA, iac, will, optSGA, iac, will, optTType, iac, will, optNAWS,
		iac, wont, 34, iac, dont, 5, iac, wont, optEcho}
	got := readN(t, server, len(want))
	if !bytes.Equal(got, want) {
		t.Fatalf("replies\n got %v\nwant %v", got, want)
	}
	size := readN(t, server, 9)
	if !bytes.Equal(size, []byte{iac, sb, optNAWS, 0, 100, 0, 30, iac, se}) {
		t.Fatalf("window size = %v", size)
	}
	// The terminal type, asked for, is answered.
	server.Write([]byte{iac, sb, optTType, ttypeSend, iac, se})
	reply := readN(t, server, 4+len("xterm-256color")+2)
	if !bytes.Equal(reply[:4], []byte{iac, sb, optTType, ttypeIs}) || string(reply[4:4+len("xterm-256color")]) != "xterm-256color" {
		t.Fatalf("terminal type = %q", reply)
	}
	// A resize is reported.
	session.Resize(80, 24)
	if got := readN(t, server, 9); !bytes.Equal(got, []byte{iac, sb, optNAWS, 0, 80, 0, 24, iac, se}) {
		t.Fatalf("resize = %v", got)
	}
}

func TestAServerThatRepeatsItselfIsAnsweredOnce(t *testing.T) {
	session, server := pair(t, Config{})
	server.Write([]byte{iac, will, optEcho, iac, will, optEcho, iac, will, optEcho, 'x'})
	waitOutput(t, session, "x")
	readN(t, server, 3)
	server.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if n, _ := server.Read(make([]byte, 8)); n != 0 {
		t.Fatalf("the repeats were answered again (%d bytes)", n)
	}
}

func TestDataKeepsEscapedIACAndDropsTheNULAfterACR(t *testing.T) {
	session, server := pair(t, Config{})
	server.Write([]byte("a\r\x00b\r\nc"))
	server.Write([]byte{iac, iac, 'd', iac, 241 /* NOP */, 'e'})
	got := waitOutput(t, session, "e")
	if got != "a\rb\r\nc\xffde" {
		t.Fatalf("output = %q", got)
	}
}

func TestTheStreamParsesTheSameWhereverItIsCut(t *testing.T) {
	stream := []byte{'h', 'i', iac, will, optEcho, iac, sb, optTType, ttypeSend, iac, se, '\r', 0, 'o', 'k', iac, iac}
	for cut := 1; cut < len(stream); cut++ {
		session, server := pair(t, Config{})
		server.Write(stream[:cut])
		time.Sleep(time.Millisecond)
		server.Write(stream[cut:])
		got := waitOutput(t, session, "ok")
		if got != "hi\rok\xff" && got != "hi\rok" {
			t.Fatalf("cut at %d: output %q", cut, got)
		}
		time.Sleep(5 * time.Millisecond)
		got += string(session.Drain(nil))
		if got != "hi\rok\xff" {
			t.Fatalf("cut at %d: output %q", cut, got)
		}
		session.Close()
	}
}

func TestWhatIsTypedIsEscapedAndALoneCRBecomesCRNUL(t *testing.T) {
	session, server := pair(t, Config{})
	session.Write([]byte("ls\r"))
	if got := readN(t, server, 4); string(got) != "ls\r\x00" {
		t.Fatalf("sent %q", got)
	}
	session.Write([]byte("a\r\nb\xff"))
	if got := readN(t, server, 6); string(got) != "a\r\nb\xff\xff" {
		t.Fatalf("sent %q", got)
	}
	// In binary mode a carriage return goes as it is.
	server.Write([]byte{iac, do, optBinary})
	readN(t, server, 3)
	session.Write([]byte("x\r"))
	if got := readN(t, server, 2); string(got) != "x\r" {
		t.Fatalf("binary send %q", got)
	}
}

func TestAnOversizedSubnegotiationIsIgnoredAndNothingIsLost(t *testing.T) {
	session, server := pair(t, Config{})
	big := append([]byte{iac, sb, 99}, bytes.Repeat([]byte{'z'}, 10000)...)
	server.Write(append(big, iac, se, 'o', 'k'))
	if got := waitOutput(t, session, "ok"); got != "ok" {
		t.Fatalf("output = %q", got)
	}
}

func TestACleanCloseEndsWithoutAnErrorAndAnAbruptOneIsReported(t *testing.T) {
	session, server := pair(t, Config{})
	server.Write([]byte("bye"))
	server.Close()
	select {
	case <-session.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the session did not end when the server closed")
	}
	if session.Err() != nil {
		t.Fatalf("a normal close is an error: %v", session.Err())
	}
	if got := session.Drain(nil); string(got) != "bye" {
		t.Fatalf("output after the end = %q", got)
	}
	if session.Write([]byte("x")) {
		t.Fatal("a closed session accepted input")
	}
}

func TestInputToAServerThatNeverReadsIsRefusedNotQueuedForever(t *testing.T) {
	session, _ := pair(t, Config{})
	chunk := bytes.Repeat([]byte{'a'}, 16<<10)
	refused := false
	for range 100 {
		if !session.Write(chunk) {
			refused = true
			break
		}
	}
	if !refused {
		t.Fatal("input was queued without a bound")
	}
}

func TestDialUsesTheDialerAndReportsItsFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		socket, err := listener.Accept()
		if err == nil {
			socket.Write([]byte("login: "))
			socket.Close()
		}
	}()
	session, err := Dial(context.Background(), listener.Addr().String(), time.Second, nil, Config{})
	if err != nil {
		t.Fatal(err)
	}
	waitOutput(t, session, "login: ")
	wait.Wait()
	called := false
	_, err = Dial(context.Background(), "x:23", time.Second, func(context.Context, string) (net.Conn, error) {
		called = true
		return nil, net.ErrClosed
	}, Config{})
	if !called || err == nil {
		t.Fatalf("custom dialer called %v, err %v", called, err)
	}
}

func FuzzServerStream(f *testing.F) {
	f.Add([]byte{iac, will, optEcho, 'a', iac, sb, optTType, ttypeSend, iac, se})
	f.Add([]byte("plain\r\x00text\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		client, server := net.Pipe()
		session := Start(client, Config{Cols: 80, Rows: 24})
		go func() { io.Copy(io.Discard, server) }()
		server.Write(data)
		server.Close()
		select {
		case <-session.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("the session did not end")
		}
		session.Drain(nil)
	})
}
