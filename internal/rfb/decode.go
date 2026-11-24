package rfb

import (
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// decodeRaw reads width*height pixels and hands them over a band of rows at a
// time, so what is held in memory does not depend on the rectangle's size.
func (client *Client) decodeRaw(sink Sink, x, y, width, height int) error {
	stride := width * client.format.size()
	if stride == 0 {
		return nil
	}
	rows := max(1, min(height, (64<<10)/stride))
	if need := rows * stride; cap(client.rowBuf) < need {
		client.rowBuf = make([]byte, need)
	}
	for top := 0; top < height; top += rows {
		band := min(rows, height-top)
		buffer := client.rowBuf[:band*stride]
		if _, err := io.ReadFull(client.r, buffer); err != nil {
			return err
		}
		client.draw(sink, x, y+top, width, band, buffer)
	}
	return nil
}

// draw hands the sink pixels as they came off the wire in the format asked
// for, widened to four bytes each when the format is a smaller one.
func (client *Client) draw(sink Sink, x, y, width, height int, wire []byte) {
	if client.format == Pixel32 {
		sink.Update(x, y, width, height, wire, width*4)
		return
	}
	if need := width * height * 4; cap(client.convBuf) < need {
		client.convBuf = make([]byte, need)
	}
	buffer := client.convBuf[:width*height*4]
	client.format.expand(buffer, wire)
	sink.Update(x, y, width, height, buffer, width*4)
}

func (client *Client) decodeCopyRect(sink Sink, x, y, width, height int) error {
	var source [4]byte
	if _, err := io.ReadFull(client.r, source[:]); err != nil {
		return err
	}
	fromX, fromY := int(binary.BigEndian.Uint16(source[0:])), int(binary.BigEndian.Uint16(source[2:]))
	if fromX+width > client.width || fromY+height > client.height {
		return fmt.Errorf("%w: a copy from outside the desktop", ErrProtocol)
	}
	sink.Copy(x, y, width, height, fromX, fromY)
	return nil
}

// readPixel reads one pixel in the format asked for and returns its colour.
func (client *Client) readPixel() (blue, green, red byte, err error) {
	sz := client.format.size()
	if _, err = io.ReadFull(client.r, client.pixelBuf[:sz]); err != nil {
		return 0, 0, 0, err
	}
	blue, green, red = client.format.bgr(client.pixelBuf[:])
	return blue, green, red, nil
}

func (client *Client) decodeRRE(sink Sink, x, y, width, height int) error {
	var header [4]byte
	if _, err := io.ReadFull(client.r, header[:]); err != nil {
		return err
	}
	count := int64(binary.BigEndian.Uint32(header[:]))
	// A subrectangle covers at least a pixel; more of them than pixels is not a
	// picture, it is a way to keep the client reading.
	if count > int64(width)*int64(height) {
		return fmt.Errorf("%w: %d subrectangles in a %dx%d rectangle", ErrProtocol, count, width, height)
	}
	blue, green, red, err := client.readPixel()
	if err != nil {
		return err
	}
	sink.Fill(x, y, width, height, blue, green, red)
	// A subrectangle is its pixel followed by x, y, width and height.
	pixelSize := client.format.size()
	for ; count > 0; count-- {
		var sub [12]byte
		if _, err := io.ReadFull(client.r, sub[:pixelSize+8]); err != nil {
			return err
		}
		blue, green, red := client.format.bgr(sub[:])
		geometry := sub[pixelSize:]
		subX, subY := int(binary.BigEndian.Uint16(geometry[0:])), int(binary.BigEndian.Uint16(geometry[2:]))
		subWidth, subHeight := int(binary.BigEndian.Uint16(geometry[4:])), int(binary.BigEndian.Uint16(geometry[6:]))
		if subX+subWidth > width || subY+subHeight > height {
			return fmt.Errorf("%w: an RRE subrectangle outside its rectangle", ErrProtocol)
		}
		sink.Fill(x+subX, y+subY, subWidth, subHeight, blue, green, red)
	}
	return nil
}

// Hextile subencoding flags.
const (
	hextileRaw        = 1
	hextileBackground = 2
	hextileForeground = 4
	hextileSubrects   = 8
	hextileColored    = 16
)

func (client *Client) decodeHextile(sink Sink, x, y, width, height int) error {
	var bgBlue, bgGreen, bgRed, fgBlue, fgGreen, fgRed byte
	if cap(client.tileBuf) < 16*16*4 {
		client.tileBuf = make([]byte, 16*16*4)
	}
	for top := 0; top < height; top += 16 {
		tileHeight := min(16, height-top)
		for left := 0; left < width; left += 16 {
			tileWidth := min(16, width-left)
			flags, err := client.r.ReadByte()
			if err != nil {
				return err
			}
			if flags&hextileRaw != 0 {
				buffer := client.tileBuf[:tileWidth*tileHeight*client.format.size()]
				if _, err := io.ReadFull(client.r, buffer); err != nil {
					return err
				}
				client.draw(sink, x+left, y+top, tileWidth, tileHeight, buffer)
				continue
			}
			if flags&hextileBackground != 0 {
				if bgBlue, bgGreen, bgRed, err = client.readPixel(); err != nil {
					return err
				}
			}
			if flags&hextileForeground != 0 {
				if fgBlue, fgGreen, fgRed, err = client.readPixel(); err != nil {
					return err
				}
			}
			sink.Fill(x+left, y+top, tileWidth, tileHeight, bgBlue, bgGreen, bgRed)
			if flags&hextileSubrects == 0 {
				continue
			}
			count, err := client.r.ReadByte()
			if err != nil {
				return err
			}
			for range count {
				blue, green, red := fgBlue, fgGreen, fgRed
				if flags&hextileColored != 0 {
					if blue, green, red, err = client.readPixel(); err != nil {
						return err
					}
				}
				var position [2]byte
				if _, err := io.ReadFull(client.r, position[:]); err != nil {
					return err
				}
				subX, subY := int(position[0]>>4), int(position[0]&15)
				subWidth, subHeight := int(position[1]>>4)+1, int(position[1]&15)+1
				if subX+subWidth > tileWidth || subY+subHeight > tileHeight {
					return fmt.Errorf("%w: a Hextile subrectangle outside its tile", ErrProtocol)
				}
				sink.Fill(x+left+subX, y+top+subY, subWidth, subHeight, blue, green, red)
			}
		}
	}
	return nil
}

// zrleSource is the compressed bytes of the rectangle being decoded. The zlib
// stream outlives every rectangle (the server keeps one deflater for the whole
// connection), so the inflater is created once and is pointed at each
// rectangle's bytes in turn; being a ByteReader keeps it from buffering ahead
// into the next message.
type zrleSource struct {
	r         io.Reader
	remaining int
}

func (source *zrleSource) Read(p []byte) (int, error) {
	if source.remaining <= 0 {
		return 0, io.EOF
	}
	if len(p) > source.remaining {
		p = p[:source.remaining]
	}
	n, err := source.r.Read(p)
	source.remaining -= n
	return n, err
}

func (source *zrleSource) ReadByte() (byte, error) {
	if source.remaining <= 0 {
		return 0, io.EOF
	}
	if br, ok := source.r.(io.ByteReader); ok {
		b, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		source.remaining--
		return b, nil
	}
	var one [1]byte
	n, err := source.Read(one[:])
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return one[0], nil
}

type zrleState struct {
	source  zrleSource
	inflate io.Reader
	tile    [64 * 64 * 4]byte
	palette [128][3]byte
	rawBuf  []byte
	reader  zrleReader
}

func (client *Client) decodeZRLE(sink Sink, x, y, width, height int) error {
	var header [4]byte
	if _, err := io.ReadFull(client.r, header[:]); err != nil {
		return err
	}
	length := int64(binary.BigEndian.Uint32(header[:]))
	// Compressed data is rarely larger than the pixels; a little more than that
	// is the most a legitimate server sends.
	if limit := int64(width)*int64(height)*5 + maxZRLEExtra; length > limit {
		return fmt.Errorf("%w: %d compressed bytes for a %dx%d rectangle", ErrProtocol, length, width, height)
	}
	state := &client.zrle
	state.source.r, state.source.remaining = client.r, int(length)
	if state.inflate == nil {
		if length == 0 {
			return nil
		}
		reader, err := zlib.NewReader(&state.source)
		if err != nil {
			return fmt.Errorf("%w: ZRLE stream: %v", ErrProtocol, err)
		}
		state.inflate = reader
	}
	// Whatever the structure below does not consume, the stream must still be
	// left at the end of this rectangle's bytes.
	defer func() { _, _ = io.CopyN(io.Discard, client.r, int64(state.source.remaining)) }()
	state.reader.r = state.inflate
	state.reader.format = client.format
	state.reader.pos = 0
	state.reader.end = 0
	inflated := &state.reader
	for top := 0; top < height; top += 64 {
		tileHeight := min(64, height-top)
		for left := 0; left < width; left += 64 {
			tileWidth := min(64, width-left)
			if err := state.tileDecode(inflated, tileWidth, tileHeight); err != nil {
				return err
			}
			sink.Update(x+left, y+top, tileWidth, tileHeight, state.tile[:tileWidth*tileHeight*4], tileWidth*4)
		}
	}
	return nil
}

// zrleReader reads the inflated stream and turns the end of the data into an
// error: inside a rectangle that is a truncated rectangle.
type zrleReader struct {
	r      io.Reader
	format PixelFormat
	buf    [4096]byte
	pos    int
	end    int
	wire   [4]byte
}

func (reader *zrleReader) fill() error {
	n, err := reader.r.Read(reader.buf[:])
	if n > 0 {
		reader.pos = 0
		reader.end = n
		return nil
	}
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	if err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func (reader *zrleReader) ReadByte() (byte, error) {
	if reader.pos >= reader.end {
		if err := reader.fill(); err != nil {
			return 0, err
		}
	}
	b := reader.buf[reader.pos]
	reader.pos++
	return b, nil
}

func (reader *zrleReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if reader.pos < reader.end {
		n := copy(p, reader.buf[reader.pos:reader.end])
		reader.pos += n
		return n, nil
	}
	if len(p) >= len(reader.buf) {
		n, err := reader.r.Read(p)
		if err == io.EOF && n == 0 {
			return 0, io.ErrUnexpectedEOF
		}
		return n, err
	}
	if err := reader.fill(); err != nil {
		return 0, err
	}
	n := copy(p, reader.buf[reader.pos:reader.end])
	reader.pos += n
	return n, nil
}

// pixel reads a compressed pixel of the format asked for (see
// PixelFormat.compressedSize) and returns its colour as blue, green, red.
func (reader *zrleReader) pixel() (pixel [3]byte, err error) {
	sz := reader.format.compressedSize()
	if reader.end-reader.pos >= sz {
		pixel[0], pixel[1], pixel[2] = reader.format.bgr(reader.buf[reader.pos : reader.pos+sz])
		reader.pos += sz
		return pixel, nil
	}
	if _, err = io.ReadFull(reader, reader.wire[:sz]); err != nil {
		return pixel, err
	}
	pixel[0], pixel[1], pixel[2] = reader.format.bgr(reader.wire[:sz])
	return pixel, nil
}

var errZRLE = errors.New("rfb: bad ZRLE tile")

// tileDecode decodes one tile into state.tile as blue, green, red, unused.
func (state *zrleState) tileDecode(reader *zrleReader, width, height int) error {
	mode, err := reader.ReadByte()
	if err != nil {
		return err
	}
	pixels := width * height
	out := state.tile[:pixels*4]
	put := func(offset int, pixel [3]byte) {
		out[offset], out[offset+1], out[offset+2], out[offset+3] = pixel[0], pixel[1], pixel[2], 0
	}
	paletteSize := int(mode & 0x7f)
	rle := mode&0x80 != 0
	switch {
	case mode == 0:
		sz := reader.format.compressedSize()
		need := pixels * sz
		if cap(state.rawBuf) < need {
			state.rawBuf = make([]byte, need)
		}
		raw := state.rawBuf[:need]
		if _, err := io.ReadFull(reader, raw); err != nil {
			return err
		}
		if sz == 3 && reader.format == Pixel32 {
			for i, o := 0, 0; i < need; i += 3 {
				out[o] = raw[i]
				out[o+1] = raw[i+1]
				out[o+2] = raw[i+2]
				out[o+3] = 0
				o += 4
			}
		} else {
			for i, o := 0, 0; i < need; i += sz {
				b, g, r := reader.format.bgr(raw[i : i+sz])
				out[o] = b
				out[o+1] = g
				out[o+2] = r
				out[o+3] = 0
				o += 4
			}
		}
	case mode == 1:
		pixel, err := reader.pixel()
		if err != nil {
			return err
		}
		for offset := 0; offset < len(out); offset += 4 {
			put(offset, pixel)
		}
	case !rle && paletteSize >= 2 && paletteSize <= 16:
		if err := state.readPalette(reader, paletteSize); err != nil {
			return err
		}
		bits := 1
		if paletteSize > 2 {
			bits = 2
		}
		if paletteSize > 4 {
			bits = 4
		}
		for row := 0; row < height; row++ {
			var packed byte
			for column := 0; column < width; column++ {
				if column*bits%8 == 0 {
					if packed, err = reader.ReadByte(); err != nil {
						return err
					}
				}
				shift := 8 - bits - column*bits%8
				choice := int(packed>>shift) & (1<<bits - 1)
				if choice >= paletteSize {
					return errZRLE
				}
				put((row*width+column)*4, state.palette[choice])
			}
		}
	case rle && paletteSize == 0:
		for position := 0; position < pixels; {
			pixel, err := reader.pixel()
			if err != nil {
				return err
			}
			run, err := reader.runLength()
			if err != nil {
				return err
			}
			if run > pixels-position {
				return errZRLE
			}
			for end := position + run; position < end; position++ {
				put(position*4, pixel)
			}
		}
	case rle && paletteSize >= 2:
		if err := state.readPalette(reader, paletteSize); err != nil {
			return err
		}
		for position := 0; position < pixels; {
			choice, err := reader.ReadByte()
			if err != nil {
				return err
			}
			run := 1
			if choice&0x80 != 0 {
				if run, err = reader.runLength(); err != nil {
					return err
				}
			}
			if int(choice&0x7f) >= paletteSize || run > pixels-position {
				return errZRLE
			}
			for end := position + run; position < end; position++ {
				put(position*4, state.palette[choice&0x7f])
			}
		}
	default:
		return fmt.Errorf("%w: subencoding %d", errZRLE, mode)
	}
	return nil
}

func (state *zrleState) readPalette(reader *zrleReader, size int) error {
	for index := 0; index < size; index++ {
		pixel, err := reader.pixel()
		if err != nil {
			return err
		}
		state.palette[index] = pixel
	}
	return nil
}

// runLength reads a run length: 255s are added until a byte below 255, and one
// more. The sum is bounded by what a tile can hold, because the caller checks it
// against the pixels left and this stops reading at that bound.
func (reader *zrleReader) runLength() (int, error) {
	length := 1
	for {
		value, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		length += int(value)
		if value != 255 {
			return length, nil
		}
		if length > 64*64 {
			return 0, errZRLE
		}
	}
}
