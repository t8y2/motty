#!/usr/bin/env python3
"""Drive a motty headless build from a script file, no window involved.

    go build -tags vivahead,dev -o /tmp/motty-head ./cmd/motty
    python3 tools/headless_drive.py /tmp/some-home commands.txt
    python3 tools/headless_drive.py --build /tmp/some-home commands.txt
    python3 tools/headless_drive.py --watch /tmp/some-home commands.txt

Each non-empty line of the script that does not start with # is one command of
the headless line protocol (see github.com/vivaui/viva/headless/control.go: click, type, key,
waitfor, shot, texts, ...). The script runs with VIVA_HEADLESS=1 and the given
HOME, so saved hosts and known_hosts never touch the real ones; use a fresh
directory for a first-run state. Output is each command followed by its reply.
A reply starting with "error" is printed and also makes the exit status 1.

A script is written against one interface language, and the run gives it that one on every
machine (MOTTY_LANG, which stands in for the system's language): a `# lang: zh-CN` comment line
in the script names it, else MOTTY_LANG from the environment, else Chinese when the script's
commands contain Chinese text and English when they do not.

Besides looking (texts, layout, tree), a script can check: `expect text "Connected" within
5000`, `expect state id:shell/connect enabled`, `expect layout clean`, `expect named` ... (see
Session.Expect in ../viva/headless/expect.go). A control with no name of its own is reached by
identity or role: `click id:home/auto`, `click checkbox:`. When a run fails, the last lines list
each failed command with its line in the script, so the first failure is not lost in the output;
--fail-fast stops at the first one instead of running on into the noise it causes.

--build rebuilds the headless binary first (about 1.5 s when little changed) and
prints the compiler's complaint instead of running when it fails.

--watch is the edit loop: it builds, runs the script, then waits and does both
again whenever a .go file here or in ../viva changes. Every run starts from an
empty home, so the script replays the same path to the same screen each time;
write the screenshots to fixed paths and read them again after each edit. The
`layout` command lists text that is cut off, spills out of its control or lies
over other text, which is faster to read than a picture. Stop it with Ctrl-C.
"""
import os
import re
import subprocess
import shutil
import sys
import time

BINARY = os.environ.get("MOTTY_HEAD", "/tmp/motty-head")
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def build() -> bool:
    started = time.time()
    result = subprocess.run(["go", "build", "-tags", "vivahead,dev", "-o", BINARY, "./cmd/motty"],
                            cwd=ROOT, capture_output=True, text=True)
    if result.returncode != 0:
        print("build failed:\n" + result.stdout + result.stderr)
        return False
    print(f"built in {time.time() - started:.1f}s")
    return True


def sources() -> dict:
    """Modification time of every Go source here and in ../viva."""
    found = {}
    for root in (ROOT, os.path.join(ROOT, "..", "viva")):
        for directory, names, files in os.walk(root):
            names[:] = [name for name in names if name not in (".git", "build", "node_modules")]
            for name in files:
                if name.endswith(".go") or name in ("go.mod", "go.sum"):
                    path = os.path.join(directory, name)
                    try:
                        found[path] = os.stat(path).st_mtime_ns
                    except OSError:
                        pass
    return found


def watch(home: str, script: str) -> int:
    while True:
        before = sources()
        started = time.time()
        if build():
            shutil.rmtree(home, ignore_errors=True)
            run(home, script)
        print(f"--- done in {time.time() - started:.1f}s, waiting for a change")
        while True:
            time.sleep(0.3)
            current = sources()
            if current != before:
                time.sleep(0.3)  # let a burst of saves finish
                break


def script_language(script: str, from_environment: str) -> str:
    """The interface language a script was written for."""
    commands = []
    with open(script, encoding="utf-8") as file:
        for line in file:
            declared = re.match(r"#\s*lang:\s*(\S+)", line)
            if declared:
                return declared.group(1)
            if line.strip() and not line.startswith("#"):
                commands.append(line)
    if from_environment:
        return from_environment
    return "zh-CN" if re.search(r"[\u4e00-\u9fff]", "".join(commands)) else "en"


def run(home: str, script: str, fail_fast: bool = False) -> int:
    os.makedirs(home, exist_ok=True)
    data = os.path.join(home, "motty-data")
    os.makedirs(data, exist_ok=True)
    binary = BINARY
    env = dict(os.environ, VIVA_HEADLESS="1", HOME=home, MOTTY_DATA_DIR=data)
    env["MOTTY_LANG"] = script_language(script, env.get("MOTTY_LANG", ""))
    proc = subprocess.Popen([binary], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, env=env, text=True, bufsize=1)
    try:
        return drive(proc, script, fail_fast)
    finally:
        if proc.poll() is None:
            proc.kill()
            proc.wait()


def drive(proc, script: str, fail_fast: bool = False) -> int:
    ready = proc.stdout.readline().rstrip("\n")
    print(ready)
    if not ready.startswith("ok ready "):
        print("<the process did not become ready>", proc.stderr.read())
        proc.kill()
        return 1
    failures = []
    closed_all = False
    for number, command in enumerate((line.rstrip("\n") for line in open(script)), 1):
        if not command.strip() or command.startswith("#"):
            continue
        if fail_fast and failures:
            break
        print(f"> {command}")
        proc.stdin.write(command + "\n")
        proc.stdin.flush()
        while True:
            line = proc.stdout.readline()
            if line == "":
                print("<the process ended>", proc.stderr.read())
                return 1
            line = line.rstrip("\n")
            print(line if line.startswith(("ok", "error")) else "  " + line)
            if line.startswith("error"):
                failures.append((number, command, line))
            if line.startswith("ok") and "windows=0" in line:
                closed_all = True
            if line.startswith(("ok", "error")):
                break
    if not closed_all:
        proc.stdin.write("quit\n")
        proc.stdin.flush()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
    if failures:
        print(f"--- {len(failures)} failed in {script}:")
        for number, command, reply in failures:
            print(f"  line {number}: {command}\n    {reply}")
    return 1 if failures else 0


def main() -> int:
    arguments = sys.argv[1:]
    flags = [argument for argument in arguments if argument.startswith("--")]
    arguments = [argument for argument in arguments if not argument.startswith("--")]
    if len(arguments) != 2 or set(flags) - {"--build", "--watch", "--fail-fast"}:
        print(__doc__)
        return 2
    home, script = arguments
    if "--watch" in flags:
        try:
            return watch(home, script)
        except KeyboardInterrupt:
            return 0
    if "--build" in flags and not build():
        return 1
    return run(home, script, "--fail-fast" in flags)


if __name__ == "__main__":
    sys.exit(main())
