# RealVNC RSA-AES support

RealVNC servers may announce `RFB 005.000` while accepting the standard
`RFB 003.008` client version. Motty negotiates this known banner down to 3.8;
3.3, 3.7, 3.8, and newer 3.x minor versions retain their existing behavior.
Other major versions remain unsupported.

## Connecting

For a RealVNC server using **Always on** encryption and **UNIX password**
authentication, enter the server's system username in the optional VNC username
field. If it is empty, the connection asks for username and password together.
The system password is separate from a traditional VNC-specific password.

Confirm the server's RSA key before entering credentials. The identity dialog
offers rejection, trust for this connection, and trust with saving. A saved key
change requires a new decision. Saved keys are named by the user-entered server
address and port, including when an SSH tunnel dials the server's loopback.
They use `vnc_server_keys`, separately from SSH known hosts and RDP certificates.

The connection uses AES-128 encryption for the handshake and the entire RFB
session. Its status reports encrypted VNC rather than TLS. Server-side
authentication or encryption settings do not need to be weakened for this mode.

## Protocol boundaries

Supported encrypted security: **RA2, type 5**. If it is offered alongside None
or traditional VNC Authentication, RA2 is preferred. Rejecting a server key,
canceling, or detecting a cryptographic error never falls back to unencrypted
authentication.

The handshake exchanges RSA public keys, RSAES-PKCS1-v1.5-encrypted random
values, directional SHA-1-derived AES keys, and authenticated public-key
transcript proofs before asking for credentials. It uses a new 2048-bit client
RSA key per attempt and accepts valid 1024–8192-bit server keys. Every EAX
record authenticates its length and ciphertext before making plaintext
available. The two directions have separate little-endian 128-bit record
counters; authentication and session messages share their counters.

Username and password lengths are UTF-8 byte counts, each limited to 255 bytes.
Oversized or invalid UTF-8 credentials are rejected rather than truncated.
Both system UserPass and VNC password-only RA2 subtypes are supported.

RA2ne, RA256, type 13, VeNCrypt/TLS, and other proprietary security schemes are
not supported. A server offering type 13 together with type 5 can use type 5;
type 13 alone remains unsupported. Text clipboard sharing retains the existing
Latin-1 behavior.

## Verification

Coverage includes independent published AES-EAX known-answer vectors,
encrypted authentication and framebuffer updates, legacy version/security
negotiation, key rejection and changes, cancellation, account/password vault
separation, frame tampering, replay, truncation, fragmentation, and concurrent
reads/writes.

A credential-free probe against RealVNC Server 7.13.1.57 verified negotiation
from the 5.0 banner to 3.8, the RSA-AES server proof, and the UserPass subtype.
It stopped before sending a username or password.

Windows testing against a Raspberry Pi verified system-account login, desktop
display, and clipboard sharing. Physical
keyboard and pointer interaction, the encrypted-status indicator, reconnection,
and extended sessions still need manual testing. Chinese clipboard text remains
subject to the Latin-1 limitation.

Protocol versions and server product versions are separate compatibility axes:

| Protocol banner or server | Authentication/security | Verification |
| --- | --- | --- |
| RFB 3.3 | None and VNC Authentication | Scripted handshake tests; RSA-AES is not supported with this version |
| RFB 3.7 and 3.8 | None, VNC Authentication, RSA-AES UserPass and password-only | Scripted handshake tests |
| RealVNC RFB 5.0 banner | Negotiated to 3.8; None, VNC Authentication, RSA-AES | Scripted handshake tests |
| RFB 3.9 banner | Negotiated to 3.8; None, VNC Authentication, RSA-AES UserPass and password-only | Scripted handshake tests |
| Nonstandard RFB 3.5 banner | Negotiated to 3.3; None and VNC Authentication | Scripted handshake tests |
| Vendor RFB 3.889 banner | Negotiated to 3.8 with standard security; private type 30 remains unsupported | Scripted handshake and rejection tests; no macOS compatibility claim |
| RFB 4.0, 5.1 and 6.0 banners | Unsupported protocol version | Rejection tests |
| RA2 offered with other types, in different orders or repeated | Prefer type 5; stop on key rejection | Scripted negotiation tests, with and without a cached password |
| Type 13, RA2ne, RA256, TLS or VeNCrypt alone | Unsupported security | Rejection tests without asking for credentials |
| RealVNC Server 7.13.1.57 | RSA-AES UserPass | Credential-free live protocol probe |
| Raspberry Pi server | System account | Windows client testing verified login, desktop display and clipboard sharing |
| Other RealVNC product versions and other servers' RSA-AES implementations | Depends on their offered protocol and security types | Not verified on real servers |

Authentication-failure tests also cover the version-specific SecurityResult
format: RFB 3.3/3.7 omit a reason string, while negotiated 3.8 includes it.
Both RSA-AES subtypes are tested for failures under 3.7, 3.8, and the 3.9/5.0
banners negotiated to 3.8. Connection-refusal tests separately cover the 32-bit
3.3 security field and the 3.7/3.8 security-type list. A 3.3 server offering an
extended or out-of-range security type is rejected without asking for credentials.
RFB 3.8 None authentication failure is also checked for its reason string.

Workbench regression tests cover independent password storage when copying a
host, preservation of the source password, and protection against automatically
transferring a prefilled password to a changed account or authentication mode.

Protocol references: [TigerVNC RSA-AES client](https://github.com/TigerVNC/tigervnc/blob/master/common/rfb/CSecurityRSAAES.cxx),
[noVNC shared RSA-AES handshake](https://github.com/novnc/noVNC/blob/master/core/ra2.js),
and the [EAX specification](https://web.cs.ucdavis.edu/~rogaway/papers/eax.pdf).
