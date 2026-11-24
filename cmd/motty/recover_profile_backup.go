package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/term"
	"motty/internal/protection"
)

// recoverProfileBackup extracts a single historical copy without touching the
// live profile. The password is read from a terminal with echo disabled.
func recoverProfileBackup(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: motty recover-profile-backup ARCHIVE NEW-DIRECTORY")
	}
	archive, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer archive.Close()
	const maxSize = 256 << 20
	info, err := archive.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSize {
		return errors.New("expected a regular Motty recovery archive of up to 256 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(archive, maxSize+1))
	if err != nil {
		return err
	}
	if len(data) > maxSize {
		return errors.New("recovery archive is too large")
	}
	fmt.Fprint(os.Stderr, "Master password for this archive: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return fmt.Errorf("read the password from an interactive terminal: %w", err)
	}
	defer clear(password)
	file, err := protection.OpenRecoveryFile(data, string(password))
	if err != nil {
		return err
	}
	defer clear(file.Data)
	defer clear(file.VaultKey)
	defer clear(file.ProfileKey)
	return writeRecoveredFile(args[1], file)
}

func writeRecoveredFile(directory string, file protection.RecoveryFile) (err error) {
	if !filepath.IsLocal(file.Name) || filepath.Base(file.Name) != file.Name || file.Name == "." {
		return errors.New("invalid recovery archive filename")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return fmt.Errorf("create a new recovery directory: %w", err)
	}
	var created []string
	defer func() {
		if err != nil {
			for _, path := range created {
				_ = os.Remove(path)
			}
			_ = os.Remove(directory)
		}
	}()
	write := func(name string, data []byte) error {
		path := filepath.Join(directory, name)
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		created = append(created, path)
		_, err = out.Write(data)
		return errors.Join(err, out.Close())
	}
	if err := write(file.Name, file.Data); err != nil {
		return err
	}
	if len(file.VaultKey) > 0 {
		if err := write("vault.key", file.VaultKey); err != nil {
			return err
		}
	}
	if len(file.ProtectionJSON) > 0 {
		return write("protection.json", file.ProtectionJSON)
	}
	return nil
}
