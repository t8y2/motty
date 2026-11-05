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

When no account was configured and exactly one account password is saved for
the host, the credential prompt prefills that account and password for user
confirmation. Multiple saved accounts are left for the user to choose.

The server's RSA key is verified before credentials are sent. The identity
dialog offers rejection, trust for this connection, and trust with saving. A
saved key change stops the connection, as a changed SSH host key does. Library
callers must supply `VerifyServerKey`; a missing callback rejects RA2.

Saved pins are public SHA-256 fingerprints in the host's configuration record.
They follow its catalog identity through backup, sync, copying and deletion.
The entered server host and port remain part of the pin's scope even when an
SSH tunnel dials loopback. Changing the destination does not reuse trust from
the previous destination. VNC pins cannot reuse SSH or RDP identities.

If saving an identity fails, the host was deleted, or another window saved a
different identity, authentication does not start.

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
separate passwords, backup/restart, host copying, destination changes, deletion
and failed identity saves.

A credential-free probe against RealVNC Server 7.13.1.57 verified 5.0-to-3.8
negotiation, the server's RSA-AES proof and the UserPass subtype. A Windows
client was then used with a real Raspberry Pi system account to complete RA2
login, display the encrypted desktop and share text through the clipboard.

The latest password and identity persistence changes still need a real-server
retest. Other server versions, reconnects and long sessions also need manual
interoperability testing; scripted peers do not establish those results.

Protocol references: [TigerVNC RSA-AES](https://github.com/TigerVNC/tigervnc/blob/009df04af8fe3843176636da5ea297cf6b7c7a98/common/rfb/CSecurityRSAAES.cxx)
and the [EAX specification](https://web.cs.ucdavis.edu/~rogaway/papers/eax.pdf).
