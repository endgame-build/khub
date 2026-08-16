"""Concurrency test: N processes writing one collection at once.

    python3 parity/tools/concurrency.py [--writers 8] [--rounds 3]

Collection writes are the one path with a lock (`.khub/generated/locks/<type>.lock`,
gofrs/flock in Go, fcntl.flock in Python) and nothing has ever exercised it.
Each writer adds rows to the SAME collection file concurrently; afterwards the
file must be parseable, hold exactly the expected number of rows, and contain
no duplicate slugs. Run against both binaries, and mixed.

A failure here is data loss (a lost row) or corruption (a torn file), which no
other test in the suite can see.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
PY = REPO / ".venv" / "bin" / "khub"
GO = REPO / "khub"
ENV = {**os.environ, "TTY_COMPATIBLE": "0", "TZ": "UTC", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}


def init(ws: Path) -> None:
    subprocess.run([str(PY), "-C", str(ws), "init", "preset-cells", str(ws), "--no-wire",
                    "--preset-source", str(REPO / "parity" / "corpus")],
                   env=ENV, capture_output=True, check=True)


def add_row(binary: Path, ws: Path, i: int) -> tuple[int, str]:
    p = subprocess.run([str(binary), "-C", str(ws), "add", "repo",
                        "--title", f"Repo {i:03d}", "--format", "json"],
                       env=ENV, capture_output=True, text=True)
    return p.returncode, (p.stdout + p.stderr).strip()


def inspect(ws: Path) -> tuple[int, list[str], str | None]:
    """(row count, slugs, parse error) read straight from the file."""
    f = ws / "repos.jsonl"
    if not f.exists():
        return 0, [], "repos.jsonl missing"
    slugs, err = [], None
    for n, line in enumerate(f.read_text().splitlines(), 1):
        if not line.strip():
            continue
        try:
            row = json.loads(line)
        except Exception as e:
            err = f"line {n} unparseable: {e}"
            break
        slugs.append(row.get("slug", f"<no slug line {n}>"))
    return len(slugs), slugs, err


def add_file_entity(binary: Path, ws: Path, i: int) -> tuple[int, str]:
    p = subprocess.run([str(binary), "-C", str(ws), "add", "note",
                        "--title", "Shared Title", "--format", "json"],
                       env=ENV, capture_output=True, text=True)
    return p.returncode, (p.stdout + p.stderr).strip()


def inspect_files(ws: Path) -> tuple[int, list[str]]:
    """Per-item entities on disk, by filename."""
    d = ws / "notes"
    if not d.is_dir():
        return 0, []
    names = sorted(p.name for p in d.glob("*.md"))
    return len(names), names


def scenario_per_item(label: str, binaries: list[Path], writers: int) -> bool:
    """Per-item types have NO lock: slug uniqueness rests on the O_EXCL create
    plus minting's retry. Every writer here asks for the SAME title, so they
    all mint from the same base and race for the same filenames."""
    work = Path(tempfile.mkdtemp())
    ok = True
    try:
        ws = work / "ws"
        ws.mkdir()
        init(ws)
        jobs = [(binaries[i % len(binaries)], i) for i in range(writers)]
        with ThreadPoolExecutor(max_workers=writers) as pool:
            results = list(pool.map(lambda a: add_file_entity(a[0], ws, a[1]), jobs))
        succeeded = sum(1 for rc, _ in results if rc == 0)
        count, names = inspect_files(ws)
        dupes = len(names) != len(set(names))
        status = "ok"
        if dupes:
            status, ok = "DUPLICATE FILENAMES", False
        elif count != succeeded:
            status, ok = f"MISMATCH: {succeeded} reported success, {count} files exist", False
        print(f"  {label}: {writers} concurrent adds of the SAME title -> "
              f"{succeeded} ok, {count} files [{status}]")
        for rc, out in results:
            if rc != 0:
                print(f"      refused (fine, if the reason is honest): {out[:120]}")
                break
    finally:
        shutil.rmtree(work, ignore_errors=True)
    return ok


def scenario(label: str, binaries: list[Path], writers: int, rounds: int) -> bool:
    work = Path(tempfile.mkdtemp())
    ok = True
    try:
        ws = work / "ws"
        ws.mkdir()
        init(ws)
        expected = 0
        for r in range(rounds):
            jobs = [(binaries[i % len(binaries)], expected + i) for i in range(writers)]
            with ThreadPoolExecutor(max_workers=writers) as pool:
                results = list(pool.map(lambda a: add_row(a[0], ws, a[1]), jobs))
            expected += writers
            failures = [out for rc, out in results if rc != 0]
            count, slugs, err = inspect(ws)
            dupes = len(slugs) != len(set(slugs))
            status = "ok"
            if err:
                status, ok = f"CORRUPT: {err}", False
            elif dupes:
                status, ok = "DUPLICATE SLUGS", False
            elif count != expected:
                status, ok = f"LOST ROWS: {count} of {expected}", False
            print(f"  {label} round {r + 1}: {writers} concurrent adds -> "
                  f"{count} rows, {len(failures)} write errors [{status}]")
            for f in failures[:2]:
                print(f"      write error: {f[:140]}")
    finally:
        shutil.rmtree(work, ignore_errors=True)
    return ok


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--writers", type=int, default=8)
    ap.add_argument("--rounds", type=int, default=3)
    args = ap.parse_args()
    for b in (PY, GO):
        if not b.exists():
            print(f"missing {b}", file=sys.stderr)
            return 2
    print(f"{args.writers} concurrent writers x {args.rounds} rounds, one jsonl collection\n")
    results = {
        "python-only": scenario("python-only", [PY], args.writers, args.rounds),
        "go-only": scenario("go-only", [GO], args.writers, args.rounds),
        # The lock exists so a Go and a Python khub can share a workspace during
        # dual-ship: flock(2) on Unix is the same lock table for both.
        "mixed py+go": scenario("mixed py+go", [PY, GO], args.writers, args.rounds),
    }
    print("\nper-item entities (no lock; O_EXCL + mint retry is the only guard)")
    results["per-item python"] = scenario_per_item("python-only", [PY], args.writers)
    results["per-item go"] = scenario_per_item("go-only", [GO], args.writers)
    results["per-item mixed"] = scenario_per_item("mixed py+go", [PY, GO], args.writers)

    print()
    for k, v in results.items():
        print(f"  {k:<14} {'PASS' if v else 'FAIL'}")
    return 0 if all(results.values()) else 1


if __name__ == "__main__":
    sys.exit(main())
