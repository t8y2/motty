package sessionimport

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"

	"golang.org/x/crypto/pbkdf2"
	"gopkg.in/yaml.v3"
)

const tabbyVaultIterations = 100_000

// ErrTabbyVaultUnlock is returned when the master password or the encrypted
// payload cannot be verified. It never contains decrypted data.
var ErrTabbyVaultUnlock = errors.New("could not unlock the Tabby Vault")

// Unlock reads the profiles and attaches their matching SSH login passwords.
// The master password and decrypted payload are not kept on the source.
func (source *TabbyConfigImport) Unlock(passphrase []byte) (Result, error) {
	var vault struct {
		Version  int
		Contents string
		KeySalt  string `yaml:"keySalt"`
		IV       string
	}
	if !source.HasVault() || source.vault.Decode(&vault) != nil {
		return Result{}, errors.New("invalid or missing Tabby Vault")
	}
	if vault.Version != 1 {
		return Result{}, errors.New("unsupported Tabby Vault format version")
	}
	salt, saltErr := hex.DecodeString(vault.KeySalt)
	iv, ivErr := hex.DecodeString(vault.IV)
	ciphertext, dataErr := base64.StdEncoding.DecodeString(vault.Contents)
	if saltErr != nil || ivErr != nil || dataErr != nil || len(salt) != 8 || len(iv) != aes.BlockSize ||
		len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return Result{}, errors.New("invalid Tabby Vault encryption parameters")
	}
	key := pbkdf2.Key(passphrase, salt, tabbyVaultIterations, 32, sha512.New)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return Result{}, ErrTabbyVaultUnlock
	}
	plaintext := make([]byte, len(ciphertext))
	defer clear(plaintext)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	size := len(plaintext)
	padding := int(plaintext[size-1])
	if padding < 1 || padding > aes.BlockSize {
		return Result{}, ErrTabbyVaultUnlock
	}
	var expected [aes.BlockSize]byte
	for i := range expected[:padding] {
		expected[i] = byte(padding)
	}
	if subtle.ConstantTimeCompare(plaintext[size-padding:], expected[:padding]) != 1 {
		return Result{}, ErrTabbyVaultUnlock
	}

	var contents struct {
		Config  json.RawMessage
		Secrets []json.RawMessage
	}
	if err := json.Unmarshal(plaintext[:size-padding], &contents); err != nil ||
		(len(contents.Config) == 0 && contents.Secrets == nil) {
		return Result{}, ErrTabbyVaultUnlock
	}
	config := source.config
	if config.Encrypted {
		config = tabbyConfig{}
		// JSON is valid YAML and uses the same profile fields as config.yaml.
		if err := yaml.Unmarshal(contents.Config, &config); err != nil || config.Profiles == nil {
			return Result{}, errors.New("invalid profiles in the encrypted Tabby configuration")
		}
	}
	result, err := parseTabbyConfig(source.name, config, readTabbyVaultPasswords(contents.Secrets))
	if source.RequiresUnlock() {
		// Passwords embedded in these profiles were also encrypted on disk.
		result.PlainSecrets = 0
	}
	return result, err
}

type tabbyVaultLogin struct {
	host string
	user string
	port int // zero represents an unspecified port in the source
}

type tabbyVaultPasswords map[tabbyVaultLogin]string

func readTabbyVaultPasswords(secrets []json.RawMessage) tabbyVaultPasswords {
	passwords := make(tabbyVaultPasswords)
	for _, raw := range secrets {
		var item struct {
			Type  string
			Value string
			Key   struct {
				Host *string
				User string
				Port int
			}
		}
		if err := json.Unmarshal(raw, &item); err != nil || item.Type != "ssh:password" ||
			item.Key.Host == nil || item.Key.User == "" {
			continue
		}
		login := tabbyVaultLogin{host: *item.Key.Host, user: item.Key.User, port: item.Key.Port}
		// Tabby uses the first exact match in the Vault.
		if _, exists := passwords[login]; !exists {
			passwords[login] = item.Value
		}
	}
	return passwords
}

func (passwords tabbyVaultPasswords) lookup(host, user string, port *int) string {
	login := tabbyVaultLogin{host: host, user: user}
	if port != nil {
		login.port = *port
	}
	if value, found := passwords[login]; found {
		return value
	}
	// Tabby can store a password for a user and port without a host.
	login.host = ""
	return passwords[login]
}
