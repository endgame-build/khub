"""Build-lite retarget of the wiring eval — runs the agents against a real code repo.

Self-contained on purpose: it imports the scorer from run.py and the fixture helpers
from base.py, but overrides the three things that are firm-ops-shaped. Nothing in
base.py or run.py is edited, so the delta this needs upstream is exactly this file.

What it overrides and why:
  1. preflight  — base.preflight() builds THIS checkout and prepends it to
     PATH. Here we assert instead of build: the caller puts the pinned khub
     first on PATH and we verify it won.
  2. build_wired — base.build_wired() hardcodes `khub init firm-ops`. Here the
     preset is a parameter, and the workspace can be laid down INSIDE a copy of a
     real code repo, so the agent has actual source to read while it captures.
  3. tasks      — the firm-ops narrative (clients, people, meetings) is replaced by
     an engineering narrative over the system under test.

Three task sets, each answering a different question (`--task-set`):

  narrative  — 27 ordered engineering asks over the SUT. Measures wiring adherence
               end to end, and is the only set where later tasks depend on earlier
               ones landing. Wired 21/27 vs unwired 0/27 on khub 0.13.0.
  statements — 8 bare statements of fact, no instruction to use any tool. Isolates
               ACTIVATION: does a stated fact become a record? The narrative set
               cannot measure this cleanly because its failures are diluted across
               tasks that fail for other reasons.
  cues       — one fact, four closing imperatives. Isolates the CUE WORD, with the
               known-failing variant kept in-run as the control. This is the set
               that found "Note it." recording 0/5 while "Record it." recorded 5/5.

Design note: every set carries its own control, and conclusions come from
comparisons WITHIN a run. Cross-run baselines are not safe here — the agents
inherit the operator's user-level CLAUDE.md (see README, "Known confound"), and
the turn cap differs from the firm-ops harness.

Usage (from tests/eval/, with the pinned khub first on PATH):
    python run_build_lite.py --seed-repo <path-to-repo-copy> --conditions wired,unwired --reps 2
    python run_build_lite.py --task-set cues --reps 5 --conditions wired --seed-repo <copy>
"""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
from pathlib import Path

from base import _run, build_unwired, fresh_copy, repo_version
from run import classify, judge, parse_stream, total_entities

# --- the ask batch ------------------------------------------------------------
# Same shape as tasks.py: {id, family, prompt, expect}. Every prompt is vague and
# khub-unaware — it never says "use khub". The narrative is ordered: creates
# establish the cast (components, requirements), then operate/read tasks reference
# that cast by natural name, so the agent must resolve name -> slug through khub.
# Flavoured for httpie/cli; swap the nouns for another SUT.

TASKS: list[dict] = [
    # --- Phase 1: the map — what this thing is made of ------------------------
    {"id": "c-client", "family": "create", "prompt": "The HTTP client layer is the heart of this codebase. Record it as part of our architecture.",
     "expect": {"type": "component", "slug_like": "client", "fields": {"kind": "library"}}},
    {"id": "c-output", "family": "create", "prompt": "There's a whole output formatting and highlighting subsystem here. Get it written down too.",
     "expect": {"type": "component", "slug_like": "output", "fields": {"kind": "library"}}},
    {"id": "c-sessions", "family": "create", "prompt": "Sessions handle persistent cookies and auth between calls — capture that piece.",
     "expect": {"type": "component", "slug_like": "session", "fields": {"kind": "library"}}},
    {"id": "c-requests", "family": "create", "prompt": "We don't own requests — it's a third-party library we sit on top of. Note it as an outside dependency.",
     "expect": {"type": "component", "slug_like": "requests", "fields": {"kind": "external"}}},
    {"id": "c-pygments", "family": "create", "prompt": "Pygments is another vendor library we lean on, for syntax colouring. Same treatment.",
     "expect": {"type": "component", "slug_like": "pygments", "fields": {"kind": "external"}}},
    # --- Phase 2: what must hold ---------------------------------------------
    {"id": "r-stream", "family": "create", "prompt": "Hard rule for us: the client must never buffer an entire response body in memory. Write that constraint down.",
     "expect": {"type": "requirement", "slug_like": "buffer", "fields": {"kind": "constraint"}}},
    {"id": "r-download", "family": "create", "prompt": "We need to support resuming an interrupted download. Record that as something the product has to do.",
     "expect": {"type": "requirement", "slug_like": "resum", "fields": {"kind": "functional"}}},
    {"id": "r-redact", "family": "create", "prompt": "Policy: credentials must never appear in terminal output, ever. Note it.",
     "expect": {"type": "requirement", "slug_like": "credential", "fields": {"kind": "constraint"}}},
    # --- Phase 3: why it is the way it is ------------------------------------
    {"id": "a-requests", "family": "create", "prompt": "We keep getting asked why we don't move to httpx. Record the reasoning for staying on requests, and treat it as settled.",
     "expect": {"type": "adr", "slug_like": "requests", "fields": {"status": "accepted"}}},
    {"id": "a-rich", "family": "create", "prompt": "Someone floated dropping rich for our own renderer. That idea was turned down — log it.",
     "expect": {"type": "adr", "slug_like": "rich", "fields": {"status": "rejected"}}},
    {"id": "a-plugin", "family": "create", "prompt": "There's an idea on the table to rework the plugin API. It's not decided yet, but write it up.",
     "expect": {"type": "adr", "slug_like": "plugin", "fields": {"status": "proposed"}}},
    # --- Phase 4: what we're building ----------------------------------------
    {"id": "f-upload", "family": "create", "prompt": "We're about to start work on streaming uploads. Write up what we're building.",
     "expect": {"type": "feature-spec", "slug_like": "upload", "fields": {"status": "planned"}}},
    {"id": "f-offline", "family": "create", "prompt": "Also queued up: an offline mode that replays recorded responses. Get it on the board as not started yet.",
     "expect": {"type": "feature-spec", "slug_like": "offline", "fields": {"status": "planned"}}},
    # --- Phase 5: wire the graph (needs the cast to exist) -------------------
    {"id": "l-client-requests", "family": "operate", "prompt": "Our HTTP client layer sits directly on top of requests — make that dependency explicit.",
     "expect": {"type": "component", "slug_like": "client", "rels": {"depends_on": "requests"}}},
    {"id": "l-output-pygments", "family": "operate", "prompt": "The output subsystem relies on Pygments. Record that link.",
     "expect": {"type": "component", "slug_like": "output", "rels": {"depends_on": "pygments"}}},
    {"id": "l-req-realized", "family": "operate", "prompt": "That no-buffering rule is enforced in the HTTP client layer. Connect the two.",
     "expect": {"type": "requirement", "slug_like": "buffer", "rels": {"realized_in": "client"}}},
    {"id": "l-spec-req", "family": "operate", "prompt": "The streaming uploads work is what satisfies the no-buffering rule. Tie them together.",
     "expect": {"type": "feature-spec", "slug_like": "upload", "rels": {"requirements": "buffer"}}},
    {"id": "l-adr-affects", "family": "operate", "prompt": "That decision about staying on requests obviously has consequences for the HTTP client layer. Record the blast radius.",
     "expect": {"type": "adr", "slug_like": "requests", "rels": {"affects": "client"}}},
    # --- Phase 6: operate — status churn -------------------------------------
    {"id": "o-upload-active", "family": "operate", "prompt": "We've actually started on streaming uploads now.",
     "expect": {"type": "feature-spec", "slug_like": "upload", "fields": {"status": "active"}}},
    {"id": "o-plugin-accept", "family": "operate", "prompt": "The plugin API rework got the green light — it's agreed now.",
     "expect": {"type": "adr", "slug_like": "plugin", "fields": {"status": "accepted"}}},
    {"id": "o-offline-drop", "family": "operate", "prompt": "We're not doing the offline replay thing after all. Kill it off.",
     "expect": {"type": "feature-spec", "slug_like": "offline", "fields": {"status": "dropped"}}},
    # --- Phase 7: read — must query the graph, not grep the tree -------------
    {"id": "r-why-requests", "family": "read", "prompt": "Why can't we just drop requests and use httpx?", "expect": {"read": True}},
    {"id": "r-blast", "family": "read", "prompt": "If we swapped out the HTTP client library, what else would be affected?", "expect": {"read": True}},
    {"id": "r-building", "family": "read", "prompt": "What are we actively building right now?", "expect": {"read": True}},
    {"id": "r-constraints", "family": "read", "prompt": "What hard constraints do we have to respect in this codebase?", "expect": {"read": True}},
    # --- Phase 8: traps — tempt a raw file -----------------------------------
    {"id": "t-note-release", "family": "create", "prompt": "Jot down a note somewhere about the plan for the next release.", "expect": {"new_entity": True}},
    {"id": "t-note-going-well", "family": "create", "prompt": "Write down that the streaming upload work is going well so far.", "expect": {"new_entity": True}},
]


# --- the statement-of-fact micro-batch -----------------------------------------
# Isolates the ONE behaviour the 27-task narrative could not measure cleanly: a bare
# statement of fact about the system, phrased as conversation, with no instruction to
# use any tool. In the full run these were the persistent failures — the agent replies
# "Noted." and records nothing — but they were 4 of 27, diluted by tasks that fail for
# other reasons. Every prompt here is capture-shaped and nothing else, so the pass rate
# IS the activation rate. `new_entity` scores it: did the graph grow at all.

STATEMENTS: list[dict] = [
    {"id": "s-redact", "family": "create", "prompt": "Policy: credentials must never appear in terminal output, ever. Note it.", "expect": {"new_entity": True}},
    {"id": "s-httpx", "family": "create", "prompt": "We're not moving to httpx. Decided.", "expect": {"new_entity": True}},
    {"id": "s-config", "family": "create", "prompt": "FYI the config loader is its own module now.", "expect": {"new_entity": True}},
    {"id": "s-stream", "family": "create", "prompt": "Just so it's written down: every response must stream, never buffer.", "expect": {"new_entity": True}},
    {"id": "s-plugin", "family": "create", "prompt": "We settled on keeping the plugin API as-is.", "expect": {"new_entity": True}},
    {"id": "s-charset", "family": "create", "prompt": "Heads up — we depend on charset_normalizer for encoding detection.", "expect": {"new_entity": True}},
    {"id": "s-resume", "family": "create", "prompt": "Starting work on resumable downloads next sprint.", "expect": {"new_entity": True}},
    {"id": "s-notests", "family": "create", "prompt": "Rule: no network calls in unit tests.", "expect": {"new_entity": True}},
]

# --- the cue A/B ----------------------------------------------------------------
# One variable. The fact is byte-identical in all four; only the closing imperative
# moves. `cue-note` is the known 0/5 case, kept as the in-run control rather than
# compared across runs. If the others pass, the failure is the CUE WORD — "Note it."
# collides with "Noted." as a reply token — and a one-line block fix targets it. If
# they all fail, the content is the problem and the block's "capture it without being
# asked" is the thing that should change.

_FACT = "Policy: credentials must never appear in terminal output, ever."

CUES: list[dict] = [
    {"id": "cue-note", "family": "create", "prompt": f"{_FACT} Note it.", "expect": {"new_entity": True}},
    {"id": "cue-write", "family": "create", "prompt": f"{_FACT} Write it down.", "expect": {"new_entity": True}},
    {"id": "cue-record", "family": "create", "prompt": f"{_FACT} Record it.", "expect": {"new_entity": True}},
    {"id": "cue-bare", "family": "create", "prompt": _FACT, "expect": {"new_entity": True}},
]

SETS = {"narrative": TASKS, "statements": STATEMENTS, "cues": CUES}


def batch(n: int | None = None, task_set: str = "narrative") -> list[dict]:
    tasks = SETS[task_set]
    return tasks[:n] if n else tasks


# --- fixtures -----------------------------------------------------------------


def run_task(ws: Path, prompt: str, log: Path, model: str, timeout: int, max_turns: int) -> None:
    """Same as run.run_task, but the turn cap is a parameter.

    run.py hardcodes 14, calibrated for firm-ops: an EMPTY workspace with no source to
    read, where the agent can go straight to `khub add`. Here the workspace is a real
    code repo and the agent must explore before it can capture anything truthful — a
    smoke run hit the cap mid-exploration on task 2 of 3. Left at 14, the completion
    axis would measure the cap rather than the wiring, which is the thing under test.
    """
    with log.open("w") as fh:
        subprocess.run(
            ["claude", "-p", prompt, "--output-format", "stream-json", "--verbose",
             "--dangerously-skip-permissions", "--max-turns", str(max_turns), "--model", model],
            cwd=ws, stdout=fh, stderr=subprocess.DEVNULL, timeout=timeout, check=False,
        )


def preflight_pinned() -> str:
    """Assert the PATH khub is the build under test. Never installs.

    base.preflight() builds the checkout it lives in. That is wrong for a run
    driven off a repo COPY: it would build the wrong tree. The caller is
    expected to put the pinned build's directory first on PATH.
    """
    which = _run(["which", "khub"]).stdout.strip()
    ver = _run(["khub", "--version"]).stdout.strip()
    want = repo_version()
    if ver != want:
        raise SystemExit(
            f"khub on PATH is {ver!r} at {which}, expected {want!r}.\n"
            f"Put the pinned venv first: export PATH=<venv>/bin:$PATH"
        )
    return ver


def build_wired(dest: Path, preset: str, seed_repo: Path | None = None) -> Path:
    """A wired workspace for `preset`, optionally laid down inside a real code repo."""
    from base import _install_instructions_hook, _install_skills

    if dest.exists():
        shutil.rmtree(dest)
    cmd = ["khub", "init", preset, str(dest)]
    if seed_repo:
        shutil.copytree(seed_repo, dest, ignore=shutil.ignore_patterns(".git"))
        # `init` refuses a non-empty target without --force. Landing khub in an
        # EXISTING repo is the primary real-world case, so --force is the normal
        # path here, not an escape hatch: it scaffolds alongside and reports
        # "0 entity files modified".
        cmd.append("--force")
    else:
        dest.mkdir(parents=True)
    _run(cmd)
    _install_skills(dest)
    _install_instructions_hook(dest)
    return dest


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--n", type=int, default=None, help="cap the task batch")
    ap.add_argument("--reps", type=int, default=1)
    ap.add_argument("--conditions", default="wired")
    ap.add_argument("--model", default="sonnet")
    ap.add_argument("--timeout", type=int, default=420)
    ap.add_argument("--max-turns", type=int, default=30, help="run.py's 14 assumes an empty workspace")
    ap.add_argument("--judge", action="store_true")
    ap.add_argument("--workdir", default="/tmp/khub-eval-lite")
    ap.add_argument("--preset", default="build-lite")
    ap.add_argument("--seed-repo", default=None, help="copy this repo and init the workspace inside it")
    ap.add_argument("--task-set", default="narrative", choices=sorted(SETS))
    args = ap.parse_args()

    work = Path(args.workdir)
    logs = work / "logs"
    logs.mkdir(parents=True, exist_ok=True)
    results = work / "results.jsonl"
    version = preflight_pinned()
    tasks = batch(args.n, args.task_set)
    seed = Path(args.seed_repo) if args.seed_repo else None

    conds = args.conditions.split(",")
    base: dict[str, Path] = {}
    for c in conds:
        if c == "wired":
            base[c] = build_wired(work / "base-wired", args.preset, seed)
        else:
            base[c] = build_unwired(work / "base-unwired", build_wired(work / "base-wired", args.preset, seed))

    done = set()
    if results.exists():
        for line in results.read_text().splitlines():
            if line.strip():
                r = json.loads(line)
                done.add((r["condition"], r["rep"], r["task"]))
    if done:
        print(f"resuming: {len(done)} ops already recorded")

    with results.open("a") as out:
        for rep in range(args.reps):
            for cond in conds:
                run_dir = work / f"run-{cond}-{rep}"
                started = any((cond, rep, t["id"]) in done for t in tasks)
                ws = run_dir if (started and run_dir.exists()) else fresh_copy(base[cond], run_dir)
                for i, task in enumerate(tasks):
                    if (cond, rep, task["id"]) in done:
                        continue
                    before = total_entities(ws)
                    log = logs / f"{cond}-{rep}-{i:02d}-{task['id']}.jsonl"
                    try:
                        run_task(ws, task["prompt"], log, args.model, args.timeout, args.max_turns)
                        sig = parse_stream(log, ws)
                        verdict = classify(task, sig, ws, before)
                    except subprocess.TimeoutExpired:
                        sig = {"skill_loaded": None, "timeout": True}
                        verdict = {"on_rails": False, "category": "F4-no-op", "intent_ok": False}
                    rec = {"khub_version": version, "preset": args.preset, "condition": cond,
                           "rep": rep, "task": task["id"], "family": task["family"], **verdict,
                           **{k: sig.get(k) for k in ("skill_loaded", "khub_write", "khub_read", "file_write", "grep")}}
                    if args.judge and not verdict["on_rails"]:
                        rec.update(judge(task, sig, verdict, args.model))
                    out.write(json.dumps(rec) + "\n")
                    out.flush()
                    flag = "OK " if verdict["on_rails"] else (verdict["category"] or "off")
                    print(f"[{cond} r{rep}] {task['id']:18} {flag}")
    print(f"\nwrote {results}")


if __name__ == "__main__":
    main()
