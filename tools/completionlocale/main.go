package main

import (
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"motty/internal/completion"
)

var protectedLiteral = regexp.MustCompile("`[^`]+`|\"[^\"]+\"|https?://[^\\s)]+|--?[A-Za-z][A-Za-z0-9_.-]*(?:=[^\\s,;)]+)?|[%$]\\{[^}]+\\}|\\{\\{[^}]+\\}\\}|\\$[A-Za-z_][A-Za-z0-9_]*|<[^<>]+>|\\b[A-Z0-9][A-Z0-9_]{1,}\\b")
var enumLiteral = regexp.MustCompile(`\b(auto|always|never|true|false|json|yaml|xml|text|raw|none|null|preserve)\b`)
var binaryChoiceLiteral = regexp.MustCompile(`\b(yes|no|only)\b`)
var singleQuotedLiteral = regexp.MustCompile(`(?:^|[\s(=,:])('[^']+')`)
var pathLiteral = regexp.MustCompile(`(?:^|[\s('"=])(/[A-Za-z0-9_.$~*-][A-Za-z0-9_./${}~*+-]*|\.[A-Za-z0-9_$~-][A-Za-z0-9_./${}~*+-]*)`)
var fileLiteral = regexp.MustCompile(`\b[A-Za-z0-9_.-]+\.(?:pem|key|crt|der|p12|pfx|cnf|conf|json|yaml|yml|toml|ini|log|py|go|js|ts|sh|txt|csv|ttf|otf|xml)\b`)
var bracketChoices = regexp.MustCompile(`\[[^\[\]]+\]|\{[^{}]*[,|][^{}]*\}|\([^()]*\|[^()]*\)`)
var namedChoices = regexp.MustCompile(`(?i)(?:(?:valid|allowed|possible)\s+)?(?:values|options|choices)(?:\s+are\s*|\s*:\s*)|one of\s*:?\s*`)
var choiceIdentifier = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9_.+:/=-]*`)
var choiceSequence = regexp.MustCompile(`^\s*[A-Za-z0-9_][A-Za-z0-9_.+:/=-]*(?:\s*(?:,\s*(?:or\s+|and\s+)?|\|\s*|or\s+|and\s+)[A-Za-z0-9_][A-Za-z0-9_.+:/=-]*)+`)
var comparisonLiteral = regexp.MustCompile(`\b[A-Za-z0-9_][A-Za-z0-9_.-]*(?:>=|<=|==|!=)[A-Za-z0-9_.-]+|>=|<=|==|!=|&&|\|\||:ro\b|:rw\b`)
var defaultLiteral = regexp.MustCompile(`(?i)\bdefaults?(?:\s+to|\s+is)?[\s:=('"\[]+(true|false|auto(?:-detect)?|none|null|never|always|yes|no|-?[0-9][A-Za-z0-9_.%+-]*)\b`)
var technicalName = regexp.MustCompile(`(?i)\b(systemd|cgroup|minikube|ripgrep|flutter|pacman|podman|kitty|kitten|asciinema|bazelrc|bazel|almalinux|openshift|docker|kubernetes|github|gitlab|powershell|nushell|kubectl|kubeadm|kubelet|openssl|fish|bash|zsh|tcsh|ngmodule|commonjs|goreleaser|direnv|acpid|manylinux|coreutils|tesseract|neomutt|groff|tessdata|gapplication|mintty|starlark|envoy)\b`)
var addressLiteral = regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.-]*://[^\s)]*|\b(?:[A-Za-z0-9-]+\.)+(?:com|net|org|dev|io|cn|local)(?:/[A-Za-z0-9_./%?&=:+~-]*)?|\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?:/[0-9]{1,2})?\b`)
var assignmentLiteral = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_.-]*=[A-Za-z0-9_./:+-]+|\\[A-Za-z][A-Za-z0-9_]*`)
var propertyLiteral = regexp.MustCompile(`\{[A-Za-z_][A-Za-z0-9_.-]*\}|\b(?:[A-Za-z_][A-Za-z0-9_-]*\.)+<[A-Za-z0-9_]+>(?:\.[A-Za-z0-9_-]+)*|\b[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+\b`)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	options := flag.NewFlagSet("completionlocale", flag.ContinueOnError)
	catalogPath := options.String("catalog", "internal/completion/data/catalog.bin", "catalog to translate")
	sourcePath := options.String("translations", "tools/completionlocale/translations.zh-CN.tsv", "English, zh-CN, context TSV source (.gz supported)")
	outputPath := options.String("out", "internal/completion/data/descriptions.zh-CN.gz", "compact gzip output")
	missingPath := options.String("missing", "", "export only new/changed descriptions as TSV instead of generating")
	check := options.Bool("check", false, "validate full coverage and byte-for-byte regeneration")
	if err := options.Parse(args); err != nil {
		return err
	}
	data, err := os.ReadFile(*catalogPath)
	if err != nil {
		return err
	}
	catalog, err := completion.Decode(data)
	if err != nil {
		return err
	}
	sources, err := descriptionSources(catalog)
	if err != nil {
		return err
	}
	input, err := os.Open(*sourcePath)
	if err != nil {
		return err
	}
	defer input.Close()
	var reader io.Reader = input
	if strings.HasSuffix(*sourcePath, ".gz") {
		compressed, err := gzip.NewReader(input)
		if err != nil {
			return err
		}
		defer compressed.Close()
		reader = compressed
	}
	translations, err := readTranslations(io.LimitReader(reader, 32<<20))
	if err != nil {
		return err
	}
	if *missingPath != "" {
		var output bytes.Buffer
		if err := writeMissing(&output, sources, translations); err != nil {
			return err
		}
		return os.WriteFile(*missingPath, output.Bytes(), 0o644)
	}
	asset, err := generate(sources, translations)
	if err != nil {
		return err
	}
	if *check {
		existing, err := os.ReadFile(*outputPath)
		if err != nil {
			return err
		}
		if !bytes.Equal(existing, asset) {
			return fmt.Errorf("%s does not match the current catalog and translation source", *outputPath)
		}
	} else if err := os.WriteFile(*outputPath, asset, 0o644); err != nil {
		return err
	}
	fmt.Printf("validated %d unique descriptions, %d compressed bytes\n", len(sources), len(asset))
	return nil
}

func descriptionSources(catalog *completion.Catalog) (map[string]string, error) {
	if err := completion.Audit(catalog).Err(); err != nil {
		return nil, err
	}
	sources := make(map[string]string)
	add := func(description, context string) {
		if description != "" {
			if _, exists := sources[description]; !exists {
				sources[description] = context
			}
		}
	}
	var walk func(*completion.Node)
	walk = func(node *completion.Node) {
		context := strings.Join(node.Path(), " ")
		add(node.Desc, context)
		for _, flags := range [2][]completion.Flag{node.Flags, node.Persistent} {
			for _, flag := range flags {
				flagContext := context + " " + flag.Token()
				add(flag.Desc, flagContext)
				for _, value := range flag.Values {
					add(value.Desc, flagContext+" "+value.Text)
				}
			}
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	for _, root := range catalog.Roots() {
		walk(root)
	}
	return sources, nil
}

func readTranslations(reader io.Reader) (map[string]string, error) {
	parser := csv.NewReader(reader)
	parser.Comma = '\t'
	parser.FieldsPerRecord = 3
	header, err := parser.Read()
	if err != nil {
		return nil, err
	}
	if strings.Join(header, "\t") != "english\tzh-CN\tcontext" {
		return nil, errors.New("expected english, zh-CN, context TSV header")
	}
	translations := make(map[string]string)
	var failures []error
	for {
		record, err := parser.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if record[1] == "" {
			continue
		}
		if err := validateTranslation(record[0], record[1]); err != nil {
			failures = append(failures, err)
			continue
		}
		if previous, exists := translations[record[0]]; exists && previous != record[1] {
			return nil, fmt.Errorf("conflicting translations for %q", record[0])
		}
		translations[record[0]] = record[1]
	}
	return translations, errors.Join(failures...)
}

func validateTranslation(english, translated string) error {
	for _, text := range []string{english, translated} {
		if text == "" || !utf8.ValidString(text) || completion.NormalizeText(text, 160) != text {
			return fmt.Errorf("invalid translation text for %q", completion.NormalizeText(english, 160))
		}
	}
	var failures []error
	seen := make(map[string]int)
	check := func(literal string, required int) {
		if seen[literal] >= required {
			return
		}
		seen[literal] = required
		if literalCount(translated, literal) < required {
			failures = append(failures, fmt.Errorf("translation for %q lost literal %q", english, literal))
		}
	}
	for _, position := range protectedLiteral.FindAllStringIndex(english, -1) {
		literal := english[position[0]:position[1]]
		switch literal {
		case "NOT", "NEVER", "WARNING", "WITHOUT", "DEPRECATED", "NOTE", "EXPERIMENTAL", "IMPORTANT", "ONLY", "ALL", "DATA", "AND", "APPLICATIONS", "INSIDE", "THIS":
			continue
		}
		if literal[0] == '-' && position[0] > 0 {
			previous := english[position[0]-1]
			if previous >= 'a' && previous <= 'z' || previous >= 'A' && previous <= 'Z' || previous >= '0' && previous <= '9' || previous == '_' || previous == '-' || previous == '}' || previous == '>' {
				continue
			}
		}
		if literal[0] == '-' {
			literal = strings.TrimRight(literal, ".")
		}
		check(literal, literalCount(english, literal))
	}
	values := enumLiteral.FindAllString(english, -1)
	unique := make(map[string]bool)
	for _, value := range values {
		unique[value] = true
	}
	lower := strings.ToLower(english)
	structured := strings.Contains(english, "|") || strings.Contains(lower, "values") || strings.Contains(lower, "choices") || strings.Contains(lower, "one of") || strings.Contains(lower, "options:") || strings.Contains(lower, "options are") || unique["always"] && unique["never"] && strings.ContainsAny(english, ",([{") || unique["true"] && unique["false"]
	if len(unique) > 1 && structured {
		for _, value := range values {
			check(value, 1)
		}
	}
	for _, literal := range additionalLiterals(english) {
		check(literal, literalCount(english, literal))
	}
	for _, literal := range choiceLiterals(english) {
		check(literal, 1)
	}
	return errors.Join(failures...)
}

func additionalLiterals(english string) []string {
	var literals []string
	for _, choices := range bracketChoices.FindAllString(english, -1) {
		contents := choices[1 : len(choices)-1]
		if strings.Contains(contents, "|") && !strings.ContainsAny(contents, " \t:<>") {
			literals = append(literals, contents)
		}
	}
	for _, pattern := range []*regexp.Regexp{singleQuotedLiteral, pathLiteral, defaultLiteral} {
		for _, match := range pattern.FindAllStringSubmatch(english, -1) {
			literals = append(literals, match[1])
		}
	}
	literals = append(literals, fileLiteral.FindAllString(english, -1)...)
	literals = append(literals, comparisonLiteral.FindAllString(english, -1)...)
	literals = append(literals, technicalName.FindAllString(english, -1)...)
	literals = append(literals, addressLiteral.FindAllString(english, -1)...)
	for _, literal := range assignmentLiteral.FindAllString(english, -1) {
		literals = append(literals, strings.TrimRight(literal, "."))
	}
	for _, literal := range propertyLiteral.FindAllString(english, -1) {
		if literal != "e.g" && literal != "i.e" {
			literals = append(literals, literal)
		}
	}
	return literals
}

func choiceLiterals(english string) []string {
	var literals []string
	addChoices := func(sequence string) {
		for _, item := range choiceIdentifier.FindAllString(sequence, -1) {
			switch item {
			case "or", "and", "a", "an", "the":
				continue
			}
			literals = append(literals, strings.TrimRight(item, "."))
		}
	}
	for _, choices := range bracketChoices.FindAllString(english, -1) {
		contents := choices[1 : len(choices)-1]
		if _, after, ok := strings.Cut(contents, ":"); ok {
			contents = after
		}
		if sequence := choiceSequence.FindString(contents); sequence != "" {
			addChoices(sequence)
		} else if !strings.ContainsAny(contents, ",:|=") && len(strings.Fields(contents)) > 1 && (enumLiteral.MatchString(contents) || binaryChoiceLiteral.MatchString(contents) || namedChoices.MatchString(english)) {
			addChoices(contents)
		}
	}
	for _, position := range namedChoices.FindAllStringIndex(english, -1) {
		rest := english[position[1]:]
		if sequence := choiceSequence.FindString(rest); sequence != "" {
			if remainder := strings.TrimSpace(rest[len(sequence):]); remainder != "" && remainder[0] >= 'a' && remainder[0] <= 'z' {
				items := choiceIdentifier.FindAllStringIndex(sequence, -1)
				sequence = sequence[:items[len(items)-1][0]]
			}
			addChoices(sequence)
		}
	}
	return literals
}

func literalCount(text, literal string) int {
	word := func(character byte) bool {
		return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_'
	}
	count := 0
	for offset := 0; offset < len(text); {
		index := strings.Index(text[offset:], literal)
		if index < 0 {
			break
		}
		start := offset + index
		end := start + len(literal)
		before := start == 0 || !word(literal[0]) || !word(text[start-1])
		after := end == len(text) || !word(literal[len(literal)-1]) || !word(text[end])
		if strings.HasPrefix(literal, "-") && end < len(text) && text[end] == '-' {
			after = false
		}
		if before && after {
			count++
		}
		offset = end
	}
	return count
}

func generate(sources, translations map[string]string) ([]byte, error) {
	current := make(map[string]string, len(sources))
	for english := range sources {
		translated, exists := translations[english]
		if !exists {
			return nil, fmt.Errorf("missing translation for %q; use -missing to export new/changed descriptions", english)
		}
		if err := validateTranslation(english, translated); err != nil {
			return nil, err
		}
		current[english] = translated
	}
	return completion.EncodeDescriptions(current)
}

func writeMissing(output io.Writer, sources, translations map[string]string) error {
	writer := csv.NewWriter(output)
	writer.Comma = '\t'
	if err := writer.Write([]string{"english", "zh-CN", "context"}); err != nil {
		return err
	}
	var missing []string
	for english := range sources {
		if _, exists := translations[english]; !exists {
			missing = append(missing, english)
		}
	}
	sort.Strings(missing)
	for _, english := range missing {
		if err := writer.Write([]string{english, "", sources[english]}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
