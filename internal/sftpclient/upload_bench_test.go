package sftpclient

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// discardWriterAt accepts every write at once, so the benchmark sees the cost of
// the pipeline itself: goroutines, channels and buffers per byte uploaded.
type discardWriterAt struct{}

func (discardWriterAt) Write(p []byte) (int, error)            { return len(p), nil }
func (discardWriterAt) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }

func benchmarkUploadPipeline(b *testing.B, size int) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()
	payload := make([]byte, size)
	b.SetBytes(int64(size))
	b.ReportAllocs()
	for b.Loop() {
		written, err := worker.uploadCopy(discardWriterAt{}, bytes.NewReader(payload), 0, 0, 0, 0, 0)
		if err != nil || written != int64(size) {
			b.Fatalf("written=%d err=%v", written, err)
		}
	}
}

func BenchmarkUploadPipelineLargeFile(b *testing.B) { benchmarkUploadPipeline(b, 16<<20) }
func BenchmarkUploadPipelineSmallFile(b *testing.B) { benchmarkUploadPipeline(b, 4<<10) }

// delayedProxy forwards one TCP connection to target and holds every byte for
// delay in each direction, so a round trip costs 2*delay. Throughput is not
// limited, which is what leaves the window as the only thing between a
// transfer and the link's capacity.
func delayedProxy(b *testing.B, target string, delay time.Duration) string {
	b.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { listener.Close() })
	go func() {
		for {
			inbound, err := listener.Accept()
			if err != nil {
				return
			}
			outbound, err := net.Dial("tcp", target)
			if err != nil {
				inbound.Close()
				continue
			}
			go delayedCopy(outbound, inbound, delay)
			go delayedCopy(inbound, outbound, delay)
		}
	}()
	return listener.Addr().String()
}

type delayedPacket struct {
	at   time.Time
	data []byte
}

func delayedCopy(dst, src net.Conn, delay time.Duration) {
	queue := make(chan delayedPacket, 1<<16)
	go func() {
		defer dst.Close()
		for packet := range queue {
			time.Sleep(time.Until(packet.at))
			if _, err := dst.Write(packet.data); err != nil {
				return
			}
		}
	}()
	defer close(queue)
	buffer := make([]byte, 64<<10)
	for {
		n, err := src.Read(buffer)
		if n > 0 {
			queue <- delayedPacket{at: time.Now().Add(delay), data: append([]byte(nil), buffer[:n]...)}
		}
		if err != nil {
			return
		}
	}
}

// BenchmarkUploadOverDelayedSSH uploads through a real SSH and SFTP session to a
// loopback tools/e2esshd, with a fixed delay injected in each direction. It
// needs a running server, so it runs only when one is named:
//
//	cd tools/e2esshd && go build -o /tmp/e2esshd . && /tmp/e2esshd 2395 &
//	MOTTY_E2E_SSHD=127.0.0.1:2395 MOTTY_E2E_DELAY=25ms MOTTY_E2E_SIZE=64 \
//	    go test ./internal/sftpclient -run '^$' -bench DelayedSSH -benchtime 3x
//
// MOTTY_E2E_DELAY is the one-way delay (the round trip is twice that) and
// MOTTY_E2E_SIZE the file size in MiB.
func BenchmarkUploadOverDelayedSSH(b *testing.B) {
	target := os.Getenv("MOTTY_E2E_SSHD")
	if target == "" {
		b.Skip("MOTTY_E2E_SSHD is not set")
	}
	delay := 25 * time.Millisecond
	if text := os.Getenv("MOTTY_E2E_DELAY"); text != "" {
		parsed, err := time.ParseDuration(text)
		if err != nil {
			b.Fatal(err)
		}
		delay = parsed
	}
	sizeMiB := 64
	if text := os.Getenv("MOTTY_E2E_SIZE"); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil {
			b.Fatal(err)
		}
		sizeMiB = parsed
	}

	address := delayedProxy(b, target, delay)
	sshClient, err := ssh.Dial("tcp", address, &ssh.ClientConfig{
		User:            "bench",
		Auth:            []ssh.AuthMethod{ssh.Password("secret")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer sshClient.Close()
	worker := NewWorker(func() (Remote, error) { return Open(sshClient) }, nil)
	defer worker.Close()

	local := filepath.Join(b.TempDir(), "payload.bin")
	content := make([]byte, sizeMiB<<20)
	for i := range content {
		content[i] = byte(i * 2654435761 >> 13)
	}
	if err := os.WriteFile(local, content, 0o600); err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(content)))
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		name := "bench-" + strconv.Itoa(i) + ".bin"
		if _, queued := worker.QueueUpload(local, name, false, ExistingReplace); !queued {
			b.Fatal("upload not queued")
		}
		select {
		case result := <-worker.Results():
			if result.Err != nil {
				b.Fatal(result.Err)
			}
		case <-time.After(5 * time.Minute):
			b.Fatal("upload did not finish")
		}
	}
}
