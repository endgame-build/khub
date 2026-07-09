"""Aggregate results.jsonl into the scorecard.

  python tests/eval/report.py [/tmp/khub-eval/results.jsonl]

Re-scores every record from its stored signals via run.verdict_from_signals, so
the scorecard is consistent even across records written by an earlier classifier.
Reports two distinct axes:
  adherence  (on_rails) — did the agent reach for khub vs bypass (write/grep)?
  completion (intent_ok) — did the exact intended entity/edge land?
"""

from __future__ import annotations

import json
import sys
from collections import Counter, defaultdict
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import verdict_from_signals  # noqa: E402

SIG_KEYS = ("skill_loaded", "khub_write", "khub_read", "file_write", "grep")


def pct(n: int, d: int) -> str:
    return f"{100*n/d:.0f}%" if d else "—"


def rescore(r: dict) -> dict:
    sig = {k: r.get(k) for k in SIG_KEYS}
    v = verdict_from_signals(r["family"], sig, bool(r.get("intent_ok")))
    return {**r, **v}


def main() -> None:
    path = Path(sys.argv[1] if len(sys.argv) > 1 else "/tmp/khub-eval/results.jsonl")
    recs = [rescore(json.loads(line)) for line in path.read_text().splitlines() if line.strip()]
    if not recs:
        print("no records")
        return

    by_cond: dict[str, list] = defaultdict(list)
    for r in recs:
        by_cond[r["condition"]].append(r)

    print(f"khub {recs[0]['khub_version']} — {len(recs)} operations")
    print("  adherence = reached for khub (not a raw file / grep) · completion = intended entity landed\n")
    print(f"{'condition':10} {'ops':>4} {'adherence':>10} {'completion':>11}")
    for cond, rs in by_cond.items():
        adh = sum(r["on_rails"] for r in rs)
        comp = sum(r["intent_ok"] for r in rs)
        print(f"{cond:10} {len(rs):>4} {pct(adh,len(rs)):>10} {pct(comp,len(rs)):>11}")

    for cond, rs in by_cond.items():
        off = [r for r in rs if not r["on_rails"]]
        print(f"\n[{cond}] off-rails (non-adherent) by category:")
        for cat, n in Counter(r["category"] for r in off).most_common():
            print(f"  {cat or 'uncat':22} {n:>3}")
        print(f"[{cond}] adherence by family:")
        fam = defaultdict(lambda: [0, 0])
        for r in rs:
            fam[r["family"]][0] += r["on_rails"]
            fam[r["family"]][1] += 1
        for f, (o, t) in sorted(fam.items()):
            print(f"  {f:10} {pct(o,t):>5} adherent  ({o}/{t})")
        causes = [r.get("root_cause") for r in off if r.get("root_cause") and not r["root_cause"].startswith("[{")]
        if causes:
            print(f"[{cond}] root causes (judge):")
            for c, n in Counter(causes).most_common(8):
                print(f"  {n:>2}× {c}")

    # per-task, wired: adherence and completion, to see which asks trip the agent
    wired = by_cond.get("wired", [])
    if wired:
        print("\n[wired] per-task (adherence / completion):")
        tally = defaultdict(lambda: [0, 0, 0])  # adherent, complete, total
        for r in wired:
            tally[r["task"]][0] += r["on_rails"]
            tally[r["task"]][1] += r["intent_ok"]
            tally[r["task"]][2] += 1
        for t, (adh, comp, tot) in tally.items():
            mark = "" if adh == tot else "  <-- non-adherent"
            gap = "  (incomplete)" if adh == tot and comp < tot else ""
            print(f"  {t:18} {pct(adh,tot):>5} / {pct(comp,tot):>5}{mark}{gap}")


if __name__ == "__main__":
    main()
