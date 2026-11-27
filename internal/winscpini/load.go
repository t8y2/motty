package winscpini

import (
	"errors"
	"io"
	"os"
	"strings"
)

var errNotRegular = errors.New("not a regular file")

// Load reads an existing regular INI file.
func Load(path string) (*Config, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &os.PathError{Op: "read", Path: path, Err: errNotRegular}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, &os.PathError{Op: "read", Path: path, Err: errors.New("file changed while opening")}
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return &Config{lines: strings.Split(strings.TrimPrefix(string(data), bom), "\n")}, nil
}
