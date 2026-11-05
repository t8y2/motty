// Package completion holds an offline catalog of command syntax: commands,
// subcommands, options and a small set of literal option values. It is plain
// data, built from a pinned Carapace release, and never runs anything from the
// source it was imported from.
//
// The catalog is deliberately independent of the workbench: it answers "what
// could this command line continue into?" without knowing about sessions,
// history or the screen. The workbench decides whether a question may be asked
// at all and merges the answer with what the user typed before.
package completion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unsafe"
)

// FormatVersion is the layout of the encoded catalog. A cache written by an
// older reader is ignored rather than guessed at.
const FormatVersion = 1

// Flag is one option of a command. Long and Short may both be set; at least one
// is non-empty. Value reports whether the option consumes a value, and Optional
// whether that value may be omitted (Carapace's '?' modifier).
type Flag struct {
	Long       string
	Short      string
	Value      bool
	Optional   bool
	Repeatable bool
	Required   bool
	Desc       string
	// Values are literal values accepted for the option, in source order.
	// Dynamic completions (paths, branches, containers) are never imported.
	Values []Value
}

// Token is the option as it should be typed, preferring the long form.
func (f Flag) Token() string {
	if f.Long != "" {
		return "--" + f.Long
	}
	return "-" + f.Short
}

// ShortToken is the short form, or "" when the option has none.
func (f Flag) ShortToken() string {
	if f.Short == "" {
		return ""
	}
	return "-" + f.Short
}

// Value is one literal option value with its optional description.
type Value struct {
	Text string
	Desc string
}

// Node is one command or subcommand. Children are sorted by token; aliases are
// additional tokens that reach the same node.
type Node struct {
	Token      string
	Aliases    []string
	Desc       string
	Hidden     bool
	Flags      []Flag
	Persistent []Flag
	Children   []*Node

	parent *Node
	lazy   *lazyTree // a root of a decoded catalog whose tree is built on first use
}

// Path returns the tokens from the root command to this node.
func (n *Node) Path() []string {
	depth := 0
	for p := n; p != nil; p = p.parent {
		depth++
	}
	path := make([]string, depth)
	for p := n; p != nil; p = p.parent {
		depth--
		path[depth] = p.Token
	}
	return path
}

// Parent returns the parent node, or nil for a root command.
func (n *Node) Parent() *Node { return n.parent }

// Catalog is an immutable, validated command-syntax snapshot.
type Catalog struct {
	Source  string
	Release string
	Commit  string
	License string

	roots []*Node
	index map[string]*Node // eager catalogs: every node; decoded ones: the roots

	nodeCount  int
	flagCount  int
	valueCount int

	// auditWarnings is the number of soft findings the content audit reported
	// when this catalog was imported or decoded.
	auditWarnings int
	importStats   *ImportStats
}

// NewCatalog assembles a catalog from root nodes and derives the lookup index.
// It is used by the importer and the cache loader; the result must not be
// mutated afterwards.
func NewCatalog(source, release, commit, license string, roots []*Node) (*Catalog, error) {
	c := &Catalog{
		Source:  source,
		Release: release,
		Commit:  commit,
		License: license,
		index:   make(map[string]*Node, 8192),
	}
	sortNodes(roots)
	for _, root := range roots {
		if err := c.indexTree(root, nil); err != nil {
			return nil, err
		}
	}
	if len(c.roots) == 0 {
		return nil, errors.New("completion: catalog has no root commands")
	}
	c.count()
	return c, nil
}

func sortNodes(nodes []*Node) {
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Token < nodes[j].Token })
	for _, n := range nodes {
		sort.Strings(n.Aliases)
		n.Aliases = compactStrings(n.Aliases)
		sort.Slice(n.Flags, func(i, j int) bool { return n.Flags[i].Token() < n.Flags[j].Token() })
		sort.Slice(n.Persistent, func(i, j int) bool { return n.Persistent[i].Token() < n.Persistent[j].Token() })
		sortNodes(n.Children)
	}
}

func compactStrings(in []string) []string {
	out := in[:0]
	var last string
	for i, s := range in {
		if s == "" || (i > 0 && s == last) {
			continue
		}
		out = append(out, s)
		last = s
	}
	return out
}

func (c *Catalog) indexTree(n *Node, parent *Node) error {
	if n.Token == "" || strings.ContainsAny(n.Token, " \t\r\n") {
		return fmt.Errorf("completion: invalid command token %q", n.Token)
	}
	n.parent = parent
	key := nodeKey(n.Path())
	if _, ok := c.index[key]; ok {
		return fmt.Errorf("completion: duplicate command path %q", strings.Join(n.Path(), " "))
	}
	c.index[key] = n
	if parent == nil {
		c.roots = append(c.roots, n)
	}
	for _, child := range n.Children {
		if err := c.indexTree(child, n); err != nil {
			return err
		}
	}
	return nil
}

func (c *Catalog) count() {
	c.nodeCount, c.flagCount, c.valueCount = 0, 0, 0
	for _, root := range c.roots {
		walk(root, func(n *Node) {
			c.nodeCount++
			c.flagCount += len(n.Flags) + len(n.Persistent)
			for _, f := range n.Flags {
				c.valueCount += len(f.Values)
			}
			for _, f := range n.Persistent {
				c.valueCount += len(f.Values)
			}
		})
	}
}

func nodeKey(path []string) string { return strings.Join(path, "\x00") }

// NodeCount reports how many commands and subcommands the catalog holds.
func (c *Catalog) NodeCount() int { return c.nodeCount }

// RootCount reports how many root commands were validated during import. This
// is the number shown to the user, never a file or directory count.
func (c *Catalog) RootCount() int { return len(c.roots) }

// FlagCount is how many options the catalog carries.
func (c *Catalog) FlagCount() int { return c.flagCount }

// ValueCount reports how many literal option values are available.
func (c *Catalog) ValueCount() int { return c.valueCount }

// AuditWarnings reports the soft findings of the content audit that accepted
// this catalog. Hard findings never produce a usable catalog, so they are not
// counted here.
func (c *Catalog) AuditWarnings() int {
	if c == nil {
		return 0
	}
	return c.auditWarnings
}

// ImportStats returns the persisted import counters. Older catalogs have no
// counters; unknown is distinct from an import that repaired or skipped zero.
func (c *Catalog) ImportStats() (ImportStats, bool) {
	if c == nil || c.importStats == nil {
		return ImportStats{}, false
	}
	return *c.importStats, true
}

// Roots returns the root commands in name order.
func (c *Catalog) Roots() []*Node { return c.roots }

// Lookup returns the node at path, or nil.
func (c *Catalog) Lookup(path []string) *Node {
	if len(path) == 0 {
		return nil
	}
	node := c.index[nodeKey(path[:1])]
	for _, token := range path[1:] {
		if node == nil {
			return nil
		}
		node.load()
		var next *Node
		for _, child := range node.Children {
			if child.Token == token {
				next = child
				break
			}
		}
		node = next
	}
	return node
}

// Root resolves a root command token or alias.
func (c *Catalog) Root(token string) *Node {
	for _, n := range c.roots {
		if n.Token == token {
			return n
		}
		for _, alias := range n.Aliases {
			if alias == token {
				return n
			}
		}
	}
	return nil
}

// VisibleFlags returns the options of a node including persistent options
// inherited from its ancestors, in a stable order and without duplicates.
func (c *Catalog) VisibleFlags(node *Node) []Flag {
	if node == nil {
		return nil
	}
	node.load()
	var chain []*Node
	for n := node; n != nil; n = n.parent {
		chain = append(chain, n)
	}
	seen := make(map[string]bool)
	var flags []Flag
	add := func(f Flag) {
		key := f.Token()
		if seen[key] {
			return
		}
		seen[key] = true
		flags = append(flags, f)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		for _, f := range chain[i].Persistent {
			add(f)
		}
	}
	for _, f := range node.Flags {
		add(f)
	}
	for _, f := range node.Persistent {
		add(f)
	}
	sort.SliceStable(flags, func(i, j int) bool { return flags[i].Token() < flags[j].Token() })
	return flags
}

// FindFlag resolves a typed option token ("--long", "-s", without a value part)
// among the options visible at node.
func (c *Catalog) FindFlag(node *Node, token string) (Flag, bool) {
	for _, f := range c.VisibleFlags(node) {
		if f.Long != "" && token == "--"+f.Long {
			return f, true
		}
		if f.Short != "" && token == "-"+f.Short {
			return f, true
		}
	}
	return Flag{}, false
}

// Encode serializes the catalog. The result is deterministic for the same
// catalog, so generation can be checked byte for byte.
func (c *Catalog) Encode() []byte {
	var body strings.Builder
	for _, root := range c.roots {
		encodeNode(&body, root, 0)
	}
	bodyBytes := []byte(body.String())
	sum := sha256.Sum256(bodyBytes)

	var out strings.Builder
	fmt.Fprintf(&out, "motty-completion\t%d\n", FormatVersion)
	fmt.Fprintf(&out, "source\t%s\n", c.Source)
	fmt.Fprintf(&out, "release\t%s\n", c.Release)
	fmt.Fprintf(&out, "commit\t%s\n", c.Commit)
	fmt.Fprintf(&out, "license\t%s\n", c.License)
	fmt.Fprintf(&out, "roots\t%d\n", len(c.roots))
	fmt.Fprintf(&out, "nodes\t%d\n", c.nodeCount)
	fmt.Fprintf(&out, "flags\t%d\n", c.flagCount)
	fmt.Fprintf(&out, "values\t%d\n", c.valueCount)
	if stats, known := c.ImportStats(); known {
		data, _ := json.Marshal(stats)
		fmt.Fprintf(&out, "import-stats\t%s\n", data)
	}
	fmt.Fprintf(&out, "digest\t%s\n\n", hex.EncodeToString(sum[:]))
	out.Write(bodyBytes)
	return []byte(out.String())
}

func encodeNode(b *strings.Builder, n *Node, depth int) {
	n.load()
	fmt.Fprintf(b, "N\t%d\t%s\t%s\t%d\t%s\n", depth, n.Token, strings.Join(n.Aliases, ","), boolInt(n.Hidden), n.Desc)
	for _, f := range n.Flags {
		encodeFlag(b, "f", f)
	}
	for _, f := range n.Persistent {
		encodeFlag(b, "p", f)
	}
	for _, child := range n.Children {
		encodeNode(b, child, depth+1)
	}
}

func encodeFlag(b *strings.Builder, kind string, f Flag) {
	mods := ""
	if f.Value {
		mods += "v"
	}
	if f.Optional {
		mods += "o"
	}
	if f.Repeatable {
		mods += "r"
	}
	if f.Required {
		mods += "q"
	}
	values := make([]string, len(f.Values))
	for i, v := range f.Values {
		values[i] = v.Text + "\x02" + v.Desc
	}
	fmt.Fprintf(b, "F\t%s\t%s\t%s\t%s\t%s\t%s\n", kind, f.Long, f.Short, mods, f.Desc, strings.Join(values, "\x1f"))
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Decode parses and validates an encoded catalog. Every bound is enforced here
// as well as in the importer: a cache file is untrusted input.
func Decode(data []byte) (*Catalog, error) {
	// The catalog keeps strings that point into data, so data must stay
	// unmodified for as long as the catalog lives. The built-in snapshot is
	// read-only program data; a cache file is read once and not reused.
	var whole string
	if len(data) > 0 {
		whole = unsafe.String(&data[0], len(data))
	}
	headerEnd := strings.Index(whole, "\n\n")
	if headerEnd < 0 {
		return nil, errors.New("completion: truncated catalog header")
	}
	header := whole[:headerEnd]
	text := whole[headerEnd+2:]
	body := data[headerEnd+2:]

	meta := make(map[string]string, 10)
	for _, line := range strings.Split(header, "\n") {
		key, value, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("completion: malformed catalog header line %q", line)
		}
		if _, dup := meta[key]; dup {
			return nil, fmt.Errorf("completion: duplicate catalog header %q", key)
		}
		meta[key] = value
	}
	if meta["motty-completion"] != strconv.Itoa(FormatVersion) {
		return nil, fmt.Errorf("completion: unsupported catalog format %q", meta["motty-completion"])
	}
	want := make(map[string]int, 4)
	for _, key := range []string{"roots", "nodes", "flags", "values"} {
		n, err := strconv.Atoi(meta[key])
		if err != nil || n < 0 {
			return nil, fmt.Errorf("completion: invalid catalog %s count %q", key, meta[key])
		}
		want[key] = n
	}
	sum := sha256.Sum256(body)
	if meta["digest"] != hex.EncodeToString(sum[:]) {
		return nil, errors.New("completion: catalog checksum mismatch")
	}
	if meta["source"] == "" || meta["commit"] == "" || meta["license"] == "" {
		return nil, errors.New("completion: catalog metadata is incomplete")
	}

	c := &Catalog{
		Source:  meta["source"],
		Release: meta["release"],
		Commit:  meta["commit"],
		License: meta["license"],
		index:   make(map[string]*Node, 1024),
	}
	nodes, flags, values, soft, err := c.scan(text)
	if err != nil {
		return nil, err
	}
	if nodes != want["nodes"] || len(c.roots) != want["roots"] || flags != want["flags"] || values != want["values"] {
		return nil, errors.New("completion: catalog counts do not match the header")
	}
	if len(c.roots) == 0 {
		return nil, errors.New("completion: catalog has no root commands")
	}
	c.nodeCount, c.flagCount, c.valueCount = nodes, flags, values
	shape := AuditReport{}
	auditShape(&shape, c)
	c.auditWarnings = soft + shape.SoftCount
	if data, present := meta["import-stats"]; present {
		var stats ImportStats
		if err := json.Unmarshal([]byte(data), &stats); err != nil {
			return nil, fmt.Errorf("completion: invalid import statistics: %w", err)
		}
		if !c.validImportStats(stats) {
			return nil, errors.New("completion: inconsistent import statistics")
		}
		c.importStats = &stats
	}
	return c, nil
}

func (c *Catalog) validImportStats(stats ImportStats) bool {
	for _, count := range []int{
		stats.Files, stats.Roots, stats.Nodes, stats.Flags, stats.Values,
		stats.SkippedFiles, stats.SkippedCommands, stats.SkippedFlags,
		stats.SkippedValues, stats.HiddenFlags, stats.RootlessDirs,
		stats.RepairedFiles, stats.SkippedSources, stats.AuditWarnings,
	} {
		if count < 0 {
			return false
		}
	}
	return stats.Files > 0 && stats.SkippedSources <= stats.Files &&
		stats.RepairedFiles <= stats.Files-stats.SkippedSources &&
		stats.Roots == c.RootCount() && stats.Nodes == c.NodeCount() &&
		stats.Flags == c.FlagCount() && stats.Values == c.ValueCount()
}

func decodeFlag(long, short, mods, desc, encodedValues string) (Flag, error) {
	f := Flag{Long: long, Short: short, Desc: NormalizeText(desc, maxDescRunes)}
	if f.Long == "" && f.Short == "" {
		return Flag{}, errors.New("flag has neither a long nor a short name")
	}
	if (f.Long != "" && !validTokenPart(f.Long)) || (f.Short != "" && !validTokenPart(f.Short)) {
		return Flag{}, errors.New("flag name is not a plain token")
	}
	for _, mod := range mods {
		switch mod {
		case 'v':
			f.Value = true
		case 'o':
			f.Optional = true
		case 'r':
			f.Repeatable = true
		case 'q':
			f.Required = true
		default:
			return Flag{}, fmt.Errorf("unknown flag modifier %q", mod)
		}
	}
	if f.Optional {
		f.Value = true
	}
	if encodedValues != "" {
		for _, part := range strings.Split(encodedValues, "\x1f") {
			text, valueDesc, _ := strings.Cut(part, "\x02")
			if !SafeValue(text) {
				return Flag{}, fmt.Errorf("unsafe option value %q", text)
			}
			f.Values = append(f.Values, Value{Text: text, Desc: NormalizeText(valueDesc, maxDescRunes)})
		}
		if len(f.Values) > maxValuesPerFlag {
			return Flag{}, errors.New("too many option values")
		}
	}
	return f, nil
}
