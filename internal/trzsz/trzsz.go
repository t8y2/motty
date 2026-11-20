// Package trzsz carries a file transfer between a terminal and the trzsz
// programs (trz and tsz) running in the remote shell.
//
// trzsz is not ZMODEM. The remote program announces itself with a text line
// ("::TRZSZ:TRANSFER:...") and then speaks a line protocol of its own: a
// capability exchange, one request and one acknowledgement per step, and a
// base64/zlib payload for every string and chunk. This package implements the
// terminal side of that protocol for the path the transfer takes through the
// terminal stream, which is the path every SSH client has. The tunnel and tmux
// transport modes are deliberately left out: they need a port forward or a tmux
// control client, neither of which is part of the stream.
//
// The protocol negotiates its version: a client that offers 1 keeps every server
// that ever implemented it, and the version 1 data path is the one Motty needs.
// Motty never overwrites a file it did not create, so the later versions' resume
// support would have nothing to resume, and the directory tree is carried by the
// name and size messages either way. Version 1 therefore loses no behaviour the
// transfer UI has.
package trzsz

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Errors a transfer ends with. They are values, so the owner can tell a
// cancelled transfer from a failed one.
var (
	ErrCancelled = errors.New("trzsz: transfer cancelled")
	ErrDeclined  = errors.New("trzsz: transfer declined")
)

// Trigger is the announcement the remote program writes when it starts.
type Trigger struct {
	// Mode is the remote program's direction and shape: S when tsz sends files
	// to the terminal, R when trz receives files, D when trz receives a tree,
	// and F when trz was started with a client-side file list.
	Mode byte
	// Version is the remote program's version.
	Version [3]int
}

// Sending reports whether the terminal sends the files.
func (t Trigger) Sending() bool { return t.Mode != 'S' }

// Directory reports whether the remote program was started for a directory
// transfer. F is a client-file upload, and without a client-side list it is
// answered by asking the user for a directory, exactly like D.
func (t Trigger) Directory() bool { return t.Mode == 'D' || t.Mode == 'F' }

// triggerPrefix begins the announcement. It includes the save-cursor sequence
// the remote program writes first, so a program that merely prints the words is
// not mistaken for a transfer.
const triggerPrefix = "\x1b[s::TRZSZ:TRANSFER:"

// triggerLimit bounds an announcement line. The real one is under 60 bytes; a
// longer line is not an announcement and is left on the screen.
const triggerLimit = 256

// TailSize is how many bytes at the end of a read must be kept for the next
// read, so an announcement split across two reads is still seen as one.
const TailSize = len(triggerPrefix) - 1

// MatchTrigger looks for the announcement in buf, which holds the bytes kept
// from the previous read followed by the bytes that just arrived.
//
// It returns the offsets of the whole announcement (including the trailing
// newline) in buf when one is complete, and whether more bytes are needed to
// decide. When an announcement begins but is not yet complete, start marks where
// it begins and more is true; the caller keeps buf[start:] for the next read.
func MatchTrigger(buf []byte) (start, end int, trigger Trigger, ok, more bool) {
	index := bytes.LastIndex(buf, []byte(triggerPrefix))
	if index < 0 {
		return 0, 0, Trigger{}, false, false
	}
	rest := buf[index+len(triggerPrefix):]
	if len(rest) > triggerLimit {
		return 0, 0, Trigger{}, false, false
	}
	newline := bytes.IndexByte(rest, '\n')
	if newline < 0 {
		return index, 0, Trigger{}, false, true
	}
	// The remote program writes the announcement before it puts its terminal
	// in raw mode, so a pty may turn its CR LF into CR CR LF.
	line := strings.TrimRight(string(rest[:newline]), "\r")
	trigger, ok = parseTriggerLine(line)
	if !ok {
		return 0, 0, Trigger{}, false, false
	}
	return index, index + len(triggerPrefix) + newline + 1, trigger, true, false
}

// parseTriggerLine reads "M:VERSION[:UNIQUEID[:PORT]]".
func parseTriggerLine(line string) (Trigger, bool) {
	fields := strings.Split(line, ":")
	if len(fields) < 2 || len(fields[0]) != 1 {
		return Trigger{}, false
	}
	trigger := Trigger{Mode: fields[0][0]}
	switch trigger.Mode {
	case 'S', 'R', 'D', 'F':
	default:
		return Trigger{}, false
	}
	parts := strings.Split(fields[1], ".")
	if len(parts) != 3 {
		return Trigger{}, false
	}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 1<<20 {
			return Trigger{}, false
		}
		trigger.Version[i] = value
	}
	// The unique id and tunnel port follow; the stream path ignores both. They
	// still have to be decimal, so a line that merely starts like an
	// announcement does not swallow what follows it.
	for _, field := range fields[2:] {
		if field == "" {
			return Trigger{}, false
		}
		if _, err := strconv.ParseUint(field, 10, 64); err != nil {
			return Trigger{}, false
		}
	}
	if len(fields) > 4 {
		return Trigger{}, false
	}
	return trigger, true
}

// EncodeText encodes a string or a small payload the way the protocol does:
// zlib first, then standard base64.
func EncodeText(data []byte) string {
	var buffer bytes.Buffer
	writer := zlib.NewWriter(&buffer)
	_, _ = writer.Write(data)
	_ = writer.Close()
	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}

// DecodeText reverses EncodeText. limit is how many decoded bytes are accepted;
// the caller picks it from the message kind, so a small message cannot expand
// into a large allocation.
func DecodeText(text string, limit int64) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("trzsz: %w", err)
	}
	reader, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("trzsz: %w", err)
	}
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("trzsz: %w", err)
	}
	if int64(len(decoded)) > limit {
		return nil, errors.New("trzsz: message is larger than its kind allows")
	}
	return decoded, nil
}

// declaration is what the terminal says about itself when it accepts an offer.
// The fields are the protocol's own names.
type declaration struct {
	Lang         string `json:"lang"`
	Version      string `json:"version"`
	Confirm      bool   `json:"confirm"`
	Newline      string `json:"newline"`
	Protocol     int    `json:"protocol"`
	Binary       bool   `json:"binary"`
	SupportDir   bool   `json:"support_dir"`
	Tunnel       bool   `json:"tunnel"`
	SupportFork  bool   `json:"fork"`
	TmuxInteract bool   `json:"tmuxcc"`
}

// declarationFor is the client side of the capability exchange. Binary mode is
// not offered: the stream version of the protocol carries data as base64 lines,
// which keeps every byte off the terminal's escape handling. Directory support
// is always offered because the file chooser can supply either shape.
func declarationFor(confirm bool) declaration {
	return declaration{
		Lang:       "go",
		Version:    "1.0.0",
		Confirm:    confirm,
		Newline:    "\n",
		Protocol:   1,
		Binary:     false,
		SupportDir: true,
	}
}

// config is what the remote program says about the transfer it is about to
// start.
type config struct {
	// Binary says the remote will send raw bytes instead of base64 lines. The
	// declaration refuses it, so a remote that sets it did not read the offer.
	Binary bool `json:"binary"`
	// Dir says the names carry a directory entry (a JSON path) rather than a
	// plain file name.
	Dir bool `json:"directory"`
	// Protocol is the negotiated version; this client speaks 1.
	Protocol int `json:"protocol"`
}

func parseConfig(payload string) (config, error) {
	var cfg config
	if err := json.Unmarshal([]byte(payload), &cfg); err != nil {
		return config{}, fmt.Errorf("trzsz: bad configuration: %w", err)
	}
	if cfg.Protocol > 1 {
		// The remote is honouring our lower offer; a higher number means it
		// misread the declaration, so do not guess at its message shapes.
		return config{}, fmt.Errorf("trzsz: unsupported protocol version %d", cfg.Protocol)
	}
	return cfg, nil
}
