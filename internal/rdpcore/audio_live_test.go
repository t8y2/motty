package rdp

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The live audio test connects to a real RDP server, asks it for sound, plays
// a system WAV on the server's desktop, and checks that PCM arrived here:
//
//	MOTTY_RDP_ADDR, MOTTY_RDP_USER, MOTTY_RDP_PASSWORD  the server
//	MOTTY_RDP_AUDIO=1                                   run it
//
// It types into the session, so the account must reach a desktop without a
// prompt to answer first.

type liveSound struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (s *liveSound) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Write(p)
}

func (s *liveSound) Close() error { return nil }

func (s *liveSound) length() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Len()
}

func TestLiveAudio(t *testing.T) {
	address := os.Getenv("MOTTY_RDP_ADDR")
	if address == "" || os.Getenv("MOTTY_RDP_AUDIO") == "" {
		t.Skip("MOTTY_RDP_ADDR and MOTTY_RDP_AUDIO=1 not set")
	}
	host, portText, found := strings.Cut(address, ":")
	if !found {
		t.Fatalf("bad MOTTY_RDP_ADDR %q", address)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("bad MOTTY_RDP_ADDR port %q", portText)
	}

	sound := &liveSound{}
	var negotiated struct {
		sync.Mutex
		rate     uint32
		channels uint16
	}
	config := DefaultOptions()
	config.Host, config.Port = host, port
	config.Username = os.Getenv("MOTTY_RDP_USER")
	config.Domain = os.Getenv("MOTTY_RDP_DOMAIN")
	config.Password = os.Getenv("MOTTY_RDP_PASSWORD")
	config.VerifyCertificate = func([]byte) error { return nil }
	config.Sound = func(rate uint32, channels uint16) (io.WriteCloser, error) {
		t.Logf("sound: server asked for %d Hz, %d channels", rate, channels)
		negotiated.Lock()
		negotiated.rate, negotiated.channels = rate, channels
		negotiated.Unlock()
		return sound, nil
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Wait for the desktop to draw, then open Run and play a system sound.
	time.Sleep(3 * time.Second)
	key := func(scancode uint16) {
		client.SendKeyboard(scancode, true, false)
		client.SendKeyboard(scancode, false, false)
	}
	// Win+R: the modifier must be held while R is pressed, or Windows opens
	// the Start menu and the characters go wherever focus was.
	client.SendKeyboard(0xE05B, true, false) // Left Windows
	client.SendKeyboard(0x13, true, false)   // R
	client.SendKeyboard(0x13, false, false)
	client.SendKeyboard(0xE05B, false, false)
	time.Sleep(1500 * time.Millisecond)
	command := `powershell -NoProfile -c (New-Object Media.SoundPlayer C:\Windows\Media\Alarm01.wav).PlaySync()`
	for _, r := range command {
		client.SendUnicode(uint16(r), true, false)
		client.SendUnicode(uint16(r), false, false)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	key(0x1C) // Enter

	deadline := time.Now().Add(25 * time.Second)
	for sound.length() == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sound.length() == 0 {
		t.Fatal("no PCM arrived after playing a sound on the server")
	}
	negotiated.Lock()
	rate, channels := negotiated.rate, negotiated.channels
	negotiated.Unlock()
	if rate == 0 || channels == 0 {
		t.Fatal("the server sent PCM without negotiating a format")
	}
	sound.mu.Lock()
	data := append([]byte(nil), sound.data.Bytes()...)
	sound.mu.Unlock()
	if len(data)%(int(channels)*2) != 0 {
		t.Errorf("%d bytes is not a whole number of %d-channel frames", len(data), channels)
	}
	// A WAV plays for a measurable time; a handful of bytes would mean a
	// bogus format rather than sound.
	if len(data) < int(rate/10)*int(channels)*2 {
		t.Errorf("only %d bytes of PCM for %d Hz", len(data), rate)
	}
	t.Logf("received %d bytes of PCM at %d Hz, %d channels", len(data), rate, channels)
}
