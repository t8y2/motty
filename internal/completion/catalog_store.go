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
}

// NewCatalogStore creates a store for a catalog.bin path without reading it.
func NewCatalogStore(path string) *CatalogStore {
	return &CatalogStore{path: path, writer: make(chan struct{}, 1)}
}

// Snapshot reads only memory; initialization belongs on a background worker.
func (store *CatalogStore) Snapshot() (*Catalog, uint64) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.catalog, store.generation
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
	catalog, generation = store.Snapshot()
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
		case Newer(installed.Release, builtin.Release):
			current = installed
		}
	}
	store.publish(current)
}

func validCatalogVersion(catalog *Catalog) bool {
	_, ok := parseVersion(catalog.Release)
	return ok
}

func (store *CatalogStore) publish(catalog *Catalog) {
	store.mu.Lock()
	store.catalog = catalog
	store.generation++
	store.mu.Unlock()
}

// Update serializes the entire update against the current baseline. Waiting
// and the final file replacement honor cancellation; failure keeps the old
// disk file and memory snapshot. The returned stats describe the import that
// produced the new catalog, or are zero when the catalog was already newest.
func (store *CatalogStore) Update(ctx context.Context, client *http.Client) (*Catalog, uint64, ImportStats, error) {
	if err := store.acquire(ctx); err != nil {
		return nil, 0, ImportStats{}, err
	}
	defer func() { <-store.writer }()
	store.load()
	if store.err != nil {
		return nil, 0, ImportStats{}, store.err
	}
	if store.path == "" {
		return nil, 0, ImportStats{}, errors.New("no data directory")
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

func (store *CatalogStore) updateLoaded(ctx context.Context, client *http.Client) (*Catalog, uint64, ImportStats, error) {
	current, generation := store.Snapshot()
	release, err := LatestCommit(ctx, client)
	if err != nil {
		return nil, 0, ImportStats{}, err
	}
	if !Newer(release.Tag, current.Release) {
		return current, generation, ImportStats{}, nil
	}
	catalog, stats, err := FetchCatalog(ctx, client, release)
	if err != nil {
		return nil, 0, ImportStats{}, err
	}
	// Losing more than a fifth of all commands indicates a partial or damaged
	// source; do not replace a working snapshot with it.
	if catalog.RootCount()*5 < current.RootCount()*4 {
		return nil, 0, ImportStats{}, fmt.Errorf("the catalog covers %d commands, down from %d", catalog.RootCount(), current.RootCount())
	}
	if err := installCatalog(ctx, store.path, catalog); err != nil {
		return nil, 0, ImportStats{}, err
	}
	store.publish(catalog)
	catalog, generation = store.Snapshot()
	return catalog, generation, stats, nil
}
