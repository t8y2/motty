package sftpclient

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestUploadCopyWritesSequentiallyWithoutOffsets(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()
	payload := []byte("sequential payload")
	var sink bytes.Buffer
	written, err := worker.uploadCopy(&sink, bytes.NewReader(payload), 0, 0, 0, 0, 0)
	if err != nil || written != int64(len(payload)) || !bytes.Equal(sink.Bytes(), payload) {
		t.Fatalf("written=%d sink=%q err=%v", written, sink.String(), err)
	}
}

type memoryWriterAt struct {
	mu     sync.Mutex
	data   []byte
	errAt  int64
	err    error
	writes []int64
}

func newMemoryWriterAt(capacity int) *memoryWriterAt {
	return &memoryWriterAt{
		data:  make([]byte, capacity),
		errAt: -1,
	}
}

func (m *memoryWriterAt) Write(chunk []byte) (int, error) {
	panic("unexpected sequential write call")
}

func (m *memoryWriterAt) WriteAt(p []byte, off int64) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes = append(m.writes, off)
	if m.errAt >= 0 && off == m.errAt {
		return 0, m.err
	}
	end := off + int64(len(p))
	if end > int64(len(m.data)) {
		m.data = append(m.data, make([]byte, end-int64(len(m.data)))...)
	}
	copy(m.data[off:end], p)
	return len(p), nil
}

func TestUploadPipelined_EmptyAndSmallFiles(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()

	// 1. 空文件 (0 字节)
	sink0 := newMemoryWriterAt(0)
	written0, err0 := worker.uploadCopy(sink0, bytes.NewReader(nil), 0, 0, 0, 0, 0)
	if err0 != nil || written0 != 0 {
		t.Fatalf("empty file: written=%d err=%v", written0, err0)
	}

	// 2. 小文件 (< 32 KiB)
	smallPayload := []byte("hello sftp sliding window")
	sinkSmall := newMemoryWriterAt(len(smallPayload))
	writtenSmall, errSmall := worker.uploadCopy(sinkSmall, bytes.NewReader(smallPayload), 0, 0, 0, 0, 0)
	if errSmall != nil || writtenSmall != int64(len(smallPayload)) {
		t.Fatalf("small file: written=%d err=%v", writtenSmall, errSmall)
	}
	if !bytes.Equal(sinkSmall.data[:writtenSmall], smallPayload) {
		t.Fatalf("small file data mismatch: got %q", sinkSmall.data[:writtenSmall])
	}
}

func TestUploadPipelined_LargeFileFullWindow(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()

	// 80 个分片 (80 * 32 KiB = 2,621,440 字节)，超过 64 分片并发窗口
	totalBytes := 80 * 32 * 1024
	payload := make([]byte, totalBytes)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	sink := newMemoryWriterAt(totalBytes)
	written, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 0, 0, 0, 0)
	if err != nil || written != int64(totalBytes) {
		t.Fatalf("large file: written=%d want=%d err=%v", written, totalBytes, err)
	}
	if !bytes.Equal(sink.data[:totalBytes], payload) {
		t.Fatal("large file data corruption")
	}
}

type delayedChunkWriterAt struct {
	mu           sync.Mutex
	data         []byte
	delaySeq0    chan struct{}
	seq0Written  chan struct{}
	otherWritten chan struct{}
}

func (d *delayedChunkWriterAt) Write([]byte) (int, error) { panic("unexpected sequential write") }

func (d *delayedChunkWriterAt) WriteAt(p []byte, off int64) (int, error) {
	if off == 0 {
		// 延迟第一个分片的 ACK
		<-d.delaySeq0
		d.mu.Lock()
		copy(d.data[off:], p)
		d.mu.Unlock()
		close(d.seq0Written)
		return len(p), nil
	}
	d.mu.Lock()
	end := off + int64(len(p))
	if end > int64(len(d.data)) {
		d.data = append(d.data, make([]byte, end-int64(len(d.data)))...)
	}
	copy(d.data[off:end], p)
	d.mu.Unlock()
	select {
	case d.otherWritten <- struct{}{}:
	default:
	}
	return len(p), nil
}

func TestUploadPipelined_ReorderedACKPreservesContiguousProgress(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()

	// 构造 3 个分片
	payload := bytes.Repeat([]byte("A"), 3*32*1024)
	sink := &delayedChunkWriterAt{
		data:         make([]byte, len(payload)),
		delaySeq0:    make(chan struct{}),
		seq0Written:  make(chan struct{}),
		otherWritten: make(chan struct{}, 10),
	}

	worker.mu.Lock()
	worker.transfers = append(worker.transfers, transferRecord{
		snapshot: TransferSnapshot{ID: 99, State: TransferRunning, BytesTotal: int64(len(payload))},
		rateAt:   worker.clock(),
	})
	worker.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		_, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 99, 0, 0, 0)
		done <- err
	}()

	// 等待分片 1 或分片 2 写入并返回
	select {
	case <-sink.otherWritten:
	case <-time.After(time.Second):
		t.Fatal("subsequent chunks did not arrive")
	}

	// 此时分片 0 尚未 ACK，进度必须严格为 0（不能跳跃汇报）
	if got := worker.Transfers(nil)[0].BytesDone; got != 0 {
		t.Fatalf("progress reported ahead of unacknowledged prefix: %d", got)
	}

	// 释放分片 0
	close(sink.delaySeq0)
	select {
	case <-sink.seq0Written:
	case <-time.After(time.Second):
		t.Fatal("chunk 0 did not finish")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected upload error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("upload did not finish")
	}

	// 全部分片均确认后，最终进度为完整大小
	if got := worker.Transfers(nil)[0].BytesDone; got != int64(len(payload)) {
		t.Fatalf("final confirmed bytes = %d, want %d", got, len(payload))
	}
}

func TestUploadPipelined_WriteErrorAlignsContiguousBytes(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()

	// 4 个分片，第 2 个分片 (offset = 2 * 32KB = 64KB) 出错
	payload := bytes.Repeat([]byte("B"), 4*32*1024)
	sink := newMemoryWriterAt(len(payload))
	sink.errAt = 64 * 1024
	sink.err = errors.New("remote disk I/O error")

	written, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 0, 0, 0, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Only the bytes before the failed offset are counted, even though later
	// chunks may have been written.
	if written != sink.errAt {
		t.Fatalf("written=%d, want the bytes before the failed offset %d", written, sink.errAt)
	}
}

func TestUploadPipelined_CancelInterruptsStream(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()

	payload := bytes.Repeat([]byte("C"), 100*32*1024) // 3.2 MB
	sink := newMemoryWriterAt(len(payload))

	worker.mu.Lock()
	worker.transfers = append(worker.transfers, transferRecord{
		snapshot: TransferSnapshot{ID: 101, State: TransferRunning, BytesTotal: int64(len(payload))},
		rateAt:   worker.clock(),
	})
	worker.mu.Unlock()

	// 立即取消
	worker.CancelTransfer(101)

	_, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 101, 0, 0, 0)
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("expected ErrCanceled, got %v", err)
	}
}

// A server that acknowledges fewer bytes than were sent without an error must
// not be counted as having stored the chunk.
type shortWriterAt struct{ memoryWriterAt }

func (s *shortWriterAt) WriteAt(p []byte, off int64) (int, error) {
	if off == 32*1024 {
		return len(p) - 1, nil
	}
	return s.memoryWriterAt.WriteAt(p, off)
}

func TestUploadPipelined_ShortWriteIsAnError(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()
	payload := bytes.Repeat([]byte("D"), 4*32*1024)
	sink := &shortWriterAt{memoryWriterAt{data: make([]byte, len(payload)), errAt: -1}}
	written, err := worker.uploadCopy(sink, bytes.NewReader(payload), 0, 0, 0, 0, 0)
	if !errors.Is(err, io.ErrShortWrite) || written != 32*1024 {
		t.Fatalf("written=%d err=%v, want one chunk and ErrShortWrite", written, err)
	}
}

// slowSource reads slowly and notes any read that happens after the upload
// reported it was over: the source is closed then.
type slowSource struct {
	mu       sync.Mutex
	over     bool
	lateRead bool
}

func (s *slowSource) Read(p []byte) (int, error) {
	time.Sleep(time.Millisecond)
	s.mu.Lock()
	if s.over {
		s.lateRead = true
	}
	s.mu.Unlock()
	return len(p), nil
}

func TestUploadPipelined_ReaderHasEndedWhenItReturns(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()
	sink := newMemoryWriterAt(0)
	sink.errAt, sink.err = 0, errors.New("remote refused the write")
	source := &slowSource{}
	_, err := worker.uploadCopy(sink, source, 0, 0, 0, 0, 0)
	source.mu.Lock()
	source.over = true
	source.mu.Unlock()
	if err == nil {
		t.Fatal("expected the write error")
	}
	time.Sleep(20 * time.Millisecond)
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.lateRead {
		t.Fatal("the source was read after the upload returned")
	}
}
