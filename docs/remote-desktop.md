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
| Bitmap that changes in place, partial upload of only the changed tiles | Viva: `gfx.Surface` (`github.com/vivaui/viva/gfx/surface.go`), with the painter staging only bitmaps whose identity or version changed |
| Showing such a bitmap one pixel to one device pixel at any display scale | Viva: `gfx.Surface.DrawPixels` and `gfx.Image.DevicePixels`; the painter keeps those images on device pixels instead of rounding them to logical pixels |
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
  `frame.Present` from `Present`. The workbench passes a `gfx.Surface`, which
  satisfies `Presenter`, so a changed 64x64 patch of a 1080p desktop uploads
  64 KB, not 8 MB. Pixels are BGRA with an undefined fourth byte; `Surface`
  makes it opaque.
* Everything the network goroutine produces ends in `Options.Notify`, which wakes
  the window; `Present` runs on the UI goroutine from `State.Wake` and from the
  page's paint. Nothing polls and an idle desktop schedules no frame.
* Keys go down and up as the keyboard does it (Viva's `TextInput.KeyReleases`):
  a key is down on the remote while it is down here, modifiers are keys of their
  own, so Shift-click and a held Ctrl work, and a key's auto-repeat reaches the
  remote: an RDP server does not repeat a held key by itself, so each repeat is
  sent with the key marked already down (`KBD_FLAGS_DOWN`), while a VNC server
  repeats the key itself and is sent it once. Whatever is still down when the
  window loses focus, the tab is hidden, the keyboard is released with
  `Ctrl+Alt+G` or the connection ends is released first. A backend maps `Key`/`Rune` to its own vocabulary (VNC: X11
  keysyms, `internal/desktop/vnc/keysym.go`; RDP: scancodes or Unicode events).
  `SendText` types characters that are not keys.
* A backend that can receive files through the clipboard implements
  `desktop.FileReceiver`. The workbench puts the received paths on the system
  clipboard and remembers them as what the clipboard holds, so the next sync
  does not offer the remote's own files back to it.
* Passwords are asked through `Options.AskPassword` during the handshake, on the
  same connection. RealVNC system accounts use `Options.AskCredentials` after
  server-key verification. The ordinary password dialog supports vault autofill
  and saving; system-account passwords are kept separately from a host's
  traditional VNC password. A password never reaches the settings file.
* `Dialer` is the byte stream: direct TCP, an HTTP/SOCKS proxy, or a channel of
  an SSH connection. A backend never dials by itself.

## What the workbench does for any backend

* Fit-to-window and 100% scaling; at 100% a picture larger than the pane pans
  with the pointer so that the remote pixel under the pointer stays under it.
* **Fullscreen** in the desktop toolbar or tab menu, or `Ctrl+Alt+Enter`, uses
  Viva's native fullscreen window and gives the whole monitor to the selected
  desktop, hiding other panes, tabs and side panels without changing their saved
  layout. The floating exit button and the same chord restore the window.
  `Ctrl+Alt+G` still releases the keyboard. Fullscreen exits if the connection
  ends, another page is activated, or a Motty dialog needs attention. The
  fullscreen frame is not saved as the next launch's window placement.
* RDP tabs offer **Show network latency (RTT)** in the tab menu, sharing the
  existing latency display preference with SSH terminals (off by default).
  The status line, or a floating badge in fullscreen/picture-only mode, shows
  the latest average TCP RTT reported by the RDP server through MS-RDPBCGR
  network characteristics auto-detection. Its tooltip names the sample time:
  this is not input-to-display latency, and refresh frequency is controlled by
  the server. Before a measurement, or on a server without auto-detection,
  `RTT —` explicitly means unavailable. No ping, artificial frame refresh or
  client polling is used for this display.
  Headless acceptance covers focus handoff, keyboard input, exiting for dialogs,
  restoring the normal layout and idle frame scheduling. A loopback xrdp
  session also verifies real rendering and resizing to 3840x2160. Native Win11
  fullscreen, physical keyboards, and a live Windows server's RTT reports still
  need platform verification; xrdp without auto-detection shows the unavailable
  indicator rather than a fabricated measurement.
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
  sharing, and the escape chord. A host that chose "the picture alone" (its
  editor switch, or the tab menu's checkbox) drops the bar above the picture and
  this line: their room goes to the picture, the escape chord moves to a note in
  the picture's corner while the keyboard is captured, the size and protection
  move to the tab's tooltip, and the bar's controls (scaling, Ctrl+Alt+Del, the
  keyboard switch) live in the tab's menu.
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

Spoken: RFB 3.3/3.7/3.8, including negotiating RealVNC's 5.0 banner down to 3.8;
security None, VNC Authentication, and RealVNC RA2 (type 5); a 32-bit true
colour pixel format asked from the server, so no pixel is converted; encodings
Raw, CopyRect, RRE, Hextile and ZRLE (one zlib stream for the connection);
DesktopSize; pointer, key and cut-text messages.

* **RealVNC RA2** authenticates the server's RSA public key before asking
  for credentials and keeps authentication, framebuffer, input, and clipboard
  messages encrypted with AES-128-EAX. A server using **Always on** encryption
  and **UNIX password** authentication asks for its system username/password.
  Direct VNC hosts have an optional username field; if omitted, the connection
  asks for both fields. A tunneled host's saved username continues to name the
  SSH account; the VNC system account is asked for separately.
  A new key raises a trust question; a changed saved key stops the connection.
  VNC and RDP pins share a local endpoint store, keyed by protocol, the typed
  address, and port. Deleting or merging a host preserves its server pin;
  **Forget this server** removes the pin so the next connection asks again.
  RSA-AES rejection
  never retries with a weaker security type. See [RealVNC support](vnc-realvnc.md).

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
* Not supported: RA256, RA2ne/RA256ne, VeNCrypt/TLS, Apple Remote Desktop
  authentication (macOS Screen Sharing needs "VNC viewers may control screen with
  password"), Tight/JPEG,
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
headless application.

Windows testing against a Raspberry Pi also verified system-account login,
desktop display, and clipboard sharing. See
[RealVNC support](vnc-realvnc.md) for the protocol probe and test coverage.
The latest password and server-key persistence changes still need a real-server
retest.
Physical keyboard and pointer interaction in a native window, IME composition,
hidden or covered windows, macOS Screen Sharing, and a Windows server still need
manual testing.

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
refused. NLA starts with SPNEGO; if token negotiation fails before credentials
are sent, it reconnects once using raw NTLM tokens for older WinPR servers such
as GNOME Remote Desktop on Ubuntu 22.04. Both paths keep certificate checks,
NTLMv2 MIC/channel binding and CredSSP public-key verification. After sending an
Authenticate message, failures stop automatic retries and TLS-only fallback.
Bitmap updates (RLE, Planar, NSCodec, drawing orders) and the RDPGFX
pipeline (ClearCodec, progressive RemoteFX, uncompressed; H.264 is not offered),
fast-path updates, 32-bit colour. Input: keys as scancodes of a US keyboard (the
remote layout decides characters; Shift is added where the character needs it,
`internal/desktop/rdp/scancode.go`), the committed text of an input method and
pastes as Unicode events (UTF-16 pairs; a server application that ignores Unicode
events, such as a terminal in xrdp, shows only some of them), pointer with three
buttons and horizontal/vertical wheel. Clipboard text and images, and files in
both directions (CLIPRDR, see "Copying files to the server" and "Copying files from the server"), per host, off
by default. Suppress Output while the tab is hidden. The initial desktop matches
the pane's device pixels and display scale (1600x900 only when the caller supplies
no size). A server with the RDPEDISP display-control channel follows the pane
in Fit mode, including fullscreen transitions; otherwise the picture is scaled.

### Certificates

An RDP server's certificate is self-signed, so what can be checked is that it is the
one the user accepted. It is passed to the workbench before any credential is sent
(`Options.VerifyCertificate`): a certificate not pinned for that address opens the
same dialog as an unknown SSH host key (type "RDP certificate", SHA-256 fingerprint
in `SHA256:` base64 as `ssh-keygen` prints a key's) with Trust and save, This
connection only, or Reject; a pinned certificate that changed opens it again with a
louder text that shows the old fingerprint; rejecting or dismissing ends the
connection without trying other TLS versions. Pins are in `rdp_certificates` next to
`known_hosts`, one `rdp:host:port SHA256:...` or `vnc:host:port SHA256:...` line per
server, written atomically with mode 0600 (`internal/desktop/pins/pins.go`). Legacy
untyped RDP entries remain readable. **Forget this server** is available in the
changed-certificate prompt and on the failure page after rejection; it removes
the pin without granting trust. For a host reached through SSH the pin is
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

### Copying files to the server

Files go from this computer to the server through the clipboard, the way
`mstsc` does it (MS-RDPECLIP file transfer, `internal/rdpcore/protocol/cliprdr/files.go`).
Copy files in Finder, Explorer or a Linux file manager and paste in the remote
system, or drop them on the picture and paste. The paste shortcut of this computer
(Command+V on macOS, Ctrl+V elsewhere) pastes them by pressing the remote's own
Ctrl+V; a file manager also leaves the files' names on the clipboard as text, and
those are not typed instead. The files dropped stay the remote clipboard until
something new is copied here. The host's clipboard
sharing must be on and the server must negotiate file transfer; a drop on a
connection that cannot take files says why in a notice.

Nothing is sent when the files are copied. Motty announces `FileGroupDescriptorW`
and `FileContents`; when the remote user pastes, the server asks for the list
(names relative to the copied entries, sizes, times, folders before what is in
them) and then for each file's size and byte ranges, which are read from this
computer on demand. So a paste of a large folder needs this window open until it
ends, and a file that was changed after it was copied is refused when it is
read (its size or modification time no longer matches the list) instead of
arriving half old, half new. No file is held open between requests, so a copied
file can still be moved or deleted here meanwhile.

The server only reaches what was copied: it asks by index into the list, never by
path. Limits: 20000 files and folders per copy (a larger selection is refused,
not cut), paths of at most 259 characters inside the copied folder, folders
nested at most 32 deep; symbolic links inside a copied folder are left out (one
that was itself copied is followed), as are devices and sockets, and a file of 4
GiB or more when the server did not announce support for it. What was left out is
counted in the notice. Characters Windows forbids in a name (`\ : * ? " < > |` and
control characters) become `_`. The list is made on a goroutine of its own, since
it walks folders; a second copy made meanwhile replaces a waiting one.

### Copying files from the server

Files copied on the server come back through the same protocol: the server
announces `FileGroupDescriptorW` and `FileContents`, and Motty reads the list and
each file's bytes into a private directory under the system temporary
directory. The top-level entries are then put on this computer's clipboard as a
file list (`CF_HDROP` on Windows, file URLs on macOS, `text/uri-list` and
`x-special/gnome-copied-files` on Linux), so they paste in Finder, Explorer, a
Linux file manager or any application that takes files.

The server's clipboard data is locked (`CB_LOCK_CLIPDATA`) while it is read, as
`mstsc` does, and unlocked when the read ends; a copy made on the server
meanwhile therefore cannot change the list under the read. A new remote
clipboard cancels an older read and releases its lock, so an old selection never
replaces a newer one. The lock is taken before the list request; the size and
64 KiB byte-range requests carry its id, which is what makes Windows serve the
contents after the lock.

The clipboard holds paths, not copies: the staging directory has to stay while
a paste may still read it, and the system's temporary-directory cleanup or the
user removes it afterwards. Names that could leave the staging directory
(`..`, absolute paths, empty components) are refused and counted; Windows'
forbidden characters and device names (`CON`, `COM1`, …) are made safe the same
way as in the other direction; paths are limited to 32 levels and the list to
20000 entries. An entry that cannot be written is counted in the notice rather
than leaving a paste that looks complete.

### Limits

No audio, drive/printer/smartcard/USB redirection, multiple monitors, dynamic
resolution change from the client, RD Gateway, Kerberos or certificate-based logon
(NTLM only), network-level authentication to a domain server that disables NTLM,
H.264, clipboard formats other than text, images and files. A fresh Windows logon
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
`STATUS_LOGON_FAILURE`), a refused certificate (one connection), a clean close, and
files copied to the server: four files and two folders (one file of 3 MiB, so many
requests, one folder two levels deep, a name outside ASCII) copied by the test and pasted
by the remote Explorer, every file compared by SHA-256
(`TestLiveFilesCopiedHereArePastedInExplorer`, `MOTTY_RDP_PASTE_DIR`); files
copied on the server (a `CF_HDROP` list, the same form Explorer's Copy leaves)
locked, read back into the client and compared by SHA-256
(`TestLiveFilesCopiedOnTheServerArriveHere`); text copied on the server arriving
on this computer (`TestLiveTextCopiedOnTheServerArrivesHere`); and the Windows
file-list writer's result read back by PowerShell's
`Get-Clipboard -Format FileDropList` as the same paths
(`TestSetClipboardFilesRoundTrip` in `../viva`). On
**xrdp** in Docker (amd64 under Rosetta): TLS without NLA, pixels via the RDPGFX
path, keys typed as scancodes with Shift (`echo Key-Typed_1` shown in the xterm),
and the headless Motty application end to end (`tools/e2e/rdp_e2e.txt`: add the host,
certificate dialog, password dialog, the picture, click and typing, status line
"TLS"). The workbench's certificate question (new, trusted, changed, rejected) is
covered by a unit test with a fake server. Not exercised: a domain-joined server,
Windows Server editions, Windows 10, an RD Gateway, Kerberos, a real window,
physical keyboards and input-method composition, the macOS and Linux builds of the
window, resizing, the image clipboard against a real server, a real Windows or
Linux desktop pasting the file list the other way, reconnect after a
network loss.
The console session switch was not exercised either: it needs a Windows host with
a console session, and xrdp ignores the field.

## Follow-ups

* VeNCrypt/TLS with a certificate trust prompt like the SSH host-key prompt.
* A cursor shape drawn locally (needs a hidden-cursor option in Viva).
* Telling a covered or minimized window apart (Viva).
