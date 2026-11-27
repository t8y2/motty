package sessionimport

import (
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var errWinSCPNotRegular = errors.New("not a regular file")

const (
	bom           = "\xef\xbb\xbf"
	sessionPrefix = `Sessions\`
	defaultName   = "Default Settings"
	defaultPort   = 22
)

type winSCPSite struct {
	Name    string
	Host    string
	User    string
	Port    uint16
	KeyPath string
	Proxy   *OutboundProxy
	Jump    *SSHJump
}

type winSCPConfig []string

type winSCPSection struct {
	name       string
	start, end int
	safe       bool
}

func (c winSCPConfig) sections() []winSCPSection {
	var sites []winSCPSection
	active := -1
	for i, line := range c {
		header := strings.TrimSpace(line)
		if len(header) < 2 || header[0] != '[' || header[len(header)-1] != ']' {
			continue
		}
		if active != -1 {
			sites[active].end = i
			active = -1
		}
		title := header[1 : len(header)-1]
		if len(title) < len(sessionPrefix) || !strings.EqualFold(title[:len(sessionPrefix)], sessionPrefix) {
			continue
		}
		rawName := title[len(sessionPrefix):]
		name, ok := decodeWinSCP(rawName)
		sites = append(sites, winSCPSection{
			name: name, start: i, end: len(c),
			safe: ok && validName(name) && !strings.Contains(rawName, `\`),
		})
		active = len(sites) - 1
	}
	return sites
}

// ParseWinSCP reads SFTP and SCP sites from a WinSCP INI file.
func ParseWinSCP(path string) (Result, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Result{}, err
	}
	if !info.Mode().IsRegular() {
		return Result{}, &os.PathError{Op: "read", Path: path, Err: errWinSCPNotRegular}
	}
	data, err := readFileBytes(path)
	if err != nil {
		return Result{}, err
	}
	c := winSCPConfig(strings.Split(strings.TrimPrefix(string(data), bom), "\n"))
	sections := c.sections()
	counts := make(map[string]int, len(sections))
	for _, s := range sections {
		counts[fold(s.name)]++
	}
	var result Result
	source := filepath.Base(path)
	for _, s := range sections {
		if strings.EqualFold(s.name, defaultName) {
			continue
		}
		site, ok := c.parse(s)
		if counts[fold(s.name)] != 1 || !ok {
			result.Skipped++
			continue
		}
		group, name := "", site.Name
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			group, name = name[:i], name[i+1:]
		}
		entry := Entry{
			Protocol: SSH, Name: name, Group: group, Host: site.Host,
			User: site.User, Port: int(site.Port), Source: source,
		}
		if site.KeyPath != "" || site.Proxy != nil || site.Jump != nil {
			entry.SSHSettings = &SSHSettings{Proxy: site.Proxy, Jump: site.Jump}
			if site.KeyPath != "" {
				entry.SSHSettings.IdentityFiles = []string{site.KeyPath}
			}
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

func fold(s string) string {
	return strings.Map(func(r rune) rune {
		minimum := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < minimum {
				minimum = next
			}
		}
		return minimum
	}, s)
}

func (c winSCPConfig) parse(s winSCPSection) (winSCPSite, bool) {
	if !s.safe {
		return winSCPSite{}, false
	}
	site := winSCPSite{Name: s.name, Port: defaultPort}
	fields := make(map[string]string)
	for i := s.start + 1; i < s.end; i++ {
		line := strings.TrimSpace(c[i])
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "hostname", "username", "portnumber", "fsprotocol", "publickeyfile", "isworkspace",
			"proxymethod", "proxyhost", "proxyport", "proxyusername",
			"tunnel", "tunnelhostname", "tunnelportnumber", "tunnelusername", "tunnelpublickeyfile":
			if _, duplicate := fields[key]; duplicate {
				return winSCPSite{}, false
			}
			fields[key] = value
		default:
			continue
		}
		switch key {
		case "hostname":
			site.Host, ok = decodeWinSCP(value)
		case "username":
			site.User, ok = decodeWinSCP(value)
		case "publickeyfile":
			site.KeyPath, ok = decodeWinSCP(value)
		case "portnumber":
			port, err := strconv.ParseUint(value, 10, 16)
			site.Port, ok = uint16(port), err == nil && port != 0
		case "fsprotocol":
			protocol, err := strconv.Atoi(value)
			// WinSCP's factory default is 1 (SFTP with SCP fallback).
			// It also supports 0 (SCP) and 2 (SFTP only).
			ok = err == nil && protocol >= 0 && protocol <= 2
		case "isworkspace":
			ok = value == "0" || strings.EqualFold(value, "false")
		}
		if !ok {
			return winSCPSite{}, false
		}
	}
	if at := strings.LastIndexByte(site.Host, '@'); at >= 0 {
		site.User, site.Host = site.Host[:at], site.Host[at+1:]
	}
	if !validEndpoint(site) || !validText(site.KeyPath) {
		return winSCPSite{}, false
	}
	proxy, jump, ok := parseWinSCPRoute(fields)
	site.Proxy, site.Jump = proxy, jump
	return site, ok
}

func parseWinSCPRoute(fields map[string]string) (*OutboundProxy, *SSHJump, bool) {
	var proxy *OutboundProxy
	switch fields["proxymethod"] {
	case "", "0":
	case "2", "3":
		host, hostOK := decodeWinSCP(fields["proxyhost"])
		user, userOK := decodeWinSCP(fields["proxyusername"])
		port, portOK := winSCPRoutePort(fields, "proxyport", 80)
		if !hostOK || !validWinSCPRouteHost(host) || !userOK || !validText(user) || !portOK {
			return nil, nil, false
		}
		kind := ProxySOCKS5
		if fields["proxymethod"] == "3" {
			kind = ProxyHTTPConnect
		}
		proxy = &OutboundProxy{
			Kind: kind, Address: net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(int(port))), User: user,
		}
	default:
		return nil, nil, false
	}
	switch strings.ToLower(fields["tunnel"]) {
	case "", "0", "false":
		return proxy, nil, true
	case "1", "true":
		host, hostOK := decodeWinSCP(fields["tunnelhostname"])
		user, userOK := decodeWinSCP(fields["tunnelusername"])
		if i := strings.LastIndexByte(host, '@'); i >= 0 {
			user, host = host[:i], host[i+1:]
		}
		key, keyOK := decodeWinSCP(fields["tunnelpublickeyfile"])
		port, portOK := winSCPRoutePort(fields, "tunnelportnumber", defaultPort)
		if !hostOK || !validWinSCPRouteHost(host) || !userOK || !validText(user) || !keyOK || !validText(key) || !portOK {
			return nil, nil, false
		}
		return proxy, &SSHJump{Host: strings.Trim(host, "[]"), User: user, Port: port, KeyPath: key}, true
	default:
		return nil, nil, false
	}
}

func winSCPRoutePort(fields map[string]string, key string, fallback uint16) (uint16, bool) {
	value, exists := fields[key]
	if !exists {
		return fallback, true
	}
	port, err := strconv.ParseUint(value, 10, 16)
	return uint16(port), err == nil && port != 0
}

func validWinSCPRouteHost(host string) bool {
	unbracketed := strings.Trim(host, "[]")
	if strings.ContainsAny(host, ":[]") {
		return net.ParseIP(unbracketed) != nil
	}
	return host != "" && validText(host) && !strings.ContainsAny(host, "@/\\") && !strings.ContainsFunc(host, unicode.IsSpace)
}

func validEndpoint(site winSCPSite) bool {
	return site.Host != "" && strings.TrimSpace(site.Host) == site.Host &&
		!strings.Contains(site.Host, "@") && validText(site.Host) &&
		validText(site.User)
}

func validText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

func validName(name string) bool {
	if !validText(name) {
		return false
	}
	for part := range strings.SplitSeq(name, "/") {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}

func decodeWinSCP(raw string) (string, bool) {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw, false
	}
	decoded = strings.TrimPrefix(decoded, bom)
	return decoded, utf8.ValidString(decoded)
}
