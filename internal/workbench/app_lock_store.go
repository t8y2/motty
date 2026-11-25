package workbench

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	lockVerifierVersion  = 1
	lockSaltBytes        = 16
	lockHashBytes        = 32
	lockPasswordMinRunes = 6
	lockPasswordMaxBytes = 1024
)

var (
	errLockWrongPassword    = errors.New("incorrect application lock password")
	errLockCorrupt          = errors.New("invalid application lock settings")
	errLockPasswordTooShort = errors.New("application lock password is too short")
	errLockPasswordTooLong  = errors.New("application lock password is too long")
)

type lockVerifier struct {
	Version  int    `json:"version"`
	Salt     []byte `json:"salt"`
	Hash     []byte `json:"hash"`
	SystemID string `json:"system_id,omitempty"`
}

func readLockVerifier(path string) (lockVerifier, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return lockVerifier{}, nil
	}
	if err != nil {
		return lockVerifier{}, err
	}
	defer file.Close()
	var record lockVerifier
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return lockVerifier{}, err
	}
	if len(data) > 4096 || json.Unmarshal(data, &record) != nil || record.Version != lockVerifierVersion ||
		len(record.Salt) != lockSaltBytes || len(record.Hash) != lockHashBytes {
		return lockVerifier{}, errLockCorrupt
	}
	if record.SystemID != "" {
		raw, err := hex.DecodeString(record.SystemID)
		if err != nil || len(raw) != 16 || hex.EncodeToString(raw) != record.SystemID {
			return lockVerifier{}, errLockCorrupt
		}
	}
	return record, nil
}

func deriveLockHash(password string, salt []byte) []byte {
	passwordBytes := []byte(password)
	defer clear(passwordBytes)
	return argon2.IDKey(passwordBytes, salt, 3, 64*1024, 4, lockHashBytes)
}

func validateNewLockPassword(password string) error {
	if len(password) > lockPasswordMaxBytes {
		return errLockPasswordTooLong
	}
	if utf8.RuneCountInString(password) < lockPasswordMinRunes {
		return errLockPasswordTooShort
	}
	return nil
}

func newLockVerifier(password, systemID string) (lockVerifier, error) {
	if err := validateNewLockPassword(password); err != nil {
		return lockVerifier{}, err
	}
	salt := make([]byte, lockSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return lockVerifier{}, err
	}
	return lockVerifier{Version: lockVerifierVersion, Salt: salt, Hash: deriveLockHash(password, salt), SystemID: systemID}, nil
}

func (r lockVerifier) verify(password string) error {
	if len(password) > lockPasswordMaxBytes {
		return errLockPasswordTooLong
	}
	if r.Version != lockVerifierVersion {
		return errLockCorrupt
	}
	candidate := deriveLockHash(password, r.Salt)
	defer clear(candidate)
	if subtle.ConstantTimeCompare(candidate, r.Hash) != 1 {
		return errLockWrongPassword
	}
	return nil
}

func (r lockVerifier) save(path string) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}
