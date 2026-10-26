package workbench

import (
	"errors"

	"github.com/vivaui/viva/app"
	"motty/internal/vault"
)

// systemKeystore is the vault's key store on the real machine: the system's own
// secret store, through Viva.
type systemKeystore struct{}

func (systemKeystore) Store(service, account string, secret []byte) error {
	return keystoreError(app.StoreSecret(service, account, secret))
}

func (systemKeystore) Load(service, account string) ([]byte, error) {
	secret, err := app.LoadSecret(service, account)
	return secret, keystoreError(err)
}

func (systemKeystore) Delete(service, account string) error {
	return keystoreError(app.DeleteSecret(service, account))
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
