package completion

// Bounds for anything that enters the catalog. They are deliberately generous
// for the real Carapace snapshot and small enough that a hostile archive or a
// corrupt cache cannot exhaust memory or make the UI stall.
const (
	// maxDepth is the deepest command nesting accepted from a source file.
	maxDepth = 32
	// maxDescRunes keeps a description short enough for a one-line list row.
	maxDescRunes = 160
	// maxValueBytes bounds one literal option value.
	maxValueBytes = 128
	// maxValuesPerFlag bounds the literal values imported for a single option.
	maxValuesPerFlag = 64
	// maxCatalogNodes bounds the whole catalog.
	maxCatalogNodes = 200_000

	// maxArchiveBytes bounds the compressed archive read during an update.
	maxArchiveBytes = 64 << 20
	// maxExpandedBytes bounds the total uncompressed bytes read from an archive
	// stream. Carapace v1.8.2 expands to about 108 MB and grows slowly, so this
	// leaves two and a half times the headroom; what is kept in memory is
	// bounded separately by maxSourceBytes.
	maxExpandedBytes = 256 << 20
	// maxArchiveMembers bounds the archive members visited.
	maxArchiveMembers = 20_000
	// maxSourceFileBytes bounds one YAML file.
	maxSourceFileBytes = 1 << 20
	// maxSourceBytes bounds the total source and license bytes kept in memory
	// for one import, so a 64 MiB gzip bomb cannot allocate the work bound.
	maxSourceBytes = 128 << 20
	// maxYAMLDepth bounds nesting inside one YAML document.
	maxYAMLDepth = 64
	// maxYAMLNodes bounds the nodes of one YAML document.
	maxYAMLNodes = 200_000
)
