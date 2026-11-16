package x11fwd

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Address families of an Xauthority entry.
const (
	FamilyInternet  = 0
	FamilyInternet6 = 6
	FamilyLocal     = 256
	FamilyWild      = 65535
)

// maxXauthoritySize bounds the file read: a real one is a few hundred bytes.
const maxXauthoritySize = 1 << 20

// cookieProtocol is the only authorization scheme forwarded.
const cookieProtocol = "MIT-MAGIC-COOKIE-1"

// Entry is one record of an Xauthority file.
type Entry struct {
	Family  uint16
	Address []byte
	Number  string
	Name    string
	Data    []byte
}

// Auth is the authorization a connection to the real X server presents.
type Auth struct {
	Name string
	Data []byte
}

// ParseXauthority reads the file format (all integers big-endian: family,
// then length-prefixed address, display number, name and data). It returns the
// entries before the first truncated or malformed one together with the error.
func ParseXauthority(data []byte) ([]Entry, error) {
	var entries []Entry
	for len(data) > 0 {
		var entry Entry
		if len(data) < 2 {
			return entries, errors.New("xauthority: truncated entry")
		}
		entry.Family = binary.BigEndian.Uint16(data)
		data = data[2:]
		var fields [4][]byte
		for index := range fields {
			if len(data) < 2 {
				return entries, errors.New("xauthority: truncated entry")
			}
			length := int(binary.BigEndian.Uint16(data))
			data = data[2:]
			if len(data) < length {
				return entries, errors.New("xauthority: truncated entry")
			}
			fields[index], data = data[:length], data[length:]
		}
		entry.Address = append([]byte(nil), fields[0]...)
		entry.Number = string(fields[1])
		entry.Name = string(fields[2])
		entry.Data = append([]byte(nil), fields[3]...)
		entries = append(entries, entry)
	}
	return entries, nil
}

// Lookup picks the MIT-MAGIC-COOKIE-1 entry for display, as libXau would: the
// entry must name this machine (or any, FamilyWild) and this display number
// (or any, an empty number); a more specific entry beats a wildcard one and
// the first of equals wins. hostname is this machine's name.
func Lookup(entries []Entry, display Display, hostname string) (Auth, bool) {
	best, bestScore := -1, -1
	for index, entry := range entries {
		if entry.Name != cookieProtocol {
			continue
		}
		score := 0
		if entry.Number != "" {
			if entry.Number != strconv.Itoa(display.Number) {
				continue
			}
			score += 2
		}
		if entry.Family != FamilyWild {
			if !familyMatches(entry, display, hostname) {
				continue
			}
			score++
		}
		if score > bestScore {
			best, bestScore = index, score
		}
	}
	if best < 0 {
		return Auth{}, false
	}
	return Auth{Name: entries[best].Name, Data: entries[best].Data}, true
}

func familyMatches(entry Entry, display Display, hostname string) bool {
	local := display.Host == "" || isLoopbackName(display.Host)
	switch entry.Family {
	case FamilyLocal:
		address := string(entry.Address)
		if local {
			return sameHostname(address, hostname)
		}
		return strings.EqualFold(address, display.Host)
	case FamilyInternet, FamilyInternet6:
		ip := net.IP(entry.Address)
		if len(ip) != 4 && len(ip) != 16 {
			return false
		}
		if local && ip.IsLoopback() {
			return true
		}
		if literal := net.ParseIP(display.Host); literal != nil {
			return literal.Equal(ip)
		}
	}
	return false
}

func isLoopbackName(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sameHostname compares a machine name as an Xauthority entry spells it with
// os.Hostname's, tolerating case and a differing domain suffix.
func sameHostname(a, b string) bool {
	if strings.EqualFold(a, b) {
		return true
	}
	shortA, _, _ := strings.Cut(a, ".")
	shortB, _, _ := strings.Cut(b, ".")
	return shortA != "" && strings.EqualFold(shortA, shortB)
}

// Environ is what display and cookie discovery read, so tests need no process
// environment.
type Environ struct {
	Display    string
	Xauthority string
	Home       string
	Hostname   string
	GOOS       string
	ReadFile   func(string) ([]byte, error)
}

// Local describes the display forwarded to and the real authorization for it.
type Local struct {
	Display Display
	Auth    Auth
}

// Resolve finds the local display and its real cookie. A missing Xauthority or
// no matching entry is not an error: the server may need no cookie (VcXsrv with
// access control off), and if it does it will refuse the connection itself.
func Resolve(env Environ) (Local, error) {
	display, err := ParseDisplay(env.Display, env.GOOS)
	if err != nil {
		return Local{}, err
	}
	local := Local{Display: display}
	path := env.Xauthority
	if path == "" && env.Home != "" {
		path = filepath.Join(env.Home, ".Xauthority")
	}
	if path == "" {
		return local, nil
	}
	read := env.ReadFile
	if read == nil {
		read = readLimited
	}
	data, err := read(path)
	if err != nil {
		return local, nil
	}
	entries, _ := ParseXauthority(data) // entries before damage still count
	if auth, ok := Lookup(entries, display, env.Hostname); ok {
		local.Auth = auth
	}
	return local, nil
}

func readLimited(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, maxXauthoritySize))
}

// CurrentEnviron reads this process's environment. Without DISPLAY it looks
// for the X server the platform usually runs; see DefaultDisplay.
func CurrentEnviron() Environ {
	home, _ := os.UserHomeDir()
	hostname, _ := os.Hostname()
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = DefaultDisplay(goos, filepath.Glob, fileExists)
	}
	return Environ{
		Display: display, Xauthority: os.Getenv("XAUTHORITY"),
		Home: home, Hostname: hostname, GOOS: goos,
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
