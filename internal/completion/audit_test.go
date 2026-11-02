package completion

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// TestBuiltinSnapshotIsClean is the regression that keeps the shipped data
// honest: the embedded catalog must pass the same content audit as a download,
// and it must carry no invisible characters in any description.
func TestBuiltinSnapshotIsClean(t *testing.T) {
	catalog, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	report := Audit(catalog)
	if report.HardCount != 0 || len(report.Hard) != 0 {
		t.Fatalf("built-in catalog has hard audit findings: %+v", report)
	}
	if report.SoftCount != 0 {
		t.Fatalf("built-in catalog has %d soft audit findings: %+v", report.SoftCount, report.Soft)
	}
	if warnings := catalog.AuditWarnings(); warnings != 0 {
		t.Fatalf("built-in catalog reports %d audit warnings", warnings)
	}
	for _, root := range catalog.Roots() {
		walk(root, func(n *Node) {
			checkSanitizedDescription(t, "command "+n.Token, n.Desc)
			for _, flag := range append(append([]Flag(nil), n.Flags...), n.Persistent...) {
				checkSanitizedDescription(t, "option "+flag.Token(), flag.Desc)
				for _, value := range flag.Values {
					if !SafeValue(value.Text) {
						t.Errorf("built-in value %q is not safe", value.Text)
					}
					checkSanitizedDescription(t, "value "+value.Text, value.Desc)
				}
			}
		})
	}
}

func checkSanitizedDescription(t *testing.T, owner, desc string) {
	t.Helper()
	if got := NormalizeText(desc, maxDescRunes); got != desc {
		t.Errorf("%s: description %q is not normalized (%q)", owner, desc, got)
	}
	if utf8.RuneCountInString(desc) > maxDescRunes {
		t.Errorf("%s: description is %d runes", owner, utf8.RuneCountInString(desc))
	}
	for _, r := range desc {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' || isVariationSelector(r) {
			t.Errorf("%s: description contains invisible %U", owner, r)
		}
	}
}

func TestAuditRejectsPoisonedCatalog(t *testing.T) {
	cases := map[string]*Catalog{
		"bidi description": catalogWith(t, &Node{Token: "tool", Desc: "safe\u202eevil"}),
		"oversized description": catalogWith(t, &Node{Token: "tool",
			Desc: strings.Repeat("a", maxDescRunes+1)}),
		"unsafe value": catalogWith(t, &Node{Token: "tool", Flags: []Flag{
			{Long: "config", Value: true, Values: []Value{{Text: "evil;rm"}}}}}),
		"unsafe flag value": catalogWith(t, &Node{Token: "tool", Flags: []Flag{
			{Long: "exec", Value: true, Values: []Value{{Text: "$(id)"}}}}}),
		"bad command name": catalogWith(t, &Node{Token: "push;rm"}),
		"bad alias":        catalogWith(t, &Node{Token: "tool", Aliases: []string{"$(id)"}}),
		"bad flag name":    catalogWith(t, &Node{Token: "tool", Flags: []Flag{{Long: "a b"}}}),
		"unsanitized flag description": catalogWith(t, &Node{Token: "tool", Flags: []Flag{
			{Long: "all", Desc: "one\ntwo"}}}),
	}
	for name, catalog := range cases {
		t.Run(name, func(t *testing.T) {
			report := Audit(catalog)
			if report.HardCount == 0 {
				t.Fatal("audit accepted the poisoned catalog")
			}
			if err := report.Err(); err == nil {
				t.Fatal("audit error is nil")
			}
		})
	}
	if report := Audit(nil); report.HardCount == 0 {
		t.Fatal("audit accepted a nil catalog")
	}
}

func catalogWith(t *testing.T, nodes ...*Node) *Catalog {
	t.Helper()
	catalog, err := NewCatalog("carapace-bin", "v9.9.9", "abcdef", "MIT", nodes)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return catalog
}

func TestAuditCountsSoftFindings(t *testing.T) {
	catalog := catalogWith(t, &Node{Token: "tool", Desc: "Latin то Cyrillic"})
	report := Audit(catalog)
	if report.HardCount != 0 {
		t.Fatalf("hard findings: %+v", report.Hard)
	}
	if report.SoftCount < 2 {
		t.Fatalf("soft findings = %d, want the mixed script and the small catalog shape: %+v", report.SoftCount, report.Soft)
	}
	if report.Err() != nil {
		t.Fatalf("soft findings rejected the catalog: %v", report.Err())
	}
}

// Decode counts soft findings without re-running the hard checks it has already
// made; the number it stores must still be the one Audit reports.
func TestDecodeCountsTheSameSoftFindingsAsAudit(t *testing.T) {
	catalog := catalogWith(t,
		&Node{Token: "tool", Desc: "Latin то Cyrillic", Flags: []Flag{
			{Long: "mode", Value: true, Desc: "Latin то Cyrillic", Values: []Value{{Text: "fast", Desc: "Latin то Cyrillic"}}}}},
		&Node{Token: "plain", Desc: "ASCII only"},
	)
	want := Audit(catalog).SoftCount
	decoded, err := Decode(catalog.Encode())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := decoded.AuditWarnings(); got != want || want < 4 {
		t.Fatalf("decoded warnings = %d, Audit reports %d; want equal and at least 4", got, want)
	}
}

// A description injected into the encoded cache must be normalized on decode;
// the same file with an unsafe literal value must be rejected even when its
// digest and counts are recomputed.
func TestDecodeNormalizesPoisonedDescriptions(t *testing.T) {
	encoded := testCatalog(t).Encode()
	for name, tamper := range map[string]struct{ old, new, want string }{
		"bidi": {
			old:  "the stupid content tracker",
			new:  "safe\u202eevil",
			want: "safeevil",
		},
		"oversized": {
			old:  "the stupid content tracker",
			new:  strings.Repeat("a", maxDescRunes*8),
			want: strings.Repeat("a", maxDescRunes),
		},
		"c1 control": {
			old:  "the stupid content tracker",
			new:  "safe\u009bevil",
			want: "safeevil",
		},
	} {
		t.Run(name, func(t *testing.T) {
			decoded, err := Decode(resealCatalog(encoded, tamper.old, tamper.new))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got := decoded.Root("git").Desc; got != tamper.want {
				t.Fatalf("description = %q, want %q", got, tamper.want)
			}
		})
	}
	t.Run("unsafe value", func(t *testing.T) {
		poisoned := resealCatalog(encoded, "json\x02machine readable", "evil;rm\x02machine readable")
		if _, err := Decode(poisoned); err == nil {
			t.Fatal("Decode accepted an unsafe literal value")
		}
	})
}

// resealCatalog rewrites the first occurrence of old in an encoded catalog and
// recomputes the body digest, simulating a tampered cache file.
func resealCatalog(encoded []byte, old, replacement string) []byte {
	bodyStart := strings.Index(string(encoded), "\n\n") + 2
	header, body := string(encoded[:bodyStart]), string(encoded[bodyStart:])
	body = strings.Replace(body, old, replacement, 1)
	sum := sha256.Sum256([]byte(body))
	lines := strings.Split(header, "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, "digest\t") {
			lines[index] = "digest\t" + hex.EncodeToString(sum[:])
		}
	}
	return []byte(strings.Join(lines, "\n") + body)
}

func TestImportSanitizesInsteadOfKeepingInvisibleCharacters(t *testing.T) {
	catalog, stats := importFiles(t, []SourceFile{{Name: "spoof/spoof.yaml", Data: []byte(
		"name: spoof\n" +
			"description: run\u202Eevil\uFE0F\n" +
			"flags:\n" +
			"  --all=: show\u200Fall\n" +
			"completion:\n" +
			"  flag:\n" +
			"    all:\n" +
			"      - evil;rm\n" +
			"      - safe\tvalue\u200Fdesc\n",
	)}}, ImportOptions{})
	root := catalog.Root("spoof")
	if root == nil || root.Desc != "runevil" {
		t.Fatalf("root = %+v", root)
	}
	flag, ok := catalog.FindFlag(root, "--all")
	if !ok || flag.Desc != "showall" {
		t.Fatalf("flag = %+v, found=%v", flag, ok)
	}
	if len(flag.Values) != 1 || flag.Values[0].Text != "safe" || flag.Values[0].Desc != "valuedesc" {
		t.Fatalf("values = %+v", flag.Values)
	}
	if stats.SkippedValues == 0 {
		t.Fatal("the unsafe value was not counted")
	}
	if report := Audit(catalog); report.HardCount != 0 {
		t.Fatalf("imported catalog has hard findings: %+v", report.Hard)
	}
}
