//go:build windows

package serial

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procGetCommState = kernel32.NewProc("GetCommState")
	procSetCommState = kernel32.NewProc("SetCommState")
	procSetTimeouts  = kernel32.NewProc("SetCommTimeouts")
	procPurgeComm    = kernel32.NewProc("PurgeComm")
)

// dcb is the Windows DCB structure: the line state of a communications device.
type dcb struct {
	DCBlength  uint32
	BaudRate   uint32
	Flags      uint32
	WReserved  uint16
	XonLim     uint16
	XoffLim    uint16
	ByteSize   uint8
	Parity     uint8
	StopBits   uint8
	XonChar    byte
	XoffChar   byte
	ErrorChar  byte
	EOFChar    byte
	EvtChar    byte
	WReserved1 uint16
}

// Bits of dcb.Flags.
const (
	flagBinary     = 1 << 0
	flagParity     = 1 << 1
	flagOutxCTS    = 1 << 2
	flagDTRControl = 1 << 4 // DTR_CONTROL_ENABLE is 1 in bits 4-5
	flagOutX       = 1 << 8
	flagInX        = 1 << 9
	flagRTSControl = 1 << 12 // RTS_CONTROL_ENABLE 1, HANDSHAKE 2, in bits 12-13
)

type commTimeouts struct {
	ReadIntervalTimeout         uint32
	ReadTotalTimeoutMultiplier  uint32
	ReadTotalTimeoutConstant    uint32
	WriteTotalTimeoutMultiplier uint32
	WriteTotalTimeoutConstant   uint32
}

// port is an open COM port using overlapped I/O, so a blocked read can be
// cancelled by Close.
type port struct {
	handle windows.Handle

	mu     sync.Mutex
	closed bool
	users  sync.WaitGroup
}

func open(device string, settings Settings) (io.ReadWriteCloser, error) {
	name := device
	if !strings.HasPrefix(name, `\\.\`) {
		name = `\\.\` + name // needed for COM10 and above, harmless below
	}
	pointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(pointer, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, fmt.Errorf("serial: opening %s: %w", device, err)
	}
	if err := configure(handle, settings); err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("serial: configuring %s: %w", device, err)
	}
	return &port{handle: handle}, nil
}

func configure(handle windows.Handle, settings Settings) error {
	var state dcb
	state.DCBlength = uint32(unsafe.Sizeof(state))
	if result, _, err := procGetCommState.Call(uintptr(handle), uintptr(unsafe.Pointer(&state))); result == 0 {
		return err
	}
	applySettings(&state, settings)
	if result, _, err := procSetCommState.Call(uintptr(handle), uintptr(unsafe.Pointer(&state))); result == 0 {
		return err
	}
	// With only an interval timeout a read waits for the first byte for as long
	// as it takes, then returns once the line has been quiet for a millisecond.
	timeouts := commTimeouts{ReadIntervalTimeout: 1}
	if result, _, err := procSetTimeouts.Call(uintptr(handle), uintptr(unsafe.Pointer(&timeouts))); result == 0 {
		return err
	}
	_, _, _ = procPurgeComm.Call(uintptr(handle), 0x0004|0x0008|0x0001|0x0002) // abort and clear both directions
	return nil
}

// applySettings writes every field that defines the line into state. A port
// keeps the settings of whoever opened it last, so a field left as GetCommState
// returned it lets another program's parity or stop bits decide the frame this
// one runs. Windows takes the wire format from these fields alone; fParity only
// turns error reporting on, so selecting no parity must write NOPARITY itself.
func applySettings(state *dcb, settings Settings) {
	state.BaudRate = uint32(settings.Baud)
	state.ByteSize = uint8(settings.DataBits)
	// NOPARITY, ODDPARITY, EVENPARITY, MARKPARITY and SPACEPARITY are 0 to 4, the
	// order of the Parity named here.
	state.Parity = uint8(settings.Parity)
	state.StopBits = 0 // ONESTOPBIT
	if settings.StopBits == 2 {
		state.StopBits = 2 // TWOSTOPBITS
	}
	state.Flags = flagBinary | flagDTRControl
	if settings.Parity != ParityNone {
		state.Flags |= flagParity
	}
	switch settings.Flow {
	case FlowHardware:
		state.Flags |= flagOutxCTS | 2*flagRTSControl
	case FlowSoftware:
		state.Flags |= flagOutX | flagInX | flagRTSControl
		state.XonChar, state.XoffChar = 0x11, 0x13
		state.XonLim, state.XoffLim = 2048, 512
	default:
		state.Flags |= flagRTSControl
	}
}

func (p *port) enter() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.users.Add(1)
	return true
}

// transfer runs one overlapped read or write and waits for it.
func (p *port) transfer(data []byte, write bool) (int, error) {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(event)
	overlapped := windows.Overlapped{HEvent: event}
	var count uint32
	if write {
		err = windows.WriteFile(p.handle, data, &count, &overlapped)
	} else {
		err = windows.ReadFile(p.handle, data, &count, &overlapped)
	}
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		err = windows.GetOverlappedResult(p.handle, &overlapped, &count, true)
	}
	if err != nil {
		if errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
			return int(count), io.EOF
		}
		return int(count), err
	}
	return int(count), nil
}

func (p *port) Read(buffer []byte) (int, error) {
	if !p.enter() {
		return 0, io.EOF
	}
	defer p.users.Done()
	for {
		n, err := p.transfer(buffer, false)
		if n > 0 || err != nil {
			return n, err
		}
		// An interval timeout with nothing read: wait again.
	}
}

func (p *port) Write(data []byte) (int, error) {
	if !p.enter() {
		return 0, io.ErrClosedPipe
	}
	defer p.users.Done()
	written := 0
	for written < len(data) {
		n, err := p.transfer(data[written:], true)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (p *port) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	_ = windows.CancelIoEx(p.handle, nil)
	p.users.Wait()
	return windows.CloseHandle(p.handle)
}

// Ports lists the COM ports the system has registered, in numeric order.
func Ports() []Port {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	names, err := key.ReadValueNames(0)
	if err != nil {
		return nil
	}
	var ports []Port
	for _, name := range names {
		if device, _, err := key.GetStringValue(name); err == nil && strings.HasPrefix(strings.ToUpper(device), "COM") {
			ports = append(ports, Port{Device: device, Description: description(name)})
		}
	}
	sort.Slice(ports, func(i, j int) bool { return comNumber(ports[i].Device) < comNumber(ports[j].Device) })
	return ports
}

// description names a port by its driver: \Device\Serial0 is a built-in port,
// \Device\USBSER000 a USB one.
func description(value string) string {
	switch {
	case strings.Contains(value, "USB"):
		return "USB"
	case strings.Contains(value, "Bth"):
		return "Bluetooth"
	}
	return ""
}

func comNumber(device string) int {
	number := 0
	for _, r := range device[min(3, len(device)):] {
		if r < '0' || r > '9' {
			break
		}
		number = number*10 + int(r-'0')
	}
	return number
}
