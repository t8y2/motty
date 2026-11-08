package gate

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
	"unicode/utf8"

	"motty/internal/ai/tools"
)

const MaxRetainedOutput = 4 << 20
const outputBudget = 16 << 20
const outputTTL = 10 * time.Minute

type ownerKey struct{}

func WithOutputOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, ownerKey{}, owner)
}

// OutputOwner is the MCP client session a call arrived on, or "" for a caller
// without one. Backends use it to scope what they keep for later calls.
func OutputOwner(ctx context.Context) string { return outputOwner(ctx) }

func outputOwner(ctx context.Context) string {
	owner, _ := ctx.Value(ownerKey{}).(string)
	return owner
}

type savedOutput struct {
	owner, session, data string
	expires              time.Time
}

// OutputStore owns short-lived, bounded command results for one HTTP hub.
type OutputStore struct {
	mu    sync.Mutex
	items map[string]savedOutput
	bytes int
}

func (store *OutputStore) Clear() {
	store.mu.Lock()
	store.items = nil
	store.bytes = 0
	store.mu.Unlock()
}

func (store *OutputStore) prune(now time.Time) {
	for id, item := range store.items {
		if !now.Before(item.expires) {
			store.bytes -= len(item.data)
			delete(store.items, id)
		}
	}
}

func (store *OutputStore) save(owner, session, data string) (string, error) {
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(random[:])
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.items == nil {
		store.items = make(map[string]savedOutput)
	}
	now := time.Now()
	store.prune(now)
	for store.bytes+len(data) > outputBudget {
		var oldestID string
		var oldest time.Time
		for key, item := range store.items {
			if oldestID == "" || item.expires.Before(oldest) {
				oldestID, oldest = key, item.expires
			}
		}
		if oldestID == "" {
			return "", errors.New("output storage is full")
		}
		store.bytes -= len(store.items[oldestID].data)
		delete(store.items, oldestID)
	}
	store.items[id] = savedOutput{owner, session, data, now.Add(outputTTL)}
	store.bytes += len(data)
	return id, nil
}

func (store *OutputStore) read(owner string, args tools.ToolOutputReadArgs) (savedOutput, string, int, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.prune(time.Now())
	item, ok := store.items[args.OutputID]
	if owner == "" || !ok || item.owner != owner {
		return savedOutput{}, "", 0, errors.New("output not found or expired")
	}
	if args.Offset > len(item.data) {
		return savedOutput{}, "", 0, errors.New("offset exceeds output length")
	}
	if args.Offset < len(item.data) && !utf8.RuneStart(item.data[args.Offset]) {
		return savedOutput{}, "", 0, errors.New("offset splits a UTF-8 character")
	}
	end := min(len(item.data), args.Offset+args.MaxBytes)
	for end < len(item.data) && !utf8.RuneStart(item.data[end]) {
		end--
	}
	return item, item.data[args.Offset:end], end, nil
}
