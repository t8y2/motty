//go:build !windows

package selfupdate

func windowsInstallDirs() (user, machine string) { return "", "" }
