//go:build !darwin || !cgo

package sshconn

import "golang.org/x/crypto/ssh"

// KerberosAvailable reports whether the build can use macOS system credentials.
const KerberosAvailable = false

func (*Conn) kerberosAuth(string) ssh.AuthMethod { return nil }
