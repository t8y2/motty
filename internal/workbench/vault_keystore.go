package workbench

import (
	"errors"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"motty/internal/buildinfo"
	"motty/internal/vault"
)

// systemKeystore is the vault's key store on the real machine: the system's own
// secret store, through Viva.
type systemKeystore struct {
	release bool
}

func (store systemKeystore) service(name string) string {
	if store.release {
		return name
	}
	return keyService(name)
}

func (store systemKeystore) Store(service, account string, secret []byte) error {
	return keystoreError(app.StoreSecret(store.service(service), account, secret))
}

func (store systemKeystore) Load(service, account string) ([]byte, error) {
	secret, err := app.LoadSecret(store.service(service), account)
	return secret, keystoreError(err)
}

func (store systemKeystore) Delete(service, account string) error {
	return keystoreError(app.DeleteSecret(store.service(service), account))
}

func keyService(service string) string {
	if buildinfo.Development {
		return service + " development"
	}
	return service
}

// defaultKeystore keeps the data key of the vault at path in the system's secret
// store, and in a key file next to the vault where the system has none.
func defaultKeystore(vaultPath string) vault.Keystore {
	return vault.FallbackKeystore{System: systemKeystore{}, File: vault.FileKeystore{Path: filepath.Join(filepath.Dir(vaultPath), "vault.key")}}
}

// keystoreError names what the system's store said in the vault's terms.
func keystoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, app.ErrSecretNotFound):
		return vault.ErrKeystoreNotFound
	case errors.Is(err, app.ErrSecretStoreUnavailable):
		return vault.ErrKeystoreUnavailable
	case errors.Is(err, app.ErrPermissionDenied):
		return vault.ErrKeystoreDenied
	}
	return err
}
