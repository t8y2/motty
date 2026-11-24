package protection

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"

	"motty/internal/vault"
)

// An installation's ordinary system credential can be shared by several data
// directories. Preparing one directory must never rotate another's vault key.
func (g *Gate) normalVaultKey() ([]byte, error) {
	key, err := g.options.Keystore.Load(vault.KeyService, vault.KeyAccount)
	switch {
	case err == nil:
		if len(key) != 32 {
			clear(key)
			return nil, errors.New("invalid saved vault key")
		}
		return key, nil
	case errors.Is(err, vault.ErrKeystoreNotFound):
		fresh := make([]byte, 32)
		if _, err := rand.Read(fresh); err != nil {
			return nil, err
		}
		return fresh, nil
	default:
		return nil, fmt.Errorf("read ordinary vault key: %w", err)
	}
}

func (g *Gate) storeNormalVaultKey(key []byte) error {
	if len(key) != 32 {
		return errors.New("invalid replacement vault key")
	}
	existing, err := g.options.Keystore.Load(vault.KeyService, vault.KeyAccount)
	if err == nil {
		defer clear(existing)
		if !bytes.Equal(existing, key) {
			return errors.New("the ordinary vault key changed during migration")
		}
		return nil
	}
	if !errors.Is(err, vault.ErrKeystoreNotFound) {
		return err
	}
	if err := g.options.Keystore.Store(vault.KeyService, vault.KeyAccount, key); err != nil {
		return err
	}
	saved, err := g.options.Keystore.Load(vault.KeyService, vault.KeyAccount)
	defer clear(saved)
	if err != nil {
		return err
	}
	if !bytes.Equal(saved, key) {
		return errors.New("ordinary vault key storage verification failed")
	}
	return nil
}
