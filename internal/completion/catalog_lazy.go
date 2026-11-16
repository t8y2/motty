package completion

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// A decoded catalog does not keep its command trees. Decode checks every record
// of every root once, then keeps only each root's name, aliases and
// description beside the text of its records, which point into the encoded
// bytes. The records are parsed again the first time something reads below
// that root, so a process that completes `git` and `docker` never builds the
// other 740 command trees. The built-in snapshot is embedded program data, so
// the text itself costs no heap.

// lazyTree is the records of one root command: its own N line through the line
// before the next root's.
type lazyTree struct {
	once  sync.Once
	text  string
	first int // the line number of the first record in the catalog body, for messages
}

// load builds the tree below n when n belongs to a decoded catalog that has not
// built it yet. Reading a node's Children, Flags or Persistent goes through it.
// It is safe to call from several goroutines.
func (n *Node) load() {
	root := n
	for root.parent != nil {
		root = root.parent
	}
	lazy := root.lazy
	if lazy == nil {
		return
	}
	lazy.once.Do(func() {
		// Decode parsed these exact records without an error.
		_, _, _, _ = parseTree(lazy.text, lazy.first, root, nil)
	})
}

// scan validates every record of body and keeps the roots as lazy stubs. It
// reports the totals it counted, and how many soft audit findings the nodes had.
func (c *Catalog) scan(body string) (nodes, flags, values, soft int, err error) {
	seen := make(map[string]struct{}, 8192)
	var report AuditReport
	line := 0
	for rest := body; rest != ""; {
		start, startLine := rest, line+1
		// The root's records run to the next depth 0 N line.
		consumed := 0
		for rest != "" {
			text, after, _ := strings.Cut(rest, "\n")
			if consumed > 0 && isRootLine(text) {
				break
			}
			consumed += len(text) + 1
			rest = after
			line++
		}
		segment := start[:min(consumed, len(start))]
		root := &Node{}
		check := &treeCheck{seen: seen, report: &report}
		n, f, v, perr := parseTree(segment, startLine, root, check)
		if perr != nil {
			return 0, 0, 0, 0, perr
		}
		nodes, flags, values = nodes+n, flags+f, values+v
		if nodes > maxCatalogNodes {
			return 0, 0, 0, 0, fmt.Errorf("completion: catalog has more than %d nodes", maxCatalogNodes)
		}
		stub := &Node{Token: root.Token, Aliases: root.Aliases, Desc: root.Desc, Hidden: root.Hidden,
			lazy: &lazyTree{text: segment, first: startLine}}
		c.roots = append(c.roots, stub)
		c.index[nodeKey([]string{stub.Token})] = stub
	}
	return nodes, flags, values, report.SoftCount, nil
}

func isRootLine(line string) bool {
	if len(line) < 3 || line[0] != 'N' || line[1] != '\t' {
		return false
	}
	depth, _, _ := strings.Cut(line[2:], "\t")
	value, err := strconv.Atoi(depth)
	return err == nil && value == 0
}

// splitTabs splits line at tabs into at most len(out) fields without
// allocating, and reports how many there are; a line with more fields than
// that reports len(out).
func splitTabs(line string, out *[8]string) int {
	count := 0
	for count < len(out) {
		field, rest, more := strings.Cut(line, "\t")
		out[count] = field
		count++
		if !more {
			return count
		}
		line = rest
	}
	return count
}

// parseTree parses the records of one root into root, which may be a blank node
// (Decode checking the records) or the stub of a lazy root (building its tree on
// first use; its own fields are already set and are not written again, since
// another goroutine may be reading them). check, when set, makes it a pure
// check: it keeps no options, and reports every path and description to check.
func parseTree(text string, firstLine int, root *Node, check *treeCheck) (nodes, flags, values int, err error) {
	stub := root.lazy != nil
	var stack []*Node
	var last *Node
	var fields [8]string
	number := firstLine - 1
	for rest := text; rest != ""; {
		var line string
		line, rest, _ = strings.Cut(rest, "\n")
		number++
		if line == "" {
			continue
		}
		count := splitTabs(line, &fields)
		switch fields[0] {
		case "N":
			if count != 6 {
				return 0, 0, 0, fmt.Errorf("completion: malformed node line %d", number)
			}
			depth, aerr := strconv.Atoi(fields[1])
			if aerr != nil || depth < 0 || depth > maxDepth {
				return 0, 0, 0, fmt.Errorf("completion: invalid node depth on line %d", number)
			}
			var n *Node
			if depth == 0 && len(stack) == 0 {
				n = root
			} else {
				n = &Node{}
			}
			if !(stub && n == root) {
				n.Token, n.Hidden, n.Desc = fields[2], fields[4] == "1", NormalizeText(fields[5], maxDescRunes)
				if fields[3] != "" {
					n.Aliases = strings.Split(fields[3], ",")
				}
			}
			// The digest only detects damage; a node name is typed into the
			// shell, so it must be as plain as the importer insists.
			if !validTokenPart(n.Token) {
				return 0, 0, 0, fmt.Errorf("completion: command name on line %d is not a plain token", number)
			}
			for _, alias := range n.Aliases {
				if !validTokenPart(alias) {
					return 0, 0, 0, fmt.Errorf("completion: command alias on line %d is not a plain token", number)
				}
			}
			if depth > len(stack) || (depth == 0 && len(stack) != 0) {
				return 0, 0, 0, fmt.Errorf("completion: inconsistent node depth on line %d", number)
			}
			stack = stack[:depth]
			if depth > 0 {
				parent := stack[depth-1]
				n.parent = parent
				parent.Children = append(parent.Children, n)
			}
			if check != nil {
				if err := check.node(n, depth); err != nil {
					return 0, 0, 0, err
				}
			}
			stack = append(stack, n)
			last = n
			nodes++
		case "F":
			if count != 7 || last == nil || stack[len(stack)-1] != last {
				return 0, 0, 0, fmt.Errorf("completion: malformed flag line %d", number)
			}
			f, ferr := decodeFlag(fields[2], fields[3], fields[4], fields[5], fields[6])
			if ferr != nil {
				return 0, 0, 0, fmt.Errorf("completion: line %d: %w", number, ferr)
			}
			switch fields[1] {
			case "f", "p":
			default:
				return 0, 0, 0, fmt.Errorf("completion: unknown flag kind %q on line %d", fields[1], number)
			}
			if check != nil {
				check.flag(f)
			} else if fields[1] == "f" {
				last.Flags = append(last.Flags, f)
			} else {
				last.Persistent = append(last.Persistent, f)
			}
			flags++
			values += len(f.Values)
		default:
			return 0, 0, 0, fmt.Errorf("completion: unknown catalog record %q on line %d", fields[0], number)
		}
	}
	if len(stack) == 0 {
		return 0, 0, 0, errors.New("completion: catalog root has no records")
	}
	return nodes, flags, values, nil
}

// treeCheck is what Decode learns from one root's records without keeping the
// options: that no command path repeats, and the soft audit findings.
type treeCheck struct {
	seen   map[string]struct{}
	report *AuditReport
	keys   []string // the path key of each node on the stack
}

func (c *treeCheck) node(n *Node, depth int) error {
	key := n.Token
	if depth > 0 {
		key = c.keys[depth-1] + "\x00" + n.Token
	}
	if _, dup := c.seen[key]; dup {
		return fmt.Errorf("completion: duplicate command path %q", strings.ReplaceAll(key, "\x00", " "))
	}
	c.seen[key] = struct{}{}
	c.keys = append(c.keys[:depth], key)
	auditDescription(c.report, n.Desc)
	return nil
}

func (c *treeCheck) flag(f Flag) {
	auditDescription(c.report, f.Desc)
	for _, value := range f.Values {
		auditDescription(c.report, value.Desc)
	}
}
