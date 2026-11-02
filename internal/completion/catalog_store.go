package completion

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
)

// CatalogStore owns one installed catalog. Windows using the same data path
// share this store, so checking, installing and publishing have one writer.
type CatalogStore struct {
	path   string
	writer chan struct{}
	loaded bool
	note   error
	err    error

	mu         sync.RWMutex
	catalog    *Catalog
	generation uint64
	// stats describe the import that produced catalog; they are stored beside
	// the generation so every window reads one coherent snapshot. A catalog
	// loaded from disk or embedded in the binary carries zero stats.
	stats ImportStats
}

// NewCatalogStore creates a store for a catalog.bin path without reading it.
func NewCatalogStore(path string) *CatalogStore {
	return &CatalogStore{path: path, writer: make(chan struct{}, 1)}
}

// Snapshot reads only memory; initialization belongs on a background worker.
// Catalog, generation and import stats are returned together so a caller never
// mixes the counters of one catalog with the identity of another.
func (store *CatalogStore) Snapshot() (*Catalog, uint64, ImportStats) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.catalog, store.generation, store.stats
}

// Load initializes once, choosing the newest usable installed or built-in
// snapshot. A cache failure is a note; a missing usable baseline is an error.
func (store *CatalogStore) Load() (catalog *Catalog, generation uint64, note error, err error) {
	return store.LoadContext(context.Background())
}

// LoadContext allows a closing window to stop waiting for another window's
// update. Once loading starts, its bounded local file read runs to completion.
func (store *CatalogStore) LoadContext(ctx context.Context) (catalog *Catalog, generation uint64, note error, err error) {
	if err := store.acquire(ctx); err != nil {
		return nil, 0, nil, err
	}
	defer func() { <-store.writer }()
	store.load()
	catalog, generation, _ = store.Snapshot()
	return catalog, generation, store.note, store.err
}

func (store *CatalogStore) load() {
	if store.loaded {
		return
	}
	store.loaded = true
	builtin, err := Builtin()
	if err != nil {
		store.err = err
		return
	}
	current := builtin
	if store.path != "" {
		installed, err := LoadInstalled(store.path)
		switch {
		case err != nil:
			if !errors.Is(err, os.ErrNotExist) {
				store.note = err
			}
		case !validCatalogVersion(installed):
			store.note = errors.New("the installed catalog has an invalid release")
		case installed.RootCount() < minimumInstalledRoots(builtin.RootCount()):
			// A cache file is untrusted input: a tiny catalog that claims a
			// high version must not silently replace the built-in one.
			store.note = fmt.Errorf("the installed catalog has only %d commands", installed.RootCount())
		case Newer(installed.Release, builtin.Release):
			current = installed
		}
	}
	store.publish(current, ImportStats{})
}

func validCatalogVersion(catalog *Catalog) bool {
	_, ok := parseVersion(catalog.Release)
	return ok
}

// minimumInstalledRoots is the smallest installed catalog the load path trusts;
// the update path applies the same fifth against the current snapshot. It stays
// above zero so a test catalog is still a usable baseline.
func minimumInstalledRoots(builtinRoots int) int {
	floor := builtinRoots / 5
	if floor < 1 {
		floor = 1
	}
	return floor
}

func (store *CatalogStore) publish(catalog *Catalog, stats ImportStats) {
	store.mu.Lock()
	store.catalog, store.stats = catalog, stats
	store.generation++
	store.mu.Unlock()
}

// Update serializes the entire update against the current baseline. Waiting
// and the final file replacement honor cancellation; failure keeps the old
// disk file and memory snapshot. The returned stats describe the import that
// produced the new catalog, or are zero when the catalog was already newest.
// installed reports whether a new snapshot was written, which a caller cannot
// infer from the generation alone: a wake may have published the new catalog
// before the update result is adopted.
func (store *CatalogStore) Update(ctx context.Context, client *http.Client) (*Catalog, uint64, ImportStats, bool, error) {
	if err := store.acquire(ctx); err != nil {
		return nil, 0, ImportStats{}, false, err
	}
	defer func() { <-store.writer }()
	store.load()
	if store.err != nil {
		return nil, 0, ImportStats{}, false, store.err
	}
	if store.path == "" {
		return nil, 0, ImportStats{}, false, errors.New("no data directory")
	}
	return store.updateLoaded(ctx, client)
}

func (store *CatalogStore) acquire(ctx context.Context) error {
	select {
	case store.writer <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-store.writer
		return err
	}
	return nil
}

func (store *CatalogStore) updateLoaded(ctx context.Context, client *http.Client) (*Catalog, uint64, ImportStats, bool, error) {
	current, generation, _ := store.Snapshot()
	release, err := LatestCommit(ctx, client)
	if err != nil {
		return nil, 0, ImportStats{}, false, err
	}
	if !Newer(release.Tag, current.Release) {
		return current, generation, ImportStats{}, false, nil
	}
	catalog, stats, err := FetchCatalog(ctx, client, release)
	if err != nil {
		return nil, 0, ImportStats{}, false, err
	}
	// Losing more than a fifth of the commands, options or nodes indicates a
	// partial or damaged source; do not replace a working snapshot with it.
	// Root count alone is not enough: unparsable subcommand files can vanish
	// while every canonical root file still imports.
	if !coversBaseline(catalog, current) {
		return nil, 0, ImportStats{}, false, fmt.Errorf(
			"the catalog covers %d roots, %d commands and %d options, down from %d, %d and %d",
			catalog.RootCount(), catalog.NodeCount(), catalog.FlagCount(),
			current.RootCount(), current.NodeCount(), current.FlagCount())
	}
	if err := installCatalog(ctx, store.path, catalog); err != nil {
		return nil, 0, ImportStats{}, false, err
	}
	store.publish(catalog, stats)
	catalog, generation, _ = store.Snapshot()
	return catalog, generation, stats, true, nil
}

// coversBaseline reports whether candidate keeps at least four fifths of the
// roots, commands and options of the snapshot it would replace.
func coversBaseline(candidate, current *Catalog) bool {
	return candidate.RootCount()*5 >= current.RootCount()*4 &&
		candidate.NodeCount()*5 >= current.NodeCount()*4 &&
		candidate.FlagCount()*5 >= current.FlagCount()*4
}
