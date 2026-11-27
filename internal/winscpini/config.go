// Package winscpini reads the Sessions sections of a WinSCP INI file.
package winscpini

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	bom           = "\xef\xbb\xbf"
	sessionPrefix = `Sessions\`
	defaultName   = "Default Settings"
	defaultPort   = 22
)

// Site contains the importable endpoint and private key path of a WinSCP site.
type Site struct {
	Name    string
	Host    string
	User    string
	Port    uint16
	KeyPath string
}

// Config is a read-only snapshot of an INI file.
type Config struct {
	lines []string
}

type section struct {
	name       string
	start, end int
	safe       bool
}

func (c *Config) sections() []section {
	var sites []section
	active := -1
	for i, line := range c.lines {
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
		name, ok := decode(rawName)
		sites = append(sites, section{
			name: name, start: i, end: len(c.lines),
			safe: ok && validName(name) && !strings.Contains(rawName, `\`),
		})
		active = len(sites) - 1
	}
	return sites
}

// Sites returns unambiguous SSH sites in file order. The skipped count omits
// the reserved Default Settings section, which is not a saved site.
func (c *Config) Sites() ([]Site, int) {
	sections := c.sections()
	counts := make(map[string]int, len(sections))
	for _, s := range sections {
		counts[fold(s.name)]++
	}
	var sites []Site
	skipped := 0
	for _, s := range sections {
		if strings.EqualFold(s.name, defaultName) {
			continue
		}
		site, ok := c.parse(s)
		if counts[fold(s.name)] != 1 || !ok {
			skipped++
			continue
		}
		sites = append(sites, site)
	}
	return sites, skipped
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

func (c *Config) parse(s section) (Site, bool) {
	if !s.safe {
		return Site{}, false
	}
	site := Site{Name: s.name, Port: defaultPort}
	seen := make(map[string]bool)
	for i := s.start + 1; i < s.end; i++ {
		line := strings.TrimSpace(c.lines[i])
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "hostname", "username", "portnumber", "fsprotocol", "publickeyfile", "isworkspace":
			if seen[key] {
				return Site{}, false
			}
			seen[key] = true
		default:
			continue
		}
		switch key {
		case "hostname":
			site.Host, ok = decode(value)
		case "username":
			site.User, ok = decode(value)
		case "publickeyfile":
			site.KeyPath, ok = decode(value)
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
			return Site{}, false
		}
	}
	if at := strings.LastIndexByte(site.Host, '@'); at >= 0 {
		site.User, site.Host = site.Host[:at], site.Host[at+1:]
	}
	return site, validEndpoint(site) && validText(site.KeyPath)
}

func validEndpoint(site Site) bool {
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

func decode(raw string) (string, bool) {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw, false
	}
	decoded = strings.TrimPrefix(decoded, bom)
	return decoded, utf8.ValidString(decoded)
}
