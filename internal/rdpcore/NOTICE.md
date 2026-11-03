# Origin of this directory

`internal/rdpcore` is a trimmed and modified copy of
<https://github.com/bouncyball-git/gopher-rdp> at commit
`bfc357b3a95b9bf770c38b10fd8041e0251a0955` (2026-06-15), MIT licensed; its licence
text is `LICENSE` here. It is a copy and not a `go.mod` dependency because Motty
needs to change how it dials, how it checks the server and what it decodes (below),
and because the upstream module's `go.mod` requires a GUI toolkit (Ebiten) that
only its demo client uses; the library itself imports the Go standard library
only (`go list -deps` shows nothing else).

This is a fork; it does not track upstream. Every change is listed, and marked
"Motty" in a comment where it is in code:

Removed (no Motty use): `client/`, `display/`, `util/ppm.go`, and the protocol
packages `audin`, `rdpdr`, `rdpecam`, `rdpsnd`, `urbdrc` (audio, drive, printer,
serial, parallel, smartcard, USB and camera redirection), with the options and
code in `client.go` that used them, and `Dump*` debugging functions that wrote
files.

Changed:

* Network characteristics auto-detection (MS-RDPBCGR 2.2.14): negotiate and join
  the MCS message channel, answer server RTT/bandwidth requests during licensing
  and the connected session, and expose the server's average RTT with its sample
  timestamp. No synthetic input, graphics probes or client polling are added.
  Continuous bandwidth measurements count normal slow-path, fast-path and
  virtual-channel PDU data after the security header and before decompression;
  connect-time measurements count only their explicit payloads.
* `Options.Dial`, `Options.VerifyCertificate`: the connection comes from the
  caller (SSH tunnel, proxy) and the server's certificate is passed to the caller
  before any credential is sent, instead of `net.DialTimeout` and a blanket
  `InsecureSkipVerify`. A refused certificate ends the attempt (`ErrCertificateRejected`)
  and does not fall back to other TLS versions.
* Standard RDP Security (RC4 without TLS) is off unless `Options.AllowLegacySecurity`.
* A wrong password stops after one attempt (`%w` instead of `%v` kept
  `nla.ErrCredentialsFatal` visible; a server that drops the connection after the
  credentials were sent counts as a refusal). Upstream made 14 logon attempts for
  one wrong password, which locks an account out.
* CredSSP can reconnect once with raw NTLM tokens when SPNEGO negotiation fails
  before credentials are sent, for older WinPR servers. Raw NTLM keeps TLS,
  NTLMv2 MIC/channel binding and public-key verification, but does not consume
  SPNEGO mechanism-list MIC sequence numbers. Failures after an Authenticate
  write stop probing and TLS-only fallback; protocol errors remain distinct
  from credential refusals so the UI does not discard a saved password for them.
* Extended scancodes (`0xE0xx`) set the extended flag. `ReadRectBGRA` and
  `SetOutputSuppressed` were added. The receive loop recovers from a panic in a
  decoder and ends the connection.
* The Bitmap Codecs capability advertises NSCodec only (RemoteFX was advertised
  without being usable by the session's pixel path and made xrdp send surface
  commands that were not drawn).
* Fixes found by fuzzing (`fuzz_test.go`, corpus in `testdata/fuzz`): zero-length
  runs in the Planar codec, ZGFX history distance, padding and expansion limits,
  an MPPC history bound.
