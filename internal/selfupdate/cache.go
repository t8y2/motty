package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"

	"motty/internal/update"
)

func verifyCachedPackage(path string, release update.Release) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != release.Asset.Size {
		return ErrChecksum
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), release.Asset.SHA256) {
		return ErrChecksum
	}
	if err := verifyReleasePackage(path, release); err != nil {
		return err
	}
	return nil
}
