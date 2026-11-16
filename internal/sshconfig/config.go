package sshconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrChanged reports that a file was changed by someone else between loading
// it and saving it. Nothing was written; load it again and redo the edits.
var ErrChanged = errors.New("SSH config changed while it was being edited")

// backupSuffix names the copy of a file kept before Motty first rewrites it.
const backupSuffix = ".motty-backup"

// Config is an OpenSSH client configuration, with its Include files, held so it
// can be read as the statically evaluable hosts it describes and edited without
// disturbing anything the edits do not name: comments, blank lines, indentation,
// directives this package does not know and the order of blocks all stay.
type Config struct {
	root   string
	docs   map[string]*doc
	order  []string
	result Result
	// stale is set by an edit: the hosts are worked out again when next asked
	// for, not after each of many edits.
	stale bool
}

// Load reads the configuration at path and every file it includes. A root file
// that does not exist yet is an empty configuration that Save creates.
func Load(path string) (*Config, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("resolve SSH config: %w", err)
	}
	root := canonicalPath(absolute)
	config := &Config{root: root, docs: make(map[string]*doc)}
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		config.docs[root] = newDoc(root, nil, false, 0o600)
		config.order = append(config.order, root)
	}
	if err := config.evaluate(); err != nil {
		return nil, err
	}
	return config, nil
}

// evaluate reads the graph again from the documents held, so edits show up in
// the hosts. Files the edits did not reach are read from disk once.
func (c *Config) evaluate() error {
	p := newParser(c.docs, &c.order)
	result, err := p.run(c.root)
	if err != nil {
		return err
	}
	c.result, c.stale = result, false
	return nil
}

func (c *Config) fresh() {
	if c.stale {
		_ = c.evaluate()
	}
}

// Exists reports whether the root file is on disk, not just waiting to be made.
func (c *Config) Exists() bool { return c.docs[c.root].exists }

// Path is the file the configuration was loaded from, symbolic links resolved.
func (c *Config) Path() string { return c.root }

// Files lists the files the configuration was read from: the root first, then
// each file it includes, in the order they were met.
func (c *Config) Files() []string { return append([]string(nil), c.order...) }

// Result is the hosts of the configuration as they stand now, edits included.
func (c *Config) Result() Result {
	c.fresh()
	return c.result
}

// Host is the evaluated host called alias, matched without regard to case.
func (c *Config) Host(alias string) (Host, bool) {
	c.fresh()
	for _, host := range c.result.Hosts {
		if equalFold(host.Alias, alias) {
			return host, true
		}
	}
	return Host{}, false
}

// Changed reports whether any file has edits to save.
func (c *Config) Changed() bool {
	for _, d := range c.docs {
		if d.dirty && !bytes.Equal(d.bytes(), d.raw) {
			return true
		}
	}
	return false
}

// Save writes every edited file. Each is replaced whole through a temporary file
// and a rename, keeping its permissions, and only if it still holds what was
// loaded; a file that changed meanwhile is ErrChanged and nothing is written.
// The first time a file is rewritten, its previous content is kept beside it
// under the same name with ".motty-backup" added.
func (c *Config) Save() error {
	var edited []*doc
	for _, path := range c.order {
		d := c.docs[path]
		if d.dirty && !bytes.Equal(d.bytes(), d.raw) {
			edited = append(edited, d)
		}
	}
	for _, d := range edited {
		current, err := os.ReadFile(d.path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if d.exists {
				return fmt.Errorf("%w: %s was removed", ErrChanged, d.path)
			}
		case err != nil:
			return err
		case !d.exists || !bytes.Equal(current, d.raw):
			return fmt.Errorf("%w: %s", ErrChanged, d.path)
		}
	}
	for _, d := range edited {
		if err := writeDoc(d); err != nil {
			return err
		}
		d.raw, d.exists, d.dirty = d.bytes(), true, false
	}
	return nil
}

func writeDoc(d *doc) error {
	directory := filepath.Dir(d.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	mode := d.mode
	if !d.exists {
		mode = 0o600
	}
	if d.exists {
		backup := d.path + backupSuffix
		if _, err := os.Lstat(backup); errors.Is(err, fs.ErrNotExist) {
			if err := os.WriteFile(backup, d.raw, 0o600); err != nil {
				return fmt.Errorf("keep a copy of %s: %w", d.path, err)
			}
		}
	}
	temporary, err := os.CreateTemp(directory, ".ssh-config-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	_, writeErr := temporary.Write(d.bytes())
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, d.path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
