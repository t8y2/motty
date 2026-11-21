// Package sessionimport reads the saved sessions of other clients, PuTTY,
// Xshell, MobaXterm, WindTerm, Electerm, Netcatty, SecureCRT, FinalShell,
// Xterminal and XPipe, into the host fields Motty keeps. It reads files or
// folders the user chose and never executes, resolves or contacts anything in
// them.
//
// Only connection fields, a session's command after login and readable SSH
// login password candidates from Electerm and Netcatty are taken. The caller
// decides whether to save those candidates in a vault. Other clients' encrypted
// credentials and key files are not imported; SecureCRT, FinalShell,
// Xterminal and XPipe store every saved password encrypted, so their imports
// never carry one.
package sessionimport

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Limits keep a damaged or hostile file from using the machine: a session file
// is a few hundred bytes, and a whole export a few megabytes.
const (
	MaxFileSize    = 16 << 20
	MaxEntries     = 5000
	maxArchiveSize = 64 << 20
	maxNameLength  = 200
	maxGroupLength = 512
	// maxGroupDepth is how many folders of a nested group tree are followed; real
	// bookmark trees are a few levels deep, so only a damaged or hostile file
	// reaches it.
	maxGroupDepth = 32
)

var ErrAmbiguousMobaEncoding = errors.New("MobaXterm text is valid as both UTF-8 and GB18030; choose an encoding")

// Protocol is how a session is reached.
type Protocol uint8

const (
	SSH Protocol = iota
	Telnet
	Serial
	VNC
	RDP
)

// Entry is one session. A serial one keeps its device in Host and its line in
// Line ("9600 8N1") with Flow "rtscts", "xonxoff" or empty.
type Entry struct {
	Protocol Protocol
	Line     string
	Flow     string

	Name     string
	Group    string // folders joined with "/", empty at the top
	Host     string
	Port     int // zero when the file named none
	User     string
	Password string // ephemeral candidate; never serialized with a host
	// Command is typed into a new shell once it is ready, when the source
	// client saved one.
	Command string
	// Source names where the entry came from, for warnings.
	Source string
}

// Result is what a file held.
type Result struct {
	Entries []Entry
	// Skipped counts sessions of a kind that is not read (remote desktop, FTP,
	// and the serial ports of clients whose serial settings are not read).
	Skipped int
	// Keys counts sessions that name a private key file, which was not imported.
	Keys int
	// PlainSecrets counts imported sessions whose file carried a password, key
	// passphrase or private key in readable text, so the user can be told to
	// protect the export even if password saving was declined.
	PlainSecrets int
	Warnings     []string
}

func (result *Result) warn(format string, args ...any) {
	if len(result.Warnings) < 64 {
		result.Warnings = append(result.Warnings, fmt.Sprintf(format, args...))
	}
}

func (result *Result) add(entry Entry) {
	if len(result.Entries) >= MaxEntries {
		result.warn("%s: more than %d sessions; the rest were not read", entry.Source, MaxEntries)
		return
	}
	entry.Name = clean(entry.Name, maxNameLength)
	entry.Group = clean(entry.Group, maxGroupLength)
	entry.Host = clean(entry.Host, 255)
	entry.User = clean(entry.User, 128)
	if entry.Name == "" {
		entry.Name = entry.Host
	}
	if entry.Protocol == Serial {
		if entry.Host == "" || strings.HasPrefix(entry.Host, "-") {
			result.warn("%s: no serial device", entry.Source)
			return
		}
		entry.User, entry.Port = "", 0
		result.Entries = append(result.Entries, entry)
		return
	}
	if entry.Host == "" || strings.ContainsAny(entry.Host, " \t/\\") {
		result.warn("%s: no usable host address", entry.Source)
		return
	}
	if entry.Port < 0 || entry.Port > 65535 {
		entry.Port = 0
	}
	result.Entries = append(result.Entries, entry)
}

func (result *Result) merge(other Result) {
	for _, entry := range other.Entries {
		result.add(entry)
	}
	result.Skipped += other.Skipped
	result.Keys += other.Keys
	result.PlainSecrets += other.PlainSecrets
	for _, warning := range other.Warnings {
		result.warn("%s", warning)
	}
}

// clean makes one printable line of a name: control characters go, the ends are
// trimmed, and a name too long to show is cut at a character.
func clean(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = strings.ToValidUTF8(value[:limit], "")
	}
	return value
}

// readFile reads at most MaxFileSize bytes and decodes them to UTF-8.
func readFile(path string) (string, error) {
	data, err := readFileBytes(path)
	if err != nil {
		return "", err
	}
	return decode(data), nil
}

func readFileBytes(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("%s is too large to be a session file", filepath.Base(path))
	}
	return data, nil
}

func readMobaFile(path string, encoding MobaEncoding) (string, error) {
	data, err := readFileBytes(path)
	if err != nil {
		return "", err
	}
	problem := fmt.Errorf("%s has an unsupported or damaged text encoding", filepath.Base(path))
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		bigEndian := data[0] == 0xfe
		if len(data[2:])%2 != 0 {
			return "", problem
		}
		units := make([]uint16, 0, len(data[2:])/2)
		for index := 2; index < len(data); index += 2 {
			unit := uint16(data[index+1])<<8 | uint16(data[index])
			if bigEndian {
				unit = uint16(data[index])<<8 | uint16(data[index+1])
			}
			if unit >= 0xd800 && unit <= 0xdbff {
				if index+3 >= len(data) {
					return "", problem
				}
				next := uint16(data[index+3])<<8 | uint16(data[index+2])
				if bigEndian {
					next = uint16(data[index+2])<<8 | uint16(data[index+3])
				}
				if next < 0xdc00 || next > 0xdfff {
					return "", problem
				}
			} else if unit >= 0xdc00 && unit <= 0xdfff {
				if len(units) == 0 || units[len(units)-1] < 0xd800 || units[len(units)-1] > 0xdbff {
					return "", problem
				}
			}
			units = append(units, unit)
		}
		return string(utf16.Decode(units)), nil
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		if !utf8.Valid(data[3:]) {
			return "", problem
		}
		return string(data[3:]), nil
	}
	if encoding == MobaUTF8 || encoding == MobaAuto && utf8.Valid(data) {
		if !utf8.Valid(data) {
			return "", problem
		}
		if encoding == MobaAuto {
			if other, ok := decodeGB18030Verified(data); ok && other != string(data) {
				return "", ErrAmbiguousMobaEncoding
			}
		}
		return string(data), nil
	}
	if decoded, ok := decodeGB18030Verified(data); ok {
		return decoded, nil
	}
	return "", problem
}

func decodeGB18030Verified(data []byte) (string, bool) {
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	if err == nil {
		var encoded []byte
		encoded, err = simplifiedchinese.GB18030.NewEncoder().Bytes(decoded)
		if err == nil && bytes.Equal(data, encoded) {
			return string(decoded), true
		}
	}
	return "", false
}

// decode turns a file's bytes into text: UTF-16 with a byte-order mark (what
// Windows tools write), or UTF-8 with or without one. Bytes that are neither
// get one more chance — the round-trip-verified GB18030 the MobaXterm reader
// uses, since ANSI exports from a Chinese Windows (Xshell's among them) keep
// their text in that encoding — and only what survives none of those is read
// as UTF-8 with its bad bytes dropped (issue #332).
func decode(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		return decodeUTF16(data[2:], false)
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		return decodeUTF16(data[2:], true)
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		data = data[3:]
	}
	if utf8.Valid(data) {
		return string(data)
	}
	if decoded, ok := decodeGB18030Verified(data); ok {
		return decoded
	}
	return strings.ToValidUTF8(string(data), "")
}

func decodeUTF16(data []byte, bigEndian bool) string {
	units := make([]uint16, 0, len(data)/2)
	for index := 0; index+1 < len(data); index += 2 {
		if bigEndian {
			units = append(units, uint16(data[index])<<8|uint16(data[index+1]))
		} else {
			units = append(units, uint16(data[index+1])<<8|uint16(data[index]))
		}
	}
	return string(utf16.Decode(units))
}

// ini is a parsed INI file: sections in file order, each with its keys in
// lower case.
type ini struct {
	sections []iniSection
}

type iniSection struct {
	name   string
	values map[string]string
	order  []string // keys as they appeared, lower-cased, for formats whose keys are names
	raw    map[string]string
}

func parseINI(text string) ini {
	var file ini
	current := -1
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			end := strings.IndexByte(line, ']')
			if end < 0 {
				continue
			}
			file.sections = append(file.sections, iniSection{
				name: line[1:end], values: map[string]string{}, raw: map[string]string{},
			})
			current = len(file.sections) - 1
			continue
		}
		if current < 0 {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		section := &file.sections[current]
		if _, seen := section.raw[key]; !seen {
			section.order = append(section.order, key)
		}
		section.raw[key] = value
		section.values[strings.ToLower(key)] = strings.TrimSpace(value)
	}
	return file
}

func (file ini) section(name string) (iniSection, bool) {
	for _, section := range file.sections {
		if strings.EqualFold(section.name, name) {
			return section, true
		}
	}
	return iniSection{}, false
}

// Parse reads each path with the reader for client, and joins the results.
func Parse(client Client, paths []string) (Result, error) {
	return ParseWithOptions(client, paths, ParseOptions{})
}

type MobaEncoding uint8

const (
	MobaAuto MobaEncoding = iota
	MobaUTF8
	MobaGB18030
)

type ParseOptions struct {
	MobaEncoding MobaEncoding
}

// ParseWithOptions resolves BOM-less MobaXterm files whose bytes are valid in
// both UTF-8 and GB18030. The default keeps valid UTF-8 files as UTF-8.
func ParseWithOptions(client Client, paths []string, options ParseOptions) (Result, error) {
	var result Result
	if len(paths) == 0 {
		return result, errors.New("no file was chosen")
	}
	var failed []string
	for _, path := range paths {
		var (
			part Result
			err  error
		)
		switch client {
		case PuTTY:
			part, err = ParsePuTTY(path)
		case Xshell:
			part, err = ParseXshell(path)
		case MobaXterm:
			part, err = ParseMobaXtermWithEncoding(path, options.MobaEncoding)
		case WindTerm:
			part, err = ParseWindTerm(path)
		case Electerm:
			part, err = ParseElecterm(path)
		case Netcatty:
			part, err = ParseNetcatty(path)
		case SecureCRT:
			part, err = ParseSecureCRT(path)
		case FinalShell:
			part, err = ParseFinalShell(path)
		case Xterminal:
			part, err = ParseXterminal(path)
		case XPipe:
			part, err = ParseXPipe(path)
		default:
			err = fmt.Errorf("unknown client %d", client)
		}
		if err != nil {
			if errors.Is(err, ErrAmbiguousMobaEncoding) {
				return Result{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
			}
			failed = append(failed, fmt.Sprintf("%s: %v", filepath.Base(path), err))
			continue
		}
		result.merge(part)
	}
	if len(result.Entries) == 0 && len(failed) > 0 {
		return result, errors.New(strings.Join(failed, "; "))
	}
	for _, failure := range failed {
		result.warn("%s", failure)
	}
	return result, nil
}

// Client says whose files a path holds.
type Client uint8

const (
	PuTTY Client = iota + 1
	Xshell
	MobaXterm
	WindTerm
	Electerm
	Netcatty
	SecureCRT
	FinalShell
	Xterminal
	XPipe
)

// ---- PuTTY ----

// ParsePuTTY reads a PuTTY session: a file of a Unix PuTTY's sessions folder
// (HostName=... lines), or a Windows registry export (.reg) holding any number.
func ParsePuTTY(path string) (Result, error) {
	text, err := readFile(path)
	if err != nil {
		return Result{}, err
	}
	if strings.HasPrefix(strings.TrimSpace(text), "Windows Registry Editor") {
		return parsePuTTYRegistry(text, filepath.Base(path)), nil
	}
	var result Result
	name, err := url.PathUnescape(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if err != nil {
		name = filepath.Base(path)
	}
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(strings.TrimRight(line, "\r"), "=")
		if ok {
			values[key] = value
		}
	}
	if values["HostName"] == "" {
		return result, errors.New("not a PuTTY session: it has no HostName")
	}
	addPuTTYSession(&result, name, values, filepath.Base(path))
	return result, nil
}

func addPuTTYSession(result *Result, name string, values map[string]string, source string) {
	protocol := strings.ToLower(values["Protocol"])
	switch protocol {
	case "serial":
		addPuTTYSerial(result, name, values, source)
		return
	case "telnet":
		port, _ := strconv.Atoi(values["PortNumber"])
		result.add(Entry{Name: name, Protocol: Telnet, Host: values["HostName"], Port: port, Source: source})
		return
	case "", "ssh":
	default:
		result.Skipped++
		return
	}
	port, _ := strconv.Atoi(values["PortNumber"])
	host, user := values["HostName"], values["UserName"]
	if at := strings.LastIndexByte(host, '@'); at >= 0 && user == "" {
		host, user = host[at+1:], host[:at]
	}
	if values["PublicKeyFile"] != "" {
		result.Keys++
	}
	result.add(Entry{Name: name, Host: host, Port: port, User: user, Source: source})
}

// addPuTTYSerial reads a serial session: SerialLine is the port, SerialSpeed the
// baud rate, SerialParity 0 to 4 for none, odd, even, mark and space,
// SerialStopHalfbits 2 or 4 for one or two stop bits and SerialFlowControl 0 to 3
// for none, XON/XOFF, RTS/CTS and DSR/DTR (which is not available and is read as
// none).
func addPuTTYSerial(result *Result, name string, values map[string]string, source string) {
	speed, _ := strconv.Atoi(values["SerialSpeed"])
	if speed == 0 {
		speed = 9600 // PuTTY's own default
	}
	bits, _ := strconv.Atoi(values["SerialDataBits"])
	if bits == 0 {
		bits = 8
	}
	parity := "NOEMS"
	letter := byte('N')
	if index, err := strconv.Atoi(values["SerialParity"]); err == nil && index >= 0 && index < len(parity) {
		letter = parity[index]
	}
	stop := 1
	if values["SerialStopHalfbits"] == "4" {
		stop = 2
	}
	flow := ""
	switch values["SerialFlowControl"] {
	case "1":
		flow = "xonxoff"
	case "2":
		flow = "rtscts"
	}
	device := values["SerialLine"]
	if device == "" {
		device = "COM1"
	}
	result.add(Entry{Name: name, Protocol: Serial, Host: device, Line: fmt.Sprintf("%d %d%c%d", speed, bits, letter, stop), Flow: flow, Source: source})
}

// parsePuTTYRegistry reads the Sessions keys of an exported
// HKEY_CURRENT_USER\Software\SimonTatham\PuTTY.
func parsePuTTYRegistry(text, source string) Result {
	var (
		result Result
		name   string
		values map[string]string
		inKey  bool
		flush  = func() {
			if inKey && values != nil {
				addPuTTYSession(&result, name, values, source)
			}
			inKey, values = false, nil
		}
		marker = `\software\simontatham\putty\sessions\`
	)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			key := line[1 : len(line)-1]
			if at := strings.Index(strings.ToLower(key), marker); at >= 0 {
				raw := key[at+len(marker):]
				if raw == "" || strings.Contains(raw, `\`) {
					continue
				}
				unescaped, err := url.PathUnescape(raw)
				if err != nil {
					unescaped = raw
				}
				name, values, inKey = unescaped, map[string]string{}, true
			}
			continue
		}
		if !inKey || !strings.HasPrefix(line, `"`) {
			continue
		}
		key, value, ok := registryValue(line)
		if ok {
			values[key] = value
		}
	}
	flush()
	return result
}

// registryValue reads one "Name"="text" or "Name"=dword:0000001a line.
func registryValue(line string) (key, value string, ok bool) {
	end := strings.Index(line[1:], `"`)
	if end < 0 {
		return "", "", false
	}
	key = line[1 : 1+end]
	rest := strings.TrimPrefix(line[end+2:], "=")
	switch {
	case strings.HasPrefix(rest, "dword:"):
		number, err := strconv.ParseUint(strings.TrimPrefix(rest, "dword:"), 16, 32)
		if err != nil {
			return "", "", false
		}
		return key, strconv.FormatUint(number, 10), true
	case strings.HasPrefix(rest, `"`) && strings.HasSuffix(rest, `"`) && len(rest) >= 2:
		value = rest[1 : len(rest)-1]
		value = strings.ReplaceAll(value, `\\`, "\x00")
		value = strings.ReplaceAll(value, `\"`, `"`)
		return key, strings.ReplaceAll(value, "\x00", `\`), true
	}
	return "", "", false
}

// ---- Xshell ----

// ParseXshell reads an Xshell session file (.xsh), or an exported archive of
// them (.xts), whose folders become groups.
func ParseXshell(path string) (Result, error) {
	if strings.EqualFold(filepath.Ext(path), ".xts") {
		return parseXshellArchive(path)
	}
	text, err := readFile(path)
	if err != nil {
		return Result{}, err
	}
	var result Result
	if err := parseXshellSession(&result, text, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), "", filepath.Base(path)); err != nil {
		return result, err
	}
	return result, nil
}

func parseXshellSession(result *Result, text, name, group, source string) error {
	file := parseINI(text)
	connection, ok := file.section("CONNECTION")
	if !ok || connection.values["host"] == "" && connection.values["protocol"] == "" {
		return errors.New("not an Xshell session: it has no [CONNECTION] section")
	}
	telnet := false
	switch protocol := strings.ToUpper(connection.values["protocol"]); protocol {
	case "", "SSH", "SSH2":
		// SSH2 is the name some clients write for the same protocol.
	case "TELNET":
		telnet = true
	default:
		result.Skipped++
		return nil
	}
	port, _ := strconv.Atoi(connection.values["port"])
	if telnet {
		result.add(Entry{Name: name, Group: group, Protocol: Telnet, Host: connection.values["host"], Port: port, Source: source})
		return nil
	}
	user := ""
	if auth, ok := file.section("CONNECTION:AUTHENTICATION"); ok {
		user = auth.values["username"]
		// Bastion clients often put a placeholder such as fake_rsa in UserKey;
		// that is not a private key file to import or to fail a login for.
		if key := auth.values["userkey"]; key != "" && !IsPlaceholderKey(key) {
			result.Keys++
		}
	}
	result.add(Entry{Name: name, Group: group, Host: connection.values["host"], Port: port, User: user, Source: source})
	return nil
}

// IsLoopbackHost reports whether host is this computer: 127.0.0.1, ::1 or
// localhost. Bastion clients write those into one-shot tunnel session files.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

// IsPlaceholderKey reports a UserKey name that is not a real private key file.
// Bastion-generated .xsh files often use fake_rsa (and similar) as a stand-in.
func IsPlaceholderKey(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	lower := strings.ToLower(name)
	return lower == "fake_rsa" || strings.HasPrefix(lower, "fake_")
}

func parseXshellArchive(path string) (Result, error) {
	var result Result
	archive, err := zip.OpenReader(path)
	if err != nil {
		return result, fmt.Errorf("not an Xshell export: %v", err)
	}
	defer archive.Close()
	var total uint64
	for _, member := range archive.File {
		if member.FileInfo().IsDir() || !strings.EqualFold(filepath.Ext(member.Name), ".xsh") {
			continue
		}
		name := member.Name
		if !utf8.ValidString(name) {
			decoded, ok := decodeGB18030Verified([]byte(name))
			if !ok {
				result.warn("%s: unsupported member name encoding", filepath.Base(path))
				continue
			}
			name = decoded
		}
		total += member.UncompressedSize64
		if member.UncompressedSize64 > MaxFileSize || total > maxArchiveSize {
			result.warn("%s: too large; the rest of the archive was not read", filepath.Base(path))
			break
		}
		file, err := member.Open()
		if err != nil {
			result.warn("%s: %v", member.Name, err)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
		file.Close()
		if err != nil || len(data) > MaxFileSize {
			result.warn("%s: unreadable", member.Name)
			continue
		}
		slashed := strings.ReplaceAll(name, `\`, "/")
		group, base := "", slashed
		if at := strings.LastIndexByte(slashed, '/'); at >= 0 {
			group, base = strings.Trim(slashed[:at], "/"), slashed[at+1:]
		}
		if err := parseXshellSession(&result, decode(data), strings.TrimSuffix(base, filepath.Ext(base)), group, name); err != nil {
			result.warn("%s: %v", name, err)
		}
	}
	return result, nil
}

// ---- MobaXterm ----

// ParseMobaXterm reads an exported MobaXterm sessions file (.mxtsessions), or the
// [Bookmarks] sections of MobaXterm.ini. A session is a line
// Name=#icon#type%host%port%user%...; type 0 is SSH. Folders are the SubRep of
// each [Bookmarks] section.
func ParseMobaXterm(path string) (Result, error) {
	return ParseMobaXtermWithEncoding(path, MobaAuto)
}

func ParseMobaXtermWithEncoding(path string, encoding MobaEncoding) (Result, error) {
	text, err := readMobaFile(path, encoding)
	if err != nil {
		return Result{}, err
	}
	var result Result
	file := parseINI(text)
	found := false
	for _, section := range file.sections {
		if !strings.HasPrefix(strings.ToLower(section.name), "bookmarks") {
			continue
		}
		found = true
		group := strings.Trim(strings.ReplaceAll(section.values["subrep"], `\`, "/"), "/")
		for _, key := range section.order {
			if strings.EqualFold(key, "SubRep") || strings.EqualFold(key, "ImgNum") {
				continue
			}
			parseMobaSession(&result, key, strings.TrimSpace(section.raw[key]), group, filepath.Base(path))
		}
	}
	if !found {
		return result, errors.New("not a MobaXterm sessions file: it has no [Bookmarks] section")
	}
	return result, nil
}

func parseMobaSession(result *Result, name, value, group, source string) {
	if !strings.HasPrefix(value, "#") {
		return
	}
	rest := value[1:]
	hash := strings.IndexByte(rest, '#')
	if hash < 0 {
		return
	}
	fields := strings.Split(rest[hash+1:], "%")
	if len(fields) < 4 {
		return
	}
	port, _ := strconv.Atoi(fields[2])
	switch fields[0] {
	case "0":
		result.add(Entry{Name: name, Group: group, Host: fields[1], Port: port, User: mobaUsername(fields[3]), Source: source})
	case "1":
		result.add(Entry{Name: name, Group: group, Protocol: Telnet, Host: fields[1], Port: port, Source: source})
	default:
		result.Skipped++
	}
}

// mobaUsername returns the login of a MobaXterm session. MobaXterm shows a
// login taken from its saved credentials in square brackets, and the export
// stores that text verbatim ("[petalinux]"); the brackets are MobaXterm's own
// notation and are not part of the name the server receives.
func mobaUsername(field string) string {
	if len(field) >= 2 && field[0] == '[' && field[len(field)-1] == ']' {
		return field[1 : len(field)-1]
	}
	return field
}

// ---- WindTerm ----

// windTermSession is one entry of WindTerm's user.sessions, a JSON array it
// keeps under profiles/<profile>/terminal/. A saved login (session.autoLogin) is
// encrypted and is not read. session.autoExecution is the command WindTerm types
// after authentication; its delay has no Motty counterpart because the command
// waits for the shell's prompt instead.
type windTermSession struct {
	Label         string `json:"session.label"`
	Group         string `json:"session.group"`
	Target        string `json:"session.target"`
	Port          int    `json:"session.port"`
	Protocol      string `json:"session.protocol"`
	User          string `json:"session.user"`
	Identity      string `json:"session.identityFile"`
	AutoExecution string `json:"session.autoExecution"`
}

// ParseWindTerm reads a WindTerm user.sessions file.
func ParseWindTerm(path string) (Result, error) {
	text, err := readFile(path)
	if err != nil {
		return Result{}, err
	}
	var sessions []windTermSession
	if err := json.Unmarshal([]byte(text), &sessions); err != nil {
		return Result{}, errors.New("not a WindTerm sessions file: it is not a JSON list of sessions")
	}
	var result Result
	shortened := false
	source := filepath.Base(path)
	for _, session := range sessions {
		if session.Target == "" && session.Protocol == "" && session.Label == "" {
			continue
		}
		kind := SSH
		switch strings.ToUpper(session.Protocol) {
		case "", "SSH":
		case "TELNET":
			kind = Telnet
		default:
			result.Skipped++
			continue
		}
		if session.Identity != "" {
			result.Keys++
		}
		host, user := session.Target, session.User
		if at := strings.LastIndexByte(host, '@'); at >= 0 && user == "" {
			host, user = host[at+1:], host[:at]
		}
		if kind == Telnet {
			user = ""
		}
		group, cut := windTermGroup(session.Group)
		shortened = shortened || cut
		result.add(Entry{
			Name: session.Label, Group: group,
			Protocol: kind, Host: host, Port: session.Port, User: user,
			Command: session.AutoExecution, Source: source,
		})
	}
	if shortened {
		result.warn("%s: folders nested more than %d levels deep were shortened", source, maxGroupDepth)
	}
	return result, nil
}

// windTermGroup turns a WindTerm group into the "/" path Motty keeps. WindTerm
// separates nested folders with ">", so "A>B" is the folder B inside A. A
// backslash, which an older or hand-edited file may use, separates them too.
// Empty folders go, and a path deeper than maxGroupDepth keeps its innermost
// folders; it reports whether it cut.
func windTermGroup(group string) (string, bool) {
	group = strings.ReplaceAll(group, `\`, "/")
	group = strings.ReplaceAll(group, ">", "/")
	var folders []string
	for _, folder := range strings.Split(group, "/") {
		if folder = clean(folder, maxNameLength); folder != "" {
			folders = append(folders, folder)
		}
	}
	cut := len(folders) > maxGroupDepth
	if cut {
		folders = folders[len(folders)-maxGroupDepth:]
	}
	return strings.Join(folders, "/"), cut
}

// ---- Electerm ----

// electermExport is what Electerm writes when its bookmarks are exported: a flat
// list of bookmarks and a flat list of groups, each naming the other by id. A
// group names the groups inside it (BookmarkGroupIDs) and the bookmarks inside it
// (BookmarkIDs), so the tree is rebuilt from those ids; Level only says how deep
// a group is and is not trusted.
type electermExport struct {
	Bookmarks      []electermBookmark `json:"bookmarks"`
	BookmarkGroups []electermGroup    `json:"bookmarkGroups"`
}

type electermBookmark struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Host     string       `json:"host"`
	Port     electermPort `json:"port"`
	User     string       `json:"username"`
	Type     string       `json:"type"`
	AuthType string       `json:"authType"`
	// Key material is counted but not imported. A password is offered only for
	// SSH sessions whose authentication does not select a private key.
	Password   string `json:"password"`
	Passphrase string `json:"passphrase"`
	PrivateKey string `json:"privateKey"`
}

// electermPort is a port that Electerm writes as a number but that an edited or
// older export may hold as text. Anything that is not a port reads as 0, "the
// default", rather than failing the whole file.
type electermPort int

func (port *electermPort) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if value, err := strconv.Atoi(text); err == nil {
		*port = electermPort(value)
	} else {
		*port = 0
	}
	return nil
}

type electermGroup struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	BookmarkIDs      []string `json:"bookmarkIds"`
	BookmarkGroupIDs []string `json:"bookmarkGroupIds"`
}

// electermDefaultGroup is the id of the group Electerm creates for itself and
// puts every bookmark in that was never filed anywhere. Its title is the word
// "default" in the interface language, so it is told apart by id, and it is not a
// folder the user made.
const electermDefaultGroup = "default"

// ParseElecterm reads an Electerm bookmarks export (a .json file). Its groups
// nest, and a session is SSH unless Type says otherwise.
func ParseElecterm(path string) (Result, error) {
	text, err := readFile(path)
	if err != nil {
		return Result{}, err
	}
	var export electermExport
	if err := json.Unmarshal([]byte(text), &export); err != nil {
		return Result{}, fmt.Errorf("not an Electerm bookmarks export: %s", electermJSONProblem(err))
	}
	if export.Bookmarks == nil && export.BookmarkGroups == nil {
		return Result{}, errors.New("not an Electerm bookmarks export: it has neither bookmarks nor groups")
	}
	var result Result
	tree := newElectermTree(export)
	source := filepath.Base(path)
	shortened := false
	for _, bookmark := range export.Bookmarks {
		kind := SSH
		switch strings.ToLower(bookmark.Type) {
		case "", "ssh":
		case "telnet":
			kind = Telnet
		default:
			result.Skipped++
			continue
		}
		if len(result.Entries) >= MaxEntries {
			result.warn("%s: more than %d sessions; the rest were not read", source, MaxEntries)
			break
		}
		if strings.EqualFold(bookmark.AuthType, "privateKey") {
			result.Keys++
		}
		user := bookmark.User
		if kind == Telnet {
			user = ""
		}
		group := ""
		if owner, ok := tree.owner[bookmark.ID]; ok {
			var cut bool
			group, cut = tree.groupPath(owner)
			shortened = shortened || cut
		}
		before := len(result.Entries)
		password := ""
		if kind == SSH && bookmark.PrivateKey == "" && (bookmark.AuthType == "" || strings.EqualFold(bookmark.AuthType, "password")) {
			password = bookmark.Password
		}
		result.add(Entry{
			Name: bookmark.Title, Group: group, Protocol: kind,
			Host: bookmark.Host, Port: int(bookmark.Port), User: user, Password: password, Source: source,
		})
		if len(result.Entries) > before && (bookmark.Password != "" || bookmark.Passphrase != "" || bookmark.PrivateKey != "") {
			result.PlainSecrets++
		}
	}
	if shortened {
		result.warn("%s: folders nested more than %d levels deep were shortened", source, maxGroupDepth)
	}
	return result, nil
}

// electermJSONProblem says what is wrong with a file that is not an Electerm
// export, in words that name the JSON rather than a Go type.
func electermJSONProblem(err error) string {
	var wrong *json.UnmarshalTypeError
	if errors.As(err, &wrong) {
		if wrong.Field == "" {
			return "it is a JSON " + wrong.Value + ", not an object of bookmarks"
		}
		return fmt.Sprintf("%q holds a JSON %s where a %s was expected", wrong.Field, wrong.Value, wrong.Type.Kind())
	}
	return err.Error()
}

// electermTree answers the two questions the flat export cannot: which group a
// bookmark is in, and where that group sits.
type electermTree struct {
	groups map[string]electermGroup
	parent map[string]string // group id -> the id of the group it sits in
	owner  map[string]string // bookmark id -> the id of the group holding it
}

func newElectermTree(export electermExport) *electermTree {
	tree := &electermTree{
		groups: make(map[string]electermGroup, len(export.BookmarkGroups)),
		parent: map[string]string{},
		owner:  map[string]string{},
	}
	for _, group := range export.BookmarkGroups {
		if _, seen := tree.groups[group.ID]; !seen && group.ID != "" {
			tree.groups[group.ID] = group
		}
	}
	// The first group that names something owns it, so a bookmark or group listed
	// twice still lands in one folder.
	for _, group := range export.BookmarkGroups {
		for _, id := range group.BookmarkGroupIDs {
			if _, seen := tree.parent[id]; !seen && id != "" && id != group.ID {
				tree.parent[id] = group.ID
			}
		}
		for _, id := range group.BookmarkIDs {
			if _, seen := tree.owner[id]; !seen && id != "" {
				tree.owner[id] = group.ID
			}
		}
	}
	return tree
}

// groupPath is the folder a group sits in followed by its own title, joined with
// "/" the way the other imports join nested folders, and whether the path had to
// be cut at maxGroupDepth levels (the innermost ones are kept). A group that is
// its own ancestor, as a damaged file can make it, stops the climb where it
// would come round to itself. Electerm's own default group is not a folder.
func (tree *electermTree) groupPath(id string) (string, bool) {
	var ids []string
	cut := false
	for current := id; ; {
		if _, known := tree.groups[current]; !known || slices.Contains(ids, current) {
			break
		}
		if len(ids) == maxGroupDepth {
			cut = true
			break
		}
		ids = append(ids, current)
		above, nested := tree.parent[current]
		if !nested {
			break
		}
		current = above
	}
	titles := make([]string, 0, len(ids))
	for _, groupID := range slices.Backward(ids) {
		if groupID == electermDefaultGroup {
			continue
		}
		if title := clean(tree.groups[groupID].Title, maxNameLength); title != "" {
			titles = append(titles, title)
		}
	}
	return strings.Join(titles, "/"), cut
}

// ---- Netcatty ----

// ParseNetcatty reads the hosts file Netcatty exports from its vault: a CSV whose
// first row names the columns (Groups, Label, Hostname/IP, Protocol, Port,
// Username, ...). Netcatty keeps its vault encrypted with the system secret
// store, so this export is the only form another program can read. A group is
// already a "/" path. Password and Passphrase are written into the file in the
// clear; SSH login passwords without a key are offered to the caller, while
// passphrases and key material are not. ProxyJump, Proxy, tags and notes are
// not read.
func ParseNetcatty(path string) (Result, error) {
	text, err := readFile(path)
	if err != nil {
		return Result{}, err
	}
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return Result{}, errors.New("not a Netcatty hosts export: the file is empty")
		}
		return Result{}, fmt.Errorf("not a Netcatty hosts export: %v", err)
	}
	column := func(names ...string) int {
		for index, cell := range header {
			cell = strings.ToLower(strings.TrimSpace(cell))
			if slices.Contains(names, cell) {
				return index
			}
		}
		return -1
	}
	columns := struct{ group, label, host, protocol, port, user, password, key, passphrase int }{
		group: column("groups", "group"), label: column("label", "name"),
		host: column("hostname/ip", "hostname", "host"), protocol: column("protocol"),
		port: column("port"), user: column("username", "user"), password: column("password"),
		key: column("keypath"), passphrase: column("passphrase"),
	}
	if columns.host < 0 {
		return Result{}, errors.New("not a Netcatty hosts export: it has no Hostname/IP column")
	}
	cell := func(row []string, index int) string {
		if index < 0 || index >= len(row) {
			return ""
		}
		return row[index]
	}

	var result Result
	source := filepath.Base(path)
	shortened := false
	for line := 2; ; line++ {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			result.warn("%s: line %d: %v", source, line, err)
			continue
		}
		host := strings.TrimSpace(cell(row, columns.host))
		if host == "" {
			continue
		}
		kind := SSH
		switch strings.ToLower(strings.TrimSpace(cell(row, columns.protocol))) {
		case "", "ssh", "ssh2", "ssh-2":
		case "telnet":
			kind = Telnet
		default:
			result.Skipped++
			continue
		}
		if len(result.Entries) >= MaxEntries {
			result.warn("%s: more than %d sessions; the rest were not read", source, MaxEntries)
			break
		}
		port, _ := strconv.Atoi(strings.TrimSpace(cell(row, columns.port)))
		user := netcattyText(cell(row, columns.user))
		if kind == Telnet {
			user = ""
		}
		group, cut := netcattyGroup(netcattyText(cell(row, columns.group)))
		shortened = shortened || cut
		before := len(result.Entries)
		password := ""
		if kind == SSH && strings.TrimSpace(cell(row, columns.key)) == "" {
			password = netcattyText(cell(row, columns.password))
		}
		result.add(Entry{
			Name: netcattyText(cell(row, columns.label)), Group: group, Protocol: kind,
			Host: strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"), Port: port, User: user, Password: password,
			Source: fmt.Sprintf("%s: line %d", source, line),
		})
		if len(result.Entries) == before {
			continue
		}
		if kind == SSH && strings.TrimSpace(cell(row, columns.key)) != "" {
			result.Keys++
		}
		if cell(row, columns.password) != "" || cell(row, columns.passphrase) != "" {
			result.PlainSecrets++
		}
	}
	if shortened {
		result.warn("%s: folders nested more than %d levels deep were shortened", source, maxGroupDepth)
	}
	return result, nil
}

// netcattyText undoes the guard Netcatty puts before a text cell that a
// spreadsheet would read as a formula: a leading "'" before =, +, -, @, tab or
// carriage return.
func netcattyText(value string) string {
	if len(value) > 1 && value[0] == '\'' && strings.ContainsRune("=+-@\t\r", rune(value[1])) {
		return value[1:]
	}
	return value
}

// netcattyGroup tidies a group path: empty folders go, and a path deeper than
// maxGroupDepth keeps its innermost folders. It reports whether it cut.
func netcattyGroup(path string) (string, bool) {
	var folders []string
	for _, folder := range strings.Split(path, "/") {
		if folder = clean(folder, maxNameLength); folder != "" {
			folders = append(folders, folder)
		}
	}
	cut := len(folders) > maxGroupDepth
	if cut {
		folders = folders[len(folders)-maxGroupDepth:]
	}
	return strings.Join(folders, "/"), cut
}

// ---- SecureCRT ----

// ParseSecureCRT reads a SecureCRT Sessions folder — the tree of .ini files
// under Config/Sessions — or one session file from it. The folders inside
// become groups. SecureCRT stores every saved password encrypted, so a session
// brings its address, login name and port but never a password; the caller
// warns about that the way it does for any other client whose credentials
// cannot be read.
func ParseSecureCRT(path string) (Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	if info.IsDir() {
		return parseSecureCRTFolder(path)
	}
	text, err := readFile(path)
	if err != nil {
		return Result{}, err
	}
	var result Result
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if !parseSecureCRTSession(&result, text, name, "", filepath.Base(path)) {
		return result, errors.New("not a SecureCRT session: it has no Hostname or Protocol Name")
	}
	return result, nil
}

func parseSecureCRTFolder(root string) (Result, error) {
	var (
		result    Result
		sessions  int
		totalSize uint64
		shortened bool
	)
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			result.warn("%s: %v", filepath.Base(path), err)
			return nil
		}
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		base := entry.Name()
		lower := strings.ToLower(base)
		// A folder's own marker and the inherited defaults are not sessions.
		if !strings.HasSuffix(lower, ".ini") || lower == "__folderdata__.ini" || lower == "default.ini" {
			return nil
		}
		if len(result.Entries) >= MaxEntries {
			result.warn("%s: more than %d sessions; the rest were not read", filepath.Base(root), MaxEntries)
			return filepath.SkipAll
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			totalSize += uint64(info.Size())
			if totalSize > maxArchiveSize {
				result.warn("%s: too large; the rest of the folder was not read", filepath.Base(root))
				return filepath.SkipAll
			}
		}
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			relative = base
		}
		relative = filepath.ToSlash(relative)
		text, readErr := readFile(path)
		if readErr != nil {
			result.warn("%s: %v", relative, readErr)
			return nil
		}
		group, cut := secureCRTGroup(filepath.ToSlash(filepath.Dir(relative)), root)
		shortened = shortened || cut
		name := strings.TrimSuffix(base, filepath.Ext(base))
		if parseSecureCRTSession(&result, text, name, group, relative) {
			sessions++
		}
		return nil
	})
	if walkErr != nil {
		return result, walkErr
	}
	if sessions == 0 {
		return result, errors.New("not a SecureCRT Sessions folder: it holds no session files")
	}
	if shortened {
		result.warn("%s: folders nested more than %d levels deep were shortened", filepath.Base(root), maxGroupDepth)
	}
	return result, nil
}

// secureCRTGroup turns the folders between the chosen root and a session's file
// into the "/" path the other imports use. The root may be Config, so the
// Sessions folder at its top is not a group of the user's.
func secureCRTGroup(relativeDir, root string) (string, bool) {
	if relativeDir == "" || relativeDir == "." {
		return "", false
	}
	folders := strings.Split(relativeDir, "/")
	if !strings.EqualFold(filepath.Base(root), "Sessions") && strings.EqualFold(folders[0], "Sessions") {
		folders = folders[1:]
	}
	titles := make([]string, 0, len(folders))
	for _, folder := range folders {
		if folder = clean(folder, maxNameLength); folder != "" {
			titles = append(titles, folder)
		}
	}
	cut := len(titles) > maxGroupDepth
	if cut {
		titles = titles[len(titles)-maxGroupDepth:]
	}
	return strings.Join(titles, "/"), cut
}

// parseSecureCRTSession reads one session file. It reports whether the file
// described a session at all, so an unrelated .ini in the folder, or a session
// of a kind that is skipped, is not mistaken for an unreadable folder.
func parseSecureCRTSession(result *Result, text, name, group, source string) bool {
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if key, value, ok := secureCRTLine(line); ok {
			values[key] = value
		}
	}
	protocol := strings.ToUpper(strings.TrimSpace(values["protocol name"]))
	host := strings.TrimSpace(values["hostname"])
	if protocol == "" && host == "" {
		return false
	}
	kind := SSH
	switch protocol {
	case "", "SSH", "SSH1", "SSH2", "SSH-1", "SSH-2":
	case "TELNET":
		kind = Telnet
	default:
		result.Skipped++
		return true
	}
	port := 0
	for _, key := range secureCRTPortKeys(protocol) {
		if port = secureCRTPort(values[key]); port > 0 {
			break
		}
	}
	user := strings.TrimSpace(values["username"])
	if kind == Telnet {
		user = ""
	}
	identity := strings.TrimSpace(values["identity filename v2"])
	if identity == "" {
		identity = strings.TrimSpace(values["identity filename"])
	}
	if identity != "" && !strings.EqualFold(identity, "none") && !IsPlaceholderKey(identity) {
		result.Keys++
	}
	if sessionName := strings.TrimSpace(values["session name"]); sessionName != "" {
		name = sessionName
	}
	result.add(Entry{
		Name: name, Group: group, Protocol: kind, Host: host, Port: port, User: user, Source: source,
	})
	return true
}

// secureCRTPortKeys is the port field a protocol writes, most specific first.
func secureCRTPortKeys(protocol string) []string {
	switch protocol {
	case "SSH1":
		return []string{"[ssh1] port", "[ssh2] port", "port"}
	case "TELNET":
		return []string{"[telnet] port", "port", "[ssh2] port", "[ssh1] port"}
	default:
		return []string{"[ssh2] port", "[ssh1] port", "[telnet] port", "port"}
	}
}

// secureCRTLine reads one S:"Key"=value, D:"Key"=value or B:"Key"=value line.
// B values continue on indented lines of hex, which this does not match.
func secureCRTLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(strings.TrimRight(line, "\r"))
	if len(line) < 5 || (line[0] != 'S' && line[0] != 'D' && line[0] != 'B') || line[1] != ':' || line[2] != '"' {
		return "", "", false
	}
	end := strings.IndexByte(line[3:], '"')
	if end < 0 {
		return "", "", false
	}
	key = strings.ToLower(line[3 : 3+end])
	rest := line[4+end:]
	if !strings.HasPrefix(rest, "=") {
		return "", "", false
	}
	value = strings.TrimSpace(rest[1:])
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = strings.ReplaceAll(value[1:len(value)-1], `\"`, `"`)
		value = strings.ReplaceAll(value, `\\`, `\`)
	}
	return key, value, true
}

// secureCRTPort reads a port stored either as SecureCRT's eight hex digits or,
// as some edited files hold it, as decimal text.
func secureCRTPort(value string) int {
	value = strings.TrimSpace(value)
	if len(value) == 8 {
		if number, err := strconv.ParseUint(value, 16, 32); err == nil {
			if number > 0 && number <= 65535 {
				return int(number)
			}
			return 0
		}
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 || number > 65535 {
		return 0
	}
	return number
}

// ---- FinalShell ----

// FinalShell keeps its data in a directory: config.json at the top, and under
// conn/ a folder.json for every folder and an <id>_connect_config.json for
// every host. The folder and host records name each other by id, so the tree is
// rebuilt from parent_id rather than from the paths on disk. Passwords are
// encrypted by FinalShell and are not read; keys named by secret_key_id are
// counted but not imported. A record's conection_type is 100 for SSH and 101
// for Windows remote desktop, which is skipped like any other unread protocol.
const (
	finalShellRoot = "root"
	finalShellSSH  = 100
)

// finalShellIgnored keeps historical copies and deleted entries out of the
// import; FinalShell writes both with the same shape as live records.
var finalShellIgnored = map[string]bool{
	"backup": true, "deleted": true, "temp": true, "blur_image_cache": true,
}

// finalShellFolder is one conn/<id>/folder.json.
type finalShellFolder struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
}

// finalShellHost is one *_connect_config.json. FinalShell spells "connection"
// as "conection" on disk, and an edited file may hold a number as text.
type finalShellHost struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Host        string        `json:"host"`
	Port        finalShellInt `json:"port"`
	UserName    string        `json:"user_name"`
	AuthType    finalShellInt `json:"authentication_type"`
	SecretKeyID string        `json:"secret_key_id"`
	ConnType    finalShellInt `json:"conection_type"`
	ParentID    string        `json:"parent_id"`

	source string // where it was read, for warnings
}

type finalShellInt int

func (value *finalShellInt) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	number, err := strconv.Atoi(text)
	if err != nil {
		*value = 0
		return nil
	}
	*value = finalShellInt(number)
	return nil
}

// ParseFinalShell reads the FinalShell data directory the user chose. On
// Windows that is the folder FinalShell was installed in, on macOS
// ~/Library/FinalShell, and on Linux ~/.finalshell.
func ParseFinalShell(path string) (Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, errors.New("not a FinalShell data folder: choose the folder that holds config.json and conn")
	}
	return parseFinalShellFolder(path)
}

func parseFinalShellFolder(root string) (Result, error) {
	return parseFinalShellFolderWithin(root, MaxEntries)
}

// parseFinalShellFolderWithin reads a data folder with an explicit record
// limit, so a test can exercise the cap without thousands of files.
func parseFinalShellFolderWithin(root string, limit int) (Result, error) {
	var (
		result    Result
		folders   = map[string]finalShellFolder{}
		seenHosts = map[string]bool{}
		hosts     []finalShellHost
		records   int
		hostFiles int
		totalSize uint64
		shortened bool
	)
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			result.warn("%s: %v", filepath.Base(path), err)
			return nil
		}
		if entry.IsDir() {
			relative, relativeErr := filepath.Rel(root, path)
			if relativeErr == nil && relative != "." && finalShellIgnoredDir(filepath.ToSlash(relative)) {
				// History and cache folders are never imported; do not walk them.
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		lower := strings.ToLower(entry.Name())
		isFolder := lower == "folder.json"
		isHost := strings.HasSuffix(lower, "_connect_config.json")
		if !isFolder && !isHost {
			return nil
		}
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			relative = entry.Name()
		}
		relative = filepath.ToSlash(relative)
		if finalShellIgnoredPath(relative) {
			return nil
		}
		if records >= limit {
			result.warn("%s: more than %d records; the rest were not read", filepath.Base(root), limit)
			return filepath.SkipAll
		}
		records++
		if info, infoErr := entry.Info(); infoErr == nil {
			totalSize += uint64(info.Size())
			if totalSize > maxArchiveSize {
				result.warn("%s: too large; the rest of the folder was not read", filepath.Base(root))
				return filepath.SkipAll
			}
		}
		text, readErr := readFile(path)
		if readErr != nil {
			result.warn("%s: %v", relative, readErr)
			return nil
		}
		if isFolder {
			var folder finalShellFolder
			if err := json.Unmarshal([]byte(text), &folder); err != nil {
				result.warn("%s: not valid JSON; skipped", relative)
				return nil
			}
			if folder.ID == "" {
				result.warn("%s: no folder id; skipped", relative)
				return nil
			}
			if _, seen := folders[folder.ID]; !seen {
				folders[folder.ID] = folder
			}
			return nil
		}
		hostFiles++
		var host finalShellHost
		if err := json.Unmarshal([]byte(text), &host); err != nil {
			result.warn("%s: not valid JSON; skipped", relative)
			return nil
		}
		if host.ID == "" {
			result.warn("%s: no host id; skipped", relative)
			return nil
		}
		if seenHosts[host.ID] {
			return nil
		}
		seenHosts[host.ID] = true
		host.source = relative
		hosts = append(hosts, host)
		return nil
	})
	if walkErr != nil {
		return result, walkErr
	}
	if hostFiles == 0 {
		return result, errors.New("not a FinalShell data folder: it holds no host configurations")
	}
	orphans := 0
	for _, host := range hosts {
		group := ""
		if host.ParentID != "" && host.ParentID != finalShellRoot {
			var cut bool
			group, cut = finalShellGroupPath(folders, host.ParentID)
			shortened = shortened || cut
			if _, known := folders[host.ParentID]; !known {
				orphans++
			}
		}
		switch int(host.ConnType) {
		case 0, finalShellSSH:
		default:
			result.Skipped++
			continue
		}
		if host.SecretKeyID != "" {
			result.Keys++
		}
		result.add(Entry{
			Name: host.Name, Group: group, Protocol: SSH, Host: host.Host,
			Port: int(host.Port), User: host.UserName, Source: host.source,
		})
	}
	if orphans > 0 {
		result.warn("%d sessions are in folders outside the chosen folder and were imported without one; choose the FinalShell data folder to keep their groups", orphans)
	}
	if shortened {
		result.warn("%s: folders nested more than %d levels deep were shortened", filepath.Base(root), maxGroupDepth)
	}
	return result, nil
}

// finalShellIgnoredPath reports whether a record sits under backup/, deleted/,
// temp/ or blur_image_cache/, which hold history rather than saved sessions.
func finalShellIgnoredPath(relative string) bool {
	parts := strings.Split(relative, "/")
	return finalShellIgnoredParts(parts[:len(parts)-1])
}

// finalShellIgnoredDir reports whether a directory itself belongs to the
// history and cache trees a FinalShell folder keeps but never imports.
func finalShellIgnoredDir(relative string) bool {
	return finalShellIgnoredParts(strings.Split(relative, "/"))
}

func finalShellIgnoredParts(parts []string) bool {
	for _, part := range parts {
		if finalShellIgnored[strings.ToLower(part)] {
			return true
		}
	}
	return false
}

// finalShellGroupPath is the folder a record sits in followed by its own name,
// joined with "/" the way the other imports join nested folders, and whether
// the path had to be cut at maxGroupDepth levels. A folder that is its own
// ancestor, as a damaged file can make it, stops the climb where it would come
// round to itself.
func finalShellGroupPath(folders map[string]finalShellFolder, id string) (string, bool) {
	var ids []string
	cut := false
	for current := id; ; {
		folder, known := folders[current]
		if !known || slices.Contains(ids, current) {
			break
		}
		if len(ids) == maxGroupDepth {
			cut = true
			break
		}
		ids = append(ids, current)
		if folder.ParentID == "" || folder.ParentID == finalShellRoot {
			break
		}
		current = folder.ParentID
	}
	titles := make([]string, 0, len(ids))
	for _, folderID := range slices.Backward(ids) {
		if name := clean(folders[folderID].Name, maxNameLength); name != "" {
			titles = append(titles, name)
		}
	}
	return strings.Join(titles, "/"), cut
}

// ---- Xterminal ----

// Xterminal keeps its local repository in a SQLite database, local-store.db,
// inside a sqlite folder under its user data directory. Connections and their
// folders are rows of one generic table, their JSON payloads under a store key
// that starts with the local user's id. Only the SSH connections and the folder
// tree above them are read; RDP, Telnet and tunnel records sit under other
// store keys and are left alone. Xterminal encrypts passwords, private keys and
// saved credentials with the repository password, so an import carries the
// address, port, login, name, folder and command after login, never a secret.
const (
	// xtermServerStoreSuffix ends the key of the local SSH connections, which
	// Xterminal names after its local server database. The part before it
	// identifies the local user the rows belong to.
	xtermServerStoreSuffix = "GeneralDatabaseLocalImp-ServerDatabaseLocalImp"
	// xtermGroupStoreSuffix ends the key of that user's SSH folders.
	xtermGroupStoreSuffix = "GeneralDatabaseLocalImp-SERVER"
)

// xtermServer is the part of one connection record this package reads.
// Xterminal spells the login username, and a connection's saved password,
// private key and credential stay encrypted and are not read.
type xtermServer struct {
	Title      string     `json:"title"`
	Host       string     `json:"host"`
	Port       lenientInt `json:"port"`
	User       string     `json:"username"`
	GroupID    string     `json:"groupId"`
	AuthType   string     `json:"authType"`
	PrivateKey string     `json:"privateKey"`
	// Xterminal's editor keeps the command it runs after login as a Bash
	// script under connectionConfig.init; a one-line script is the same thing
	// as a host's command after login.
	ConnectionConfig struct {
		Init struct {
			Bash string `json:"bash"`
		} `json:"init"`
	} `json:"connectionConfig"`
}

// xtermGroup is one folder. A missing ParentID means the top level.
type xtermGroup struct {
	ID       string `json:"_id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId"`
}

// lenientInt reads a number that an edited or older record may hold as text.
type lenientInt int

func (value *lenientInt) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	number, err := strconv.Atoi(text)
	if err != nil {
		*value = 0
		return nil
	}
	*value = lenientInt(number)
	return nil
}

// ParseXterminal reads Xterminal's local repository: the local-store.db file
// itself, the sqlite folder holding it, or the user data folder that holds that
// folder. The SSH sessions and their folder tree come out; their saved
// credentials do not.
func ParseXterminal(path string) (Result, error) {
	database, err := xterminalDatabase(path)
	if err != nil {
		return Result{}, err
	}
	// The path becomes a SQLite URI, where ? and # would start query options.
	if strings.ContainsAny(database, "?#") {
		return Result{}, errors.New("the Xterminal local store path holds characters a database URI cannot carry")
	}
	store, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(database, `\`, "/")+"?mode=ro&_pragma=busy_timeout(3000)")
	if err != nil {
		return Result{}, fmt.Errorf("not an Xterminal local store: %v", err)
	}
	defer store.Close()
	store.SetMaxOpenConns(1)
	store.SetMaxIdleConns(1)
	var table string
	err = store.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'local_store'`).Scan(&table)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, errors.New("not an Xterminal local store: it has no local_store table")
	}
	if err != nil {
		return Result{}, fmt.Errorf("not an Xterminal local store: %v", err)
	}
	source := filepath.Base(database)
	keys, err := xtermStoreKeys(store)
	if err != nil {
		return Result{}, err
	}
	groups, err := xtermGroups(store, keys)
	if err != nil {
		return Result{}, err
	}
	result, rows, err := xtermServers(store, keys, groups, source)
	if err != nil {
		return Result{}, err
	}
	if rows == 0 {
		return result, errors.New("not an Xterminal local store: it holds no SSH connections")
	}
	return result, nil
}

// xterminalDatabase resolves the chosen path to the local-store.db it names or
// holds. The folder layouts are the ones Xterminal itself uses: a chosen user
// data folder, its sqlite subfolder, and the userData folder of the portable
// build that lives under xt_data.
func xterminalDatabase(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return path, nil
	}
	for _, relative := range []string{
		"local-store.db",
		filepath.Join("sqlite", "local-store.db"),
		filepath.Join("userData", "sqlite", "local-store.db"),
	} {
		candidate := filepath.Join(path, relative)
		if candidateInfo, candidateErr := os.Stat(candidate); candidateErr == nil && !candidateInfo.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("not an Xterminal data folder: it holds no sqlite/local-store.db")
}

// xtermStoreKeys lists the store keys the database holds. Xterminal keys every
// kind of record with the local user's id, so the suffix is what tells a set of
// SSH connections from a set of notes or a different protocol.
func xtermStoreKeys(store *sql.DB) ([]string, error) {
	rows, err := store.Query(`SELECT DISTINCT store_key FROM local_store LIMIT ?`, MaxEntries)
	if err != nil {
		return nil, fmt.Errorf("not an Xterminal local store: %v", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("not an Xterminal local store: %v", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("not an Xterminal local store: %v", err)
	}
	return keys, nil
}

// xtermGroups reads every SSH folder in the store, grouped by the local user
// whose key holds it, so a connection finds its own user's folders and not
// another user's. A record that is not a folder is skipped.
func xtermGroups(store *sql.DB, keys []string) (map[string]map[string]xtermGroup, error) {
	groups := map[string]map[string]xtermGroup{}
	for _, key := range keys {
		prefix, ok := strings.CutSuffix(key, "-"+xtermGroupStoreSuffix)
		if !ok || prefix == "" {
			continue
		}
		rows, err := store.Query(
			`SELECT payload FROM local_store WHERE store_key = ? AND length(payload) <= ? ORDER BY created_at ASC`,
			key, MaxFileSize,
		)
		if err != nil {
			return nil, fmt.Errorf("not an Xterminal local store: %v", err)
		}
		for rows.Next() {
			var payload string
			if err := rows.Scan(&payload); err != nil {
				rows.Close()
				return nil, fmt.Errorf("not an Xterminal local store: %v", err)
			}
			var folder xtermGroup
			if err := json.Unmarshal([]byte(payload), &folder); err != nil || folder.ID == "" {
				continue
			}
			if groups[prefix] == nil {
				groups[prefix] = map[string]xtermGroup{}
			}
			if _, seen := groups[prefix][folder.ID]; !seen {
				groups[prefix][folder.ID] = folder
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("not an Xterminal local store: %v", err)
		}
		rows.Close()
	}
	return groups, nil
}

// xtermServers reads the SSH connections. rows is how many records the store
// held for them, so a database of only other protocols is told apart from an
// empty one.
func xtermServers(store *sql.DB, keys []string, groups map[string]map[string]xtermGroup, source string) (Result, int, error) {
	var (
		result    Result
		servers   int
		orphans   int
		shortened bool
	)
	for _, key := range keys {
		if len(result.Entries) >= MaxEntries {
			result.warn("%s: more than %d sessions; the rest were not read", source, MaxEntries)
			break
		}
		prefix, ok := strings.CutSuffix(key, "-"+xtermServerStoreSuffix)
		if !ok || prefix == "" {
			continue
		}
		rows, err := store.Query(
			`SELECT payload FROM local_store WHERE store_key = ? AND length(payload) <= ? ORDER BY created_at ASC`,
			key, MaxFileSize,
		)
		if err != nil {
			return Result{}, 0, fmt.Errorf("not an Xterminal local store: %v", err)
		}
		for rows.Next() {
			if len(result.Entries) >= MaxEntries {
				result.warn("%s: more than %d sessions; the rest were not read", source, MaxEntries)
				break
			}
			var payload string
			if err := rows.Scan(&payload); err != nil {
				rows.Close()
				return Result{}, 0, fmt.Errorf("not an Xterminal local store: %v", err)
			}
			servers++
			var server xtermServer
			if err := json.Unmarshal([]byte(payload), &server); err != nil {
				result.warn("%s: a connection record is not valid JSON; skipped", source)
				continue
			}
			group := ""
			if server.GroupID != "" {
				var cut bool
				group, cut = xtermGroupPath(groups[prefix], server.GroupID)
				shortened = shortened || cut
				if _, known := groups[prefix][server.GroupID]; !known {
					orphans++
				}
			}
			if server.AuthType == "privateKey" && server.PrivateKey != "" {
				result.Keys++
			}
			result.add(Entry{
				Name: server.Title, Group: group, Protocol: SSH, Host: server.Host,
				Port: int(server.Port), User: server.User, Command: server.ConnectionConfig.Init.Bash,
				Source: source,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return Result{}, 0, fmt.Errorf("not an Xterminal local store: %v", err)
		}
		rows.Close()
	}
	if orphans > 0 {
		result.warn("%d sessions are in folders that are not in this repository and were imported without one", orphans)
	}
	if shortened {
		result.warn("%s: folders nested more than %d levels deep were shortened", source, maxGroupDepth)
	}
	return result, servers, nil
}

// xtermGroupPath is the folder a group sits in followed by its own name, joined
// with "/" the way the other imports join nested folders, and whether the path
// had to be cut at maxGroupDepth levels (the innermost ones are kept). A group
// that is its own ancestor, as a damaged record can make it, stops the climb
// where it would come round to itself.
func xtermGroupPath(groups map[string]xtermGroup, id string) (string, bool) {
	var ids []string
	cut := false
	for current := id; ; {
		folder, known := groups[current]
		if !known || slices.Contains(ids, current) {
			break
		}
		if len(ids) == maxGroupDepth {
			cut = true
			break
		}
		ids = append(ids, current)
		if folder.ParentID == "" {
			break
		}
		current = folder.ParentID
	}
	titles := make([]string, 0, len(ids))
	for _, groupID := range slices.Backward(ids) {
		if name := clean(groups[groupID].Name, maxNameLength); name != "" {
			titles = append(titles, name)
		}
	}
	return strings.Join(titles, "/"), cut
}

// ---- XPipe ----

// XPipe keeps a vault as a directory: every connection is stores/<uuid>/ with
// an entry.json naming it and a store.json holding its type and address, its
// folders are categories/<uuid>/category.json, and a connection can point at
// an identity store by uuid. Only the connection types Motty can open are
// read: SSH, RDP and VNC. Docker containers, shell environments, scripts and
// identities are not hosts to import. XPipe encrypts passwords and keys, so an
// import carries the address, port, login, name and folder, never a secret.
const (
	xpipeAllConnectionsCategory    = "bfb0b51a-e7a3-4ce4-8878-8d4cb5828d6c"
	xpipeDefaultCategory           = "97458c07-75c0-4f9d-a06e-92d8cdf67c40"
	xpipeAllIdentitiesCategory     = "23a5565d-b343-4ab2-abf4-48a5d12dda22"
	xpipeLocalIdentitiesCategory   = "e784de4e-abea-4cb8-a839-fc557cd23097"
	xpipeSyncedIdentitiesCategory  = "69aa5040-28dc-451e-b4ff-1192ce5e1e3c"
	xpipeAllScriptsCategory        = "19024cf9-d192-41a9-88a6-a22694cf716a"
	xpipePredefinedScriptsCategory = "5faf1d71-0efc-4293-8b70-299406396973"
	xpipeCustomScriptsCategory     = "d3496db5-b709-41f9-abc0-ee0a660fbab9"
	xpipeScriptSourcesCategory     = "4b766928-372b-4ac4-9d10-260ce65288cd"
)

// xpipeBuiltinCategories are the folders XPipe creates for itself. They say
// where entries are kept in its own interface, not what the user called a
// folder, so they do not become groups here.
var xpipeBuiltinCategories = map[string]bool{
	xpipeAllConnectionsCategory:    true,
	xpipeDefaultCategory:           true,
	xpipeAllIdentitiesCategory:     true,
	xpipeLocalIdentitiesCategory:   true,
	xpipeSyncedIdentitiesCategory:  true,
	xpipeAllScriptsCategory:        true,
	xpipePredefinedScriptsCategory: true,
	xpipeCustomScriptsCategory:     true,
	xpipeScriptSourcesCategory:     true,
}

// xpipeEntry is the entry.json of one store: its name and the folder it sits
// in. Tags, colours and icons have no host field to go to.
type xpipeEntry struct {
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	CategoryUUID string `json:"categoryUuid"`
}

// xpipeCategory is one folder. A missing ParentUUID means the top level.
type xpipeCategory struct {
	UUID       string `json:"uuid"`
	Name       string `json:"name"`
	ParentUUID string `json:"parentUuid"`
}

// xpipeSSHIdentity is the key part of an identity. A key stored inside an
// encrypted wrapper has no readable type and is not counted.
type xpipeSSHIdentity struct {
	Type string `json:"type"`
}

// xpipeIdentityRef is an identity store's login name and key, both when it is
// embedded in a connection and when a connection points at it.
type xpipeIdentityRef struct {
	Username    string           `json:"username"`
	SSHIdentity xpipeSSHIdentity `json:"sshIdentity"`
}

// xpipeIdentity is how a connection says who logs in: an identity written into
// the connection itself, or a reference to an identity store.
type xpipeIdentity struct {
	Type          string           `json:"type"`
	Ref           string           `json:"ref"`
	IdentityStore xpipeIdentityRef `json:"identityStore"`
}

// xpipeStore is the part of a store.json this package reads. A connection uses
// Host, Port and Identity; an identity store uses Username and SSHIdentity.
type xpipeStore struct {
	Type        string           `json:"type"`
	Host        string           `json:"host"`
	Port        lenientInt       `json:"port"`
	Username    string           `json:"username"`
	SSHIdentity xpipeSSHIdentity `json:"sshIdentity"`
	Identity    xpipeIdentity    `json:"identity"`
}

type xpipeRecord struct {
	dir   string // the store folder, for warnings and the source
	uuid  string // the identity a connection refers to
	entry xpipeEntry
	store xpipeStore
}

// ParseXPipe reads an XPipe vault: the folder that holds stores and categories
// (its storage folder), the data folder above it, or a clone of the git
// repository it syncs to. SSH, RDP and VNC connections come out; encrypted
// passwords and keys do not.
func ParseXPipe(path string) (Result, error) {
	root, err := xpipeVaultRoot(path)
	if err != nil {
		return Result{}, err
	}
	source := filepath.Base(root)
	var result Result
	categories, err := xpipeCategories(root, &result)
	if err != nil {
		return Result{}, err
	}
	records, err := xpipeRecords(root, &result)
	if err != nil {
		return Result{}, err
	}
	identities := map[string]xpipeIdentityRef{}
	for _, record := range records {
		switch record.store.Type {
		case "localIdentity", "syncedIdentity":
			identities[record.uuid] = xpipeIdentityRef{
				Username: record.store.Username, SSHIdentity: record.store.SSHIdentity,
			}
		}
	}
	connections := 0
	shortened := false
	for _, record := range records {
		protocol := SSH
		switch record.store.Type {
		case "ssh":
		case "rdpSimple":
			protocol = RDP
		case "vnc", "vncSimple":
			protocol = VNC
		default:
			continue
		}
		connections++
		if record.entry.Name == "" && record.store.Host == "" {
			continue
		}
		identity := xpipeIdentityRef{Username: record.store.Username, SSHIdentity: record.store.SSHIdentity}
		if record.store.Identity.Type == "inPlace" {
			identity = record.store.Identity.IdentityStore
		} else if record.store.Identity.Type == "ref" {
			if referenced, ok := identities[record.store.Identity.Ref]; ok {
				identity = referenced
			} else {
				identity = xpipeIdentityRef{}
			}
		}
		if protocol == SSH && identity.SSHIdentity.Type == "file" {
			result.Keys++
		}
		group := ""
		if record.entry.CategoryUUID != "" {
			var cut bool
			group, cut = xpipeGroupPath(categories, record.entry.CategoryUUID)
			shortened = shortened || cut
		}
		result.add(Entry{
			Name: record.entry.Name, Group: group, Protocol: protocol,
			Host: record.store.Host, Port: int(record.store.Port), User: identity.Username,
			Source: filepath.Join("stores", record.dir),
		})
	}
	if shortened {
		result.warn("%s: folders nested more than %d levels deep were shortened", source, maxGroupDepth)
	}
	if connections == 0 {
		return result, errors.New("not an XPipe data folder: it holds no SSH, RDP or VNC connections")
	}
	return result, nil
}

// xpipeVaultRoot resolves the chosen folder to the one that holds stores and
// categories: the vault itself, or the storage folder below XPipe's data
// folder.
func xpipeVaultRoot(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("not an XPipe data folder: choose the folder that holds stores and categories")
	}
	for _, root := range []string{path, filepath.Join(path, "storage")} {
		valid := true
		for _, name := range []string{"stores", "categories"} {
			entry, err := os.Stat(filepath.Join(root, name))
			if err != nil || !entry.IsDir() {
				valid = false
				break
			}
		}
		if valid {
			return root, nil
		}
	}
	return "", errors.New("not an XPipe data folder: it holds no stores and categories")
}

// xpipeCategories reads the folder tree. A category that is not a folder is
// skipped with a warning, so one damaged record does not hide the rest.
func xpipeCategories(root string, result *Result) (map[string]xpipeCategory, error) {
	dirs, err := os.ReadDir(filepath.Join(root, "categories"))
	if err != nil {
		return nil, fmt.Errorf("not an XPipe data folder: %v", err)
	}
	categories := map[string]xpipeCategory{}
	var total int64
	for _, dir := range dirs {
		if !dir.IsDir() || dir.Type()&fs.ModeSymlink != 0 {
			continue
		}
		text, readErr := readFile(filepath.Join(root, "categories", dir.Name(), "category.json"))
		if readErr != nil {
			result.warn("%s: %v", filepath.Join("categories", dir.Name()), readErr)
			continue
		}
		total += int64(len(text))
		if total > maxArchiveSize {
			result.warn("%s: too large; the rest of the vault was not read", filepath.Base(root))
			break
		}
		var category xpipeCategory
		if err = json.Unmarshal([]byte(text), &category); err != nil || category.Name == "" {
			result.warn("%s: not a readable category; skipped", filepath.Join("categories", dir.Name()))
			continue
		}
		if category.UUID == "" {
			category.UUID = dir.Name()
		}
		categories[category.UUID] = category
	}
	return categories, nil
}

// xpipeRecords reads every store, keeping the entry name and folder when the
// folder holds one. A store that is not readable JSON is skipped; that is what
// a vault encrypted at the connection level looks like.
func xpipeRecords(root string, result *Result) ([]xpipeRecord, error) {
	dirs, err := os.ReadDir(filepath.Join(root, "stores"))
	if err != nil {
		return nil, fmt.Errorf("not an XPipe data folder: %v", err)
	}
	var (
		records []xpipeRecord
		total   int64
	)
	for _, dir := range dirs {
		if !dir.IsDir() || dir.Type()&fs.ModeSymlink != 0 {
			continue
		}
		if len(records) >= MaxEntries*2 {
			result.warn("%s: more than %d records; the rest were not read", filepath.Base(root), MaxEntries*2)
			break
		}
		relative := filepath.Join("stores", dir.Name())
		text, readErr := readFile(filepath.Join(root, relative, "store.json"))
		if readErr != nil {
			result.warn("%s: %v", relative, readErr)
			continue
		}
		total += int64(len(text))
		if total > maxArchiveSize {
			result.warn("%s: too large; the rest of the vault was not read", filepath.Base(root))
			break
		}
		var store xpipeStore
		if err = json.Unmarshal([]byte(text), &store); err != nil {
			result.warn("%s: a record is not readable JSON; skipped", relative)
			continue
		}
		record := xpipeRecord{dir: dir.Name(), uuid: dir.Name(), store: store}
		if entryText, entryErr := readFile(filepath.Join(root, relative, "entry.json")); entryErr == nil {
			total += int64(len(entryText))
			if total > maxArchiveSize {
				result.warn("%s: too large; the rest of the vault was not read", filepath.Base(root))
				break
			}
			if err = json.Unmarshal([]byte(entryText), &record.entry); err != nil {
				result.warn("%s: its entry is not readable JSON; skipped", relative)
				continue
			}
			if record.entry.UUID == "" {
				record.entry.UUID = dir.Name()
			}
			record.uuid = record.entry.UUID
		}
		records = append(records, record)
	}
	return records, nil
}

// xpipeGroupPath is the folder a category sits in followed by its own name,
// joined with "/" the way the other imports join nested folders, and whether
// the path had to be cut at maxGroupDepth levels. XPipe's own categories are
// not folders of the user's and are left out. A category that is its own
// ancestor, as a damaged record can make it, stops where it would come round to
// itself.
func xpipeGroupPath(categories map[string]xpipeCategory, id string) (string, bool) {
	var ids []string
	cut := false
	for current := id; ; {
		category, known := categories[current]
		if !known || slices.Contains(ids, current) {
			break
		}
		if len(ids) == maxGroupDepth {
			cut = true
			break
		}
		ids = append(ids, current)
		if category.ParentUUID == "" || xpipeBuiltinCategories[category.ParentUUID] {
			break
		}
		current = category.ParentUUID
	}
	titles := make([]string, 0, len(ids))
	for _, categoryID := range slices.Backward(ids) {
		if xpipeBuiltinCategories[categoryID] {
			continue
		}
		if name := clean(categories[categoryID].Name, maxNameLength); name != "" {
			titles = append(titles, name)
		}
	}
	return strings.Join(titles, "/"), cut
}
