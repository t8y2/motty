# Hardware security keys (FIDO2/U2F)

An OpenSSH security key — a key whose type starts with `sk-`, such as
`sk-ssh-ed25519@openssh.com` or `sk-ecdsa-sha2-nistp256@openssh.com` — signs on
the device. The private half in the key file is only a handle, so the key can
authenticate only through an ssh-agent that can reach the device.

Motty does not drive CTAP itself. It uses OpenSSH's `ssh-agent` and
`ssh-sk-helper`, the same components `ssh` uses.

## How a connection uses the key

1. Motty reads the public half of an `sk-*` identity from the key file or the
   vault. The public half is in the clear even when the file is encrypted, so
   the key can be named before the device is touched.
2. After the server's host key is accepted, the key is offered to the server by
   its public half. A key the server does not accept is never signed with, so
   the agent is not started for it.
3. When the server accepts a security key, Motty prepares a short-lived agent
   for that connection: it starts `ssh-agent`, loads the key with `ssh-add`,
   and points `SSH_ASKPASS` at Motty's own hidden helper. A PIN or passphrase
   prompt from `ssh-add` or `ssh-sk-helper` opens a dialog in the window
   instead of waiting for a terminal. If that agent cannot be started, a key
   already loaded in the default agent still works.
4. While the agent waits for the device, the connection page says **Touch your
   security key**. The agent and its temporary directory are removed when the
   connection closes.

The keychain page labels these keys **Security key** and does not offer to
save their passphrase: `ssh-add` asks for it while connecting, and the agent
that keeps it exists only for that connection.

## Requirements

- OpenSSH tools with FIDO support: `ssh-agent`, `ssh-add`, `ssh-sk-helper` and
  `libfido2`.
- macOS: a Homebrew OpenSSH is preferred (`/opt/homebrew/bin/ssh-agent`).
  `brew install openssh libfido2` when the built-in tools do not support the
  key.
- Windows: Git for Windows' user-mode `ssh-agent.exe` is preferred. The
  OpenSSH Authentication Agent service may not support `sk-*` keys or show a
  PIN prompt.
- Linux: the distribution's `openssh-client` package.
- The authentication helper starts Motty itself in a hidden mode. This works
  for the installed application and for a development build that runs from its
  own executable.

## Limits

- CTAP2/HID is not implemented; a key must be usable through an agent.
- Motty does not create security keys. Generate one with
  `ssh-keygen -t ed25519-sk` (or `-t ecdsa-sk`) and import the private key.
- Agent forwarding forwards the host's configured agent, not the
  per-connection security-key agent. If a remote shell needs the security key,
  load it into your login agent and forward that.
- The passphrase of an encrypted `sk-*` key file is asked by `ssh-add` on each
  connection; Motty does not keep it.
- The protocol path is covered by tests with a simulated authenticator and a
  fake agent. Touch, PIN and device-enumeration behavior on real hardware
  depends on the device, `libfido2` and the agent, and has not been verified on
  physical keys in this change.
