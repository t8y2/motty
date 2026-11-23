# Patched golang.org/x/crypto

This is `golang.org/x/crypto` v0.59.0, reduced to the packages Motty builds
(`ssh`, `ssh/agent`, `ssh/knownhosts` and what they import) with test files and
test data removed. `go.mod` replaces the module with this directory. The
upstream BSD license and patent grant are in `LICENSE` and `PATENTS`.

## Change

`ssh/client_auth.go`: a public key query (the unsigned request that precedes
the signed one) may be answered with `SSH_MSG_USERAUTH_FAILURE` carrying the
partial success flag instead of `SSH_MSG_USERAUTH_PK_OK`. Servers that treat the
key as the first of several factors do this, notably bastion hosts written in Go
on an older `x/crypto` server, which sent a failure for a `PartialSuccessError`
at the query. OpenSSH accepts that answer as the method having succeeded and
continues with the methods the server lists, without signing. Unpatched
`x/crypto` read it as a rejected key, dropped the flag, and ended with "no
supported methods remain" (Motty issue #1226).

`ssh/client_auth_keyquery_test.go` covers it against a scripted server: it fails
on the unpatched client with the error above.

## Removal

Drop this directory and the `replace` line in `go.mod` once `x/crypto` releases
a client that handles the partial success flag on the key query. The change is
meant to go upstream; nothing else in this tree differs from v0.59.0.
