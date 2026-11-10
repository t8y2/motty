# VNC security negotiation

Motty supports the common RSA-AES and certificate-based VeNCrypt security
schemes below. Server product versions, compiled features, and configuration
determine which schemes a server actually offers.

| Security scheme | RFB type | Authentication | Session encryption |
| --- | --- | --- | --- |
| None | 1 | None | None |
| VNC Authentication | 2 | Traditional VNC password | None |
| RA2 | 5 | Username/password or VNC password | AES-128-EAX |
| RA256 (`RA2_256`) | 129 | Username/password or VNC password | AES-256-EAX |
| RA2ne | 6 | Username/password or VNC password | None; only the authentication exchange is encrypted |
| RA256ne (`RA2ne_256`) | 130 | Username/password or VNC password | None; only the authentication exchange is encrypted |
| VeNCrypt 0.2, X509None | 19 / 260 | None | TLS 1.2 or newer |
| VeNCrypt 0.2, X509Vnc | 19 / 261 | Traditional VNC password | TLS 1.2 or newer |
| VeNCrypt 0.2, X509Plain | 19 / 262 | Username/password | TLS 1.2 or newer |

Full session encryption is preferred: RA256, then RA2, then VeNCrypt. An
identity rejection, cancellation, or cryptographic error ends the attempt; no
weaker authentication is tried after a failure. Legacy-only servers retain
their None/VNC Authentication selection behavior.

## Connecting and trust

For account-based authentication, enter the optional VNC username or answer the
username/password prompt during connection. A deliberately empty username is
allowed for servers such as WayVNC configured without a username. Account-based
passwords are stored separately from traditional VNC passwords, including the
empty-username account.

When no system account was configured and exactly one nonempty system username
is saved for the host, Motty reuses that account and password after verifying
the server's identity. This also keeps an SSH tunnel's account separate from
the VNC system account. Multiple saved accounts are left for the user to choose.

Confirm the RSA key or TLS certificate before sending credentials. The identity
dialog allows rejection, trust for this connection, and trust with saving. A
saved identity change stops the connection, as a changed SSH host key does.

Saved pins are public SHA-256 fingerprints in the endpoint pin store shared
with RDP, in `rdp_certificates` next to SSH's `known_hosts`. VNC entries use
`vnc:host:port`; RDP entries use `rdp:host:port`, and existing RDP entries remain
readable. Catalog entries pointing to the same endpoint share its trust.
Deleting or merging a host does not remove the endpoint's pin. Saved account
passwords continue to belong to the host and account.

The entered server host and port remain part of the pin's scope even when an
SSH tunnel dials loopback. Changing the destination does not reuse trust from
the previous destination. VNC pins cannot reuse SSH or RDP identities.

After a saved key changes, the connection stops before sending credentials.
Once the server's new identity has been verified, use **Forget this server**
and retry to receive a new trust prompt. The forget operation is shared with
RDP. Forgetting a pin does not trust the replacement key or remove passwords.
If reading, saving or forgetting a pin fails, the error remains visible and
does not grant trust. Saving also checks that another connection has not
changed the pin while the trust prompt was open.

## Upgrade note

When a server offers both RA2 and VNC Authentication, Motty now selects RA2.
Existing RealVNC connections may therefore show a server-key trust prompt on
their first connection after upgrading.

## Protocol and cryptographic boundaries

The known RealVNC `RFB 005.000` banner negotiates to `RFB 003.008`.
RFB 3.3, 3.7, 3.8, and newer 3.x minor versions retain their existing version
selection. Other major versions are rejected. Extended security types require
RFB 3.7 or newer; RFB 3.3 supports only None/VNC Authentication.

RSA-AES exchanges RSAES-PKCS1-v1.5-encrypted random values and binds public keys
with authenticated transcript proofs. RA2 uses 16-byte random values and SHA-1;
RA256 uses 32-byte values and SHA-256. Every attempt generates a 2048-bit client
RSA key and accepts valid 1024–8192-bit server keys. Each EAX record authenticates
its length and ciphertext before making plaintext available. Directions have
separate little-endian 128-bit counters, shared by authentication and session
messages. The `ne` modes return to the original stream after credentials and
reject unexpected encrypted plaintext at that boundary.

Usernames/passwords use UTF-8 byte lengths, each limited to 255 bytes in the
current implementation. Oversized or invalid UTF-8 values are rejected without
truncation. VeNCrypt X509Plain encodes these lengths as 32-bit values.

VeNCrypt version 0.1, bare Plain(256), anonymous TLSNone/TLSVnc/TLSPlain(257–259),
standalone ANONTLS(18), UltraVNC DSM/MS-Logon, Apple DH(30), RealVNC private type
13, and unspecified vendor extensions are not supported. Go's TLS library does
not implement anonymous DH/ECDH suites. A VeNCrypt server offering only anonymous
TLS is rejected; it is not automatically retried using a plaintext alternative.
Configure an X.509 mode for certificate-based TLS interoperability.

A server announcing a newer VeNCrypt version is offered 0.2. The connection
continues only if the server explicitly accepts that version; Motty does not
interpret a newer wire format as 0.2.

Text clipboard sharing retains its existing Latin-1 behavior. UTF-8 clipboard
support belongs to the separate clipboard branch.

Automated tests cover the ten published AES-128-EAX vectors, split input and
record boundaries, fuzzing of authentication and record parsing, both RA2
credential subtypes, framebuffer traffic, tampering, replay, truncation,
cancellation and concurrent reads/writes. Workbench tests cover trust decisions,
separate passwords, endpoint pin persistence, trust shared across catalog
entries, destination changes, deletion, failed identity saves and changed-key
recovery through forgetting and a new trust prompt.

Automated coverage includes published AES-128/256-EAX vectors, all four RSA-AES
types and both credential subtypes, encrypted and plaintext session boundaries,
framebuffer traffic, TLS certificate authentication, security preference,
identity rejection/change, cancellation, fragmentation, tampering, replay,
truncation, and concurrent reads/writes. Workbench tests cover explicit
authentication-only permission, empty usernames, separate credential/identity
storage, host copying, and preservation of the source password.

On 2026-11-06, the user tested `motty-vnc-ra2-autologin.exe` against a real
Raspberry Pi. System-account login and desktop display succeeded, and the saved
account/password were reused automatically without a credential dialog.
The user also reported progressive, scan-like desktop updates during window
movement and scrolling. This observation has not been independently reproduced;
no rendering change is included in this authentication work.

On 2026-11-07, independent TigerVNC 1.15 virtual-machine testing passed RA256,
RA2ne and RA256ne with both system-account and VNC-password authentication, and
all three X.509 VeNCrypt subtypes. The Windows desktop backend connected to each
of the nine services three times. Linux tests verified desktop encodings,
pointer/key input, resize, bidirectional Latin-1 clipboard traffic, continuous
records, credential and identity rejection, and default refusal of ne modes.
All three X.509 subtypes also passed forced TLS 1.2 and TLS 1.3 handshakes.

These historical checks used the earlier security extension, before the shared
endpoint pin store and the security details interface. They do not establish
the current interface's native behavior, physical keyboard/IME or OS clipboard
notification behavior. See the local 2026-11-07 test reports for separate
WayVNC/NeatVNC, long-session and native portable-profile results.

## Security information in the interface

The host editor explains automatic negotiation and keeps the SSH tunnel account,
traditional VNC password and VNC system-account password separate. Allowing
RA2ne or RA256ne requires explicit permission in that host's VNC settings.

The connected status strip names the selected VNC mode and its protection.
Security details are available from the desktop toolbar, tab menu and command
palette, including fullscreen. Details show authentication, cipher, negotiated
TLS version, server identity and the security types read during this handshake.
VeNCrypt subtypes are shown only if VeNCrypt was selected and its offers read.
Long values can be scrolled and copied. SSH tunnel protection is listed
separately; RA2ne/RA256ne encrypt only authentication, while X509None encrypts
the VNC session without password authentication.

Unsupported offers show their names and unknown numeric identifiers. A permanent
unsupported-security error stops an active automatic reconnect cycle. Network
EOF remains a connection failure and is not classified as a password rejection.

Old WayVNC 0.6.2 with NeatVNC 0.6.x uses VeNCrypt/X509Plain for authenticated
connections. Newer NeatVNC versions can additionally offer RA2/RA256. The actual
installed library and build features must be checked before claiming support
for a particular distribution or server version.

Protocol references: [TigerVNC RSA-AES](https://github.com/TigerVNC/tigervnc/blob/009df04af8fe3843176636da5ea297cf6b7c7a98/common/rfb/CSecurityRSAAES.cxx),
[TigerVNC VeNCrypt](https://github.com/TigerVNC/tigervnc/blob/009df04af8fe3843176636da5ea297cf6b7c7a98/common/rfb/CSecurityVeNCrypt.cxx),
[NeatVNC X509Plain](https://github.com/any1/neatvnc/blob/175676b6f4505c8d51bfac9d1c7b75271f9e43a3/src/auth/vencrypt.c),
and the [EAX specification](https://web.cs.ucdavis.edu/~rogaway/papers/eax.pdf).
