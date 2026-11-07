#!/usr/bin/env python3
"""Verify directory-tree keyboard navigation over loopback SSH and SFTP.

Run from the repository root with `python3 tools/e2e/sftp_tree_keyboard_e2e.py`.
The test owns its server, home and build artifacts and opens no real window.
"""

import os
from pathlib import Path
import re
import socket
import subprocess
import tempfile
import time


ROOT = Path(__file__).resolve().parents[2]
TREE_ROWS = "sftp-window/sides/remote/tree-rows/"
TREE_DISCLOSURES = "sftp-window/sides/remote/tree-disclosures/"


def free_loopback_port() -> int:
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def command(process: subprocess.Popen, text: str) -> list[str]:
    process.stdin.write(text + "\n")
    process.stdin.flush()
    lines = []
    while True:
        line = process.stdout.readline()
        if not line:
            raise AssertionError(f"headless process exited during {text!r}: {process.stderr.read()}")
        line = line.rstrip("\n")
        if line.startswith("error"):
            raise AssertionError(f"{text}: {line}")
        if line.startswith("ok"):
            return lines
        lines.append(line)


def tree_rows(process: subprocess.Popen) -> list[str]:
    return [line for line in command(process, "tree") if f"id={TREE_ROWS}" in line]


def row_id(line: str) -> str:
    match = re.search(r"\bid=(\S+)", line)
    if match is None:
        raise AssertionError(f"tree row has no identity: {line}")
    return match.group(1)


def wait_for_row(process: subprocess.Popen, label: str) -> str:
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        for line in tree_rows(process):
            if f'label="{label}"' in line:
                return row_id(line)
        command(process, "settle 100")
    raise AssertionError(f"directory tree did not show {label!r}")


def wait_for_selected_row(process: subprocess.Popen) -> str:
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        selected = [line for line in tree_rows(process) if "state=selected" in line]
        if len(selected) == 1:
            return row_id(selected[0])
        command(process, "settle 100")
    raise AssertionError("directory tree did not select the current folder")


def verify() -> None:
    with tempfile.TemporaryDirectory(prefix="motty-sftp-tree-e2e-") as scratch:
        root = Path(scratch)
        binary = Path(os.environ.get("MOTTY_HEAD", str(root / "motty-head")))
        server_binary = root / "e2esshd"
        if "MOTTY_HEAD" not in os.environ:
            subprocess.run(["go", "build", "-tags", "vivahead", "-o", str(binary), "./cmd/motty"], cwd=ROOT, check=True)
        subprocess.run(["go", "build", "-o", str(server_binary), "."], cwd=ROOT / "tools/e2esshd", check=True)
        port = free_loopback_port()
        server = subprocess.Popen([str(server_binary), str(port)], cwd=ROOT,
                                  env=dict(os.environ, TMPDIR=str(root)),
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            ready = server.stdout.readline().strip()
            if ready != f"listening on 127.0.0.1:{port}":
                raise AssertionError(f"loopback server did not start: {ready!r}")
            home = root / "home"
            data = home / "motty-data"
            data.mkdir(parents=True)
            process = subprocess.Popen([str(binary)], cwd=ROOT,
                                       env=dict(os.environ, VIVA_HEADLESS="1", HOME=str(home),
                                                MOTTY_DATA_DIR=str(data), MOTTY_LANG="en"),
                                       stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, text=True, bufsize=1)
            try:
                ready = process.stdout.readline()
                if not ready.startswith("ok ready "):
                    raise AssertionError(f"headless app did not start: {ready!r}")
                for text in (
                    'click "Search"', f"type tester@127.0.0.1:{port}", 'click "Connect"',
                    'waitfor "Trust this host" 5000', 'click "Connect Once"',
                    'waitfor "Password" 5000', "type secret", "key enter", "settle 2500",
                    "click id:sftp/sftp-open", 'waitfor "alpha.txt" 5000',
                    "click id:sftp/detach", "settle 500",
                ):
                    command(process, text)
                if not any('label="alpha.txt"' in line and
                           "id=sftp-window/sides/remote/rows/" in line
                           for line in command(process, "tree")):
                    raise AssertionError("loopback SFTP did not populate the remote file list")
                command(process, "click id:sftp-window/sides/remote/tree-toggle")
                parent = wait_for_selected_row(process)
                disclosure = parent.replace(TREE_ROWS, TREE_DISCLOSURES, 1)
                command(process, f"click id:{parent}")
                command(process, "key left")
                command(process, f"expect state id:{disclosure} collapsed")
                command(process, "key right")
                command(process, f"expect state id:{disclosure} expanded")
                command(process, "key right")
                child = wait_for_row(process, "docs")
                command(process, f"expect focus id:{child}")
                command(process, f"expect state id:{child} selected")
                command(process, "key left")
                command(process, f"expect focus id:{parent}")
                command(process, f"expect state id:{parent} selected")
                command(process, "key left")
                command(process, f"expect state id:{disclosure} collapsed")
                if any('label="docs"' in line for line in tree_rows(process)):
                    raise AssertionError("collapsing the parent left its child visible")
                command(process, "key right")
                command(process, "key right")
                command(process, "key enter")
                command(process, 'waitfor "guide.txt" 5000')
                if not any('label="guide.txt"' in line and
                           "id=sftp-window/sides/remote/rows/" in line
                           for line in command(process, "tree")):
                    raise AssertionError("keyboard activation did not open the remote directory")
                print("loopback SFTP tree keyboard navigation: passed")
            finally:
                if process.poll() is None:
                    command(process, "quit")
                    process.wait(timeout=5)
        finally:
            server.terminate()
            server.wait(timeout=5)


if __name__ == "__main__":
    verify()
