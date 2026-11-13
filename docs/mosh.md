# Mosh terminals

A host can carry its interactive terminal over Mosh instead of a plain SSH
session. Mosh keeps the session alive while the network changes (Wi-Fi,
hotspot, VPN) and keeps working through packet loss; on a slow link, typed text
is drawn before the server echoes it.

Enable it in the host form: **New Host → Mosh**. Everything
else about the host (address, port, user, password or key, one-time codes, host
key prompt, proxy and jump route for the bootstrap) is the SSH configuration it
already had.

## What the SSH connection is for

Mosh is not a second protocol beside SSH. The SSH connection authenticates the
user, starts `mosh-server` on the host and reads the one-time key and UDP port
it announces. The SSH connection is then closed, and the terminal runs over
UDP. A Mosh host therefore has no SSH server in the session:

- no file panel or SFTP, no port forwarding, no remote monitor
- no assistant commands that run on the server
- no shell integration: folder tracking, prompt-aware completion and the
  login command's prompt readiness fall back to their no-integration behavior
- no X11 or ssh-agent forwarding

If you need those, keep the host on SSH and use Mosh for the shell on a second
host, or run `mosh` inside an SSH terminal.

## Requirements

- `mosh-server` installed on the remote host (for example `apt install mosh` on
  Debian/Ubuntu, `dnf install mosh` on Fedora, `brew install mosh` on macOS).
- A UTF-8 locale on the host. Motty finds one (`locale`, then `locale -a`) and
  passes it with `-l`. A host configured for a non-UTF-8 encoding cannot use
  Mosh; the host form rejects that combination.
- A direct UDP path from this computer to the host. Mosh's UDP port is chosen by
  `mosh-server` from 60000–61000 by default; a firewall must allow it. A host
  reachable only through a jump host or an outbound proxy can still run the SSH
  bootstrap, but the UDP session cannot reach it, and Mosh does not silently
  fall back to SSH.

## Behavior

- The first screen appears when `mosh-server` answers over UDP. If it has not
  answered after a few seconds, the terminal says so and the session keeps
  retrying; the UDP path is the thing to check.
- **Test Connection** in the host form checks the SSH sign-in (host key,
  password or key). The mosh bootstrap runs when the tab connects, so a missing
  `mosh-server` or a blocked UDP port is reported there.
- The session survives address changes and long silences. Closing the tab asks
  `mosh-server` to shut down, so it does not linger on the host.
- Mosh has no way to attach a new client to a session after the app restarts.
  A reconnect starts a new session.
- Prediction uses the same adaptive policy as an SSH terminal: a keystroke is
  drawn locally when the measured echo is slow, and taken back if the server
  does not show it. Mosh's acknowledgement of input lets a guess wait out a
  long round trip; an input the server has applied but never echoes (a password
  prompt) is withdrawn by the normal rule.

## Not verified

The implementation is checked against `mosh-server` 1.4.0 on loopback (connect,
type, resize, server exit, client shutdown) and through the headless app with a
real loopback SSH bootstrap. Real Wi-Fi/hotspot/VPN roaming, a real high-latency
path, real windows, physical keyboards and input methods, Windows and Linux
rendering, and Mosh servers older than 1.4 were not exercised here.
