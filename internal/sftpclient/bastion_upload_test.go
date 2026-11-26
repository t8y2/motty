package sftpclient

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// serialWriteProxy wraps the server end of an SFTP pipe and simulates a bastion
// SFTP audit proxy that creates opened files on disk but fails SSH_FXP_WRITE
// with SSH_FX_FAILURE whenever more than one write handle is open concurrently.
type serialWriteProxy struct {
	net.Conn

	writeDelay       time.Duration
	mu               sync.Mutex
	writeMu          sync.Mutex
	clientBuf        []byte
	serverBuf        []byte
	outQueue         []byte
	pendingWriteOpen map[uint32]bool
	openWriteHandles []string
	rejectedWrites   atomic.Int32
}

func newSerialWriteProxy(conn net.Conn) *serialWriteProxy {
	return &serialWriteProxy{
		Conn:             conn,
		pendingWriteOpen: make(map[uint32]bool),
	}
}

func (p *serialWriteProxy) Read(dst []byte) (int, error) {
	for {
		p.mu.Lock()
		if len(p.outQueue) > 0 {
			n := copy(dst, p.outQueue)
			p.outQueue = p.outQueue[n:]
			p.mu.Unlock()
			return n, nil
		}
		p.mu.Unlock()

		tmp := make([]byte, 64*1024)
		n, err := p.Conn.Read(tmp)
		if n > 0 {
			p.mu.Lock()
			p.clientBuf = append(p.clientBuf, tmp[:n]...)
			var rejected []uint32
			for len(p.clientBuf) >= 4 {
				pktLen := int(binary.BigEndian.Uint32(p.clientBuf[:4]))
				if len(p.clientBuf) < 4+pktLen {
					break
				}
				full := p.clientBuf[:4+pktLen]
				pkt := full[4:]
				if p.handleClientPacketLocked(pkt) {
					p.outQueue = append(p.outQueue, full...)
				} else if len(pkt) >= 5 {
					rejected = append(rejected, binary.BigEndian.Uint32(pkt[1:5]))
				}
				p.clientBuf = p.clientBuf[4+pktLen:]
			}
			hasOut := len(p.outQueue) > 0
			p.mu.Unlock()

			for _, id := range rejected {
				if sendErr := p.sendStatusFailure(id); sendErr != nil {
					return 0, sendErr
				}
			}
			if hasOut {
				if p.writeDelay > 0 {
					time.Sleep(p.writeDelay)
				}
				continue
			}
		}
		if err != nil {
			return 0, err
		}
	}
}

func (p *serialWriteProxy) Write(src []byte) (int, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	p.mu.Lock()
	p.serverBuf = append(p.serverBuf, src...)
	for len(p.serverBuf) >= 4 {
		pktLen := int(binary.BigEndian.Uint32(p.serverBuf[:4]))
		if len(p.serverBuf) < 4+pktLen {
			break
		}
		pkt := p.serverBuf[4 : 4+pktLen]
		p.handleServerPacketLocked(pkt)
		p.serverBuf = p.serverBuf[4+pktLen:]
	}
	p.mu.Unlock()

	return p.Conn.Write(src)
}

func (p *serialWriteProxy) handleClientPacketLocked(pkt []byte) bool {
	if len(pkt) < 5 {
		return true
	}
	typ := pkt[0]
	id := binary.BigEndian.Uint32(pkt[1:5])
	switch typ {
	case 3: // SSH_FXP_OPEN: id(4) + path(string) + pflags(4)
		if len(pkt) < 9 {
			return true
		}
		pathLen := int(binary.BigEndian.Uint32(pkt[5:9]))
		pflagsPos := 9 + pathLen
		if len(pkt) >= pflagsPos+4 {
			pflags := binary.BigEndian.Uint32(pkt[pflagsPos : pflagsPos+4])
			if pflags&0x02 != 0 { // SSH_FXF_WRITE
				p.pendingWriteOpen[id] = true
			}
		}
	case 4: // SSH_FXP_CLOSE: id(4) + handle(string)
		if len(pkt) >= 9 {
			hLen := int(binary.BigEndian.Uint32(pkt[5:9]))
			if len(pkt) >= 9+hLen {
				handle := string(pkt[9 : 9+hLen])
				for i, openHandle := range p.openWriteHandles {
					if openHandle == handle {
						p.openWriteHandles = append(p.openWriteHandles[:i], p.openWriteHandles[i+1:]...)
						break
					}
				}
			}
		}
	case 6: // SSH_FXP_WRITE: id(4) + handle(string)
		if len(pkt) >= 9 {
			hLen := int(binary.BigEndian.Uint32(pkt[5:9]))
			if len(pkt) >= 9+hLen {
				handle := string(pkt[9 : 9+hLen])
				if len(p.openWriteHandles) > 1 && p.openWriteHandles[0] != handle {
					p.rejectedWrites.Add(1)
					return false
				}
			}
		}
	}
	return true
}

func (p *serialWriteProxy) handleServerPacketLocked(pkt []byte) {
	if len(pkt) < 5 {
		return
	}
	typ := pkt[0]
	id := binary.BigEndian.Uint32(pkt[1:5])
	if typ == 102 && p.pendingWriteOpen[id] { // SSH_FXP_HANDLE
		delete(p.pendingWriteOpen, id)
		if len(pkt) >= 9 {
			hLen := int(binary.BigEndian.Uint32(pkt[5:9]))
			if len(pkt) >= 9+hLen {
				p.openWriteHandles = append(p.openWriteHandles, string(pkt[9:9+hLen]))
			}
		}
	}
}

func (p *serialWriteProxy) sendStatusFailure(id uint32) error {
	const msg = "General failure"
	pktLen := 1 + 4 + 4 + 4 + len(msg) + 4
	buf := make([]byte, 4+pktLen)
	binary.BigEndian.PutUint32(buf[0:4], uint32(pktLen))
	buf[4] = 101 // SSH_FXP_STATUS
	binary.BigEndian.PutUint32(buf[5:9], id)
	binary.BigEndian.PutUint32(buf[9:13], uint32(sftp.ErrSSHFxFailure))
	binary.BigEndian.PutUint32(buf[13:17], uint32(len(msg)))
	copy(buf[17:17+len(msg)], msg)
	binary.BigEndian.PutUint32(buf[17+len(msg):], 0)

	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, err := p.Conn.Write(buf)
	return err
}

func TestBastionFolderUploadFallsBackToSerialAfterConcurrentFailure(t *testing.T) {
	var proxy *serialWriteProxy
	worker, root := newProtocolWorkerOn(t, func(conn net.Conn) net.Conn {
		proxy = newSerialWriteProxy(conn)
		return proxy
	}, nil)

	local := filepath.Join(t.TempDir(), "assets")
	if err := os.MkdirAll(filepath.Join(local, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	const fileCount = 25
	expected := make(map[string][]byte, fileCount)
	var totalBytes int64
	for i := 0; i < fileCount; i++ {
		rel := fmt.Sprintf("index-%02d.js", i)
		size := 44 * 1024
		if i == 0 {
			rel = "_commonjsHelpers-Cpj98o6Y.js"
			size = 236
		} else if i%5 == 0 {
			rel = filepath.Join("nested", fmt.Sprintf("chunk-%02d.css", i))
			size = 16 * 1024
		}
		data := bytes.Repeat([]byte{byte('a' + i%26)}, size)
		full := filepath.Join(local, rel)
		if err := os.WriteFile(full, data, 0o600); err != nil {
			t.Fatal(err)
		}
		expected[filepath.ToSlash(rel)] = data
		totalBytes += int64(len(data))
	}

	id, queued := worker.QueueUpload(local, "assets", true, ExistingFail)
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Files != fileCount || result.Bytes != totalBytes {
		t.Fatalf("folder upload result = %+v (want Files=%d Bytes=%d, rejected=%d)", result, fileCount, totalBytes, proxy.rejectedWrites.Load())
	}
	if proxy.rejectedWrites.Load() == 0 {
		t.Fatal("expected the concurrent pass to hit at least one bastion rejection before falling back")
	}
	for rel, want := range expected {
		got, err := os.ReadFile(filepath.Join(root, "assets", filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("remote %s: err=%v len=%d want=%d", rel, err, len(got), len(want))
		}
	}
	partials, err := filepath.Glob(filepath.Join(root, "assets", ".motty.*.part"))
	if err != nil || len(partials) != 0 {
		t.Fatalf("leftover partials = %v, %v", partials, err)
	}
}

func TestBastionMultiFileSelectionUploadFallsBackToSerial(t *testing.T) {
	var proxy *serialWriteProxy
	worker, root := newProtocolWorkerOn(t, func(conn net.Conn) net.Conn {
		proxy = newSerialWriteProxy(conn)
		proxy.writeDelay = 2 * time.Millisecond
		return proxy
	}, nil)
	worker.SetParallel(2)

	localDir := t.TempDir()
	const fileCount = 5
	ids := make([]uint64, fileCount)
	contents := make([][]byte, fileCount)
	for i := 0; i < fileCount; i++ {
		name := fmt.Sprintf("file-%02d.bin", i)
		contents[i] = bytes.Repeat([]byte{byte('0' + i)}, 128*1024)
		localPath := filepath.Join(localDir, name)
		if err := os.WriteFile(localPath, contents[i], 0o600); err != nil {
			t.Fatal(err)
		}
		id, queued := worker.QueueUpload(localPath, name, false, ExistingFail)
		if !queued {
			t.Fatalf("file %d not queued", i)
		}
		ids[i] = id
	}

	results := make(map[uint64]Result, fileCount)
	for range fileCount {
		res := waitResult(t, worker)
		results[res.TransferID] = res
	}
	for i, id := range ids {
		res := results[id]
		if res.Err != nil || res.Bytes != int64(len(contents[i])) {
			t.Fatalf("file %d result = %+v", i, res)
		}
		got, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("file-%02d.bin", i)))
		if err != nil || !bytes.Equal(got, contents[i]) {
			t.Fatalf("remote file %d mismatch: err=%v len=%d", i, err, len(got))
		}
	}
	if proxy.rejectedWrites.Load() == 0 {
		t.Fatal("expected initial parallel transfers to trigger bastion rejection before serializing")
	}
}

type concurrentMkdirRejectRemote struct {
	remote
	mu     sync.Mutex
	inCall int
	failed atomic.Int32
}

func (r *concurrentMkdirRejectRemote) Mkdir(path string) error {
	r.mu.Lock()
	r.inCall++
	concurrent := r.inCall > 1
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.inCall--
		r.mu.Unlock()
	}()
	if concurrent {
		r.failed.Add(1)
		return &sftp.StatusError{Code: uint32(sftp.ErrSSHFxFailure)}
	}
	return r.remote.Mkdir(path)
}

func TestBastionConcurrentDirectoryCreationFallsBackToSerial(t *testing.T) {
	var wrapper *concurrentMkdirRejectRemote
	worker, root := newProtocolWorkerOn(t, nil, func(base remote) Remote {
		wrapper = &concurrentMkdirRejectRemote{remote: base}
		return wrapper
	})

	local := filepath.Join(t.TempDir(), "tree")
	for i := 0; i < 8; i++ {
		dir := filepath.Join(local, fmt.Sprintf("sub-%02d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	id, queued := worker.QueueUpload(local, "tree", true, ExistingFail)
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Files != 8 {
		t.Fatalf("tree upload result = %+v", result)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(root, "tree", fmt.Sprintf("sub-%02d", i), "a.txt")); err != nil {
			t.Fatalf("missing uploaded file %d: %v", i, err)
		}
	}
}

type failingWriteRemote struct {
	remote
	opens atomic.Int32
}

func (r *failingWriteRemote) OpenTransferFile(name string, flags int) (TransferFile, error) {
	r.opens.Add(1)
	file, err := r.remote.OpenTransferFile(name, flags)
	if err != nil {
		return nil, err
	}
	return failingWriteFile{TransferFile: file}, nil
}

type failingWriteFile struct {
	TransferFile
}

func (f failingWriteFile) Write([]byte) (int, error) {
	return 0, &sftp.StatusError{Code: uint32(sftp.ErrSSHFxFailure)}
}

func TestSingleFileFailureDoesNotRetryAsConcurrent(t *testing.T) {
	var wrapper *failingWriteRemote
	worker, _ := newProtocolWorkerOn(t, nil, func(base remote) Remote {
		wrapper = &failingWriteRemote{remote: base}
		return wrapper
	})

	source := filepath.Join(t.TempDir(), "one.bin")
	if err := os.WriteFile(source, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, queued := worker.QueueUpload(source, "one.bin", false, ExistingFail)
	result := queueResult(t, id, queued, worker)
	var status *sftp.StatusError
	if !errors.As(result.Err, &status) || status.FxCode() != sftp.ErrSSHFxFailure {
		t.Fatalf("result.Err = %v, want SSH_FX_FAILURE", result.Err)
	}
	if got := wrapper.opens.Load(); got != 1 {
		t.Fatalf("single file upload opened %d times, want 1 (must not retry)", got)
	}
	_ = io.EOF
}

func TestBastionOverlappedFolderAndFileUploadBothSucceed(t *testing.T) {
	var proxy *serialWriteProxy
	worker, root := newProtocolWorkerOn(t, func(conn net.Conn) net.Conn {
		proxy = newSerialWriteProxy(conn)
		proxy.writeDelay = 2 * time.Millisecond
		return proxy
	}, nil)
	worker.SetParallel(2)

	folder := filepath.Join(t.TempDir(), "single-file-dir")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	folderPayload := bytes.Repeat([]byte("f"), 128*1024)
	if err := os.WriteFile(filepath.Join(folder, "only.bin"), folderPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	extraLocal := filepath.Join(t.TempDir(), "extra.bin")
	extraPayload := bytes.Repeat([]byte("e"), 128*1024)
	if err := os.WriteFile(extraLocal, extraPayload, 0o600); err != nil {
		t.Fatal(err)
	}

	folderID, ok1 := worker.QueueUpload(folder, "single-file-dir", true, ExistingFail)
	extraID, ok2 := worker.QueueUpload(extraLocal, "extra.bin", false, ExistingFail)
	if !ok1 || !ok2 {
		t.Fatalf("queued = (%t, %t)", ok1, ok2)
	}

	results := map[uint64]Result{}
	for range 2 {
		res := waitResult(t, worker)
		results[res.TransferID] = res
	}
	if res := results[folderID]; res.Err != nil || res.Files != 1 || res.Bytes != int64(len(folderPayload)) {
		t.Fatalf("folder result = %+v", res)
	}
	if res := results[extraID]; res.Err != nil || res.Bytes != int64(len(extraPayload)) {
		t.Fatalf("extra result = %+v", res)
	}
	if got, err := os.ReadFile(filepath.Join(root, "single-file-dir", "only.bin")); err != nil || !bytes.Equal(got, folderPayload) {
		t.Fatalf("folder file mismatch: err=%v len=%d", err, len(got))
	}
	if got, err := os.ReadFile(filepath.Join(root, "extra.bin")); err != nil || !bytes.Equal(got, extraPayload) {
		t.Fatalf("extra file mismatch: err=%v len=%d", err, len(got))
	}
}

type genuineQuotaFailureRemote struct {
	remote
	mu               sync.Mutex
	opens            map[string]int
	openHandles      int
	maxOpenHandles   int
	committed        chan struct{}
	commitOnce       sync.Once
	probedFail       bool
	openedAfterProbe bool
}

func (r *genuineQuotaFailureRemote) OpenTransferFile(name string, flags int) (TransferFile, error) {
	r.mu.Lock()
	if r.opens == nil {
		r.opens = map[string]int{}
	}
	r.opens[name]++
	if strings.Contains(name, "01-fail.bin") {
		if r.opens[name] >= 2 {
			r.probedFail = true
		}
		r.mu.Unlock()
		<-r.committed
		return nil, &sftp.StatusError{Code: uint32(sftp.ErrSSHFxFailure)}
	}
	if r.probedFail {
		r.openedAfterProbe = true
	}
	r.openHandles++
	if r.openHandles > r.maxOpenHandles {
		r.maxOpenHandles = r.openHandles
	}
	r.mu.Unlock()

	file, err := r.remote.OpenTransferFile(name, flags)
	if err != nil {
		r.releaseHandle()
		return nil, err
	}
	return &countedTransferFile{TransferFile: file, release: r.releaseHandle}, nil
}

func (r *genuineQuotaFailureRemote) releaseHandle() {
	r.mu.Lock()
	r.openHandles--
	r.mu.Unlock()
}

func (r *genuineQuotaFailureRemote) Rename(oldPath, newPath string) error {
	err := r.remote.Rename(oldPath, newPath)
	if err == nil && strings.HasSuffix(newPath, "00-ok.bin") {
		r.commitOnce.Do(func() { close(r.committed) })
	}
	return err
}

func (r *genuineQuotaFailureRemote) PosixRename(oldPath, newPath string) error {
	err := r.remote.PosixRename(oldPath, newPath)
	if err == nil && strings.HasSuffix(newPath, "00-ok.bin") {
		r.commitOnce.Do(func() { close(r.committed) })
	}
	return err
}

func TestDirectoryUploadGenuineFailureAfterCommittedFileDoesNotRetryBatchOrLatchSerial(t *testing.T) {
	var wrapper *genuineQuotaFailureRemote
	worker, root := newProtocolWorkerOn(t, nil, func(base remote) Remote {
		wrapper = &genuineQuotaFailureRemote{
			remote:    base,
			committed: make(chan struct{}),
		}
		return wrapper
	})

	dir := filepath.Join(t.TempDir(), "batch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const totalFiles = 6
	for i := 0; i < totalFiles; i++ {
		name := fmt.Sprintf("%02d-later.bin", i)
		switch i {
		case 0:
			name = "00-ok.bin"
		case 1:
			name = "01-fail.bin"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("payload-"+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	id, queued := worker.QueueUpload(dir, "batch", true, ExistingFail)
	result := queueResult(t, id, queued, worker)
	var status *sftp.StatusError
	if !errors.As(result.Err, &status) || status.FxCode() != sftp.ErrSSHFxFailure {
		t.Fatalf("result = %+v, want SSH_FX_FAILURE (not ConflictError)", result)
	}
	if result.Files < 1 {
		t.Fatalf("result.Files = %d, want >= 1 committed file", result.Files)
	}
	if got, err := os.ReadFile(filepath.Join(root, "batch", "00-ok.bin")); err != nil || string(got) != "payload-00-ok.bin" {
		t.Fatalf("committed file = %q, %v", got, err)
	}

	wrapper.mu.Lock()
	var okOpens, failOpens, maxOtherOpens int
	for path, count := range wrapper.opens {
		switch {
		case strings.Contains(path, "00-ok.bin"):
			okOpens += count
		case strings.Contains(path, "01-fail.bin"):
			failOpens += count
		default:
			if count > maxOtherOpens {
				maxOtherOpens = count
			}
		}
	}
	openedAfterProbe := wrapper.openedAfterProbe
	wrapper.mu.Unlock()
	if okOpens != 1 {
		t.Fatalf("00-ok.bin opened %d times, want 1 (committed file must not be retried)", okOpens)
	}
	if failOpens != 2 {
		t.Fatalf("01-fail.bin opened %d times, want 2 (1 concurrent attempt + 1 serial probe)", failOpens)
	}
	if maxOtherOpens > 1 || openedAfterProbe {
		t.Fatalf("batch files must not be re-run after failed serial probe: maxOtherOpens=%d openedAfterProbe=%t", maxOtherOpens, openedAfterProbe)
	}
	if worker.isSerial(worker.snapshot()) {
		t.Fatal("genuine per-file SSH_FX_FAILURE must not latch worker into serial mode")
	}

	// Subsequent folder upload on the same worker must still run concurrently.
	dir2 := filepath.Join(t.TempDir(), "healthy")
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if err := os.WriteFile(filepath.Join(dir2, fmt.Sprintf("h-%02d.bin", i)), []byte("ok"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wrapper.mu.Lock()
	wrapper.maxOpenHandles = 0
	wrapper.mu.Unlock()

	id2, queued2 := worker.QueueUpload(dir2, "healthy", true, ExistingFail)
	res2 := queueResult(t, id2, queued2, worker)
	if res2.Err != nil || res2.Files != 6 {
		t.Fatalf("second healthy upload = %+v", res2)
	}
	wrapper.mu.Lock()
	maxOpen := wrapper.maxOpenHandles
	wrapper.mu.Unlock()
	if maxOpen < 2 {
		t.Fatalf("healthy folder upload after per-file failure opened at most %d handle at once, want >= 2", maxOpen)
	}
}

func TestBastionSerialLatchIsScopedToSubsystemGeneration(t *testing.T) {
	worker, _ := newProtocolWorkerOn(t, func(conn net.Conn) net.Conn {
		return newSerialWriteProxy(conn)
	}, nil)

	local := filepath.Join(t.TempDir(), "pair")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := os.WriteFile(filepath.Join(local, fmt.Sprintf("f-%02d.bin", i)), bytes.Repeat([]byte("a"), 16*1024), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	id, queued := worker.QueueUpload(local, "pair", true, ExistingFail)
	if res := queueResult(t, id, queued, worker); res.Err != nil {
		t.Fatalf("upload failed: %+v", res)
	}
	gen := worker.snapshot()
	if !worker.isSerial(gen) {
		t.Fatal("expected serial mode to latch for the active subsystem generation")
	}
	if old := worker.invalidate(); old != nil {
		_ = old.Close()
	}
	if newGen := worker.snapshot(); newGen == gen || worker.isSerial(newGen) {
		t.Fatalf("serial latch survived subsystem invalidation: old=%d new=%d serial=%t", gen, newGen, worker.isSerial(newGen))
	}
}
