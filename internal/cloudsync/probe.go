package cloudsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// TestReadWrite checks a disposable object, never the configured backup. Its
// permission checks cover the probe location; object-specific policies can
// still make a subsequent backup upload fail.
func TestReadWrite(ctx context.Context, store Store) error {
	tester, ok := store.(interface{ TestReadWrite(context.Context) error })
	if !ok {
		return errors.New("sync: this service cannot test writes")
	}
	return tester.TestReadWrite(ctx)
}

func ProbeName() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return ".motty-test-" + hex.EncodeToString(id[:]) + ".tmp", nil
}

// Probe checks create and read-back, and removes only the newly created test
// object. Cleanup gets its own deadline even if the user canceled the test.
func Probe(ctx context.Context, store Store, remove func(context.Context) error) (err error) {
	payload := make([]byte, 32)
	if _, err = rand.Read(payload); err != nil {
		return err
	}
	_, err = store.Put(ctx, payload, "")
	// A canceled upload may already have reached the server. Try cleanup even
	// without its acknowledgement; a rejected create must never delete an
	// existing object that won the race.
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cleanupErr := remove(cleanup); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("sync test: could not remove the temporary test object: %w", cleanupErr))
		}
	}()
	if err != nil {
		return err
	}
	actual, _, err := store.Get(ctx)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, payload) {
		return errors.New("sync test: the downloaded test data does not match")
	}
	return nil
}
