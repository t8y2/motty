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
* Standard RDP Security (RC4 without TLS) is used only when
  `Options.Security` is `SecurityStandard`; the default refuses a server that
  offers nothing else.
* A wrong password stops after one attempt (`%w` instead of `%v` kept
  `nla.ErrCredentialsFatal` visible). A server that drops the connection after the
  credentials were sent is still not retried, but it is reported as an unproven
  outcome (`nla.ErrClosedAfterCredentials`) instead of a refusal, and the saved
  password is kept: Windows Server 2012 and other old CredSSP peers close on
  handshake mismatches as well. Upstream made 14 logon attempts for one wrong
  password, which locks an account out.
* CredSSP can reconnect once with raw NTLM tokens when SPNEGO negotiation fails
  before credentials are sent, for older WinPR servers. Raw NTLM keeps TLS,
  NTLMv2 MIC/channel binding and public-key verification, but does not consume
  SPNEGO mechanism-list MIC sequence numbers. Failures after an Authenticate
  write stop probing and TLS-only fallback; protocol errors remain distinct
  from credential refusals so the UI does not discard a saved password for them.
* CredSSP runs the SPNEGO token exchange to its end before sending the
  public-key binding. When the acceptor requests a mechListMIC (WinPR never
  treats NTLM as its preferred mechanism, so GNOME Remote Desktop does), the
  client signs the mechanism list it offered and verifies the acceptor's MIC
  with sequence zero; the binding then travels in a separate TSRequest. The MIC
  exchange counts as a signed message on both peers — WinPR's Negotiate layer
  adds one to every later EncryptMessage/DecryptMessage sequence and Windows
  advances its own counters — so the CredSSP sealing sequences start at one,
  while the RC4 handles stay at their start (MS-SPNG 3.2.5.1). Sending the
  Authenticate token and the binding in one TSRequest made libfreerdp 3.x
  answer with its own token in a message that still carried the client's
  pubKeyAuth bytes; the client unsealed them as the server's binding and failed
  the MAC check (issue #661). Windows accepts both forms.
* CredSSP keeps the client's own supported version in every TSRequest and uses
  the server's lower version only to pick the public-key binding. Echoing the
  server's version back made every target older than Windows 10 abort the TLS
  session (internal_error) right after the Authenticate message; patched
  Windows 7 and Server 2008 R2 report version 5 and expect the client's own.
* Extended scancodes (`0xE0xx`) set the extended flag. `ReadRectBGRA` and
  `SetOutputSuppressed` were added. The receive loop recovers from a panic in a
  decoder and ends the connection.
* A key's auto-repeat is sent instead of left to the server, which does not
  repeat a held key: `SendKeyboard` and `SendUnicode` take a `repeat` argument
  and a repeat press carries `KBD_FLAGS_DOWN` (`0x4000`), the flag FreeRDP and
  mstsc set on a key the server already has down.
* The EGFX `ResetGraphics` PDU clears surfaces, output maps and frame state but
  keeps the progressive-codec decoder. Its tile context (SYNC, CONTEXT and
  simple tiles) is not part of the graphics output; Windows sends only UPGRADE
  tiles for a surface it recreates after resizing and does not repeat the first
  pass. Rebuilding the decoder dropped those base tiles and left the resized
  desktop black or fragmented.
* The Bitmap Codecs capability advertises NSCodec only (RemoteFX was advertised
  without being usable by the session's pixel path and made xrdp send surface
  commands that were not drawn).
* Fixes found by fuzzing (`fuzz_test.go`, corpus in `testdata/fuzz`): zero-length
  runs in the Planar codec, ZGFX history distance, padding and expansion limits,
  an MPPC history bound.
