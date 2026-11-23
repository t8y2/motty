package rfb

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"strings"
	"unicode/utf8"
)

// The extended clipboard is a pseudo-encoding of the TigerVNC family (also
// spoken by noVNC, RealVNC-compatible viewers and libvncserver) that replaces
// the Latin-1 cut text of RFC 6143 with UTF-8. A client advertises it in
// SetEncodings; a server that supports it then sends its capabilities as a
// ServerCutText whose length is negative, and the two sides exchange
// ServerCutText and ClientCutText messages of that shape:
//
//	type (3 or 6), 3 bytes padding, S32 -(4+n), U32 flags, n bytes
//
// The flags carry one action in the top byte and the formats it concerns in
// the low 16 bits. Caps lists the actions and formats the sender supports, with
// a U32 size limit per format. Notify announces that formats are available,
// Request asks for them and Provide sends them: a zlib stream of one U32 length
// and the data per format, in ascending format bit. Peek asks for a Notify.
// Text is UTF-8 with CRLF line ends and a terminating NUL.
const encodingExtendedClipboard int32 = -1063131698 // 0xC0A1E5CE

const (
	clipUTF8    uint32 = 1 << 0
	clipCaps    uint32 = 1 << 24
	clipRequest uint32 = 1 << 25
	clipPeek    uint32 = 1 << 26
	clipNotify  uint32 = 1 << 27
	clipProvide uint32 = 1 << 28

	clipFormats uint32 = 0xffff
	// clipMessage bounds the bytes of one extended message that are read: a
	// Provide's compressed text is no longer than twice the text it holds.
	clipMessage = 2*MaxClipboard + 1<<10
)

// extendedMessage is a ClientCutText of the extended clipboard.
func extendedMessage(flags uint32, payload []byte) []byte {
	message := make([]byte, 8, 12+len(payload))
	message[0] = 6
	binary.BigEndian.PutUint32(message[4:], uint32(int32(-(4 + len(payload)))))
	message = binary.BigEndian.AppendUint32(message, flags)
	return append(message, payload...)
}

// clipboardWire is text as the extended clipboard carries it: UTF-8, CRLF line
// ends and a NUL, at most MaxClipboard bytes with the NUL.
func clipboardWire(text string) []byte {
	data := make([]byte, 0, min(len(text)+1, MaxClipboard))
	for _, r := range text {
		if r == '\r' {
			continue
		}
		if r == '\n' {
			if len(data)+2 >= MaxClipboard {
				break
			}
			data = append(data, '\r', '\n')
			continue
		}
		if len(data)+utf8.RuneLen(r) >= MaxClipboard {
			break
		}
		data = utf8.AppendRune(data, r)
	}
	return append(data, 0)
}

// provideMessage is the Provide of one text in wire form.
func provideMessage(wire []byte) ([]byte, error) {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, err := writer.Write(binary.BigEndian.AppendUint32(nil, uint32(len(wire))))
	if err == nil {
		_, err = writer.Write(wire)
	}
	if err == nil {
		err = writer.Close()
	}
	if err != nil {
		return nil, err
	}
	return extendedMessage(clipProvide|clipUTF8, compressed.Bytes()), nil
}

// extendedCutText reads one extended ServerCutText whose body is length bytes.
func (client *Client) extendedCutText(sink Sink, length uint32) error {
	if length < 4 {
		return fmt.Errorf("%w: a %d byte extended clipboard message", ErrProtocol, length)
	}
	if length > maxCutText {
		return fmt.Errorf("%w: a %d byte clipboard", ErrProtocol, length)
	}
	if length > clipMessage {
		_, err := io.CopyN(io.Discard, client.r, int64(length))
		return err
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(client.r, body); err != nil {
		return err
	}
	flags, payload := binary.BigEndian.Uint32(body), body[4:]
	switch {
	case flags&clipCaps != 0:
		if len(payload) < 4*bits.OnesCount32(flags&clipFormats) {
			return fmt.Errorf("%w: truncated extended clipboard capabilities", ErrProtocol)
		}
		return client.clipboardCaps(flags)
	case flags&clipProvide != 0:
		return client.clipboardProvided(sink, flags, payload)
	case flags&clipRequest != 0:
		return client.clipboardRequested(flags)
	case flags&clipPeek != 0:
		return client.clipboardPeeked()
	case flags&clipNotify != 0:
		if flags&clipUTF8 != 0 {
			return client.write(extendedMessage(clipRequest|clipUTF8, nil))
		}
	}
	return nil
}

// clipboardCaps takes the server's capabilities. A server that can announce,
// request and provide UTF-8 text gets ours in return, which it waits for before
// it uses the extended clipboard; from then on SendClipboard sends UTF-8. The
// size limit given for text is 0, which the protocol recommends: the server then
// announces every change with a Notify and the client asks for the text, so a
// Provide is never in doubt about what it replaces.
func (client *Client) clipboardCaps(flags uint32) error {
	const needed = clipRequest | clipNotify | clipProvide
	if flags&clipUTF8 == 0 || flags&needed != needed {
		return client.settleClipboard()
	}
	client.mu.Lock()
	client.extClipboard = true
	client.mu.Unlock()
	if err := client.write(extendedMessage(clipCaps|needed|clipUTF8, binary.BigEndian.AppendUint32(nil, 0))); err != nil {
		return err
	}
	return client.settleClipboard()
}

// settleClipboard ends the wait for the server's capabilities, on them or on the
// first update, which a server sends them before, and sends what SendClipboard
// held in the meantime.
func (client *Client) settleClipboard() error {
	client.mu.Lock()
	if client.clipSettled {
		client.mu.Unlock()
		return nil
	}
	client.clipSettled = true
	text, waiting := client.clipPending, client.clipWaiting
	client.clipPending, client.clipWaiting = "", false
	client.mu.Unlock()
	if !waiting {
		return nil
	}
	return client.SendClipboard(text)
}

// clipboardProvided delivers the text of a Provide. Text is the first format,
// so nothing after it is read.
func (client *Client) clipboardProvided(sink Sink, flags uint32, payload []byte) error {
	if flags&clipUTF8 == 0 {
		return nil
	}
	stream, err := zlib.NewReader(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: extended clipboard: %v", ErrProtocol, err)
	}
	defer stream.Close()
	var size [4]byte
	if _, err := io.ReadFull(stream, size[:]); err != nil {
		return fmt.Errorf("%w: extended clipboard: %v", ErrProtocol, err)
	}
	length := binary.BigEndian.Uint32(size[:])
	if length > MaxClipboard {
		return nil
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(stream, data); err != nil {
		return fmt.Errorf("%w: extended clipboard: %v", ErrProtocol, err)
	}
	if end := bytes.IndexByte(data, 0); end >= 0 {
		data = data[:end]
	}
	text := strings.ToValidUTF8(string(data), "\uFFFD")
	sink.ServerText(strings.ReplaceAll(text, "\r", ""))
	return nil
}

// clipboardRequested answers a Request with the text last sent by SendClipboard.
func (client *Client) clipboardRequested(flags uint32) error {
	client.mu.Lock()
	wire := client.clip
	client.mu.Unlock()
	if flags&clipUTF8 == 0 || wire == nil {
		return nil
	}
	message, err := provideMessage(wire)
	if err != nil {
		return err
	}
	return client.write(message)
}

// clipboardPeeked answers a Peek with what is available.
func (client *Client) clipboardPeeked() error {
	client.mu.Lock()
	have := client.clip != nil
	client.mu.Unlock()
	flags := clipNotify
	if have {
		flags |= clipUTF8
	}
	return client.write(extendedMessage(flags, nil))
}

// sendExtendedClipboard keeps text for the server's Request and announces it.
func (client *Client) sendExtendedClipboard(text string) error {
	client.mu.Lock()
	client.clip = clipboardWire(text)
	client.mu.Unlock()
	return client.write(extendedMessage(clipNotify|clipUTF8, nil))
}
