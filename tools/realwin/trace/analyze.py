#!/usr/bin/env python3 -I
"""Merge the three probe logs and report key-to-screen latency and output pacing.

usage: analyze.py DIR [--from MARK --to MARK]

DIR holds trace.log, pump.log, shell.log (and marks.log when a scenario wrote marks).
With --from/--to the report covers only that stretch.
"""
import statistics
import sys
from pathlib import Path


def load(path, tag):
    rows = []
    if not path.exists():
        return rows
    for line in path.read_text(errors="replace").splitlines():
        head, _, rest = line.strip().partition(" ")
        try:
            rows.append((float(head), tag, rest.strip()))
        except ValueError:
            pass
    return rows


def pct(values, q):
    values = sorted(values)
    return values[min(int(len(values) * q), len(values) - 1)]


def line(name, values, unit="ms"):
    if not values:
        print(f"  {name:34s} (none)")
        return
    print(f"  {name:34s} n={len(values):5d} p50={statistics.median(values):8.1f} "
          f"p90={pct(values, .9):8.1f} p99={pct(values, .99):8.1f} max={max(values):8.1f} {unit}")


def main():
    args = sys.argv[1:]
    if not args:
        sys.exit(__doc__)
    root = Path(args[0])
    marks = {m: t for t, _, m in load(root / "marks.log", "mark")}
    lo, hi = float("-inf"), float("inf")
    if "--from" in args:
        lo = marks[args[args.index("--from") + 1]]
    if "--to" in args:
        hi = marks[args[args.index("--to") + 1]]
    rows = sorted(load(root / "trace.log", "win") + load(root / "pump.log", "pump") + load(root / "shell.log", "shell"))
    rows = [r for r in rows if lo <= r[0] <= hi]
    if not rows:
        sys.exit("no events in range; is the probe build running and the scenario finished?")
    print(f"events {len(rows)} over {rows[-1][0] - rows[0][0]:.0f} ms")

    # key down -> first output parsed -> first frame presented
    to_pump, to_screen = [], []
    for i, (t, g, e) in enumerate(rows):
        if g != "win" or not e.startswith("keydown"):
            continue
        pumped = None
        for t2, g2, e2 in rows[i + 1:i + 400]:
            if g2 == "pump" and pumped is None:
                pumped = t2
            if pumped is not None and g2 == "win" and e2 == "Present-returned":
                to_pump.append(pumped - t)
                to_screen.append(t2 - t)
                break
    print("key latency (a key down until its echo is on screen):")
    line("key -> output parsed", to_pump)
    line("key -> frame presented", to_screen)

    win = [(t, e) for t, g, e in rows if g == "win"]
    posts = [t for t, e in win if e == "post"]
    paints = [t for t, e in win if e == "paint-begin"]
    print("frame pacing:")
    line("gap between paints", [b - a for a, b in zip(paints, paints[1:])])
    line("gap between wakeups posted", [b - a for a, b in zip(posts, posts[1:])])
    cost, begin = [], None
    for t, e in win:
        if e == "paint-begin":
            begin = t
        elif e == "draw-done" and begin is not None:
            cost.append(t - begin)
            begin = None
    line("paint-begin -> draw-done", cost)
    handled = painted = 0
    per_paint = []
    for t, e in win:
        if e == "redraw-begin":
            handled += 1
        elif e == "paint-begin":
            per_paint.append(handled)
            handled = 0
    line("wakeups handled per paint", per_paint, unit="")

    pumps = [(t, int(e.split("=")[1])) for t, g, e in rows if g == "pump"]
    reads = [(t, int(e.split("=")[1])) for t, g, e in rows if g == "shell" and e.startswith("read")]
    print("output:")
    print(f"  bytes read from pty {sum(n for _, n in reads)} in {len(reads)} reads; parsed {sum(n for _, n in pumps)} bytes in {len(pumps)} pumps")
    if paints and pumps:
        per_frame, count, idx = [], 0, 0
        for t, n in pumps:
            count += n
            while idx < len(paints) and paints[idx] < t:
                idx += 1
        # bytes parsed between consecutive paints
        bounds = paints
        j = 0
        for a, b in zip(bounds, bounds[1:]):
            total = 0
            for t, n in pumps:
                if a <= t < b:
                    total += n
            per_frame.append(total)
        line("bytes parsed per frame", per_frame, unit="B")


if __name__ == "__main__":
    main()
