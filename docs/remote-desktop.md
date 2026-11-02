# Remote desktops (VNC and RDP)

A remote-desktop host is a tab that shows a picture instead of a terminal. The
protocol-independent part lives in `internal/desktop` and the workbench; a
protocol is a backend that implements `desktop.Session`. VNC is the first
(`internal/rfb` + `internal/desktop/vnc`). RDP is the second
(`internal/rdpcore` + `internal/desktop/rdp`); it needed `desktop.Options` to grow
the account, the size and the certificate hook, and the workbench the protocol
entry, the certificate question and the pin store.

## Boundaries

| Part | Owner |
| --- | --- |
| Bitmap that changes in place, partial upload of only the changed tiles | Viva: `ui.Surface` (`github.com/vivaui/viva/ui/surface.go`), with the painter staging only bitmaps whose identity or version changed |
| Showing such a bitmap one pixel to one device pixel at any display scale | Viva: `ui.Surface.DrawPixels` and `ui.Image.DevicePixels`; the painter keeps those images on device pixels instead of rounding them to logical pixels |
| Pointer with buttons and positions, wheel, raw keys, committed IME text, focus | Viva (already: `RoleTerminal` focus gives raw keys and text events) |
| RFB wire protocol, decoders, security types | `internal/rfb` (pure Go, no UI, no cgo) |
| What every desktop backend offers (`Session`, `Frame`, key/button vocabulary) | `internal/desktop` |
| Tab, scaling, focus capture, status line, reconnect, host editor, vault | `internal/workbench` (`desktop.go`, `desktop_conn.go`) |

## The interface (`internal/desktop/desktop.go`)

```go
type Session interface {
    Info() Info                       // protocol, remote name, whether encrypted
    Size() (w, h int)
    Present(dst Presenter) bool       // UI goroutine: Resize + Update for what changed
    Done() <-chan struct{}; Err() error
    SendPointer(x, y int, b Buttons) error
    SendWheel(x, y int, w Wheel, b Buttons) error
    SendKey(KeyEvent) error           // Key (named) or Rune, Down true/false
    SendText(string) error            // committed IME text, pastes
    SetClipboard(string) error; RemoteClipboard() (string, bool)
    Bells() int
    SetActive(bool)                   // false: ask for no more picture data
    Close() error
}
```

* A backend draws into a `desktop.Frame` (BGRA back buffer + a short list of
  dirty rectangles; `Update`, `Fill`, `Copy` for the decoders) and returns
  `frame.Present` from `Present`. The workbench passes a `ui.Surface`, which
  satisfies `Presenter`, so a changed 64x64 patch of a 1080p desktop uploads
  64 KB, not 8 MB. Pixels are BGRA with an undefined fourth byte; `Surface`
  makes it opaque.
* Everything the network goroutine produces ends in `Options.Notify`, which wakes
  the window; `Present` runs on the UI goroutine from `State.Wake` and from the
  page's paint. Nothing polls and an idle desktop schedules no frame.
* Keys go down and up as the keyboard does it (Viva's `TextInput.KeyReleases`):
  a key is down on the remote while it is down here, modifiers are keys of their
  own, so Shift-click and a held Ctrl work, and a key's auto-repeat is not sent
  again (the remote repeats a key it sees held). Whatever is still down when the
  window loses focus, the tab is hidden, the keyboard is released with
  `Ctrl+Alt+G` or the connection ends is released first. A backend maps `Key`/`Rune` to its own vocabulary (VNC: X11
  keysyms, `internal/desktop/vnc/keysym.go`; RDP: scancodes or Unicode events).
  `SendText` types characters that are not keys.
* Passwords are asked through `Options.AskPassword` during the handshake, on the
  same connection. The workbench answers it with the ordinary password dialog,
  the vault autofill (name `password:vnc@host:port`), and "save password" which
  writes to the vault. A password never reaches the settings file.
* `Dialer` is the byte stream: direct TCP, an HTTP/SOCKS proxy, or a channel of
  an SSH connection. A backend never dials by itself.

## What the workbench does for any backend

* Fit-to-window and 100% scaling; at 100% a picture larger than the pane pans
  with the pointer so that the remote pixel under the pointer stays under it.
* A backend whose server can change its desktop's size while it runs implements
  `desktop.Resizer` (RDP, once the server has opened its display control
  channel). The workbench then asks for the pane's pixels and the display's
  scale when it connects (`desktop.Options.Width/Height/Scale`), draws the
  picture pixel for pixel (`Surface.DrawPixels`) and, in Fit mode, asks the server
  for a new size when the pane has held one for 250 ms. Scaling the picture to
  the pane (what VNC and a server without the channel still get) resamples text
  that the server rasterized for another size, which is what made RDP text look
  soft at 125% and 150%. The status line reads 100% for such a picture.
  Actual size stops the following and keeps the size the server has.
* Pointer events with left, middle, right, wheel notches, and the held modifiers.
* Focus capture: while the picture has the focus, none of Motty's shortcuts run
  and the keys go to the remote. `Ctrl+Alt+G` releases the keyboard (the status
  line says so) and takes it again. The shortcut handler returns before any
  configured shortcut sees the chord.
* The status line: remote size, scale, "Through SSH" or "Not encrypted", clipboard
  sharing, and the escape chord.
* Hidden tabs ask for nothing (`SetActive`); the tab visible in the active
  workspace is active. Viva cannot say whether the window is minimized or covered,
  so that case still receives updates.
* A lost connection keeps the last picture, dimmed, with a Reconnect button and
  Enter; the reconnect policy of Settings applies (the session reports
  `sshconn.ErrDisconnected`).

To add a backend: implement `Session`, add a protocol to the host model
(`protocol.go`: `protocolRDP`, saved transport, default port) and the editor,
and return it from `openDesktopSession`. The host editor's VNC rows
(`paintVNCRows`) are the template for protocol specific switches.

## VNC specifics

Spoken: RFB 3.3/3.7/3.8; security None and VNC Authentication; a 32-bit true
colour pixel format asked from the server, so no pixel is converted; encodings
Raw, CopyRect, RRE, Hextile and ZRLE (one zlib stream for the connection);
DesktopSize; pointer, key and cut-text messages.

* **VNC Authentication is weak**: DES with at most eight password characters and a
  challenge that crosses the network in the clear, so a captured exchange can be
  searched offline. The tab says "Not encrypted" and the log warns. Use
  **Connect through SSH**: the host's address is then the SSH server; Motty uses the
  saved SSH host with the same address and account (its port, algorithms and
  jump route) or plain SSH on port 22, asks the SSH questions first, and dials
  `127.0.0.1:<port>` from the SSH server through a `direct-tcpip` channel.
* Limits on a server: every length is checked before allocating (desktop at most
  7680x4320 pixels, names, reasons, clipboard 256 KB, ZRLE data in proportion to
  the rectangle, RRE sub-rectangles at most the pixels), every rectangle and copy
  must lie inside the desktop, and decoders read no further than the structure
  they decode. Fuzz tests cover the message loop, ZRLE tiles and the handshake.
* The remote cursor is drawn by the server into the framebuffer (the cursor
  pseudo-encoding is not requested). The local pointer over the picture is a
  crosshair.
* Clipboard: per host, default off, text only. Remote to local is applied at
  once; local to remote is sent when the pointer is pressed or a paste chord is
  typed, if it changed. RFB carries Latin-1: other characters go as `?`. Pasting
  with the system paste shortcut types the clipboard on the remote instead.
* Not supported: VeNCrypt/TLS, Apple Remote Desktop authentication (macOS Screen
  Sharing needs "VNC viewers may control screen with password"), Tight/JPEG,
  extended clipboard, images in the clipboard, audio, file transfer, changing the
  remote resolution, multiple screens.
* Key releases reach the remote on macOS, Windows and X11; they were exercised
  with synthetic key events on the Windows and Linux machines (xdotool,
  `keybd_event`), not with a physical keyboard or an input method. Viva's web
  backend does not deliver them. macOS menu equivalents (Cmd+Q, Cmd+W,
  Cmd+H, Cmd+M) are taken by the system before Viva sees them and cannot be
  captured; Command is sent as Super/Windows.
* KasmVNC (a TigerVNC fork) reads a PointerEvent as 12 bytes (16-bit button mask
  plus scroll deltas) instead of the 6 of RFC 6143, so it cannot be driven by a
  standards-following client. TigerVNC, and any server that follows the RFC,
  works.

## Verification

`tools/live/vnc_live.sh` starts throwaway servers and runs the live tests of
`internal/rfb` (`MOTTY_VNC_ADDR` and friends; skipped without them). What was
exercised on real servers when this landed: KasmVNC `Xvnc` on the Linux test
machine (None and VNC Authentication, every encoding, window moves drawn,
DesktopSize, key events seen by `xev`) and TigerVNC in Docker (the same, plus
the headless application: password dialog, picture, pointer position, typed text,
through an SSH tunnel to the e2e SSH fixture). `tools/e2e/vnc_e2e.txt` drives the
headless application. Not exercised: a real window, physical keyboards, IME
composition, hidden or covered windows, macOS Screen Sharing, a Windows server.

## RDP

### Library decision

| | `rcarmo/go-rdp` | `bouncyball-git/gopher-rdp` |
| --- | --- | --- |
| Licence | MIT | MIT |
| Dependencies | pion/dtls, golang.org/x/net, testify, yaml.v3 (MIT/BSD/Apache) | core: standard library only (the Ebiten GUI of its demo is not imported by the library) |
| Aim | browser/WASM client; its README: "primarily tested with XRDP; Windows servers may have issues", NLA "with limitations" | native client, Windows servers, NLA by default |
| NLA/CredSSP | NTLMv2, own MD4 | CredSSP v2-v6, NTLMv2 over SPNEGO with EPA channel binding and MIC, own MD4 (`crypto/rc4`, `hmac`, `md5`, `sha256` from the standard library) |
| Bitmaps | RLE, NSCodec, planar, RemoteFX | RLE (8-32), Planar, NSCodec, RDPGFX with ClearCodec, progressive RemoteFX, ZGFX, MPPC, drawing orders |
| Maintenance | active (July 2026) | active (June 2026) |

The grdp family (x90skysn3k, nakagami, icodeface) is GPL-3.0 and cannot be used in
this Apache-2.0 project. gopher-rdp was chosen because it is the only one of the two
that is a native-Windows-oriented client, and it was tried first: its demo logic
ran against the Windows 11 test machine (NLA on) and connected, authenticated and
drew the logon screen before any Motty code was written.

**It is a copy, not a dependency** (`internal/rdpcore`, see its `NOTICE.md`, origin
commit `bfc357b`, licence kept): the library hard-codes `net.DialTimeout` (no SSH
tunnel or proxy) and `InsecureSkipVerify: true` with no hook to see the certificate
(no pinning), starts 14 logon attempts for one wrong password (account lockout), and
carries 20,000 lines for audio/drive/printer/USB redirection that Motty does not
expose. 37,000 lines remain in the tree (24,000 of code, the rest tests). It is
a fork that does not track upstream; `NOTICE.md` lists every change. The cost of
the copy in the stripped macOS binary is +1.04 MB (19.53 MB to 20.57 MB), with no new
module in `go.mod`.

### What it speaks

TLS (1.0-1.3; TLS 1.2 for CredSSP) with NLA (CredSSP + NTLMv2) preferred, TLS
without NLA if the server allows it; standard RDP Security (RC4 without TLS) is
refused. Bitmap updates (RLE, Planar, NSCodec, drawing orders) and the RDPGFX
pipeline (ClearCodec, progressive RemoteFX, uncompressed; H.264 is not offered),
fast-path updates, 32-bit colour. Input: keys as scancodes of a US keyboard (the
remote layout decides characters; Shift is added where the character needs it,
`internal/desktop/rdp/scancode.go`), the committed text of an input method and
pastes as Unicode events (UTF-16 pairs; a server application that ignores Unicode
events, such as a terminal in xrdp, shows only some of them), pointer with three
buttons and horizontal/vertical wheel. Clipboard text (CLIPRDR), per host, off by
default. Suppress Output while the tab is hidden. The desktop is **1600x900 and
fixed**: Motty does not resize the remote desktop to the pane (the picture is
scaled instead), the server may change it and the picture follows.

### Certificates

An RDP server's certificate is self-signed, so what can be checked is that it is the
one the user accepted. It is passed to the workbench before any credential is sent
(`Options.VerifyCertificate`): a certificate not pinned for that address opens the
same dialog as an unknown SSH host key (type "RDP certificate", SHA-256 fingerprint
in `SHA256:` base64 as `ssh-keygen` prints a key's) with Trust and save, This
connection only, or Reject; a pinned certificate that changed opens it again with a
louder text that shows the old fingerprint; rejecting or dismissing ends the
connection without trying other TLS versions. Pins are in `rdp_certificates` next to
`known_hosts`, one `address SHA256:...` line per server, written atomically with
mode 0600 (`internal/desktop/rdp/pins.go`). For a host reached through SSH the pin is
kept under the name typed in the host, not the loopback address that is dialed.
Nothing is trusted silently: `InsecureSkipVerify` stays on only because Go's chain
check cannot succeed, and `VerifyConnection` does the real check.

### Accounts

The host's user is the Windows account (`user`, `DOMAIN\user` or a principal name).
The password is asked on the same connection after the certificate (so a server
that was not trusted never sees it), with autofill and "save" through the vault
under `password:rdp:<user>@<host>:<port>`. A wrong password is tried once: Windows
counts every failed NLA logon towards the account lockout (10 on the test machine);
Motty reports the refusal and asks again. xrdp checks the password after the
handshake and shows its own login screen, so a wrong one there is not detected by the
client. "Connect through SSH" uses the first saved SSH host with the same address
(or plain SSH on port 22), as VNC does.

**Console session.** An RDP host can ask Windows for its console session instead
of a new one, which is what `mstsc /admin` does: the client sets
`REDIRECTED_SESSIONID_FIELD_VALID` in its cluster data and sends session id 0
(`internal/rdpcore/client.go`). The switch is per host (`console` in the state
file), off by default, and only the RDP form shows it. Windows decides whether to
grant the request: the account still needs the right to log on, policy still
applies, and what happens when someone is already sitting at the console is
Windows' decision, not the client's. Only Windows has a console session; xrdp
ignores the field and always starts a new session, so the switch does nothing
there.

### Limits

No audio, drive/printer/smartcard/USB redirection, multiple monitors, dynamic
resolution change from the client, RD Gateway, Kerberos or certificate-based logon
(NTLM only), network-level authentication to a domain server that disables NTLM,
H.264, clipboard formats other than text, or file transfer. A fresh Windows logon
that finds the user signed in elsewhere asks the usual "sign out the other user?"
question inside the picture, and a console session is disconnected when the same
user signs in remotely (that is Windows policy, `fSingleSessionPerUser`).
Server-controlled lengths are capped (a segmented ZGFX message at 64 MB, history
distances, MPPC history, Planar runs); the decoders are fuzzed (`fuzz_test.go`,
`go test ./internal/rdpcore -run '^$' -fuzz FuzzZGFX -parallel 3`; fuzzing found
several faults in the upstream decoders, all fixed with their inputs kept in
`testdata/fuzz`), and the receive loop recovers from a decoder panic and ends only
that connection.

### What was exercised

On a real **Windows 11 Pro** machine (the Viva test VM, test binary run ON it against
localhost:3389 with a throwaway local user): NLA off and NLA on; TLS 1.2 handshake,
certificate pinned, logon, the "other user is signed in" question answered by a
click, the profile build, the desktop (non-uniform, 1000+ colours), the Start menu
opened by the Windows key, the taskbar hover, a wrong password (one attempt,
`STATUS_LOGON_FAILURE`), a refused certificate (one connection), a clean close. On
**xrdp** in Docker (amd64 under Rosetta): TLS without NLA, pixels via the RDPGFX
path, keys typed as scancodes with Shift (`echo Key-Typed_1` shown in the xterm),
and the headless Motty application end to end (`tools/e2e/rdp_e2e.txt`: add the host,
certificate dialog, password dialog, the picture, click and typing, status line
"TLS"). The workbench's certificate question (new, trusted, changed, rejected) is
covered by a unit test with a fake server. Not exercised: a domain-joined server,
Windows Server editions, Windows 10, an RD Gateway, Kerberos, a real window,
physical keyboards and input-method composition, the macOS and Linux builds of the
window, resizing, clipboard against a real server, reconnect after a network loss.
The console session switch was not exercised either: it needs a Windows host with
a console session, and xrdp ignores the field.

## Follow-ups

* VeNCrypt/TLS with a certificate trust prompt like the SSH host-key prompt.
* A cursor shape drawn locally (needs a hidden-cursor option in Viva).
* Telling a covered or minimized window apart (Viva).
