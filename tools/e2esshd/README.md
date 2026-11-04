# e2esshd

A throwaway SSH server for end-to-end checks of the terminal. Password `secret`
for any user, loopback only, runs `/bin/bash --norc` on a real pty (so colors,
`stty size`, `less`, Ctrl+C and resizing all behave as on a real server), and
serves an SFTP fixture containing `alpha.txt` and `docs/guide.txt`. The fixture
root is a fresh temporary directory and is writable so upload, recursive
transfer, atomic replacement, cancellation, and resume paths can be exercised;
the entire root is removed when the server exits.
It also prints the path to a temporary encrypted Ed25519 identity accepted by
the server; its passphrase is `key-secret`, so public-key and passphrase flows
can be exercised without touching the user's real SSH files.
For local, remote, SOCKS5, and HTTP CONNECT forwarding checks it also starts and
prints a loopback uppercase echo endpoint plus an `echo.gop.test` alias. The
alias is resolved by the SSH fixture to the echo endpoint, which proves dynamic
proxies preserve remote DNS semantics. The SSH server accepts `direct-tcpip`
only for those endpoints and any additional loopback endpoints named after the
port argument; all other destinations are rejected, so the fixture is never a
general proxy. Remote `tcpip-forward` requests must name a literal loopback IP;
their listeners close on cancellation or with the owning SSH connection, and
the server logs each `remote-listen` and `remote-forward` event for path checks.
The terminal resource-monitor fixture accepts only the fixed
`motty-stats-v2` exec protocol and streams deterministic CPU, memory, and
network counters, a reading every 200 ms until the client closes the session; arbitrary exec requests are rejected unless the server is started with `E2E_ALLOW_EXEC=1`, which runs them with `/bin/sh -c` (for checking features that run a command on the server).
This permits one instance to act as a jump host for another local fixture:

With `E2E_ALLOW_EXEC=1` a command requested together with a terminal runs on a pty, as sshd does, which is how Motty starts its folder-reporting shell. `E2E_LOGIN_SHELL=/path/to/bash` makes that shell the account's login shell: `$SHELL` names it for the client's probe, and a plain shell request starts it too. Use it to check Bash versions older than the system's.

`E2E_HANG_EXEC=1` accepts every command except the resource-monitor one and never answers it. A client that probes the host for its system therefore has to give up at its own timeout, and the session must be left open: closing a session whose command still runs ends the whole connection on a minimal server such as dropbear. The fixture only stops answering — it never ends the connection itself — so an e2e run shows the client keeps the session, not that it survives dropbear tearing the connection down.

`E2E_NO_SFTP=1` makes it a server without an SFTP subsystem: the subsystem request is refused and the only commands accepted are the `scp -f`/`scp -t` source and sink and the fixed listing, `mkdir`, `rm`, `rmdir`, `mv`, `chmod` and `ln` commands of Motty's SCP backend, matched by exact shape, run without a shell and confined to the fixture root (see `tools/e2e/scp_e2e.txt`).

`E2E_MAX_SFTP=n` refuses an SFTP subsystem request while n are open, as a server with a small `MaxSessions` does; every subsystem opened, refused or closed is logged. Use it to see how a second channel for a folder search behaves (see `tools/e2e/sftp_search_e2e.txt`).

`E2E_SFTP_ONLY=1` makes it an account restricted to SFTP: a shell request is accepted, prints `This service allows sftp connections only.` and ends with exit status 1, while the SFTP subsystem keeps working (see `tools/e2e/sftp_only_e2e.txt`).

It is its own module so the pty dependency never reaches the application.

```sh
cd tools/e2esshd && go build -o /tmp/e2esshd . && /tmp/e2esshd 2222
# In another shell, after starting a target on 2223:
/tmp/e2esshd 2222 127.0.0.1:2223
```

Never expose it: anyone who can reach the port gets a shell as you.

With `E2E_X11=1` it also answers a session's `x11-req` the way sshd does: it
listens on `127.0.0.1:6000+N` (N from 10), gives the shell `DISPLAY=127.0.0.1:N.0`
and `E2E_X11_COOKIE` (the hex of the fake cookie the client sent), and opens an
`x11` channel back to the client for every connection a program makes there.

With `E2E_ALLOW_EXEC=1` the resource-monitor handler also runs the GPU section of
the real monitor command, so a fake `nvidia-smi` first on the fixture's PATH
shows up as GPUs (see `tools/e2e/gpu_stats_e2e.txt`).

With `E2ESSHD_KBI=1` it authenticates through one keyboard-interactive round
instead of a password: it asks `Password: ` and `Verification code: ` together
and accepts `secret` and `123456`. Password authentication is refused, so a
client that answers only one question cannot get in. Run it on a port of its
own (`E2ESSHD_KBI=1 /tmp/e2esshd 2223`) to drive the multi-factor login path.

With `E2ESSHD_KBI=password` the only keyboard-interactive question is `Password: `
(accepting `secret`), which is how a default OpenSSH with PAM takes the password;
the plain password method stays refused (see `tools/e2e/vault_kbi_e2e.txt`).
