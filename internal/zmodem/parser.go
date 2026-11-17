package zmodem

// parser turns the bytes of a ZMODEM stream into headers and data subpackets,
// one byte at a time, so a transfer that is cut anywhere in the stream by the
// way the bytes happened to arrive parses the same as one delivered whole.
type parser struct {
	state parserState
	// esc is set after a ZDLE that has not been resolved yet.
	esc bool
	// cans counts consecutive CAN bytes: five abort the transfer.
	cans int

	// header being read.
	format byte
	raw    [9]byte // type, four arguments, up to four check bytes
	count  int
	want   int // decoded bytes the header has in all

	hex     [14]byte
	hexSeen int

	// data subpacket being read. wide says its check has 32 bits.
	wide    bool
	data    []byte
	end     byte
	checked int // check bytes read after the ending
	check   [4]byte

	// results of the last event.
	hdr header
}

type parserState uint8

const (
	stSeek parserState = iota
	stPad
	stFormat
	stBinaryHeader
	stHexHeader
	stData
	stDataCheck
)

// maxSubpacket bounds one data subpacket. Senders use 1 to 8 KiB; a stream with
// a longer one is damaged or hostile.
const maxSubpacket = 16 << 10

type event uint8

const (
	evNone   event = iota
	evHeader       // p.hdr is a header with a good check
	evBadHeader
	evData    // p.data and p.end are a subpacket with a good check
	evBadData // a subpacket was damaged, or too long
	evCancel  // five CANs
)

// wantData switches to reading data subpackets with the check width of the
// header that announced them.
func (p *parser) wantData(wide bool) {
	p.state, p.wide, p.data, p.esc = stData, wide, p.data[:0], false
}

// step takes one byte and reports what it completed, if anything.
func (p *parser) step(b byte) event {
	if b == zdle {
		p.cans++
		if p.cans >= 5 {
			p.cans = 0
			p.state, p.esc = stSeek, false
			return evCancel
		}
	} else {
		p.cans = 0
	}
	switch p.state {
	case stSeek:
		if b == zpad {
			p.state = stPad
		}
	case stPad:
		switch b {
		case zpad:
		case zdle:
			p.state = stFormat
		default:
			p.state = stSeek
		}
	case stFormat:
		p.format = b
		p.count = 0
		switch b {
		case zbin:
			p.state, p.want, p.esc = stBinaryHeader, 7, false
		case zbin32:
			p.state, p.want, p.esc = stBinaryHeader, 9, false
		case zhex:
			p.state, p.hexSeen = stHexHeader, 0
		default:
			p.state = stSeek
		}
	case stBinaryHeader:
		value, status := p.unescape(b)
		switch status {
		case escMore:
		case escByte:
			p.raw[p.count] = value
			p.count++
			if p.count == p.want {
				p.state = stSeek
				return p.finishBinaryHeader()
			}
		default:
			p.state = stSeek
			return evBadHeader
		}
	case stHexHeader:
		if b == xon || b == xoff {
			return evNone
		}
		digit := hexValue(b)
		if digit < 0 {
			p.state = stSeek
			return evBadHeader
		}
		p.hex[p.hexSeen] = byte(digit)
		p.hexSeen++
		if p.hexSeen == len(p.hex) {
			p.state = stSeek
			return p.finishHexHeader()
		}
	case stData:
		value, status := p.unescape(b)
		switch status {
		case escMore:
		case escByte:
			if len(p.data) >= maxSubpacket {
				p.state = stSeek
				return evBadData
			}
			p.data = append(p.data, value)
		case escEnd:
			p.end, p.state, p.checked = value, stDataCheck, 0
		default:
			p.state = stSeek
			return evBadData
		}
	case stDataCheck:
		value, status := p.unescape(b)
		switch status {
		case escMore:
		case escByte:
			p.check[p.checked] = value
			p.checked++
			if p.checked == p.checkSize() {
				return p.finishData()
			}
		default:
			p.state = stSeek
			return evBadData
		}
	}
	return evNone
}

func (p *parser) checkSize() int {
	if p.wide {
		return 4
	}
	return 2
}

func hexValue(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10
	}
	return -1
}

type escStatus uint8

const (
	escMore escStatus = iota // nothing yet: the byte was a prefix or flow control
	escByte                  // a data byte
	escEnd                   // a subpacket ending
	escBad
)

// unescape decodes one byte of ZDLE-escaped data.
func (p *parser) unescape(b byte) (byte, escStatus) {
	if p.esc {
		p.esc = false
		switch {
		case b >= zcrce && b <= zcrcw:
			return b, escEnd
		case b == 'l':
			return 0x7f, escByte
		case b == 'm':
			return 0xff, escByte
		case b&0x60 == 0x40:
			return b ^ 0x40, escByte
		}
		return 0, escBad
	}
	switch b {
	case zdle:
		p.esc = true
		return 0, escMore
	case xon, xoff, 0x91, 0x93:
		return 0, escMore
	}
	return b, escByte
}

func (p *parser) finishBinaryHeader() event {
	body := p.raw[:5]
	if p.format == zbin {
		got := uint16(p.raw[5])<<8 | uint16(p.raw[6])
		if crc16Fast(0, body) != got {
			return evBadHeader
		}
	} else {
		got := uint32(p.raw[5]) | uint32(p.raw[6])<<8 | uint32(p.raw[7])<<16 | uint32(p.raw[8])<<24
		if crc32Update(0, body) != got {
			return evBadHeader
		}
	}
	p.setHeader(body, p.format == zbin32, false)
	return evHeader
}

func (p *parser) finishHexHeader() event {
	var raw [7]byte
	for index := range raw {
		raw[index] = p.hex[2*index]<<4 | p.hex[2*index+1]
	}
	if crc16Fast(0, raw[:5]) != uint16(raw[5])<<8|uint16(raw[6]) {
		return evBadHeader
	}
	p.setHeader(raw[:5], false, true)
	return evHeader
}

func (p *parser) setHeader(body []byte, wide, hex bool) {
	p.hdr = header{
		kind:     body[0],
		position: uint32(body[1]) | uint32(body[2])<<8 | uint32(body[3])<<16 | uint32(body[4])<<24,
		flags:    [4]byte{body[1], body[2], body[3], body[4]},
		crc32:    wide,
		hex:      hex,
	}
}

// finishData checks the subpacket that has just been read completely.
func (p *parser) finishData() event {
	ok := false
	if p.wide {
		got := uint32(p.check[0]) | uint32(p.check[1])<<8 | uint32(p.check[2])<<16 | uint32(p.check[3])<<24
		ok = crc32Update(crc32Update(0, p.data), []byte{p.end}) == got
	} else {
		got := uint16(p.check[0])<<8 | uint16(p.check[1])
		ok = crc16Fast(crc16Fast(0, p.data), []byte{p.end}) == got
	}
	if !ok {
		p.state = stSeek
		return evBadData
	}
	if p.end == zcrcg || p.end == zcrcq {
		// More data follows in the same frame: stay in data mode with room for it.
		p.state = stData
		return evData
	}
	p.state = stSeek
	return evData
}

// resumeData starts the next subpacket of a frame after the engine has used the
// data of the last one.
func (p *parser) resumeData() {
	p.data = p.data[:0]
}
