package selfupdate

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

func extractPortable(archive, dest string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	if len(reader.File) != 2 {
		return errors.New("selfupdate: the portable package has unexpected contents")
	}
	program, note := false, false
	for _, entry := range reader.File {
		if !entry.Mode().IsRegular() {
			return errors.New("selfupdate: the portable package contains a non-file")
		}
		switch entry.Name {
		case "motty.exe":
			program = entry.UncompressedSize64 > 0
		case "motty-data/README.txt":
			note = true
		default:
			return fmt.Errorf("selfupdate: unexpected portable package entry %q", entry.Name)
		}
	}
	if !program || !note {
		return errors.New("selfupdate: the portable package is missing its program or marker")
	}
	return ExtractZip(archive, dest)
}

func verifyPortableArchitecture(path string) error {
	program, err := os.Open(path)
	if err != nil {
		return err
	}
	defer program.Close()
	var header [64]byte
	if _, err := io.ReadFull(program, header[:]); err != nil {
		return err
	}
	if string(header[:2]) != "MZ" {
		return errors.New("selfupdate: the portable program is not a Windows executable")
	}
	peOffset := int64(binary.LittleEndian.Uint32(header[0x3c:]))
	if peOffset < 64 || peOffset > 1<<20 {
		return errors.New("selfupdate: the portable program has an invalid PE header")
	}
	var coff [6]byte
	if _, err := program.ReadAt(coff[:], peOffset); err != nil {
		return err
	}
	machine := uint16(0x8664)
	if runtime.GOARCH == "arm64" {
		machine = 0xaa64
	}
	if string(coff[:4]) != "PE\x00\x00" || binary.LittleEndian.Uint16(coff[4:]) != machine {
		return errors.New("selfupdate: the portable program is for another architecture")
	}
	if info, err := os.Stat(filepath.Clean(path)); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("selfupdate: the portable program is not a regular file")
	}
	return nil
}
