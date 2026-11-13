package workbench

import (
	"errors"
	"strings"
)

// Bastion hosts offer an "open with Xshell / Xftp" button. NetSarang documents
// no xshell:// or xftp:// scheme: vendors and the bastions themselves register
// one, and the payloads differ between them. The forms seen in the field are
//
//	xshell://ssh://user:password@host:port
//	xshell://ssh//user:password@host:port
//	xshell://ssh:user:password@host:port
//	xshell://user:password@host:port
//	xftp://sftp://user:password@host:port
//
// with the same variants for xftp. normalizeLaunchLink reduces all of them to
// the ssh:// or sftp:// URL the target parser already reads, so the rest of the
// application has one link form to care about.

// errUnsupportedLink is a link Motty understands and cannot open: plain FTP,
// which the file windows do not speak.
var errUnsupportedLink = errors.New("workbench: link needs plain FTP")

// launchLink is a link normalized to the URL form the parser reads.
type launchLink struct {
	text  string
	files bool // the file window, not a terminal
}

// normalizeLaunchLink turns a bastion's client link into the URL Motty parses.
// handled is false for the links the parser already reads (ssh://, telnet://)
// and for anything else. A link for a client Motty cannot be is reported as
// errUnsupportedLink.
func normalizeLaunchLink(text string) (link launchLink, handled bool, err error) {
	scheme, rest, known := splitLinkScheme(text)
	if !known {
		return launchLink{}, false, nil
	}
	switch scheme {
	case "ssh", "telnet":
		return launchLink{}, false, nil
	case "sftp":
		return launchLink{text: text, files: true}, true, nil
	case "xshell", "xftp":
	default:
		return launchLink{}, false, nil
	}
	// The alias scheme's own ":" and "//" are not part of the payload, and
	// runtimes that build the link from a path add another slash or two.
	payload := strings.TrimLeft(rest, ":/")
	inner := ""
	if name, after, ok := splitLinkScheme(payload); ok {
		inner, payload = name, strings.TrimLeft(after, ":/")
	}
	switch inner {
	case "ftp":
		return launchLink{}, true, errUnsupportedLink
	case "ssh", "sftp", "telnet":
	case "":
		// A bare payload is Xshell's own default (ssh) and, for a bastion's
		// Xftp button, a file session over SSH.
		if scheme == "xftp" {
			inner = "sftp"
		} else {
			inner = "ssh"
		}
	default:
		return launchLink{}, true, errUnsupportedLink
	}
	if scheme == "xftp" && inner == "telnet" {
		return launchLink{}, true, errUnsupportedLink
	}
	files := scheme == "xftp" || inner == "sftp"
	return launchLink{text: inner + "://" + payload, files: files}, true, nil
}

// splitLinkScheme reports the scheme a link starts with when it is one of the
// forms a launcher page emits. host.example.com is not a scheme even when it
// starts with a scheme's letters: a scheme is followed by ":" or "/".
func splitLinkScheme(text string) (scheme, rest string, ok bool) {
	for _, known := range [...]string{"xshell", "xftp", "sftp", "telnet", "ssh", "ftp"} {
		if len(text) <= len(known) || !strings.EqualFold(text[:len(known)], known) {
			continue
		}
		if text[len(known)] != ':' && text[len(known)] != '/' {
			continue
		}
		return known, text[len(known):], true
	}
	return "", text, false
}

// redactLaunchText hides the password in a link's userinfo or after PuTTY's
// -pw, so a status line or a report can name the request the application
// refused without showing the secret another program put in it.
func redactLaunchText(text string) string {
	words := strings.Fields(text)
	for index := 0; index+1 < len(words); index++ {
		if words[index] == "-pw" {
			words[index+1] = "***"
			index++
		}
	}
	text = strings.Join(words, " ")
	at := strings.LastIndex(text, "@")
	if at < 0 {
		return text
	}
	authority := strings.LastIndex(text[:at], "://")
	if authority < 0 {
		return text
	}
	userinfo := text[authority+3 : at]
	colon := strings.IndexByte(userinfo, ':')
	if colon < 0 {
		return text
	}
	return text[:authority+3+colon+1] + "***" + text[at:]
}
