// Command vncfixture is a loopback VNC server for screenshots, in the style of
// tools/e2esshd: it speaks the part of RFB 3.8 a reader needs to see a picture,
// and draws a fixed desktop so a shot of Motty's VNC pane shows something other
// than a black rectangle. Nothing here ships.
//
//	go build -o /tmp/vncfixture ./tools/e2e/vncfixture
//	/tmp/vncfixture 5901 &
//
// The desktop it draws is a small window manager's idea of a build machine: a
// title bar, a command line, and the tail of a test run. The text is rendered
// into the framebuffer with basicfont, so the shot needs no font files beside
// the fixture.
package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"log"
	"net"
	"os"
	"strconv"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	desktopWidth  = 1024
	desktopHeight = 640
	desktopName   = "build-runner"
)

func main() {
	port := 5901
	if len(os.Args) > 1 {
		value, err := strconv.Atoi(os.Args[1])
		if err != nil {
			log.Fatalf("port: %v", err)
		}
		port = value
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on %s", listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Print(err)
			continue
		}
		go serve(conn)
	}
}

// serve runs one connection's handshake and then answers framebuffer requests.
func serve(conn net.Conn) {
	defer conn.Close()
	frame := paint()
	if err := handshake(conn); err != nil {
		log.Printf("handshake: %v", err)
		return
	}
	if err := pump(conn, frame); err != nil && err != io.EOF {
		log.Printf("session: %v", err)
	}
}

// handshake is the server side of RFB 3.8 with no security: version, chosen
// security type, ServerInit, then the client's pixel format and encodings.
func handshake(conn net.Conn) error {
	reader := bufio.NewReader(conn)
	if _, err := conn.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	if _, err := io.ReadFull(reader, make([]byte, 12)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{1, 1}); err != nil { // one security type: None
		return err
	}
	if _, err := io.ReadFull(reader, make([]byte, 1)); err != nil { // the client's choice
		return err
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil { // SecurityResult: ok
		return err
	}
	if _, err := io.ReadFull(reader, make([]byte, 1)); err != nil { // ClientInit
		return err
	}
	init := make([]byte, 0, 24)
	init = binary.BigEndian.AppendUint16(init, desktopWidth)
	init = binary.BigEndian.AppendUint16(init, desktopHeight)
	// 32 bits per pixel, depth 24, little-endian true colour, red/green/blue in
	// bits 16/8/0: memory order blue, green, red, pad.
	init = append(init, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0)
	init = binary.BigEndian.AppendUint32(init, uint32(len(desktopName)))
	init = append(init, desktopName...)
	if _, err := conn.Write(init); err != nil {
		return err
	}
	// SetPixelFormat is 20 bytes; SetEncodings is a 4-byte header plus 4 per
	// encoding. Reading them keeps the stream aligned; the fixture only ever
	// answers with raw rectangles, which every client understands.
	if _, err := io.ReadFull(reader, make([]byte, 20)); err != nil {
		return err
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	count := binary.BigEndian.Uint16(header[2:])
	if _, err := io.ReadFull(reader, make([]byte, int(count)*4)); err != nil {
		return err
	}
	// A client may also send SetPixelFormat before ServerInit is read in some
	// orders; the reader above has consumed it, so nothing else is pending.
	return nil
}

// pump answers FramebufferUpdateRequest (type 3) with the whole desktop and
// drains everything else the client sends.
func pump(conn net.Conn, frame []byte) error {
	reader := bufio.NewReader(conn)
	header := make([]byte, 1)
	for {
		if _, err := io.ReadFull(reader, header); err != nil {
			return err
		}
		switch header[0] {
		case 3: // FramebufferUpdateRequest: incremental, x, y, w, h
			if _, err := io.ReadFull(reader, make([]byte, 9)); err != nil {
				return err
			}
			if err := sendFrame(conn, frame); err != nil {
				return err
			}
		case 4: // KeyEvent: down-flag, padding, key
			if _, err := io.ReadFull(reader, make([]byte, 7)); err != nil {
				return err
			}
		case 5: // PointerEvent: button mask, x, y
			if _, err := io.ReadFull(reader, make([]byte, 5)); err != nil {
				return err
			}
		case 6: // ClientCutText: three pad bytes, then a 4-byte length and the text
			head := make([]byte, 7)
			if _, err := io.ReadFull(reader, head); err != nil {
				return err
			}
			if _, err := io.ReadFull(reader, make([]byte, binary.BigEndian.Uint32(head[3:]))); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown message %d", header[0])
		}
	}
}

// sendFrame writes one FramebufferUpdate holding the whole desktop as a raw
// rectangle.
func sendFrame(w io.Writer, frame []byte) error {
	message := make([]byte, 0, 16+len(frame))
	message = append(message, 0, 0)                     // FramebufferUpdate
	message = binary.BigEndian.AppendUint16(message, 1) // one rectangle
	message = binary.BigEndian.AppendUint16(message, 0) // x
	message = binary.BigEndian.AppendUint16(message, 0) // y
	message = binary.BigEndian.AppendUint16(message, desktopWidth)
	message = binary.BigEndian.AppendUint16(message, desktopHeight)
	message = binary.BigEndian.AppendUint32(message, 0) // Raw encoding
	message = append(message, frame...)
	_, err := w.Write(message)
	return err
}

// paint draws the desktop: a window with a title bar, a prompt, and the tail of
// a test run. Colours are the ones a dark terminal theme would use.
func paint() []byte {
	img := image.NewRGBA(image.Rect(0, 0, desktopWidth, desktopHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0x1a, 0x1b, 0x26, 0xff}}, image.Point{}, draw.Src)

	// The window frame.
	window := image.Rect(0, 0, desktopWidth, desktopHeight)
	draw.Draw(img, window, &image.Uniform{color.RGBA{0x16, 0x17, 0x20, 0xff}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, desktopWidth, 28), &image.Uniform{color.RGBA{0x22, 0x24, 0x33, 0xff}}, image.Point{}, draw.Src)

	muted := color.RGBA{0x8b, 0x91, 0xa8, 0xff}
	text := color.RGBA{0xd8, 0xdd, 0xea, 0xff}
	green := color.RGBA{0x6c, 0xcf, 0x8a, 0xff}
	red := color.RGBA{0xf2, 0x74, 0x74, 0xff}
	yellow := color.RGBA{0xe8, 0xc4, 0x6a, 0xff}
	accent := color.RGBA{0x6e, 0xa8, 0xfe, 0xff}

	drawString(img, 14, 8, "build-runner — shell", muted)
	for i, dot := range []color.RGBA{red, yellow, green} {
		draw.Draw(img, image.Rect(desktopWidth-64+i*18, 11, desktopWidth-58+i*18, 17), &image.Uniform{dot}, image.Point{}, draw.Src)
	}

	lines := []struct {
		y     int
		text  string
		color color.RGBA
	}{
		{48, "build@ci-runner:~/src/motty$ make check", text},
		{72, "gofmt -l .", text},
		{92, "go vet ./...", text},
		{112, "go test ./...", text},
		{140, "ok      motty/internal/rfb            0.42s", green},
		{160, "ok      motty/internal/sshconn        0.67s", green},
		{180, "ok      motty/internal/sftpclient     0.31s", green},
		{200, "ok      motty/internal/forward        0.28s", green},
		{220, "ok      motty/internal/workbench      2.11s", green},
		{240, "FAIL    motty/internal/term           1.84s", red},
		{264, "--- FAIL: TestAltScreenRestore (0.02s)", red},
		{284, "    term_test.go:412: the restored screen kept the alt-screen run", text},
		{304, "    want 0 runs, got 1", muted},
		{328, "ok      motty/internal/desktop/vnc    0.55s", green},
		{348, "ok      motty/internal/desktop/rdp    0.61s", green},
		{368, "ok      motty/internal/ai             1.09s", green},
		{388, "ok      motty/internal/scpclient      0.24s", green},
		{408, "ok      motty/cmd/motty               0.38s", green},
		{432, "ran 1 of 96 packages, 95 passed, 1 failed", yellow},
		{456, "build@ci-runner:~/src/motty$ ", accent},
	}
	for _, line := range lines {
		drawString(img, 20, line.y, line.text, line.color)
	}

	// The cursor block after the prompt.
	cursorX := 20 + len(lines[len(lines)-1].text)*7
	draw.Draw(img, image.Rect(cursorX, 456+2, cursorX+7, 456+13), &image.Uniform{accent}, image.Point{}, draw.Src)
	return encodeRaw(img)
}

// drawString draws text at x,y with the built-in fixed font.
func drawString(img *image.RGBA, x, y int, value string, ink color.RGBA) {
	face := basicfont.Face7x13
	drawer := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{ink},
		Face: face,
		Dot:  fixed.P(x, y+face.Ascent),
	}
	drawer.DrawString(value)
}

// encodeRaw turns the image into the client's pixel order: blue, green, red,
// pad, four bytes per pixel, row by row.
func encodeRaw(img *image.RGBA) []byte {
	out := make([]byte, 0, desktopWidth*desktopHeight*4)
	for y := 0; y < desktopHeight; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+desktopWidth*4]
		for x := 0; x < desktopWidth; x++ {
			out = append(out, row[x*4+2], row[x*4+1], row[x*4+0], 0)
		}
	}
	return out
}
