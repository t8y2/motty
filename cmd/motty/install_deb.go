package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"motty/internal/selfupdate"
)

func runInstallDeb(args []string) int {
	flags := flag.NewFlagSet(selfupdate.DebCommand, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("path", "", "")
	version := flags.String("version", "", "")
	signature := flags.String("signature", "", "")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
	if err := selfupdate.RunPrivilegedDeb(*path, *version, *signature); err != nil {
		fmt.Fprintln(os.Stderr, "motty install-deb:", err)
		return 1
	}
	return 0
}
