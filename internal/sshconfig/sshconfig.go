// Package sshconfig imports the statically evaluable subset of OpenSSH client
// configuration files.
package sshconfig

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxIncludeDepth = 16

// WarningCode identifies a configuration construct that could not be imported
// without changing or dynamically evaluating its meaning.
type WarningCode string

const (
	WarningDynamicValue         WarningCode = "dynamic_value"
	WarningIncludeCycle         WarningCode = "include_cycle"
	WarningIncludeDepth         WarningCode = "include_depth"
	WarningIncludeRead          WarningCode = "include_read"
	WarningInvalidPort          WarningCode = "invalid_port"
	WarningInvalidSyntax        WarningCode = "invalid_syntax"
	WarningUnsupportedDirective WarningCode = "unsupported_directive"
	WarningUnsupportedMatch     WarningCode = "unsupported_match"
)

// Warning describes one skipped or invalid construct. Path and Line identify
// the source configuration; Directive is lower-case when available.
type Warning struct {
	Code      WarningCode
	Path      string
	Line      int
	Directive string
	Detail    string
}

// JumpHost is one statically parsed ProxyJump hop.
type JumpHost struct {
	Host string
	User string
	Port uint16
}

// Host is one concrete Host alias and the statically evaluable values obtained
// for it. Port zero means unspecified. HostName remains empty when its first
// obtained value was dynamic and therefore unsafe to import.
type Host struct {
	Alias        string
	HostName     string
	User         string
	Port         uint16
	IdentityFile string
	ProxyJump    []JumpHost
	// ProxyCommand is the command line as written, its %-tokens left for the
	// connection to fill in. Empty when none applies, or when it is "none".
	ProxyCommand string
}

// Result contains imported concrete aliases in source order and non-fatal
// warnings encountered while reading the selected configuration graph.
type Result struct {
	Hosts    []Host
	Warnings []Warning
}

type directive struct {
	path string
	line int
	name string
	args []string
	// raw is the unsplit value of a ProxyCommand.
	raw string
}

type parser struct {
	home string
	// docs holds every file read, by canonical path, in the order they were met.
	// A caller that edits them re-evaluates over the same documents.
	docs        map[string]*doc
	order       *[]string
	directives  []directive
	warnings    []Warning
	warningSeen map[warningKey]struct{}
	aliases     []string
	aliasSeen   map[string]struct{}
	stack       map[string]struct{}
}

type warningKey struct {
	code      WarningCode
	path      string
	line      int
	directive string
	detail    string
}

// ParseFile parses the explicitly selected OpenSSH client configuration file.
// Includes are resolved relative to the including file, except paths beginning
// with ~, which are expanded from the current user's home directory.
func ParseFile(path string) (Result, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return Result{}, fmt.Errorf("resolve SSH config: %w", err)
	}
	p := newParser(make(map[string]*doc), new([]string))
	return p.run(absolute)
}

func newParser(docs map[string]*doc, order *[]string) *parser {
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return &parser{
		home:        home,
		docs:        docs,
		order:       order,
		warningSeen: make(map[warningKey]struct{}),
		aliasSeen:   make(map[string]struct{}),
		stack:       make(map[string]struct{}),
	}
}

// run reads the configuration graph from path and evaluates every alias in it.
func (p *parser) run(path string) (Result, error) {
	inMatch := false
	if err := p.readFile(path, 0, &inMatch); err != nil {
		return Result{}, fmt.Errorf("read SSH config: %w", err)
	}
	result := Result{Hosts: make([]Host, 0, len(p.aliases))}
	for _, alias := range p.aliases {
		result.Hosts = append(result.Hosts, p.evaluate(alias))
	}
	result.Warnings = p.warnings
	return result, nil
}

// document is the file at a canonical path: the one already held (which may
// carry edits), or the one on disk.
func (p *parser) document(canonical string) (*doc, error) {
	if held, ok := p.docs[canonical]; ok {
		return held, nil
	}
	file, err := os.Open(canonical)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", canonical)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	loaded := newDoc(canonical, data, true, info.Mode().Perm())
	p.docs[canonical] = loaded
	*p.order = append(*p.order, canonical)
	return loaded, nil
}

func (p *parser) readFile(path string, depth int, inMatch *bool) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("include depth exceeds %d", maxIncludeDepth)
	}
	canonical := canonicalPath(path)
	if _, exists := p.stack[canonical]; exists {
		return fmt.Errorf("include cycle through %s", canonical)
	}
	held, err := p.document(canonical)
	if err != nil {
		return err
	}
	p.stack[canonical] = struct{}{}
	defer delete(p.stack, canonical)

	for index, line := range held.lines {
		lineNumber := index + 1
		name, args, err := parseLine(line.text)
		if err != nil {
			p.warn(WarningInvalidSyntax, canonical, lineNumber, "", err.Error())
			continue
		}
		if name == "" {
			continue
		}
		switch name {
		case "match":
			p.warn(WarningUnsupportedMatch, canonical, lineNumber, name, "Match sections require runtime evaluation and were skipped")
			*inMatch = true
			continue
		case "host":
			*inMatch = false
			d := directive{path: canonical, line: lineNumber, name: name, args: args}
			p.directives = append(p.directives, d)
			p.collectAliases(d)
			continue
		}
		if *inMatch {
			continue
		}
		if name == "include" {
			p.readIncludes(canonical, lineNumber, args, depth, inMatch)
			continue
		}
		if isSupportedDirective(name) {
			d := directive{path: canonical, line: lineNumber, name: name, args: args}
			if name == "proxycommand" {
				d.raw = rawValue(line.text)
			}
			p.directives = append(p.directives, d)
			continue
		}
		code := WarningUnsupportedDirective
		if isDynamicCommand(name) {
			code = WarningDynamicValue
		}
		p.warn(code, canonical, lineNumber, name, "directive is not safe or supported by offline import")
	}
	return nil
}

func (p *parser) readIncludes(source string, line int, patterns []string, depth int, inMatch *bool) {
	if len(patterns) == 0 {
		p.warn(WarningInvalidSyntax, source, line, "include", "Include requires at least one path")
		return
	}
	if depth >= maxIncludeDepth {
		p.warn(WarningIncludeDepth, source, line, "include", fmt.Sprintf("maximum include depth %d reached", maxIncludeDepth))
		return
	}
	for _, pattern := range patterns {
		if hasDynamicValue(pattern) {
			p.warn(WarningDynamicValue, source, line, "include", "dynamic Include path was skipped")
			continue
		}
		expanded, ok := expandTilde(pattern, p.home)
		if !ok {
			p.warn(WarningDynamicValue, source, line, "include", "Include path uses an unsupported home expansion")
			continue
		}
		if !filepath.IsAbs(expanded) {
			expanded = filepath.Join(filepath.Dir(source), expanded)
		}
		matches, err := filepath.Glob(filepath.Clean(expanded))
		if err != nil {
			p.warn(WarningInvalidSyntax, source, line, "include", err.Error())
			continue
		}
		if len(matches) == 0 {
			p.warn(WarningIncludeRead, source, line, "include", fmt.Sprintf("no files matched %q", pattern))
			continue
		}
		for _, match := range matches {
			canonical := canonicalPath(match)
			if _, exists := p.stack[canonical]; exists {
				p.warn(WarningIncludeCycle, source, line, "include", fmt.Sprintf("cycle through %s", canonical))
				continue
			}
			if err := p.readFile(canonical, depth+1, inMatch); err != nil {
				code := WarningIncludeRead
				if strings.Contains(err.Error(), "include depth") {
					code = WarningIncludeDepth
				}
				p.warn(code, source, line, "include", err.Error())
			}
		}
	}
}

func (p *parser) collectAliases(d directive) {
	for _, argument := range d.args {
		for _, pattern := range strings.Split(argument, ",") {
			if pattern == "" || pattern[0] == '!' || strings.ContainsAny(pattern, "*?%") || strings.Contains(pattern, "${") {
				continue
			}
			key := strings.ToLower(pattern)
			if _, exists := p.aliasSeen[key]; exists {
				continue
			}
			p.aliasSeen[key] = struct{}{}
			p.aliases = append(p.aliases, pattern)
		}
	}
}

func (p *parser) evaluate(alias string) Host {
	host := Host{Alias: alias}
	active := true
	// ProxyJump and ProxyCommand compete: whichever comes first decides, as in
	// OpenSSH.
	var hostNameSet, userSet, portSet, identitySet, proxySet bool
	for _, d := range p.directives {
		if d.name == "host" {
			active = matchPatternList(d.args, alias)
			continue
		}
		if !active {
			continue
		}
		switch d.name {
		case "hostname":
			if hostNameSet {
				continue
			}
			hostNameSet = true
			value, ok := p.singleValue(d)
			if ok && !p.rejectDynamic(d, value) {
				host.HostName = value
			}
		case "user":
			if userSet {
				continue
			}
			userSet = true
			value, ok := p.singleValue(d)
			if ok && !p.rejectDynamic(d, value) {
				host.User = value
			}
		case "port":
			if portSet {
				continue
			}
			portSet = true
			value, ok := p.singleValue(d)
			if !ok {
				continue
			}
			port, err := parsePort(value)
			if err != nil {
				p.warn(WarningInvalidPort, d.path, d.line, d.name, err.Error())
				continue
			}
			host.Port = port
		case "identityfile":
			if identitySet {
				continue
			}
			identitySet = true
			value, ok := p.singleValue(d)
			if !ok || p.rejectDynamic(d, value) {
				continue
			}
			expanded, ok := expandTilde(value, p.home)
			if !ok {
				p.warn(WarningDynamicValue, d.path, d.line, d.name, "IdentityFile uses an unsupported home expansion")
				continue
			}
			host.IdentityFile = expanded
		case "proxycommand":
			if proxySet {
				continue
			}
			proxySet = true
			if d.raw == "" {
				p.warn(WarningInvalidSyntax, d.path, d.line, d.name, "directive requires a command")
				continue
			}
			if !strings.EqualFold(d.raw, "none") {
				host.ProxyCommand = d.raw
			}
		case "proxyjump":
			if proxySet {
				continue
			}
			proxySet = true
			value, ok := p.singleValue(d)
			if !ok || p.rejectDynamic(d, value) || strings.EqualFold(value, "none") {
				continue
			}
			jumps, err := parseProxyJump(value)
			if err != nil {
				p.warn(WarningInvalidSyntax, d.path, d.line, d.name, err.Error())
				continue
			}
			host.ProxyJump = jumps
		}
	}
	if !hostNameSet {
		host.HostName = alias
	}
	return host
}

func (p *parser) singleValue(d directive) (string, bool) {
	if len(d.args) != 1 || d.args[0] == "" {
		p.warn(WarningInvalidSyntax, d.path, d.line, d.name, "directive requires exactly one non-empty value")
		return "", false
	}
	return d.args[0], true
}

func (p *parser) rejectDynamic(d directive, value string) bool {
	if !hasDynamicValue(value) {
		return false
	}
	p.warn(WarningDynamicValue, d.path, d.line, d.name, "token or command expansion cannot be evaluated offline")
	return true
}

func (p *parser) warn(code WarningCode, path string, line int, directive, detail string) {
	key := warningKey{code: code, path: path, line: line, directive: directive, detail: detail}
	if _, exists := p.warningSeen[key]; exists {
		return
	}
	p.warningSeen[key] = struct{}{}
	p.warnings = append(p.warnings, Warning{Code: code, Path: path, Line: line, Directive: directive, Detail: detail})
}

func parsePort(value string) (uint16, error) {
	port, err := strconv.ParseUint(value, 10, 16)
	if err != nil || port == 0 {
		return 0, fmt.Errorf("invalid port %q", value)
	}
	return uint16(port), nil
}

func parseProxyJump(value string) ([]JumpHost, error) {
	parts := strings.Split(value, ",")
	jumps := make([]JumpHost, 0, len(parts))
	for _, part := range parts {
		if part == "" || strings.ContainsAny(part, " \t\r\n") {
			return nil, fmt.Errorf("invalid ProxyJump value %q", value)
		}
		jump, err := parseJumpHost(part)
		if err != nil {
			return nil, err
		}
		jumps = append(jumps, jump)
	}
	return jumps, nil
}

func parseJumpHost(value string) (JumpHost, error) {
	jump := JumpHost{}
	hostPort := value
	if at := strings.LastIndexByte(value, '@'); at >= 0 {
		if at == 0 || at == len(value)-1 {
			return JumpHost{}, fmt.Errorf("invalid ProxyJump hop %q", value)
		}
		jump.User = value[:at]
		hostPort = value[at+1:]
	}
	if strings.HasPrefix(hostPort, "[") {
		end := strings.IndexByte(hostPort, ']')
		if end < 2 {
			return JumpHost{}, fmt.Errorf("invalid ProxyJump hop %q", value)
		}
		jump.Host = hostPort[1:end]
		rest := hostPort[end+1:]
		if rest != "" {
			if !strings.HasPrefix(rest, ":") || len(rest) == 1 {
				return JumpHost{}, fmt.Errorf("invalid ProxyJump hop %q", value)
			}
			port, err := parsePort(rest[1:])
			if err != nil {
				return JumpHost{}, err
			}
			jump.Port = port
		}
		return jump, nil
	}
	colonCount := strings.Count(hostPort, ":")
	if colonCount == 1 {
		host, portText, _ := strings.Cut(hostPort, ":")
		if host == "" || portText == "" {
			return JumpHost{}, fmt.Errorf("invalid ProxyJump hop %q", value)
		}
		port, err := parsePort(portText)
		if err != nil {
			return JumpHost{}, err
		}
		jump.Host = host
		jump.Port = port
	} else {
		jump.Host = hostPort
		if colonCount > 1 && net.ParseIP(hostPort) == nil {
			return JumpHost{}, fmt.Errorf("invalid ProxyJump hop %q", value)
		}
	}
	if jump.Host == "" {
		return JumpHost{}, fmt.Errorf("invalid ProxyJump hop %q", value)
	}
	return jump, nil
}

func expandTilde(path, home string) (string, bool) {
	if path == "~" {
		return home, home != ""
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home == "" {
			return "", false
		}
		return filepath.Join(home, path[2:]), true
	}
	if strings.HasPrefix(path, "~") {
		return "", false
	}
	return path, true
}

func canonicalPath(path string) string {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved
	}
	return absolute
}

func hasDynamicValue(value string) bool {
	return strings.Contains(value, "%") || strings.Contains(value, "${") || strings.Contains(value, "$(") || strings.Contains(value, "`")
}

func isSupportedDirective(name string) bool {
	switch name {
	case "hostname", "user", "port", "identityfile", "proxyjump", "proxycommand":
		return true
	default:
		return false
	}
}

// rawValue is everything after a directive's name and its separator, unsplit,
// which is how OpenSSH reads a ProxyCommand: its quotes and backslashes belong
// to the command.
func rawValue(line string) string {
	line = strings.TrimLeft(line, " \t")
	end := strings.IndexAny(line, " \t=")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(strings.TrimLeft(line[end:], " \t="))
}

func isDynamicCommand(name string) bool {
	switch name {
	case "knownhostscommand", "localcommand", "remotecommand":
		return true
	default:
		return false
	}
}
