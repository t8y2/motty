package workbench

import (
	"net"
	"net/netip"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// What a person points at in terminal output and wants to do something with:
// an address to connect to, a path to look at, an archive to unpack. The terminal
// menu offers these for the text under the pointer when it was opened, so no scan
// runs until the user asks, and nothing runs until they choose.

type actionKind uint8

const (
	actionSSH      actionKind = iota // connect to Target
	actionBrowser                    // open Target, a web address, in the browser
	actionCopy                       // copy Target
	actionFiles                      // show Target, selected in its folder, in the file browser
	actionExtract                    // type the command that unpacks Target
	actionDownload                   // download Target, a file on the server
)

// detected is one thing the menu offers for the text under the pointer.
type detected struct {
	kind   actionKind
	target string // what the action works on
	label  string // what the menu shows of it: the text as the user sees it
	// search, for actionFiles, says the path is a guess: when it is not in its
	// folder, the folder's subfolders are searched for its name.
	search bool
}

var (
	// A path is absolute, relative to home or to here. A trailing :line or
	// :line:column, as compilers and grep print it, is not part of the path.
	pathLine = regexp.MustCompile(`(:\d+){1,2}$`)
	// hostName is a DNS name with at least one dot, or localhost.
	hostName = regexp.MustCompile(`^(?:localhost|(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63})$`)
	userName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)
)

// notHosts are file extensions that look like a top-level domain: "main.go:42"
// is a place in a file, not a port on a host.
var notHosts = map[string]bool{
	"go": true, "py": true, "js": true, "ts": true, "rs": true, "c": true, "h": true, "cc": true, "cpp": true, "hpp": true,
	"java": true, "rb": true, "sh": true, "md": true, "txt": true, "log": true, "json": true, "yaml": true, "yml": true,
	"toml": true, "conf": true, "cfg": true, "ini": true, "html": true, "css": true, "xml": true, "sql": true, "php": true,
	"cs": true, "kt": true, "swift": true, "lua": true, "pl": true, "pm": true, "tsx": true, "jsx": true, "vue": true,
	"lock": true, "mod": true, "sum": true, "out": true, "err": true, "tmp": true, "bak": true, "pid": true, "sock": true,
}

// shellPath is a path as one shell word, keeping a leading ~/ expandable.
func shellPath(filename string) string {
	if rest, ok := strings.CutPrefix(filename, "~/"); ok {
		return "~/" + shellQuote(rest)
	}
	return shellQuote(filename)
}

// extractLine is what is typed at the prompt to unpack an archive: into a folder
// of its own, so an archive that holds loose files cannot scatter them over the
// current one. It is not run: the user reads it and presses Enter.
func extractLine(filename string) string {
	folder := shellQuote(archiveFolderName(path.Base(filename)))
	switch archiveKindOf(filename) {
	case archiveZip:
		return "unzip -q -- " + shellPath(filename) + " -d " + folder
	case archiveTar:
		return "mkdir -- " + folder + " && tar -xf " + shellPath(filename) + " -C " + folder
	}
	return ""
}

func validPort(text string) bool {
	port, err := strconv.Atoi(text)
	return err == nil && port >= 1 && port <= 65535 && strconv.Itoa(port) == text
}

// classifyToken says what can be done with the text under the pointer. user is
// the account of the session it came from, offered for an address without one.
func classifyToken(token, user string) []detected {
	if token == "" || len(token) > 300 {
		return nil
	}
	if place, ok := classifyPlace(token, user); ok {
		return place
	}
	return classifyFile(token)
}

func classifyPlace(token, user string) ([]detected, bool) {
	if strings.Contains(token, "://") {
		return nil, false
	}
	host, port, account := token, "", ""
	if at := strings.LastIndexByte(host, '@'); at > 0 {
		account, host = host[:at], host[at+1:]
		if !userName.MatchString(account) {
			return nil, false
		}
	}
	switch {
	case strings.HasPrefix(host, "["):
		close := strings.IndexByte(host, ']')
		if close < 0 {
			return nil, false
		}
		rest := host[close+1:]
		host = host[1:close]
		if rest != "" {
			if !strings.HasPrefix(rest, ":") || !validPort(rest[1:]) {
				return nil, false
			}
			port = rest[1:]
		}
		if address, err := netip.ParseAddr(host); err != nil || !address.Is6() {
			return nil, false
		}
	case strings.Count(host, ":") >= 2:
		if address, err := netip.ParseAddr(host); err != nil || !address.Is6() {
			return nil, false
		}
	default:
		if colon := strings.LastIndexByte(host, ':'); colon >= 0 {
			host, port = host[:colon], host[colon+1:]
			if !validPort(port) {
				return nil, false
			}
		}
		if address, err := netip.ParseAddr(host); err == nil {
			if !address.Is4() {
				return nil, false
			}
		} else {
			// A name needs a port or an account to be taken for a place: a bare
			// "server.log" is a file.
			last := host[strings.LastIndexByte(host, '.')+1:]
			if !hostName.MatchString(host) || notHosts[strings.ToLower(last)] || port == "" && account == "" {
				return nil, false
			}
		}
	}
	if host == "" {
		return nil, false
	}
	joined := host
	if port != "" {
		joined = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		joined = "[" + host + "]"
	}
	if account != "" {
		joined = account + "@" + joined
	} else if user != "" {
		joined = user + "@" + joined
	}
	found := []detected{{kind: actionSSH, target: joined, label: token}}
	if port != "" && account == "" {
		found = append(found, detected{kind: actionBrowser, target: "http://" + net.JoinHostPort(host, port), label: token})
	}
	return append(found, detected{kind: actionCopy, target: token, label: token}), true
}

func classifyFile(token string) []detected {
	filename := pathLine.ReplaceAllString(token, "")
	absolute := strings.HasPrefix(filename, "/") || strings.HasPrefix(filename, "~/")
	relative := strings.HasPrefix(filename, "./") || strings.HasPrefix(filename, "../")
	var found []detected
	if line := extractLine(filename); line != "" && !strings.HasSuffix(filename, "/") {
		found = append(found, detected{kind: actionExtract, target: line, label: path.Base(filename)})
	}
	if strings.HasPrefix(filename, "/") && len(filename) > 1 {
		found = append(found, detected{kind: actionFiles, target: filename, label: filename})
		if !strings.HasSuffix(filename, "/") {
			found = append(found, detected{kind: actionDownload, target: filename, label: path.Base(filename)})
		}
	}
	if (absolute && len(filename) > 1 || relative && len(filename) > 2) || len(found) > 0 {
		found = append(found, detected{kind: actionCopy, target: filename, label: filename})
	}
	return found
}
