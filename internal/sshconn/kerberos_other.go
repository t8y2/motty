//go:build !((darwin && cgo) || linux || windows)

package sshconn

import "golang.org/x/crypto/ssh"

// KerberosAvailable reports whether the build can use system Kerberos credentials.
const KerberosAvailable = false

func (*Conn) kerberosAuth(string) ssh.AuthMethod { return nil }
