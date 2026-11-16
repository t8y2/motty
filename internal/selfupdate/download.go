package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"motty/internal/update"
)

// ErrChecksum means the bytes that arrived are not the ones the release names.
var ErrChecksum = errors.New("selfupdate: the download does not match its checksum")

// Progress is how much of a download has arrived. Size is what the release
// promised.
type Progress struct {
	Done, Size int64
}

// progressEvery is how often a download reports; the last report is always sent.
const progressEvery = 100 * time.Millisecond

// Download fetches asset into dir and returns the file's path. The file only
// appears under its name once its length and SHA-256 are the release's; a
// download that is cut short, longer than promised or altered leaves nothing.
func Download(ctx context.Context, client *http.Client, asset update.Asset, dir string, progress func(Progress)) (string, error) {
	if !asset.Has() || asset.Size <= 0 || asset.Name == "" || filepath.Base(asset.Name) != asset.Name {
		return "", errors.New("selfupdate: the release names no usable package")
	}
	if client == nil {
		// No overall timeout: a large package on a slow link is allowed to take
		// its time, and the caller's context ends it.
		client = &http.Client{}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "motty/"+update.Current)
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("selfupdate: the download host answered %s", response.Status)
	}
	if response.ContentLength >= 0 && response.ContentLength != asset.Size {
		return "", fmt.Errorf("selfupdate: the package is %d bytes, the release says %d", response.ContentLength, asset.Size)
	}

	final := filepath.Join(dir, asset.Name)
	part := final + ".part"
	file, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		file.Close()
		if !committed {
			os.Remove(part)
		}
	}()

	sum := sha256.New()
	buffer := make([]byte, 64<<10)
	var done int64
	last := time.Now()
	for {
		// One byte past the promise tells a longer body from an exact one.
		count, readErr := response.Body.Read(buffer[:min(int64(len(buffer)), asset.Size+1-done)])
		if count > 0 {
			sum.Write(buffer[:count])
			if _, err := file.Write(buffer[:count]); err != nil {
				return "", err
			}
			done += int64(count)
			if done > asset.Size {
				return "", ErrChecksum
			}
			if progress != nil && time.Since(last) >= progressEvery {
				last = time.Now()
				progress(Progress{Done: done, Size: asset.Size})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	if done != asset.Size || hex.EncodeToString(sum.Sum(nil)) != asset.SHA256 {
		return "", ErrChecksum
	}
	if progress != nil {
		progress(Progress{Done: done, Size: asset.Size})
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(part, final); err != nil {
		return "", err
	}
	committed = true
	return final, nil
}
