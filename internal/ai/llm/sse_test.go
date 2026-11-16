package llm

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func parseChunks(data []byte, sizes []int) ([]sseEvent, error) {
	p := newSSEParser(maxLineBytes)
	var out []sseEvent
	emit := func(e sseEvent) error {
		e.Data = append([]byte{}, e.Data...)
		out = append(out, e)
		return nil
	}
	i, k := 0, 0
	for i < len(data) {
		n := 1
		if len(sizes) > 0 {
			n = sizes[k%len(sizes)]
			k++
		}
		if n < 1 {
			n = 1
		}
		if i+n > len(data) {
			n = len(data) - i
		}
		if err := p.Write(data[i:i+n], emit); err != nil {
			return out, err
		}
		i += n
	}
	err := p.Close(emit)
	return out, err
}

func TestSSEParserFraming(t *testing.T) {
	in := ": keepalive\r\n\r\nevent: a\r\ndata: one\r\ndata: two\r\nid: 5\r\nretry: 3\r\nbogus\r\n\r\n\n\ndata:x\n\ndata\n\ndata: tail"
	want := []sseEvent{{"a", []byte("one\ntwo")}, {"", []byte("x")}, {"", []byte("")}, {"", []byte("tail")}}
	for _, sizes := range [][]int{nil, {1}, {2, 3}, {1000}} {
		got, err := parseChunks([]byte(in), sizes)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("sizes %v: got %q err %v", sizes, got, err)
		}
	}
}

func TestSSEParserBareCRAcrossChunks(t *testing.T) {
	got, _ := parseChunks([]byte("data: a\r\rdata: b\r\n\r\n"), []int{1})
	if len(got) != 2 || string(got[0].Data) != "a" || string(got[1].Data) != "b" {
		t.Fatalf("got %q", got)
	}
}

func TestSSEParserLineLimit(t *testing.T) {
	p := newSSEParser(16)
	err := p.Write([]byte("data: "+strings.Repeat("x", 100)), func(sseEvent) error { return nil })
	if !errors.Is(err, errSSELineTooLong) {
		t.Fatalf("got %v", err)
	}
}

func TestSSEParserUTF8Split(t *testing.T) {
	got, _ := parseChunks([]byte("data: 你好🙂\n\n"), []int{1})
	if len(got) != 1 || string(got[0].Data) != "你好🙂" {
		t.Fatalf("got %q", got)
	}
}

func FuzzSSEParserChunking(f *testing.F) {
	f.Add([]byte("event: x\r\ndata: a\r\ndata: b\r\n\r\n: c\n\ndata: z"), uint8(3))
	f.Add([]byte("data\r\r\ndata:\n\n"), uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, n uint8) {
		whole, err1 := parseChunks(data, []int{len(data) + 1})
		split, err2 := parseChunks(data, []int{int(n%7) + 1, int(n%5) + 1})
		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("error mismatch %v vs %v", err1, err2)
		}
		if err1 == nil && !reflect.DeepEqual(whole, split) {
			t.Fatalf("chunking changed events:\n%q\n%q", whole, split)
		}
	})
}

func BenchmarkSSEParse1MiB(b *testing.B) {
	var sb strings.Builder
	for sb.Len() < 1<<20 {
		sb.WriteString(`data: {"choices":[{"delta":{"content":"hello world"}}]}` + "\n\n")
	}
	data := []byte(sb.String())
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := newSSEParser(maxLineBytes)
		n := 0
		emit := func(sseEvent) error { n++; return nil }
		for off := 0; off < len(data); off += 4096 {
			end := min(off+4096, len(data))
			if err := p.Write(data[off:end], emit); err != nil {
				b.Fatal(err)
			}
		}
		p.Close(emit)
	}
}

func BenchmarkOpenAIStream1MiB(b *testing.B) {
	var sb strings.Builder
	for sb.Len() < 1<<20 {
		sb.WriteString(`data: {"choices":[{"delta":{"content":"hello world"}}]}` + "\n\n")
	}
	sb.WriteString(`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n")
	data := sb.String()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch := make(chan Event, 1024)
		go func() {
			for range ch {
			}
		}()
		em := &emitter{ctx: bg, ch: ch}
		if err := consume(strings.NewReader(data), 1<<30, &oaHandler{}, em, nil); err != nil {
			b.Fatal(err)
		}
		close(ch)
	}
}
