#!/usr/bin/env python3
"""Verify external editor saves over loopback SSH without opening a window."""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time


ROOT = Path(__file__).resolve().parents[2]


def command(process, text):
    process.stdin.write(text + "\n")
    process.stdin.flush()
    while True:
        line = process.stdout.readline()
        if not line:
            raise AssertionError(f"headless process exited during {text!r}: {process.stderr.read()}")
        if line.startswith("error"):
            raise AssertionError(f"{text}: {line.rstrip()}")
        if line.startswith("ok"):
            return line.rstrip()


def free_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def verify():
    with tempfile.TemporaryDirectory(prefix="motty-external-edit-e2e-") as scratch:
        root = Path(scratch)
        binary = Path(os.environ.get("MOTTY_HEAD", str(root / "motty-head")))
        if "MOTTY_HEAD" not in os.environ:
            subprocess.run(["go", "build", "-tags", "vivahead", "-o", str(binary), "./cmd/motty"], cwd=ROOT, check=True)
        server_binary = root / "e2esshd"
        subprocess.run(["go", "build", "-o", str(server_binary), "."], cwd=ROOT / "tools/e2esshd", check=True)
        port = free_port()
        server = subprocess.Popen([str(server_binary), str(port)], cwd=ROOT,
                                  env=dict(os.environ, TMPDIR=str(root), TEMP=str(root), TMP=str(root)),
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            if server.stdout.readline().strip() != f"listening on 127.0.0.1:{port}":
                raise AssertionError("loopback SSH server failed to start")
            while True:
                line = server.stdout.readline()
                if not line:
                    raise AssertionError("loopback SSH server did not report its SFTP root")
                if line.startswith("sftp root "):
                    remote_file = Path(line.removeprefix("sftp root ").strip()) / "alpha.txt"
                    break
            home = root / "home"
            data = home / "motty-data"
            data.mkdir(parents=True)
            process = subprocess.Popen([str(binary)], cwd=ROOT,
                                       env=dict(os.environ, VIVA_HEADLESS="1", HOME=str(home),
                                                TMPDIR=str(root), TEMP=str(root), TMP=str(root),
                                                MOTTY_DATA_DIR=str(data), MOTTY_LANG="zh-CN"),
                                       stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, text=True, bufsize=1)
            try:
                if not process.stdout.readline().startswith("ok ready "):
                    raise AssertionError("headless app failed to start")
                for text in (
                    'click "搜索"', f"type tester@127.0.0.1:{port}", 'click "连接"',
                    'waitfor "信任此主机" 5000', 'click "仅本次连接"',
                    'waitfor "密码" 5000', "type secret", "key enter", "settle 2500",
                    "click id:sftp/sftp-open", 'waitfor "alpha.txt" 5000',
                    'click "alpha.txt"', 'click "用系统编辑器打开"',
                ):
                    command(process, text)
                deadline = time.monotonic() + 8
                while time.monotonic() < deadline:
                    result = command(process, "openedpath")
                    if result != "ok none":
                        local_file = Path(json.loads(result.removeprefix("ok ")))
                        break
                    command(process, "settle 100")
                else:
                    raise AssertionError("the system editor did not receive a local copy")
                original = local_file.read_bytes()
                info = local_file.stat()
                changed = original.upper()
                if changed == original or len(changed) != len(original):
                    raise AssertionError("fixture cannot exercise equal-size saves")
                local_file.write_bytes(changed)
                os.utime(local_file, ns=(info.st_atime_ns, info.st_mtime_ns))
                deadline = time.monotonic() + 12
                while time.monotonic() < deadline:
                    command(process, "settle 100")
                    if remote_file.read_bytes() == changed:
                        break
                else:
                    raise AssertionError("same-size, same-timestamp save did not reach the SFTP server")
                local_file.write_text("second external save\n")
                deadline = time.monotonic() + 12
                while time.monotonic() < deadline:
                    command(process, "settle 100")
                    if remote_file.read_text() == "second external save\n":
                        break
                else:
                    raise AssertionError("second external save did not reach the SFTP server")
                print("external editor saves reached the loopback SFTP server")
            finally:
                process.terminate()
                process.wait(timeout=10)
        finally:
            server.terminate()
            server.wait(timeout=10)


if __name__ == "__main__":
    verify()
