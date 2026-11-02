package sshconfig

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// The block Motty keeps in the configuration is fenced by two comment lines, so
// it can be found again, and holds ordinary Host blocks: the user may edit
// them, and what they change comes back into Motty. Everything outside the
// fence is the user's and is only ever edited where the user already put a
// Host block of exactly one name.
const (
	managedBegin       = "# --- motty: begin synced hosts (edits here sync back into Motty) ---"
	managedEnd         = "# --- motty: end synced hosts ---"
	managedBeginPrefix = "# --- motty: begin"
	managedEndPrefix   = "# --- motty: end"
)

var (
	// ErrShared means the alias is only named by blocks that name other hosts
	// too, which cannot be edited for one host without changing the others.
	ErrShared = errors.New("host is named by a block shared with other hosts")
	// ErrExists means a host of that name is already in the configuration.
	ErrExists = errors.New("a host of that name already exists")
	// ErrNotManaged means a host asked to be removed was written by the user.
	ErrNotManaged = errors.New("host is not in the block Motty keeps")
	// ErrMarkers means the fence of the block Motty keeps is damaged, so nothing
	// can safely be added to it.
	ErrMarkers = errors.New("the markers of the block Motty keeps are damaged")
)

// Ownership says where a host's block is.
type Ownership uint8

const (
	// Absent: no block names the host.
	Absent Ownership = iota
	// Managed: the host has a block of its own inside the fence Motty keeps.
	Managed
	// Own: the host has a block of its own that the user wrote.
	Own
	// Shared: the host is only named by blocks that name others too.
	Shared
)

// Edit says what to change in the block of one host. A nil field is left as it
// is; an empty value, or a zero port, removes the directive.
type Edit struct {
	Alias     string
	HostName  *string
	User      *string
	Port      *uint16
	ProxyJump *string
}

// block is one Host block and where it lives.
type block struct {
	d *doc
	section
	managed bool
}

type region struct {
	d          *doc
	begin, end int
}

func (c *Config) region() (region, bool, error) {
	for _, path := range c.order {
		d := c.docs[path]
		begin := -1
		for index, line := range d.lines {
			text := strings.TrimSpace(line.text)
			switch {
			case begin < 0 && strings.HasPrefix(text, managedBeginPrefix):
				begin = index
			case begin >= 0 && strings.HasPrefix(text, managedEndPrefix):
				return region{d: d, begin: begin, end: index}, true, nil
			}
		}
		if begin >= 0 {
			return region{}, false, fmt.Errorf("%w: %s has no end line", ErrMarkers, d.path)
		}
	}
	return region{}, false, nil
}

// sectionsOf is the sections of a file, with the ones next to the fence cut
// short at it, so a block never reaches across the fence.
func (c *Config) sectionsOf(d *doc, fence region, fenced bool) []section {
	sections := d.sections()
	if !fenced || fence.d != d {
		return sections
	}
	sections = slices.Clone(sections)
	for index := range sections {
		section := &sections[index]
		switch {
		case section.header < fence.begin:
			section.end = min(section.end, fence.begin)
		case section.header < fence.end:
			section.end = min(section.end, fence.end)
		}
	}
	return sections
}

func plainAlias(args []string) (string, bool) {
	if len(args) != 1 || strings.ContainsAny(args[0], ",*?!") {
		return "", false
	}
	return args[0], true
}

func (c *Config) ownBlocks(alias string) []block {
	fence, fenced, _ := c.region()
	var found []block
	for _, path := range c.order {
		d := c.docs[path]
		for _, section := range c.sectionsOf(d, fence, fenced) {
			if section.match {
				continue
			}
			if name, ok := plainAlias(section.args); ok && equalFold(name, alias) {
				managed := fenced && fence.d == d && section.header > fence.begin && section.header < fence.end
				found = append(found, block{d: d, section: section, managed: managed})
			}
		}
	}
	return found
}

func (c *Config) named(alias string) bool {
	for _, path := range c.order {
		for _, section := range c.docs[path].sections() {
			if section.match {
				continue
			}
			for _, argument := range section.args {
				for _, pattern := range strings.Split(argument, ",") {
					if !strings.HasPrefix(pattern, "!") && equalFold(pattern, alias) {
						return true
					}
				}
			}
		}
	}
	return false
}

// Ownership says where alias lives.
func (c *Config) Ownership(alias string) Ownership {
	if blocks := c.ownBlocks(alias); len(blocks) > 0 {
		if blocks[0].managed {
			return Managed
		}
		return Own
	}
	if c.named(alias) {
		return Shared
	}
	return Absent
}

// Apply makes the edit. A host no block names is added to the block Motty
// keeps, created if need be; a host with a block of its own is edited where it
// is; a host only named by shared blocks is ErrShared and untouched.
func (c *Config) Apply(edit Edit) error {
	blocks := c.ownBlocks(edit.Alias)
	if len(blocks) == 0 {
		if c.named(edit.Alias) {
			return ErrShared
		}
		return c.create(edit)
	}
	if edit.HostName != nil {
		if err := c.setDirective(edit.Alias, "HostName", *edit.HostName); err != nil {
			return err
		}
	}
	if edit.User != nil {
		if err := c.setDirective(edit.Alias, "User", *edit.User); err != nil {
			return err
		}
	}
	if edit.Port != nil {
		value := ""
		if *edit.Port != 0 {
			value = strconv.Itoa(int(*edit.Port))
		}
		if err := c.setDirective(edit.Alias, "Port", value); err != nil {
			return err
		}
	}
	if edit.ProxyJump != nil {
		if err := c.setDirective(edit.Alias, "ProxyJump", *edit.ProxyJump); err != nil {
			return err
		}
	}
	c.stale = true
	return nil
}

func (c *Config) setDirective(alias, key, value string) error {
	if value != "" && hasDynamicValue(value) {
		return fmt.Errorf("%s value %q cannot be written", key, value)
	}
	blocks := c.ownBlocks(alias)
	if len(blocks) == 0 {
		return ErrShared
	}
	want := strings.ToLower(key)
	for _, own := range blocks {
		for _, found := range own.d.directives(own.header, own.end) {
			if found.name != want {
				continue
			}
			switch {
			case value == "":
				own.d.remove(found.index, found.index+1)
			case len(found.args) == 1 && found.args[0] == value:
			default:
				own.d.setText(found.index, replaceValue(own.d.lines[found.index].text, value))
			}
			return nil
		}
	}
	if value == "" {
		return nil
	}
	own := blocks[0]
	found := own.d.directives(own.header, own.end)
	at, indent := own.header+1, "    "
	if len(found) > 0 {
		at = found[len(found)-1].index + 1
		if space := leadingSpace(own.d.lines[found[0].index].text); space != "" {
			indent = space
		}
	}
	own.d.insert(at, indent+key+" "+quoteArg(value))
	return nil
}

func (c *Config) create(edit Edit) error {
	if !ValidAlias(edit.Alias) {
		return fmt.Errorf("%q cannot be a Host name", edit.Alias)
	}
	if edit.HostName == nil || *edit.HostName == "" {
		return errors.New("a new host needs a HostName")
	}
	lines := []string{"Host " + edit.Alias}
	add := func(key, value string) error {
		if value == "" {
			return nil
		}
		if hasDynamicValue(value) {
			return fmt.Errorf("%s value %q cannot be written", key, value)
		}
		lines = append(lines, "    "+key+" "+quoteArg(value))
		return nil
	}
	if err := add("HostName", *edit.HostName); err != nil {
		return err
	}
	if edit.User != nil {
		if err := add("User", *edit.User); err != nil {
			return err
		}
	}
	if edit.Port != nil && *edit.Port != 0 {
		if err := add("Port", strconv.Itoa(int(*edit.Port))); err != nil {
			return err
		}
	}
	if edit.ProxyJump != nil {
		if err := add("ProxyJump", *edit.ProxyJump); err != nil {
			return err
		}
	}
	fence, fenced, err := c.region()
	if err != nil {
		return err
	}
	if fenced {
		at := fence.end
		if at-1 > fence.begin && !isBlank(fence.d.lines[at-1].text) {
			lines = append([]string{""}, lines...)
		}
		fence.d.insert(at, lines...)
		c.stale = true
		return nil
	}
	root := c.docs[c.root]
	at := placement(root)
	fenceLines := append(append([]string{managedBegin}, lines...), managedEnd)
	if at > 0 && !isBlank(root.lines[at-1].text) {
		fenceLines = append([]string{""}, fenceLines...)
	}
	if at < len(root.lines) {
		fenceLines = append(fenceLines, "")
	}
	root.insert(at, fenceLines...)
	c.stale = true
	return nil
}

// placement is where the fence goes in a new file of blocks: before the first
// block that is general (a Match, or a Host with a pattern), with the comments
// directly above it, because OpenSSH uses the first value it finds and Motty's
// specific hosts must come before the defaults; or at the end.
func placement(d *doc) int {
	for _, section := range d.sections() {
		general := section.match
		for _, argument := range section.args {
			if strings.ContainsAny(argument, "*?!") {
				general = true
			}
		}
		if !general {
			continue
		}
		at := section.header
		for at > 0 && isComment(d.lines[at-1].text) {
			at--
		}
		return at
	}
	return len(d.lines)
}

// Rename changes the name of a host with a block of its own.
func (c *Config) Rename(from, to string) error {
	if !ValidAlias(to) {
		return fmt.Errorf("%q cannot be a Host name", to)
	}
	blocks := c.ownBlocks(from)
	if len(blocks) == 0 {
		if c.named(from) {
			return ErrShared
		}
		return fmt.Errorf("no host called %q", from)
	}
	if !equalFold(from, to) && c.Ownership(to) != Absent {
		return ErrExists
	}
	header := blocks[0].header
	blocks[0].d.setText(header, replaceValue(blocks[0].d.lines[header].text, to))
	c.stale = true
	return nil
}

// Remove deletes the block of a host from the block Motty keeps. It reports
// whether there was one. A host the user wrote is ErrNotManaged and untouched.
func (c *Config) Remove(alias string) (bool, error) {
	blocks := c.ownBlocks(alias)
	if len(blocks) == 0 {
		return false, nil
	}
	if !blocks[0].managed {
		return false, ErrNotManaged
	}
	fence, _, err := c.region()
	if err != nil {
		return false, err
	}
	own := blocks[0]
	fence.d.remove(own.header, own.end)
	fence, _, _ = c.region()
	// The blank line that parted the block from the one before it is left
	// hanging at the end when the last block goes.
	for fence.end-1 > fence.begin && isBlank(fence.d.lines[fence.end-1].text) {
		fence.d.remove(fence.end-1, fence.end)
		fence.end--
	}
	if !holdsHosts(fence) {
		from, to := fence.begin, fence.end+1
		if from > 0 && isBlank(fence.d.lines[from-1].text) {
			from--
		}
		fence.d.remove(from, to)
	}
	c.stale = true
	return true, nil
}

func holdsHosts(fence region) bool {
	for _, section := range fence.d.sections() {
		if section.header > fence.begin && section.header < fence.end {
			return true
		}
	}
	return false
}

// ValidAlias reports whether text can be written as the name of a Host block
// and read back as that one name.
func ValidAlias(text string) bool {
	if text == "" || text[0] == '-' {
		return false
	}
	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("*?!,%#'\"\\=$`", r) {
			return false
		}
	}
	return true
}

// AliasFor turns any text, such as a host's label, into a valid alias: runs of
// characters a Host name cannot hold become one hyphen. It is empty when
// nothing usable is left.
func AliasFor(text string) string {
	var out strings.Builder
	pending := false
	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("*?!,%#'\"\\=$`", r) {
			pending = out.Len() > 0
			continue
		}
		if pending {
			out.WriteByte('-')
			pending = false
		}
		out.WriteRune(r)
	}
	alias := strings.TrimLeft(out.String(), "-")
	if !ValidAlias(alias) {
		return ""
	}
	return alias
}

// FormatJump writes one ProxyJump hop as OpenSSH reads it.
func FormatJump(jump JumpHost) string {
	host := jump.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if jump.Port != 0 {
		host += ":" + strconv.Itoa(int(jump.Port))
	}
	if jump.User != "" {
		host = jump.User + "@" + host
	}
	return host
}

// FormatJumps writes a ProxyJump value: its hops, in order, apart by commas.
func FormatJumps(jumps []JumpHost) string {
	parts := make([]string, len(jumps))
	for index, jump := range jumps {
		parts[index] = FormatJump(jump)
	}
	return strings.Join(parts, ",")
}

// quoteArg writes a value so that reading the line back gives the value.
func quoteArg(value string) string {
	if !strings.ContainsAny(value, " \t\"'#\\") {
		return value
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

// replaceValue puts value in the place of the value of a directive line,
// leaving the key, the way it was spaced and any comment after it.
func replaceValue(line, value string) string {
	i := 0
	space := func() {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
	}
	space()
	for i < len(line) && line[i] != ' ' && line[i] != '\t' && line[i] != '=' {
		i++
	}
	space()
	if i < len(line) && line[i] == '=' {
		i++
		space()
	}
	start := i
	end := commentStart(line, start)
	for end > start && (line[end-1] == ' ' || line[end-1] == '\t') {
		end--
	}
	return line[:start] + quoteArg(value) + line[end:]
}

// commentStart is where a comment begins in line at or after from, or the end.
func commentStart(line string, from int) int {
	var quote byte
	for i := from; i < len(line); i++ {
		switch character := line[i]; {
		case character == '\\' && quote != '\'':
			if i+1 < len(line) && escapes(line[i+1], quote) {
				i++
			}
		case quote != 0:
			if character == quote {
				quote = 0
			}
		case character == '\'' || character == '"':
			quote = character
		case character == '#':
			return i
		}
	}
	return len(line)
}

func equalFold(a, b string) bool { return strings.EqualFold(a, b) }
