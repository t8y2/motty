package llm

import (
	"bytes"
	"context"
	"errors"
	"io"
)

type sseEvent struct {
	Name string
	// Data is only valid during the emit callback: it is a slice of the
	// parser's reusable buffer. A handler that keeps it must copy it.
	Data []byte
}

var (
	errSSELineTooLong = errors.New("llm: SSE line or event exceeds size limit")
	errSSETooLarge    = errors.New("llm: response exceeds size limit")
)

// sseParser is an incremental Server-Sent Events parser. Its output depends
// only on the concatenated input, never on how it was split into Write calls.
type sseParser struct {
	maxLine int
	line    []byte
	cr      bool // previous byte was CR; a following LF belongs to it
	name    string
	data    []byte
	hasData bool
}

func newSSEParser(maxLine int) *sseParser { return &sseParser{maxLine: maxLine} }

func (p *sseParser) Write(b []byte, emit func(sseEvent) error) error {
	for len(b) > 0 {
		if p.cr {
			p.cr = false
			if b[0] == '\n' {
				b = b[1:]
				continue
			}
		}
		i := bytes.IndexAny(b, "\r\n")
		if i < 0 {
			if len(p.line)+len(b) > p.maxLine {
				return errSSELineTooLong
			}
			p.line = append(p.line, b...)
			return nil
		}
		if len(p.line)+i > p.maxLine {
			return errSSELineTooLong
		}
		line := b[:i]
		if len(p.line) > 0 {
			p.line = append(p.line, line...)
			line = p.line
		}
		err := p.handleLine(line, emit)
		p.line = p.line[:0]
		if err != nil {
			return err
		}
		p.cr = b[i] == '\r'
		b = b[i+1:]
	}
	return nil
}

// Close flushes a final unterminated line and event.
func (p *sseParser) Close(emit func(sseEvent) error) error {
	if len(p.line) > 0 {
		line := p.line
		p.line = nil
		if err := p.handleLine(line, emit); err != nil {
			return err
		}
	}
	return p.dispatch(emit)
}

func (p *sseParser) handleLine(line []byte, emit func(sseEvent) error) error {
	if len(line) == 0 {
		return p.dispatch(emit)
	}
	if line[0] == ':' {
		return nil
	}
	field, value := line, []byte(nil)
	if i := bytes.IndexByte(line, ':'); i >= 0 {
		field, value = line[:i], line[i+1:]
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
	}
	switch string(field) {
	case "data":
		if len(p.data)+len(value)+1 > 4*p.maxLine {
			return errSSELineTooLong
		}
		p.data = append(p.data, value...)
		p.data = append(p.data, '\n')
		p.hasData = true
	case "event":
		p.name = string(value)
	}
	return nil
}

func (p *sseParser) dispatch(emit func(sseEvent) error) error {
	if !p.hasData {
		p.name = ""
		return nil
	}
	ev := sseEvent{Name: p.name, Data: bytes.TrimSuffix(p.data, []byte{'\n'})}
	p.name, p.data, p.hasData = "", p.data[:0], false
	return emit(ev)
}

// limitReader fails once more than left bytes have been read.
type limitReader struct {
	r    io.Reader
	left int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	if int64(len(p)) > l.left+1 {
		p = p[:l.left+1]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	if l.left < 0 {
		return 0, errSSETooLarge
	}
	return n, err
}

type emitter struct {
	ctx context.Context
	ch  chan<- Event
}

// send delivers ev, or returns the context error if the consumer is gone.
func (e *emitter) send(ev Event) error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	select {
	case e.ch <- ev:
		return nil
	case <-e.ctx.Done():
		return e.ctx.Err()
	}
}

// sseHandler turns SSE events into Events. event reports done once it has
// sent the terminal Done event; eof is called if the body ends first.
type sseHandler interface {
	event(ev sseEvent, em *emitter) (done bool, err error)
	eof(em *emitter) error
}

// pump consumes body in a goroutine, emitting events on the returned channel,
// which it closes after releasing the body.
func (b *base) pump(ctx context.Context, body io.ReadCloser, h sseHandler) <-chan Event {
	ch := make(chan Event, 16)
	go func() {
		defer close(ch)
		defer body.Close()
		em := &emitter{ctx: ctx, ch: ch}
		watch := newStallWatch(b.idle, func() { _ = body.Close() })
		defer watch.stop()
		err := consume(body, b.max, h, em, watch)
		if err == nil || ctx.Err() != nil {
			return
		}
		if watch.fired() {
			err = watch.err()
		}
		_ = em.send(Event{Kind: EventError, Err: b.scrubErr(err)})
	}()
	return ch
}

func consume(body io.Reader, max int64, h sseHandler, em *emitter, watch *stallWatch) error {
	r := &limitReader{r: body, left: max}
	p := newSSEParser(maxLineBytes)
	done := false
	emit := func(ev sseEvent) error {
		if done {
			return nil
		}
		d, err := h.event(ev, em)
		done = d
		return err
	}
	buf := make([]byte, 32<<10)
	for !done {
		n, err := r.Read(buf)
		if n > 0 {
			if watch != nil {
				watch.touch()
			}
			if perr := p.Write(buf[:n], emit); perr != nil {
				return perr
			}
		}
		if err == io.EOF {
			if perr := p.Close(emit); perr != nil {
				return perr
			}
			if done {
				return nil
			}
			return h.eof(em)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
