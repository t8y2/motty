package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"motty/internal/ai/llm"
	"os"
	"os/exec"
	"strings"
	"time"
)

const maxLine = 1 << 20
const maxOutput = 16 << 20
const maxStderr = 64 << 10

type command struct {
	program   string
	version   string
	flavor    string
	args, env []string
	dir       string
	input     string
}

func processEnv(overrides []string) []string {
	// Replace rather than append duplicate names (Windows comparisons ignore case).
	env := os.Environ()
	for _, pair := range overrides {
		name, _, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		kept := env[:0]
		for _, old := range env {
			k, _, _ := strings.Cut(old, "=")
			if !strings.EqualFold(k, name) {
				kept = append(kept, old)
			}
		}
		env = append(kept, pair)
	}
	return env
}

type boundedWriter struct {
	data   []byte
	limit  int
	strict bool
	cancel context.CancelFunc
}

func (w *boundedWriter) Write(b []byte) (int, error) {
	n := len(b)
	room := w.limit - len(w.data)
	if room > 0 {
		w.data = append(w.data, b[:min(room, n)]...)
	}
	if n > room && w.strict {
		w.cancel()
		return n, errors.New("cli: output_limit")
	}
	return n, nil
}
func runCommand(ctx context.Context, spec command, stdout io.Writer) ([]byte, error) {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(child, spec.program, spec.args...)
	cmd.Dir = spec.dir
	cmd.Env = processEnv(spec.env)
	cmd.Stdin = strings.NewReader(spec.input)
	cmd.Stdout = stdout
	stderr := &boundedWriter{limit: maxStderr}
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second
	configureProcess(cmd)
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		// Use output only to categorize; never echo raw auth/provider output.
		lower := strings.ToLower(string(stderr.data))
		if strings.Contains(lower, "not logged") || strings.Contains(lower, "not authenticated") || strings.Contains(lower, "please log in") || strings.Contains(lower, "please login") {
			return nil, errors.New("cli: login_required")
		}
		var missing *exec.Error
		if errors.As(err, &missing) {
			return nil, errors.New("cli: not_installed")
		}
		return nil, errors.New("cli: execution_failed")
	}
	return stderr.data, nil
}
func capture(ctx context.Context, spec command, limit int) ([]byte, error) {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	out := &boundedWriter{limit: limit, strict: true, cancel: cancel}
	stderr, err := runCommand(child, spec, out)
	if len(out.data) >= limit {
		return nil, errors.New("cli: output_limit")
	}
	if err != nil {
		return nil, err
	}
	// Some CLIs print help on stderr. Machine-readable output stays on stdout,
	// so only an otherwise empty capture falls back.
	if len(bytes.TrimSpace(out.data)) == 0 {
		return stderr, nil
	}
	return out.data, nil
}

type jsonLines struct {
	buffer []byte
	total  int
	p      *parser
	emit   func(llm.Event)
	cancel context.CancelFunc
	err    error
}

func (w *jsonLines) Write(b []byte) (int, error) {
	n := len(b)
	w.total += n
	if w.total > maxOutput {
		w.err = errors.New("cli: output_limit")
		w.cancel()
		return n, w.err
	}
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		end := i
		if i < 0 {
			end = len(b)
		}
		if len(w.buffer)+end > maxLine {
			w.err = errors.New("cli: line_limit")
			w.cancel()
			return n, w.err
		}
		w.buffer = append(w.buffer, b[:end]...)
		if i < 0 {
			break
		}
		if err := w.flush(); err != nil {
			w.cancel()
			return n, err
		}
		b = b[i+1:]
	}
	return n, nil
}
func (w *jsonLines) flush() error {
	line := bytes.TrimSpace(w.buffer)
	if len(line) > 0 {
		es, err := w.p.line(line)
		if err != nil {
			w.err = err
			return err
		}
		for _, e := range es {
			w.emit(e)
		}
	}
	w.buffer = w.buffer[:0]
	return nil
}
func streamProcess(ctx context.Context, spec command, p *parser, emit func(llm.Event)) error {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	out := &jsonLines{p: p, emit: emit, cancel: cancel}
	_, err := runCommand(child, spec, out)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if out.err != nil {
		return out.err
	}
	if err != nil {
		return err
	}
	if err = out.flush(); err != nil {
		return err
	}
	if !p.done {
		return errors.New("cli: incomplete_output")
	}
	emit(llm.Event{Kind: llm.EventDone, Usage: p.usage})
	return nil
}
