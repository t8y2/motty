# RealVNC RA2 support

Motty supports RealVNC RSA-AES **RA2 (security type 5)** for servers using
system-account authentication with session encryption, such as **Always on**
encryption and **UNIX password** authentication on a Raspberry Pi.

| Security scheme | RFB type | Authentication | Session encryption |
| --- | --- | --- | --- |
| None | 1 | None | None |
| VNC Authentication | 2 | Traditional VNC password | None |
| RA2 | 5 | System username/password or VNC password | AES-128-EAX |

RA2 is selected ahead of unencrypted authentication when offered. Rejection of
the server key, cancellation, or a cryptographic error ends the connection;
Motty does not retry with a weaker scheme after that failure. Legacy-only
servers retain their existing None/VNC Authentication selection behavior.

## Connecting and trust

Enter the optional VNC system username, or answer the username/password prompt
during connection. A host reached through SSH keeps its SSH username separate
from the VNC system account. Saved system-account passwords belong to the host
and account and are separate from traditional VNC passwords.

When no system account was configured and exactly one nonempty system username
is saved for the host, Motty reuses that account and password after verifying
the server's identity. This also keeps an SSH tunnel's account separate from
the VNC system account. Multiple saved accounts are left for the user to choose.

The server's RSA key is verified before credentials are sent. The identity
dialog offers rejection, trust for this connection, and trust with saving. A
saved key change stops the connection, as a changed SSH host key does. Library
callers must supply `VerifyServerKey`; a missing callback rejects RA2.

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

The known RealVNC `RFB 005.000` banner negotiates to `RFB 003.008`. Existing RFB
3.3, 3.7, 3.8 and newer 3.x minor-version negotiation is preserved. Other major
versions, including `RFB 004.001`, are rejected. RA2 requires RFB 3.7 or newer;
RFB 3.3 supports only None/VNC Authentication.

RA2 exchanges RSAES-PKCS1-v1.5-encrypted 16-byte random values and binds the
public keys with authenticated transcript proofs. Each attempt generates a
2048-bit client RSA key and accepts valid 1024–8192-bit server keys. SHA-1 is
required by the RA2 wire protocol for these proofs and directional key
derivation; it is not used as a password hash or certificate signature.

Authentication and the subsequent framebuffer, input and clipboard traffic
use AES-128-EAX. Each record authenticates its length and ciphertext before
releasing plaintext. Each direction has its own little-endian 128-bit counter,
used continuously across authentication and session traffic.

RA2 supports both UserPass and password-only credential subtypes. Username and
password lengths are measured in UTF-8 bytes and limited to 255 bytes each.
Oversized or invalid UTF-8 values are rejected without truncation.

RA256, RA2ne/RA256ne, VeNCrypt/TLS, RealVNC private type 13 and other vendor
authentication extensions are outside this implementation's scope. Text
clipboard sharing retains its existing Latin-1 behavior; UTF-8 clipboard
support belongs to a separate change.

## Verification

Automated tests cover the ten published AES-128-EAX vectors, split input and
record boundaries, fuzzing of authentication and record parsing, both RA2
credential subtypes, framebuffer traffic, tampering, replay, truncation,
cancellation and concurrent reads/writes. Workbench tests cover trust decisions,
separate passwords, endpoint pin persistence, trust shared across catalog
entries, destination changes, deletion, failed identity saves and changed-key
recovery through forgetting and a new trust prompt.

A credential-free probe against RealVNC Server 7.13.1.57 verified 5.0-to-3.8
negotiation, the server's RSA-AES proof and the UserPass subtype. A Windows
client was then used with a real Raspberry Pi system account to complete RA2
login, display the encrypted desktop and share text through the clipboard.

On 2026-11-06, the user tested `motty-vnc-ra2-autologin.exe` against a real
Raspberry Pi. System-account login and desktop display succeeded, and the saved
account/password were reused automatically without a credential dialog.
The user also reported progressive, scan-like desktop updates during window
movement and scrolling. This observation has not been independently reproduced;
no rendering change is included in this authentication work.

This report does not establish saved server-key persistence on RealVNC, a native
application restart, clipboard behavior in this build, other server versions,
or long sessions; those still need manual interoperability testing.

Protocol references: [TigerVNC RSA-AES](https://github.com/TigerVNC/tigervnc/blob/009df04af8fe3843176636da5ea297cf6b7c7a98/common/rfb/CSecurityRSAAES.cxx)
and the [EAX specification](https://web.cs.ucdavis.edu/~rogaway/papers/eax.pdf).
