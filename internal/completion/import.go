package completion

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Metadata identifies where a catalog came from. It is recorded in the encoded
// snapshot so a future reader can tell which data a cached file holds.
type Metadata struct {
	Source  string
	Release string
	Commit  string
	License string
}

// SourceFile is one YAML file from the Carapace man tree. Name is relative to
// man/cmd, for example "git/git.bisect.yaml".
type SourceFile struct {
	Name string
	Data []byte
}

// ImportStats is what the generator prints and what makes an import auditable:
// how many files were read, how many contributed, and everything refused.
type ImportStats struct {
	Files        int
	Roots        int
	Nodes        int
	Flags        int
	Values       int
	SkippedFiles int
	// SkippedCommands counts command entries whose name or path did not agree.
	SkippedCommands int
	// SkippedFlags counts option entries with an unsupported name or modifier.
	SkippedFlags int
	// SkippedValues counts literal values that were dynamic, unsafe or over the
	// per-option bound.
	SkippedValues int
	// HiddenFlags were valid but marked hidden in the source and are not
	// completed.
	HiddenFlags int
	// RootlessDirs counts directories without a canonical <dir>/<dir>.yaml.
	RootlessDirs int
	// RepairedFiles counts source files that shipped invalid YAML of the known
	// plain-scalar kind and were imported after quoting the offending value.
	RepairedFiles int
	// SkippedSources counts source files that stayed unparsable and were left
	// out of the catalog instead of failing the whole import.
	SkippedSources int
	// AuditWarnings counts soft findings of the content audit. Hard findings
	// reject the catalog instead of being counted.
	AuditWarnings int
}

// ImportOptions bounds one import. Zero values take the package defaults.
type ImportOptions struct {
	MaxFiles     int
	MaxNodes     int
	MaxFileBytes int
	MaxYAMLDepth int
	MaxYAMLNodes int
}

// DefaultImportOptions returns the bounds used by the built-in snapshot and by
// catalog updates.
func DefaultImportOptions() ImportOptions {
	return ImportOptions{
		MaxFiles:     maxArchiveMembers,
		MaxNodes:     maxCatalogNodes,
		MaxFileBytes: maxSourceFileBytes,
		MaxYAMLDepth: maxYAMLDepth,
		MaxYAMLNodes: maxYAMLNodes,
	}
}

// Import builds a catalog from the files of one Carapace man tree. Parsing is
// pure: a source file can describe structure, options and literal values, and
// everything else is ignored without being evaluated.
//
// One bad file must not cost the whole catalog. A source that only has the
// known upstream defect (an unquoted plain scalar containing ": ") is repaired
// in place, and a source that still cannot be parsed is skipped and counted;
// the bounds on archive size, files, duplicates and command depth stay fatal.
func Import(files []SourceFile, meta Metadata, opts ImportOptions) (*Catalog, ImportStats, error) {
	defaults := DefaultImportOptions()
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaults.MaxFiles
	}
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = defaults.MaxNodes
	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = defaults.MaxFileBytes
	}
	if opts.MaxYAMLDepth <= 0 {
		opts.MaxYAMLDepth = defaults.MaxYAMLDepth
	}
	if opts.MaxYAMLNodes <= 0 {
		opts.MaxYAMLNodes = defaults.MaxYAMLNodes
	}

	stats := ImportStats{Files: len(files)}
	if len(files) > opts.MaxFiles {
		return nil, stats, fmt.Errorf("completion: source has %d files, limit is %d", len(files), opts.MaxFiles)
	}
	if meta.Source == "" || meta.Commit == "" || meta.License == "" {
		return nil, stats, errors.New("completion: source metadata is incomplete")
	}

	ordered := append([]SourceFile(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })

	byDir := make(map[string]map[string]yamlCommand, 800)
	seen := make(map[string]struct{}, len(ordered))
	var dirs []string
	for _, file := range ordered {
		dir, base, ok := splitSourceName(file.Name)
		if !ok {
			stats.SkippedFiles++
			continue
		}
		if len(file.Data) > opts.MaxFileBytes {
			return nil, stats, fmt.Errorf("completion: source %s exceeds the file size limit", file.Name)
		}
		// Duplicate detection cannot depend on parse success: a malformed
		// source must not let a later file claim the same path, so the name is
		// recorded before the file is parsed.
		canonical := dir + "/" + base
		if _, exists := seen[canonical]; exists {
			return nil, stats, fmt.Errorf("completion: duplicate source %s", file.Name)
		}
		seen[canonical] = struct{}{}
		files := byDir[dir]
		if files == nil {
			files = make(map[string]yamlCommand, 8)
			byDir[dir] = files
			dirs = append(dirs, dir)
		}
		command, repaired, err := parseSourceRecovering(file.Data, opts)
		if err != nil {
			// A file that violates a hard bound still fails the import; a file
			// that simply cannot be parsed is skipped and counted.
			if errors.Is(err, errSourceLimit) {
				return nil, stats, fmt.Errorf("completion: source %s: %w", file.Name, err)
			}
			stats.SkippedSources++
			continue
		}
		if repaired {
			stats.RepairedFiles++
		}
		files[base] = command
	}
	// Skipping a few broken files keeps a release usable, but a source set
	// that loses more than a token few is partial: installing it would replace
	// a working catalog with one missing most of its subcommands while the root
	// count still looks healthy. The tolerance is intentionally small.
	if tolerated := toleratedSkippedSources(len(ordered)); stats.SkippedSources > tolerated {
		return nil, stats, fmt.Errorf("completion: %d source files could not be parsed, limit is %d", stats.SkippedSources, tolerated)
	}
	sort.Strings(dirs)

	roots := make([]*Node, 0, len(dirs))
	for _, dir := range dirs {
		if !validTokenPart(dir) {
			stats.RootlessDirs++
			continue
		}
		files := byDir[dir]
		root, ok := files[dir+".yaml"]
		if !ok {
			stats.RootlessDirs++
			continue
		}
		if token := commandToken(root.Name); token != "" && token != dir {
			stats.SkippedFiles++
			continue
		}
		node := &Node{Token: dir}
		if err := applyCommand(node, root, 1, &stats); err != nil {
			return nil, stats, fmt.Errorf("completion: source %s/%s.yaml: %w", dir, dir, err)
		}
		applied := 1
		for _, base := range sortedKeys(files) {
			if base == dir+".yaml" {
				continue
			}
			stem := strings.TrimSuffix(base, ".yaml")
			segments := strings.Split(stem, ".")
			if len(segments) > maxDepth {
				return nil, stats, fmt.Errorf("completion: source %s/%s exceeds command depth limit", dir, base)
			}
			if len(segments) < 2 || segments[0] != dir {
				continue
			}
			command := files[base]
			if token := commandToken(command.Name); token != "" && token != segments[len(segments)-1] {
				continue
			}
			child, ok := ensureChild(node, segments[1:], &stats)
			if !ok {
				continue
			}
			if err := applyCommand(child, command, len(segments), &stats); err != nil {
				return nil, stats, fmt.Errorf("completion: source %s/%s: %w", dir, base, err)
			}
			applied++
		}
		stats.SkippedFiles += len(files) - applied
		roots = append(roots, node)
	}

	catalog, err := NewCatalog(meta.Source, meta.Release, meta.Commit, meta.License, roots)
	if err != nil {
		return nil, stats, err
	}
	if catalog.NodeCount() > opts.MaxNodes {
		return nil, stats, fmt.Errorf("completion: catalog has %d nodes, limit is %d", catalog.NodeCount(), opts.MaxNodes)
	}
	report := Audit(catalog)
	if err := report.Err(); err != nil {
		return nil, stats, err
	}
	catalog.auditWarnings = report.SoftCount
	stats.AuditWarnings = report.SoftCount
	stats.Roots = catalog.RootCount()
	stats.Nodes = catalog.NodeCount()
	for _, root := range roots {
		walk(root, func(n *Node) {
			stats.Flags += len(n.Flags) + len(n.Persistent)
			for _, f := range n.Flags {
				stats.Values += len(f.Values)
			}
			for _, f := range n.Persistent {
				stats.Values += len(f.Values)
			}
		})
	}
	catalog.importStats = &stats
	return catalog, stats, nil
}

// toleratedSkippedSources bounds how many unparsable source files an import
// accepts before treating the source as partial. Real releases can carry a
// handful of upstream YAML defects; a source that lost one percent of its files
// (or more than the floor) is not the release that was advertised.
func toleratedSkippedSources(files int) int {
	if ratio := files / 100; ratio > 16 {
		return ratio
	}
	return 16
}

// splitSourceName accepts only "<dir>/<file>.yaml" directly under man/cmd.
func splitSourceName(name string) (dir, base string, ok bool) {
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return "", "", false
	}
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if !strings.HasSuffix(parts[1], ".yaml") || parts[1] == ".yaml" {
		return "", "", false
	}
	for _, part := range parts {
		if part == "." || part == ".." {
			return "", "", false
		}
	}
	return parts[0], parts[1], true
}

func sortedKeys(m map[string]yamlCommand) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func commandToken(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// ensureChild walks path below parent, creating intermediate nodes. A path that
// cannot be a bare command token is refused as a whole.
func ensureChild(parent *Node, path []string, stats *ImportStats) (*Node, bool) {
	node := parent
	for _, token := range path {
		if !validTokenPart(token) {
			stats.SkippedCommands++
			return nil, false
		}
		var child *Node
		for _, candidate := range node.Children {
			if candidate.Token == token {
				child = candidate
				break
			}
		}
		if child == nil {
			child = &Node{Token: token}
			node.Children = append(node.Children, child)
		}
		node = child
	}
	return node, true
}

func walk(n *Node, fn func(*Node)) {
	n.load()
	fn(n)
	for _, child := range n.Children {
		walk(child, fn)
	}
}

// yamlCommand whitelists the fields the catalog understands. Everything else in
// the source (run, documentation, examples, generators) is ignored by design.
type yamlCommand struct {
	Name            string          `yaml:"name"`
	Aliases         []string        `yaml:"aliases"`
	Description     string          `yaml:"description"`
	Hidden          bool            `yaml:"hidden"`
	Flags           flagSpecList    `yaml:"flags"`
	PersistentFlags flagSpecList    `yaml:"persistentflags"`
	Commands        []yamlCommand   `yaml:"commands"`
	Completion      *yamlCompletion `yaml:"completion"`
}

type flagSpec struct {
	Key  string
	Desc string
}

type flagSpecList []flagSpec

// UnmarshalYAML accepts both supported flag forms: a mapping from option to
// description, and a sequence of single-entry mappings.
func (l *flagSpecList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.MappingNode:
		return l.appendPairs(value)
	case yaml.SequenceNode:
		for _, item := range value.Content {
			if item.Kind != yaml.MappingNode {
				return fmt.Errorf("completion: flag entry is not a mapping")
			}
			if err := l.appendPairs(item); err != nil {
				return err
			}
		}
		return nil
	case yaml.ScalarNode:
		// "flags:" with no entries decodes as null.
		if value.Tag == "!!null" {
			return nil
		}
		return errors.New("completion: flags are not a mapping or a list")
	default:
		return fmt.Errorf("completion: flags are neither a mapping nor a list")
	}
}

func (l *flagSpecList) appendPairs(value *yaml.Node) error {
	for i := 0; i+1 < len(value.Content); i += 2 {
		var desc string
		definition := value.Content[i+1]
		if definition.Kind == yaml.MappingNode {
			// Carapace also stores descriptions beside defaults in flag maps.
			// Defaults describe execution, so only the description is imported.
			var fields struct {
				Description string `yaml:"description"`
			}
			if err := definition.Decode(&fields); err != nil {
				return fmt.Errorf("completion: flag definition: %w", err)
			}
			desc = fields.Description
		} else if err := definition.Decode(&desc); err != nil {
			return fmt.Errorf("completion: flag description: %w", err)
		}
		*l = append(*l, flagSpec{Key: value.Content[i].Value, Desc: desc})
	}
	return nil
}

type yamlCompletion struct {
	Flag map[string][]string `yaml:"flag"`
}

func parseSource(data []byte, opts ImportOptions) (yamlCommand, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return yamlCommand{}, err
	}
	if document.Kind == 0 || len(document.Content) == 0 {
		return yamlCommand{}, errors.New("completion: empty YAML document")
	}
	if document.Content[0].Kind != yaml.MappingNode {
		return yamlCommand{}, errors.New("completion: command is not a mapping")
	}
	if err := checkYAMLSize(document.Content[0], opts); err != nil {
		return yamlCommand{}, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return yamlCommand{}, errors.New("completion: source must contain one YAML document")
	}
	var command yamlCommand
	if err := document.Decode(&command); err != nil {
		return yamlCommand{}, err
	}
	return command, nil
}

// errSourceLimit marks a source document that breaks a hard YAML bound. Such a
// file still fails its import: the bound exists to stop hostile input, and a
// skipped hostile file would only hide that someone tried.
var errSourceLimit = errors.New("completion: source exceeds a YAML bound")

// parseSourceRecovering parses one source file. Carapace release archives have
// shipped man/cmd files whose plain scalar values contain ": ", which makes the
// YAML parser read the rest of the line as another mapping key. That defect is
// repaired by quoting the value on the reported line, one line at a time and
// under a bound; anything else is left to the caller as a skip.
func parseSourceRecovering(data []byte, opts ImportOptions) (yamlCommand, bool, error) {
	document := data
	for repairs := 0; ; repairs++ {
		command, err := parseSource(document, opts)
		if err == nil {
			return command, repairs > 0, nil
		}
		if errors.Is(err, errSourceLimit) {
			return yamlCommand{}, false, err
		}
		line, ok := mappingValueErrorLine(err)
		if !ok || repairs >= maxRepairsPerSource {
			return yamlCommand{}, false, err
		}
		fixed, ok := quoteMappingValue(document, line)
		if !ok {
			return yamlCommand{}, false, err
		}
		repairable, boundErr := repairableDescriptionPrefix(fixed, line, opts)
		if boundErr != nil {
			return yamlCommand{}, false, boundErr
		}
		if !repairable {
			return yamlCommand{}, false, err
		}
		document = fixed
	}
}

// maxRepairsPerSource bounds the work one hostile or very broken file can
// cause. Real upstream files need one repair per defect line.
const maxRepairsPerSource = 16

const mappingValuesMessage = "mapping values are not allowed in this context"

// yamlLineError matches the parser's "yaml: line N: message" form.
var yamlLineError = regexp.MustCompile(`^yaml: line ([0-9]+): (.*)$`)

// mappingValueErrorLine reports the line of the one parser failure this import
// knows how to repair.
func mappingValueErrorLine(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	match := yamlLineError.FindStringSubmatch(err.Error())
	if match == nil || match[2] != mappingValuesMessage {
		return 0, false
	}
	line, convertErr := strconv.Atoi(match[1])
	if convertErr != nil || line <= 0 {
		return 0, false
	}
	return line, true
}

// quoteMappingValue turns the plain scalar after the first ": " of line into a
// single-quoted scalar, which is how YAML spells text that contains ": ".
// Single quotes inside the text are doubled; every other byte of the file is
// kept, so the decoded text is the same one the author wrote.
func quoteMappingValue(data []byte, line int) ([]byte, bool) {
	start, end, ok := lineSpan(data, line)
	if !ok {
		return nil, false
	}
	body := data[start:end]
	carriageReturn := []byte(nil)
	if len(body) > 0 && body[len(body)-1] == '\r' {
		carriageReturn = []byte{'\r'}
		body = body[:len(body)-1]
	}
	colon := bytes.Index(body, []byte(": "))
	if colon < 0 {
		return nil, false
	}
	key := strings.TrimPrefix(strings.TrimSpace(string(body[:colon])), "- ")
	if key != "description" {
		if _, _, ok := parseFlagSpec(key); !ok {
			return nil, false
		}
	}
	// Plain scalars strip the separation spaces around the value; doing the
	// same here keeps the decoded description byte-identical.
	value := bytes.Trim(body[colon+2:], " \t")
	if len(value) == 0 {
		return nil, false
	}
	// Quoted scalars, collections, tags and anchors have their own YAML
	// structure. Wrapping them would guess a different type instead of fixing
	// the known missing quotes in a plain description.
	if strings.ContainsRune("'\"[]{}|>&*!#%@`", rune(value[0])) {
		return nil, false
	}
	quoted := bytes.ReplaceAll(value, []byte("'"), []byte("''"))

	fixed := make([]byte, 0, len(data)+len(quoted)+2)
	fixed = append(fixed, data[:start]...)
	fixed = append(fixed, body[:colon+1]...)
	fixed = append(fixed, ' ', '\'')
	fixed = append(fixed, quoted...)
	fixed = append(fixed, '\'')
	fixed = append(fixed, carriageReturn...)
	fixed = append(fixed, data[end:]...)
	return fixed, true
}

// Parse through the repaired line to establish its YAML path even when later
// lines still need repair. Ignored fields and non-description values must not
// become valid merely because their final key resembles an option name.
func repairableDescriptionPrefix(data []byte, line int, opts ImportOptions) (bool, error) {
	_, end, ok := lineSpan(data, line)
	if !ok {
		return false, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data[:end], &document); err != nil || len(document.Content) != 1 {
		return false, nil
	}
	root := document.Content[0]
	if err := checkYAMLSize(root, opts); err != nil {
		return false, err
	}
	return descriptionAtLine(root, line, "command"), nil
}

func descriptionAtLine(node *yaml.Node, line int, scope string) bool {
	if node.Kind == yaml.SequenceNode {
		if scope != "commands" && scope != "flags" {
			return false
		}
		childScope := scope
		if scope == "commands" {
			childScope = "command"
		}
		for _, child := range node.Content {
			if descriptionAtLine(child, line, childScope) {
				return true
			}
		}
		return false
	}
	if node.Kind != yaml.MappingNode {
		return false
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key, value := node.Content[index].Value, node.Content[index+1]
		if (scope == "command" || scope == "flag") && key == "description" {
			if value.Kind == yaml.ScalarNode && value.Line == line {
				return true
			}
		}
		childScope := ""
		switch {
		case scope == "command" && key == "commands":
			childScope = "commands"
		case scope == "command" && (key == "flags" || key == "persistentflags"):
			childScope = "flags"
		case scope == "flags":
			if _, _, ok := parseFlagSpec(key); ok {
				if value.Kind == yaml.ScalarNode && value.Line == line {
					return true
				}
				childScope = "flag"
			}
		}
		if childScope != "" && descriptionAtLine(value, line, childScope) {
			return true
		}
	}
	return false
}

// lineSpan returns the byte range of one 1-based line without its newline.
func lineSpan(data []byte, line int) (start, end int, ok bool) {
	if line <= 0 {
		return 0, 0, false
	}
	start = 0
	for skipped := 1; skipped < line; skipped++ {
		index := bytes.IndexByte(data[start:], '\n')
		if index < 0 {
			return 0, 0, false
		}
		start += index + 1
	}
	if start >= len(data) {
		return 0, 0, false
	}
	end = len(data)
	if index := bytes.IndexByte(data[start:], '\n'); index >= 0 {
		end = start + index
	}
	if end == start {
		return 0, 0, false
	}
	return start, end, true
}

// checkYAMLSize bounds nesting and node count before decoding, so a hostile
// document cannot make the decoder do unbounded work.
func checkYAMLSize(root *yaml.Node, opts ImportOptions) error {
	type entry struct {
		node  *yaml.Node
		depth int
	}
	stack := []entry{{node: root, depth: 1}}
	nodes := 0
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		nodes++
		if nodes > opts.MaxYAMLNodes {
			return fmt.Errorf("%w: the document has too many nodes", errSourceLimit)
		}
		if current.depth > opts.MaxYAMLDepth {
			return fmt.Errorf("%w: the document nests too deeply", errSourceLimit)
		}
		for _, child := range current.node.Content {
			stack = append(stack, entry{node: child, depth: current.depth + 1})
		}
	}
	return nil
}

// applyCommand merges one document into a node. depth is the node depth used to
// refuse absurd command nesting.
func applyCommand(node *Node, command yamlCommand, depth int, stats *ImportStats) error {
	if depth > maxDepth {
		return errors.New("completion: command nests too deeply")
	}
	if node.Desc == "" {
		node.Desc = NormalizeText(command.Description, maxDescRunes)
	}
	if command.Hidden {
		node.Hidden = true
	}
	for _, alias := range command.Aliases {
		if alias == "" || alias == node.Token || !validTokenPart(alias) {
			stats.SkippedCommands++
			continue
		}
		node.Aliases = append(node.Aliases, alias)
	}

	flags, skipped := parseFlagSpecs(command.Flags, stats)
	stats.SkippedFlags += skipped
	mergeFlags(&node.Flags, flags)
	persistent, skipped := parseFlagSpecs(command.PersistentFlags, stats)
	stats.SkippedFlags += skipped
	mergeFlags(&node.Persistent, persistent)

	if command.Completion != nil {
		applyValues(node, command.Completion.Flag, stats)
	}
	for _, sub := range command.Commands {
		token := commandToken(sub.Name)
		if !validTokenPart(token) {
			stats.SkippedCommands++
			continue
		}
		child := findChild(node, token)
		if child == nil {
			child = &Node{Token: token}
			node.Children = append(node.Children, child)
		}
		if err := applyCommand(child, sub, depth+1, stats); err != nil {
			return err
		}
	}
	return nil
}

func findChild(node *Node, token string) *Node {
	for _, child := range node.Children {
		if child.Token == token {
			return child
		}
	}
	return nil
}

func parseFlagSpecs(specs []flagSpec, stats *ImportStats) ([]Flag, int) {
	flags := make([]Flag, 0, len(specs))
	skipped := 0
	for _, spec := range specs {
		flag, hidden, ok := parseFlagSpec(spec.Key)
		if !ok {
			skipped++
			continue
		}
		if hidden {
			stats.HiddenFlags++
			continue
		}
		flag.Desc = NormalizeText(spec.Desc, maxDescRunes)
		flags = append(flags, flag)
	}
	return flags, skipped
}

// parseFlagSpec reads Carapace's flag syntax: "-s, --long<modifiers>". It
// returns the parsed option and whether the source marks it hidden.
func parseFlagSpec(spec string) (Flag, bool, bool) {
	spec = strings.TrimSpace(spec)
	end := len(spec)
	for end > 0 && strings.ContainsRune("=*?&!", rune(spec[end-1])) {
		end--
	}
	modifiers := spec[end:]
	spec = spec[:end]
	if spec == "" {
		return Flag{}, false, false
	}

	var flag Flag
	parts := strings.Split(spec, ",")
	if len(parts) > 2 {
		return Flag{}, false, false
	}
	for index, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "--"):
			if index != len(parts)-1 || flag.Long != "" {
				return Flag{}, false, false
			}
			flag.Long = part[2:]
		case strings.HasPrefix(part, "-"):
			if index != 0 || flag.Short != "" {
				return Flag{}, false, false
			}
			flag.Short = part[1:]
		default:
			return Flag{}, false, false
		}
	}
	if (flag.Long != "" && !validTokenPart(flag.Long)) || (flag.Short != "" && !validTokenPart(flag.Short)) {
		return Flag{}, false, false
	}
	if flag.Long == "" && flag.Short == "" {
		return Flag{}, false, false
	}
	hidden := false
	for _, modifier := range modifiers {
		switch modifier {
		case '=':
			flag.Value = true
		case '?':
			flag.Value = true
			flag.Optional = true
		case '*':
			flag.Repeatable = true
		case '!':
			flag.Required = true
		case '&':
			hidden = true
		default:
			return Flag{}, false, false
		}
	}
	return flag, hidden, true
}

func mergeFlags(into *[]Flag, flags []Flag) {
	for _, flag := range flags {
		replaced := false
		for i := range *into {
			existing := &(*into)[i]
			if existing.Token() != flag.Token() {
				continue
			}
			existing.Value = existing.Value || flag.Value
			existing.Optional = existing.Optional || flag.Optional
			existing.Repeatable = existing.Repeatable || flag.Repeatable
			existing.Required = existing.Required || flag.Required
			if existing.Desc == "" {
				existing.Desc = flag.Desc
			}
			mergeValues(existing, flag.Values)
			replaced = true
			break
		}
		if !replaced {
			*into = append(*into, flag)
		}
	}
}

func mergeValues(flag *Flag, values []Value) {
	for _, value := range values {
		duplicate := false
		for i := range flag.Values {
			if flag.Values[i].Text == value.Text {
				if flag.Values[i].Desc == "" {
					flag.Values[i].Desc = value.Desc
				}
				duplicate = true
				break
			}
		}
		if !duplicate && len(flag.Values) < maxValuesPerFlag {
			flag.Values = append(flag.Values, value)
		}
	}
}

// applyValues attaches literal values from completion.flag. Keys are bare
// option names. Anything dynamic or unsafe is counted and dropped.
func applyValues(node *Node, byFlag map[string][]string, stats *ImportStats) {
	keys := make([]string, 0, len(byFlag))
	for key := range byFlag {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		name := strings.TrimLeft(key, "-")
		flag := findFlagByName(node, name)
		items := byFlag[key]
		if flag == nil || (!flag.Value && !flag.Optional) {
			stats.SkippedValues += len(items)
			continue
		}
		for _, item := range items {
			if strings.HasPrefix(item, "$") || strings.Contains(item, " ||| ") {
				stats.SkippedValues++
				continue
			}
			fields := strings.Split(item, "\t")
			if len(fields) > 3 || !SafeValue(fields[0]) {
				stats.SkippedValues++
				continue
			}
			if len(flag.Values) >= maxValuesPerFlag {
				stats.SkippedValues++
				continue
			}
			value := Value{Text: fields[0]}
			if len(fields) > 1 {
				value.Desc = NormalizeText(fields[1], maxDescRunes)
			}
			mergeValues(flag, []Value{value})
		}
	}
}

func findFlagByName(node *Node, name string) *Flag {
	for i := range node.Flags {
		if node.Flags[i].Long == name || node.Flags[i].Short == name {
			return &node.Flags[i]
		}
	}
	for i := range node.Persistent {
		if node.Persistent[i].Long == name || node.Persistent[i].Short == name {
			return &node.Persistent[i]
		}
	}
	return nil
}
