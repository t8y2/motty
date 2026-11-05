#!/usr/bin/env python3
"""End to end: download over loopback SFTP, then reveal the actual local file.

Builds a Viva headless Motty and tools/e2esshd in a throwaway directory. The
file-manager request is captured by Viva's driver; no real window is opened.
Run from the repository root with `python3 tools/e2e/reveal_download_e2e.py`.
"""

import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[2]


def free_loopback_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def verify() -> None:
    with tempfile.TemporaryDirectory(prefix="motty-reveal-e2e-") as scratch:
        root = Path(scratch)
        binary = root / "motty-head"
        server_binary = root / "e2esshd"
        subprocess.run(["go", "build", "-tags", "vivahead,dev", "-o", str(binary), "./cmd/motty"], cwd=ROOT, check=True)
        subprocess.run(["go", "build", "-o", str(server_binary), "."], cwd=ROOT / "tools/e2esshd", check=True)
        port = free_loopback_port()
        env = dict(os.environ, TMPDIR=str(root))
        server = subprocess.Popen([str(server_binary), str(port)], cwd=ROOT, env=env,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            ready = server.stdout.readline().strip()
            if ready != f"listening on 127.0.0.1:{port}":
                raise AssertionError(f"loopback fixture did not start: {ready!r}; {server.stderr.read()}")
            home = root / "home"
            home.mkdir()
            download = home / "Downloads" / "alpha.txt"
            shot = Path(os.environ.get("MOTTY_REVEAL_SHOT", str(root / "reveal.png")))
            closed_shot = Path(os.environ.get("MOTTY_REVEAL_CLOSED_SHOT", str(root / "reveal-closed.png")))
            shot.parent.mkdir(parents=True, exist_ok=True)
            closed_shot.parent.mkdir(parents=True, exist_ok=True)
            script = root / "commands.txt"
            script.write_text("\n".join([
                'click "Search"',
                f'type tester@127.0.0.1:{port}',
                'click "Connect"',
                'waitfor "Trust this host" 5000',
                'click "Connect Once"',
                'waitfor "Password" 5000',
                'type secret',
                'key enter',
                'settle 500',
                'key cmd+shift+s',
                'waitfor "alpha.txt" 8000',
                'click "alpha.txt"',
                'click "Download"',
                'click "Transfers"',
                'waitfor "Completed" 10000',
                'shot ' + str(shot),
                'layout',
                'click "Show in folder"',
                'settle 300',
                'revealedpath',
                'click "Transfers"',
                'key cmd+w',
                'click "Transfers"',
                'waitfor "Completed" 5000',
                'shot ' + str(closed_shot),
                'click "Show in folder"',
                'settle 300',
                'revealedpath',
            ]) + "\n")
            drive_env = dict(env, MOTTY_HEAD=str(binary), MOTTY_LANG="en", VIVA_HEADLESS="1")
            result = subprocess.run([sys.executable, str(ROOT / "tools/headless_drive.py"), str(home), str(script)],
                                    cwd=ROOT, env=drive_env, capture_output=True, text=True, timeout=90)
            print(result.stdout)
            if result.returncode != 0:
                raise AssertionError(f"headless UI failed: {result.stderr}")
            if download.read_bytes() != b"alpha from sftp\n":
                raise AssertionError("the real SFTP download has incorrect bytes")
            expected = "ok " + json.dumps(str(download))
            if result.stdout.splitlines().count(expected) != 2:
                raise AssertionError(f"RevealPath did not select the downloaded path before and after the connection closed: {expected}")
            if not shot.is_file() or shot.stat().st_size == 0 or not closed_shot.is_file() or closed_shot.stat().st_size == 0:
                raise AssertionError("no completed-download or closed-connection screenshot was saved")
            print("PASS: real loopback download and Viva headless reveal before and after closing SSH")
        finally:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait()


if __name__ == "__main__":
    verify()
