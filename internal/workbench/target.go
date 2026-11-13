package workbench

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// errBadTarget means the typed text does not name something to connect to.
var errBadTarget = errors.New("not a connection target")

// target is a destination read from what the user typed.
type target struct {
	user string
	host string
	port uint16 // defaultPort when none was given
}

// parseTarget reads a connection target the way people write one:
//
//	host
//	user@host
//	user@host:2222
//	user@[::1]:2222
//	ssh user@host
//	ssh -p 2222 user@host
//	ssh -l user host
//	ssh://user@host:2222/
//	sftp://user@host:2222/
//	-p 2222 -l user -pw password host   (PuTTY's options)
//
// The URL forms also read a password the userinfo carries
// (user:password@host), percent-decoded, which is what a bastion's launch
// button embeds; -pw is PuTTY's spelling of the same thing. Anything else,
// including other ssh options, is rejected rather than guessed at: connecting
// somewhere the user did not mean is worse than saying no.
//
// named reports whether the text said which protocol to speak: an SSH or SFTP
// link, or the ssh command. A bare address does not, and may then name a saved
// host of another protocol at that endpoint.
func parseTarget(input string) (parsed target, password string, named bool, err error) {
	fields := strings.Fields(input)
	urlForm := false
	switch {
	case len(fields) == 1 && hasScheme(fields[0], "ssh://"):
		fields[0] = strings.TrimSuffix(fields[0][len("ssh://"):], "/")
		urlForm, named = true, true
	case len(fields) == 1 && hasScheme(fields[0], "sftp://"):
		fields[0] = strings.TrimSuffix(fields[0][len("sftp://"):], "/")
		urlForm, named = true, true
	case len(fields) > 0 && fields[0] == "ssh":
		fields = fields[1:]
		named = true
	}
	parsed, password, err = parseTargetFields(fields, urlForm, defaultPort)
	return parsed, password, named, err
}

// hasScheme reports whether text starts with scheme and has a payload after it.
func hasScheme(text, scheme string) bool {
	return len(text) > len(scheme) && strings.EqualFold(text[:len(scheme)], scheme)
}

// parseTargetFields is the body of parseTarget: the option words already split
// off, whether URL userinfo rules apply, and the port a target without one
// defaults to.
func parseTargetFields(fields []string, urlForm bool, portDefault uint16) (target, string, error) {
	var destination, port, login, optionPassword string
	for index := 0; index < len(fields); index++ {
		field := fields[index]
		switch {
		case field == "-p":
			if index+1 >= len(fields) {
				return target{}, "", errBadTarget
			}
			index++
			port = fields[index]
		case field == "-pw":
			// PuTTY's password option, which a bastion's external-tool
			// template uses in the same way as Xshell's URL userinfo. It is
			// matched before -p, whose letters it starts with.
			if index+1 >= len(fields) {
				return target{}, "", errBadTarget
			}
			index++
			optionPassword = fields[index]
		case strings.HasPrefix(field, "-p") && len(field) > 2:
			port = field[2:]
		case field == "-l":
			if index+1 >= len(fields) {
				return target{}, "", errBadTarget
			}
			index++
			login = fields[index]
		case strings.HasPrefix(field, "-l") && len(field) > 2:
			login = field[2:]
		case strings.HasPrefix(field, "-"):
			return target{}, "", errBadTarget
		case destination != "":
			return target{}, "", errBadTarget
		default:
			destination = field
		}
	}
	if destination == "" {
		return target{}, "", errBadTarget
	}

	result := target{port: portDefault}
	password := optionPassword
	if at := strings.LastIndex(destination, "@"); at >= 0 {
		user := destination[:at]
		destination = destination[at+1:]
		if urlForm {
			var err error
			if user, password, err = splitUserinfo(user); err != nil {
				return target{}, "", err
			}
		}
		if user == "" {
			return target{}, "", errBadTarget
		}
		result.user = user
	}
	host, embedded, err := splitHostPort(destination)
	if err != nil || !validHost(host) {
		return target{}, "", errBadTarget
	}
	result.host = host
	if login != "" && result.user == "" {
		result.user = login // user@host wins over -l, as ssh does
	}
	if embedded != "" && port == "" {
		port = embedded
	}
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return target{}, "", errBadTarget
		}
		result.port = uint16(value)
	}
	if len(password) > 4096 {
		return target{}, "", errBadTarget
	}
	return result, password, nil
}

// splitUserinfo reads URL userinfo as a link writes it: user[:password],
// percent-decoded. The first colon separates the login from the password, as
// RFC 3986 does; a link with no login is not a target.
func splitUserinfo(userinfo string) (user, password string, err error) {
	user, password = userinfo, ""
	if colon := strings.IndexByte(userinfo, ':'); colon >= 0 {
		user, password = userinfo[:colon], userinfo[colon+1:]
	}
	if user, err = url.PathUnescape(user); err != nil {
		return "", "", errBadTarget
	}
	if password, err = url.PathUnescape(password); err != nil {
		return "", "", errBadTarget
	}
	if user == "" || len(password) > 4096 {
		return "", "", errBadTarget
	}
	return user, password, nil
}

// splitHostPort separates "host", "host:port", "[v6]" and "[v6]:port"; a bare
// IPv6 address (several colons, no brackets) is all host.
func splitHostPort(destination string) (host, port string, err error) {
	if strings.HasPrefix(destination, "[") {
		if !strings.Contains(destination, "]:") {
			if !strings.HasSuffix(destination, "]") {
				return "", "", errBadTarget
			}
			return destination[1 : len(destination)-1], "", nil
		}
		return net.SplitHostPort(destination)
	}
	switch strings.Count(destination, ":") {
	case 0:
		return destination, "", nil
	case 1:
		return net.SplitHostPort(destination)
	default:
		return destination, "", nil
	}
}

// validZone accepts an interface name or numeric scope of an IPv6 zone, which
// is what a link-local address is scoped with.
func validZone(zone string) bool {
	if zone == "" || len(zone) > 16 {
		return false
	}
	for _, r := range zone {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// validHost accepts DNS-style names, IP literals and zone-scoped IPv6
// literals such as fe80::1%en0.
func validHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if base, zone, found := strings.Cut(host, "%"); found && zone != "" {
		return strings.Contains(base, ":") && net.ParseIP(base) != nil && validZone(zone)
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if strings.Contains(host, ":") {
		return false
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return !strings.HasPrefix(host, "-")
}

// connection is a destination read from what the user typed or a link asked
// for, with how to reach it: ssh by default, or what the text named.
type connection struct {
	target
	protocol hostProtocol
	// password is a link's userinfo password. It answers that link's
	// connection once and is never written to a saved host or a record.
	password string
	// files opens the file window instead of a terminal: an Xftp or sftp://
	// link.
	files bool
	// viaSSH is set by the rdp+ssh:// and vnc+ssh:// forms: the desktop is
	// reached through an SSH connection, as the host form's switch does.
	viaSSH bool
	// explicitProtocol says the text named the protocol (ssh://, sftp://, the
	// ssh command, telnet, or a desktop scheme). A target typed without one
	// may name a saved host of another protocol at the same endpoint.
	explicitProtocol bool
}

// parseConnection is parseTarget that also understands Telnet, remote desktops
// and the links a bastion's "open with Xshell / Xftp" buttons emit:
//
//	telnet host
//	telnet host 2323
//	telnet://host:2323/
//	rdp://user@host:3389/      vnc://host:5900/
//	rdp+ssh://user@host/       vnc+ssh://host/
//	xshell://ssh://user:password@host:22
//	xftp://sftp://user:password@host:22
func parseConnection(input string) (connection, error) {
	fields := strings.Fields(input)
	files := false
	if len(fields) == 1 {
		link, handled, err := normalizeLaunchLink(fields[0])
		if err != nil {
			return connection{}, err
		}
		if handled {
			fields[0] = link.text
			files = link.files
		}
	}
	rest := ""
	switch {
	case len(fields) == 1 && len(fields[0]) > len("telnet://") && strings.EqualFold(fields[0][:len("telnet://")], "telnet://"):
		rest = strings.TrimSuffix(fields[0][len("telnet://"):], "/")
	case len(fields) >= 2 && len(fields) <= 3 && fields[0] == "telnet":
		rest = fields[1]
		if len(fields) == 3 {
			rest += ":" + fields[2]
		}
	case len(fields) > 0 && fields[0] == "telnet":
		return connection{}, errBadTarget // the word alone, or with too many arguments
	case len(fields) == 1 && hasScheme(fields[0], "rdp+ssh://"):
		return parseDesktopTarget(fields[0][len("rdp+ssh://"):], protocolRDP, true)
	case len(fields) == 1 && hasScheme(fields[0], "vnc+ssh://"):
		return parseDesktopTarget(fields[0][len("vnc+ssh://"):], protocolVNC, true)
	case len(fields) == 1 && hasScheme(fields[0], "rdp://"):
		return parseDesktopTarget(fields[0][len("rdp://"):], protocolRDP, false)
	case len(fields) == 1 && hasScheme(fields[0], "vnc://"):
		return parseDesktopTarget(fields[0][len("vnc://"):], protocolVNC, false)
	default:
		parsed, password, named, err := parseTarget(strings.Join(fields, " "))
		return connection{target: parsed, password: password, files: files, explicitProtocol: named}, err
	}
	host, port, err := splitHostPort(rest)
	if err != nil || !validHost(host) {
		return connection{}, errBadTarget
	}
	result := connection{target: target{host: host, port: defaultTelnetPort}, protocol: protocolTelnet, explicitProtocol: true}
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return connection{}, errBadTarget
		}
		result.port = uint16(value)
	}
	return result, nil
}

// parseDesktopTarget reads the payload of a remote desktop link, whose scheme
// named the protocol and whose port defaults to that protocol's own.
func parseDesktopTarget(payload string, protocol hostProtocol, viaSSH bool) (connection, error) {
	parsed, password, err := parseTargetFields([]string{strings.TrimSuffix(payload, "/")}, true, Host{Protocol: protocol}.defaultPort())
	if err != nil {
		return connection{}, err
	}
	return connection{
		target: parsed, protocol: protocol, password: password,
		viaSSH: viaSSH, explicitProtocol: true,
	}, nil
}
