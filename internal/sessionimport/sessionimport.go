// Package sessionimport reads the saved sessions of other clients, PuTTY,
// Xshell, MobaXterm, WindTerm, Electerm and Netcatty, into the host fields Motty
// keeps. It reads files the user chose and never executes, resolves or contacts
// anything in them.
//
// Only what a connection needs is taken: a name, a folder, an address, a port
// and a user. Passwords are never imported, whether a client encrypted them with
// a scheme of its own or, as Electerm does, wrote them into the file in the
// clear, and a key file path in a session is reported rather than followed,
// since each client has its own key format.
package sessionimport

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
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

// Protocol is how a session is reached.
type Protocol uint8

const (
	SSH Protocol = iota
	Telnet
	Serial
)

// Entry is one session. A serial one keeps its device in Host and its line in
// Line ("9600 8N1") with Flow "rtscts", "xonxoff" or empty.
type Entry struct {
	Protocol Protocol
	Line     string
	Flow     string

	Name  string
	Group string // folders joined with "/", empty at the top
	Host  string
	Port  int // zero when the file named none
	User  string
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
	// passphrase or private key in readable text (Electerm writes them), so the
	// user can be told to delete the file. None of them were imported.
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
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxFileSize {
		return "", fmt.Errorf("%s is too large to be a session file", filepath.Base(path))
	}
	return decode(data), nil
}

// decode turns a file's bytes into text: UTF-16 with a byte-order mark (what
// Windows tools write), or UTF-8 with or without one. Anything that is neither
// is read as UTF-8 with its bad bytes dropped.
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
			part, err = ParseMobaXterm(path)
		case WindTerm:
			part, err = ParseWindTerm(path)
		case Electerm:
			part, err = ParseElecterm(path)
		case Netcatty:
			part, err = ParseNetcatty(path)
		default:
			err = fmt.Errorf("unknown client %d", client)
		}
		if err != nil {
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
		slashed := strings.ReplaceAll(member.Name, `\`, "/")
		group, base := "", slashed
		if at := strings.LastIndexByte(slashed, '/'); at >= 0 {
			group, base = strings.Trim(slashed[:at], "/"), slashed[at+1:]
		}
		if err := parseXshellSession(&result, decode(data), strings.TrimSuffix(base, filepath.Ext(base)), group, member.Name); err != nil {
			result.warn("%s: %v", member.Name, err)
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
	text, err := readFile(path)
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
		result.add(Entry{Name: name, Group: group, Host: fields[1], Port: port, User: fields[3], Source: source})
	case "1":
		result.add(Entry{Name: name, Group: group, Protocol: Telnet, Host: fields[1], Port: port, Source: source})
	default:
		result.Skipped++
	}
}

// ---- WindTerm ----

// windTermSession is one entry of WindTerm's user.sessions, a JSON array it
// keeps under profiles/<profile>/terminal/. A saved login (session.autoLogin) is
// encrypted and is not read.
type windTermSession struct {
	Label    string `json:"session.label"`
	Group    string `json:"session.group"`
	Target   string `json:"session.target"`
	Port     int    `json:"session.port"`
	Protocol string `json:"session.protocol"`
	User     string `json:"session.user"`
	Identity string `json:"session.identityFile"`
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
		result.add(Entry{
			Name: session.Label, Group: strings.Trim(strings.ReplaceAll(session.Group, `\`, "/"), "/"),
			Protocol: kind, Host: host, Port: session.Port, User: user, Source: source,
		})
	}
	return result, nil
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
	// Password, Passphrase and PrivateKey are read only to count what the export
	// left in the clear; they are never put into an entry.
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
		result.add(Entry{
			Name: bookmark.Title, Group: group, Protocol: kind,
			Host: bookmark.Host, Port: int(bookmark.Port), User: user, Source: source,
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
// clear, and are counted but never imported; ProxyJump, Proxy, tags and notes
// are not read.
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
		result.add(Entry{
			Name: netcattyText(cell(row, columns.label)), Group: group, Protocol: kind,
			Host: strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"), Port: port, User: user,
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
