"""Runner + scorer for the wiring eval.

For each condition (wired / unwired) and repetition, run the task batch in order
against ONE accumulating workspace — each task is its own headless `claude -p`
session, so compounding (an off-rails create breaking a later link) is measured.
Score every operation on-/off-rails from its stream-json transcript + the
post-op khub state, and append a per-op record to results.jsonl.

  python tests/eval/run.py --n 10 --reps 1                 # smoke
  python tests/eval/run.py --reps 3 --conditions wired,unwired --judge

The judge (--judge) is an extra `claude -p` that root-causes each off-rails op;
deterministic F-categories work without it.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
from pathlib import Path

from base import build_unwired, build_wired, entity_path_prefixes, fresh_copy, preflight
from tasks import batch

WRITE_VERB = re.compile(r"\bkhub\s+(?:-C\s+\S+\s+)?(add|edit|link|unlink|remove)\b")
READ_VERB = re.compile(r"\bkhub\s+(?:-C\s+\S+\s+)?(get|query|neighbors|impact|history|search|status|schema|check|validate)\b")
GREP_CMD = re.compile(r"\b(grep|rg|cat|head|tail|find|ls|sed|awk)\b")


# --- khub oracle --------------------------------------------------------------


def khub_json(ws: Path, *args: str):
    try:
        p = subprocess.run(["khub", "-C", str(ws), *args, "--format", "json"],
                           capture_output=True, text=True, timeout=60, check=False)
        return json.loads(p.stdout)
    except (json.JSONDecodeError, subprocess.TimeoutExpired):
        return None


def total_entities(ws: Path) -> int:
    s = khub_json(ws, "status") or {}
    return int(s.get("total", 0))


def verify_expect(ws: Path, expect: dict) -> bool:
    """Did the intended entity/field/relation land (schema-valid) in the workspace?"""
    if expect.get("read") or expect.get("new_entity"):
        return True  # handled by the caller (tool signal / count delta)
    typ = expect.get("type")
    if not typ:
        return True
    rows = khub_json(ws, "query", "--type", typ) or []
    token = (expect.get("slug_like") or "").lower()
    for row in rows:
        slug = row["slug"].lower()
        if token and token not in slug:
            continue
        rec = khub_json(ws, "get", row["id"])
        fm = (rec or {}).get("frontmatter", {})
        if any(str(fm.get(f, "")).lower() != v.lower() for f, v in expect.get("fields", {}).items()):
            continue
        if any(tok.lower() not in str(fm.get(pred, "")).lower() for pred, tok in expect.get("rels", {}).items()):
            continue
        return True
    return False


# --- transcript parsing -------------------------------------------------------


def parse_stream(log: Path, ws: Path) -> dict:
    prefixes = tuple(f"{p}/" for p in entity_path_prefixes(ws)) if ws.exists() else ()
    khub_writes, khub_reads, file_writes, greps = [], [], [], []
    skill_loaded = False
    for line in log.read_text(errors="replace").splitlines():
        try:
            e = json.loads(line)
        except json.JSONDecodeError:
            continue
        if e.get("type") == "system" and e.get("subtype") == "init":
            skill_loaded = "khub" in (e.get("skills") or [])
        if e.get("type") != "assistant":
            continue
        for b in e.get("message", {}).get("content", []):
            if b.get("type") != "tool_use":
                continue
            name, inp = b.get("name", ""), b.get("input", {})
            cmd = inp.get("command", "") or ""
            if name == "Bash":
                if WRITE_VERB.search(cmd):
                    khub_writes.append(cmd)
                if READ_VERB.search(cmd):
                    khub_reads.append(cmd)
                if GREP_CMD.search(cmd) and not WRITE_VERB.search(cmd) and not READ_VERB.search(cmd):
                    greps.append(cmd)
            elif name in ("Write", "Edit", "MultiEdit", "NotebookEdit"):
                fp = str(inp.get("file_path", ""))
                rel = fp.split(str(ws) + "/", 1)[-1] if str(ws) in fp else fp.lstrip("/")
                if rel.startswith(prefixes):
                    file_writes.append(rel)
            elif name in ("Grep", "Glob"):
                greps.append(f"{name}:{inp.get('pattern') or inp.get('path')}")
    return {
        "skill_loaded": skill_loaded,
        "khub_write": bool(khub_writes),
        "khub_read": bool(khub_reads),
        "file_write": file_writes,
        "grep": bool(greps),
        "n_khub": len(khub_writes) + len(khub_reads),
    }


# --- classification -----------------------------------------------------------

# Two DISTINCT questions, deliberately not conflated:
#   on_rails  = wiring ADHERENCE — did the agent reach for khub (vs write a raw
#               entity file / grep instead of query)? This is what the eval tests.
#   intent_ok = task COMPLETION — did the exact intended entity/edge land? A vague
#               ask can be un-completable without fabricating (a meeting whose
#               required date/call_type/source the prompt never gave), and a good
#               agent then reads khub and stops/asks — adherent but not complete.
# Reporting them together hid that: correct restraint scored as "off-rails".


def verdict_from_signals(family: str, sig: dict, intent_ok: bool) -> dict:
    if sig.get("file_write"):  # wrote entity Markdown directly — the real bypass
        return {"on_rails": False, "category": "F1-bypass-write", "intent_ok": intent_ok}
    used = sig.get("khub_read") if family == "read" else (sig.get("khub_write") or sig.get("khub_read"))
    if used:
        return {"on_rails": True, "category": None, "intent_ok": intent_ok}
    if sig.get("grep"):  # skipped khub, went to grep/cat
        return {"on_rails": False, "category": "F2-bypass-read", "intent_ok": intent_ok}
    if sig.get("skill_loaded") is False:
        return {"on_rails": False, "category": "F5-wiring-not-loaded", "intent_ok": intent_ok}
    return {"on_rails": False, "category": "F4-no-op", "intent_ok": intent_ok}


def classify(task: dict, sig: dict, ws: Path, before_total: int) -> dict:
    intent_ok = verify_expect(ws, task["expect"])
    if task["expect"].get("new_entity"):
        intent_ok = total_entities(ws) > before_total
    return verdict_from_signals(task["family"], sig, intent_ok)


# --- running ------------------------------------------------------------------


def run_task(ws: Path, prompt: str, log: Path, model: str, timeout: int) -> None:
    with log.open("w") as fh:
        subprocess.run(
            ["claude", "-p", prompt, "--output-format", "stream-json", "--verbose",
             "--dangerously-skip-permissions", "--max-turns", "14", "--model", model],
            cwd=ws, stdout=fh, stderr=subprocess.DEVNULL, timeout=timeout, check=False,
        )


def judge(task: dict, sig: dict, verdict: dict, model: str) -> dict:
    prompt = (
        "You are grading whether a coding agent used a CLI tool as intended. "
        f"Task given to the agent: {task['prompt']!r}. Expected: use `khub add/edit/link/get` "
        "(a typed-entity CLI), not raw file writes or grep. Observed signals: "
        f"used_khub_write={sig.get('khub_write')}, used_khub_read={sig.get('khub_read')}, "
        f"wrote_entity_files={sig.get('file_write')}, grepped={sig.get('grep')}, "
        f"khub_skill_loaded={sig.get('skill_loaded')}, intent_landed={verdict['intent_ok']}. "
        "In one short sentence, give the single root cause of any off-rails behavior "
        "(e.g. chose-Write-first, schema-not-read, relation-unresolved, khub-error-not-recovered, "
        "ambiguous-ask, wiring-not-loaded), or say 'on-rails'. Reply with just that phrase."
    )
    try:
        p = subprocess.run(["claude", "-p", prompt, "--output-format", "json", "--model", model],
                           capture_output=True, text=True, timeout=120, check=False)
        data = json.loads(p.stdout)
        if isinstance(data, list):  # array of events: pull the result event
            data = next((e for e in data if isinstance(e, dict) and e.get("type") == "result"), {})
        result = data.get("result", "") if isinstance(data, dict) else ""
        return {"root_cause": result.strip()[:200]}
    except Exception:  # noqa: BLE001 — the judge is best-effort; any failure in it must
        return {"root_cause": "judge-unavailable"}  # degrade the report, never kill the run


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--n", type=int, default=None, help="cap the task batch")
    ap.add_argument("--reps", type=int, default=1)
    ap.add_argument("--conditions", default="wired", help="comma list: wired,unwired")
    ap.add_argument("--model", default="sonnet")
    ap.add_argument("--timeout", type=int, default=240)
    ap.add_argument("--judge", action="store_true", help="LLM root-cause on off-rails ops")
    ap.add_argument("--workdir", default="/tmp/khub-eval")
    args = ap.parse_args()

    work = Path(args.workdir)
    logs = work / "logs"
    logs.mkdir(parents=True, exist_ok=True)
    results = work / "results.jsonl"
    version = preflight()
    tasks = batch(args.n)
    base = {c: build_wired(work / f"base-{c}") if c == "wired" else build_unwired(work / "base-unwired", build_wired(work / "base-wired"))
            for c in args.conditions.split(",")}

    # resume: skip (cond, rep, task) tuples already recorded; reuse the accumulating run dir.
    # ponytail: the one task interrupted mid-scoring (log written, no record) re-runs against a
    # state it already applied — at most one duplicate-ish op per crash; acceptable noise at N=104.
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
            for cond in args.conditions.split(","):
                run_dir = work / f"run-{cond}-{rep}"
                started = any((cond, rep, t["id"]) in done for t in tasks)
                ws = run_dir if (started and run_dir.exists()) else fresh_copy(base[cond], run_dir)
                for i, task in enumerate(tasks):
                    if (cond, rep, task["id"]) in done:
                        continue
                    before = total_entities(ws)
                    log = logs / f"{cond}-{rep}-{i:02d}-{task['id']}.jsonl"
                    try:
                        run_task(ws, task["prompt"], log, args.model, args.timeout)
                        sig = parse_stream(log, ws)
                        verdict = classify(task, sig, ws, before)
                    except subprocess.TimeoutExpired:
                        sig, verdict = {"skill_loaded": None, "timeout": True}, {"on_rails": False, "category": "F4-no-op", "intent_ok": False}
                    rec = {"khub_version": version, "condition": cond, "rep": rep, "task": task["id"],
                           "family": task["family"], **verdict, **{k: sig.get(k) for k in ("skill_loaded", "khub_write", "khub_read", "file_write", "grep")}}
                    if args.judge and not verdict["on_rails"]:
                        rec.update(judge(task, sig, verdict, args.model))
                    out.write(json.dumps(rec) + "\n")
                    out.flush()
                    flag = "OK " if verdict["on_rails"] else (verdict["category"] or "off")
                    print(f"[{cond} r{rep}] {task['id']:16} {flag}")
    print(f"\nwrote {results}")


if __name__ == "__main__":
    main()
