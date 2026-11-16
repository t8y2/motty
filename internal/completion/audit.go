package completion

import (
	"fmt"
	"strings"
	"unicode"
)

// The audit is the "scan" the feature can honestly offer: the catalog never
// runs anything, so malware scanning has no object, but content that a user
// would see or insert can still be spoofed or dangerous. Audit walks a ready
// catalog and reports:
//
//   - hard findings, which make a catalog unusable: a name that is not a plain
//     token, an unsanitized or oversized description (an invisible character
//     or a newline survived normalization), or an option value outside the
//     literal alphabet;
//   - soft findings, which are counted and shown but do not reject data: a
//     catalog shape far outside the usual Carapace window, or a description
//     mixing Latin with Cyrillic/Greek (a cheap homograph hint).
//
// Descriptions containing "://" are not reported: documentation links are
// normal in Carapace and flagging them would drown the real findings.
const (
	auditDetailLimit = 20

	auditMinRoots  = 100
	auditMaxRoots  = 5000
	auditMinFlags  = 10_000
	auditMaxFlags  = 200_000
	auditMaxValues = 10_000
)

// AuditReport is the result of one catalog audit. Counts are complete; the
// detail slices are capped at auditDetailLimit entries to keep an error string
// small even for a deliberately broken catalog.
type AuditReport struct {
	HardCount int
	SoftCount int
	Hard      []string
	Soft      []string
}

func (report *AuditReport) addHard(format string, args ...any) {
	report.HardCount++
	if len(report.Hard) < auditDetailLimit {
		report.Hard = append(report.Hard, fmt.Sprintf(format, args...))
	}
}

func (report *AuditReport) addSoft(format string, args ...any) {
	report.SoftCount++
	if len(report.Soft) < auditDetailLimit {
		report.Soft = append(report.Soft, fmt.Sprintf(format, args...))
	}
}

// Err returns nil when the catalog has no hard findings, and otherwise an
// error that names a bounded sample of them.
func (report AuditReport) Err() error {
	if report.HardCount == 0 {
		return nil
	}
	return fmt.Errorf("completion: catalog audit rejected %d invalid item(s): %s", report.HardCount, strings.Join(report.Hard, "; "))
}

// Audit scans every command, option and literal value of a catalog.
func Audit(c *Catalog) AuditReport {
	var report AuditReport
	if c == nil {
		report.addHard("catalog is nil")
		return report
	}
	if c.nodeCount > maxCatalogNodes {
		report.addHard("catalog has %d nodes, more than %d", c.nodeCount, maxCatalogNodes)
	}
	auditShape(&report, c)
	for _, root := range c.roots {
		auditNode(&report, root, 1)
	}
	return report
}

// auditShape reports the soft findings about the size of the whole catalog.
func auditShape(report *AuditReport, c *Catalog) {
	switch {
	case c.RootCount() < auditMinRoots:
		report.addSoft("catalog has only %d root commands", c.RootCount())
	case c.RootCount() > auditMaxRoots:
		report.addSoft("catalog has %d root commands", c.RootCount())
	}
	if c.flagCount < auditMinFlags || c.flagCount > auditMaxFlags {
		report.addSoft("catalog has %d options", c.flagCount)
	}
	if c.valueCount > auditMaxValues {
		report.addSoft("catalog has %d literal values", c.valueCount)
	}
}

// auditSoft counts the soft findings of a catalog that Decode has just built.
// Decode already refused, record by record, every condition Audit reports as
// hard (names, aliases, depth, value count, value alphabet) and normalized every
// description, so repeating those checks over 50,000 options would only slow
// down loading the built-in catalog. Only the findings that need the whole tree
// remain.
func auditSoft(c *Catalog) AuditReport {
	var report AuditReport
	auditShape(&report, c)
	for _, root := range c.roots {
		auditSoftTree(&report, root)
	}
	return report
}

// auditSoftTree adds the soft findings of one command tree to report.
func auditSoftTree(report *AuditReport, root *Node) {
	walk(root, func(n *Node) {
		auditDescription(report, n.Desc)
		for _, flags := range [2][]Flag{n.Flags, n.Persistent} {
			for _, flag := range flags {
				auditDescription(report, flag.Desc)
				for _, value := range flag.Values {
					auditDescription(report, value.Desc)
				}
			}
		}
	})
}

func auditNode(report *AuditReport, n *Node, depth int) {
	if n == nil {
		report.addHard("catalog contains a nil command")
		return
	}
	n.load()
	if depth > maxDepth {
		report.addHard("command %q nests deeper than %d levels", n.Token, maxDepth)
	}
	if !validTokenPart(n.Token) {
		report.addHard("command name %q is not a plain token", n.Token)
	}
	if !isNormalized(n.Desc, maxDescRunes) {
		report.addHard("command %q has an unsanitized or oversized description", n.Token)
	}
	auditDescription(report, n.Desc)
	for _, alias := range n.Aliases {
		if !validTokenPart(alias) {
			report.addHard("command %q has an alias that is not a plain token", n.Token)
		}
	}
	auditFlags(report, n.Token, n.Flags)
	auditFlags(report, n.Token, n.Persistent)
	for _, child := range n.Children {
		auditNode(report, child, depth+1)
	}
}

func auditFlags(report *AuditReport, command string, flags []Flag) {
	for _, flag := range flags {
		if flag.Long == "" && flag.Short == "" {
			report.addHard("command %q has an option without a name", command)
		}
		if (flag.Long != "" && !validTokenPart(flag.Long)) || (flag.Short != "" && !validTokenPart(flag.Short)) {
			report.addHard("command %q has an option name that is not a plain token", command)
		}
		if !isNormalized(flag.Desc, maxDescRunes) {
			report.addHard("option %q has an unsanitized or oversized description", flag.Token())
		}
		auditDescription(report, flag.Desc)
		if len(flag.Values) > maxValuesPerFlag {
			report.addHard("option %q has too many literal values", flag.Token())
		}
		for _, value := range flag.Values {
			if !SafeValue(value.Text) {
				report.addHard("option %q has an unsafe value %q", flag.Token(), value.Text)
			}
			if !isNormalized(value.Desc, maxDescRunes) {
				report.addHard("option %q has an unsanitized or oversized value description", flag.Token())
			}
			auditDescription(report, value.Desc)
		}
	}
}

// auditDescription reports the soft homograph hint separately from the hard
// normalization checks; it never rejects text.
func auditDescription(report *AuditReport, text string) {
	if mixedScripts(text) {
		report.addSoft("description mixes Latin with Cyrillic or Greek text")
	}
}

// mixedScripts reports whether text contains Latin letters together with
// Cyrillic or Greek letters, which is how a homograph description is usually
// built.
func mixedScripts(text string) bool {
	var latin, confusable bool
	for _, r := range text {
		switch {
		case r < 0x80:
			// ASCII letters are Latin; nothing else in ASCII is a letter.
			latin = latin || (r|0x20 >= 'a' && r|0x20 <= 'z')
		case unicode.Is(unicode.Latin, r):
			latin = true
		case unicode.Is(unicode.Cyrillic, r), unicode.Is(unicode.Greek, r):
			confusable = true
		}
	}
	return latin && confusable
}
