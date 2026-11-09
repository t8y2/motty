// Command portablezip packs the Windows program as a portable copy: the
// executable beside a motty-data folder, which is what makes Motty keep
// everything next to itself (see internal/datadir).
//
//	portablezip -program motty.exe -time 2026-10-29T12:00:00Z -o motty-0.1.8-windows-amd64.zip
//
// The archive is a function of the program and the time, so rebuilding a commit
// gives the same bytes.
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"motty/internal/datadir"
)

const readme = `Motty portable
==============

This folder is what makes this copy of Motty portable. While it sits next to
motty.exe, Motty keeps its hosts, settings, known hosts and saved passwords here
and leaves nothing else on this computer.

- Copy or move motty.exe and this folder together to take your setup with you.
- The key to the saved passwords is a file in this folder (vault.key), not part of
  Windows. Whoever can copy this folder can read the saved passwords, so keep it
  somewhere you trust.
- Self-managed portable copies can update from within Motty when a signed update
  is available. If automatic updating is unavailable, download the new zip and
  replace only motty.exe, keeping this folder.
- Copies installed by Scoop are managed by Scoop: use scoop update motty.
- To use the normal location instead, move this folder away from motty.exe.

便携版
======

这个文件夹让这份 Motty 成为便携版。只要它和 motty.exe 放在一起,Motty 就把主机、
设置、已知主机和已保存的密码都放在这里,不会在这台电脑上留下别的东西。

- 把 motty.exe 和这个文件夹一起复制或移动,就能带走你的全部配置。
- 已保存密码的密钥是这个文件夹里的一个文件(vault.key),不依赖 Windows。
  能拷走这个文件夹的人就能读出已保存的密码,请把它放在可信的地方。
- 自行解压的便携版在有签名更新时可以通过 Motty 自动更新。如果无法自动更新,
  请下载新版 zip,仅替换 motty.exe,保留这个文件夹。
- 通过 Scoop 安装的副本由 Scoop 管理,请使用 scoop update motty。
- 想改用默认位置,把这个文件夹从 motty.exe 旁边移走即可。
`

func main() {
	program := flag.String("program", "", "the Windows executable")
	stamp := flag.String("time", "", "modification time of every entry, RFC 3339")
	out := flag.String("o", "", "the archive to write")
	flag.Parse()
	if *program == "" || *stamp == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	modified, err := time.Parse(time.RFC3339, *stamp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "portablezip: -time:", err)
		os.Exit(2)
	}
	if err := run(*program, *out, modified); err != nil {
		fmt.Fprintln(os.Stderr, "portablezip:", err)
		os.Exit(1)
	}
}

func run(program, archive string, modified time.Time) (err error) {
	source, err := os.Open(program)
	if err != nil {
		return err
	}
	defer source.Close()
	temporary := archive + ".tmp"
	file, err := os.Create(temporary)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(temporary)
		} else {
			err = os.Rename(temporary, archive)
		}
	}()
	return write(file, source, modified)
}

func write(out io.Writer, program io.Reader, modified time.Time) error {
	writer := zip.NewWriter(out)
	header := &zip.FileHeader{Name: "motty.exe", Method: zip.Deflate, Modified: modified}
	header.SetMode(0o755)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(entry, program); err != nil {
		return err
	}
	// The folder has a file in it: an archive tool may drop an empty directory
	// on extraction, and then the copy would not be portable.
	note := &zip.FileHeader{Name: path.Join(datadir.PortableName, "README.txt"), Method: zip.Deflate, Modified: modified}
	note.SetMode(0o644)
	entry, err = writer.CreateHeader(note)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(entry, readme); err != nil {
		return err
	}
	return writer.Close()
}
