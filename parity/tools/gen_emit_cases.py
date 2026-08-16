"""Generate parity/corpus/emit-cases.jsonl — the T3 emitter differential.

Each line: {"name": …, "value": <tagged>, "rt": <ruamel RT dump>, "safe": <ruamel
safe/4096 dump>}. `value` is JSON with dates tagged {"__date__": "…"} and key
order significant (the Go runner parses it order-preserving). Deterministic:
seeded RNG, fixed zoo.
"""

from __future__ import annotations

import json
import random
from datetime import date
from io import StringIO
from pathlib import Path

from ruamel.yaml import YAML

rt = YAML()
rt.default_flow_style = False
rt.representer.ignore_aliases = lambda *_: True

safe = YAML(typ="safe")
safe.default_flow_style = False
safe.representer.sort_base_mapping_type_on_output = False
safe.width = 4096


def dump(y: YAML, data) -> str:
    s = StringIO()
    y.dump(data, s)
    return s.getvalue()


def tag(v):
    if isinstance(v, date):
        return {"__date__": v.isoformat()}
    if isinstance(v, dict):
        return {k: tag(x) for k, x in v.items()}
    if isinstance(v, list):
        return [tag(x) for x in v]
    return v


STRING_ZOO = [
    "", " ", "  x", "x  ", " x ", "a b", "a  b",
    "yes", "no", "on", "off", "true", "True", "TRUE", "false", "null", "Null", "NULL", "~",
    "y", "n", "Y", "N",
    "017", "0o17", "0x1f", "1_000", "1:30", "12:34:56",
    "2026-01-15", "2026-01-15T10:00:00", "2026-1-5", "15-01-2026",
    "1", "-1", "+1", "1.5", "-1.5", ".5", "5.", "1e5", "1E5", ".inf", "-.inf", ".nan", "3.",
    "a: b", "a:b", ":a", "a:", "- a", "-a", "? a", "?a", "# c", "a # c", "a #c",
    "'quoted'", '"dquoted"', "it's", 'say "hi"', "both ' and \"",
    "*star", "&amp", "!bang", "%pct", "@at", "`tick", "|pipe", ">gt", "[br", "]br", "{cu", "}cu", ",comma",
    "a,b", "a[b", "a{b", "a]b",
    "café", "— dash", "ß", "日本語", "emoji 🎯 here",
    "line1\nline2", "line1\nline2\n", "\n", "a\n\nb", "trail\n\n",
    "tab\there", "a\tb",
    "word " * 30, ("word " * 30).strip(), "x" * 79, "x" * 80, "x" * 81, "x" * 200,
    "https://example.com/" + "a" * 110,
    "-", "--", "---", "...", ". x",
    "0", "00", "0.0", "-0", "-0.0",
    "\x07bell", "nul\x00nul" if False else "esc\x1b[0m",
    " nbsp", "line sep", "line psep", "﻿bom",
]

SCALARS = [
    True, False, None,
    0, 1, -1, 3, 42, 10**18, 10**30, -(10**30),
    0.8, 1e16, 1e-7, -2.5, 0.1, 1234567890123456.7, 1e21, 5e-324, 1.7976931348623157e308,
    date(2026, 1, 15), date(1999, 12, 31),
]


def rand_value(rng: random.Random, depth: int):
    pool = ["str", "scalar"] + (["list", "map"] if depth < 3 else [])
    kind = rng.choice(pool)
    if kind == "str":
        return rng.choice(STRING_ZOO)
    if kind == "scalar":
        v = rng.choice(SCALARS)
        # fresh object per use: a shared date object would alias (&id001) under
        # the safe profile, which the real pipeline never produces
        return date(v.year, v.month, v.day) if isinstance(v, date) else v
    if kind == "list":
        return [rand_value(rng, depth + 1) for _ in range(rng.randint(0, 4))]
    return {f"k{j}_{rng.randint(0,999)}": rand_value(rng, depth + 1) for j in range(rng.randint(0, 4))}


def main() -> None:
    out = Path("parity/corpus/emit-cases.jsonl")
    rows = []

    for i, s in enumerate(STRING_ZOO):
        rows.append((f"zoo-str-{i}", {"v": s}))
    for i, s in enumerate(SCALARS):
        rows.append((f"zoo-scalar-{i}", {"v": s}))
    for i, s in enumerate(STRING_ZOO):
        rows.append((f"zoo-key-{i}", {s if s else "empty": "v"}))  # zoo strings as KEYS
        rows.append((f"zoo-list-{i}", {"l": ["pad", s]}))
        rows.append((f"zoo-nested-{i}", {"m": {"inner": s}, "after": "x"}))
    rows.append(("empties", {"emap": {}, "elist": [], "s": ""}))
    rows.append(("long-multiline", {"v": ("word " * 20 + "\n") * 2}))
    rows.append(("long-multiline-deep", {"m": {"n": {"v": ("word " * 20 + "\n") * 2 + "tail"}}}))
    rows.append(("long-forced-double", {"v": "a: colon ' and quote " + "word " * 25}))
    # Control characters past the fold column: the escape branch leaves
    # start == end+1, which Python slices to "" and Go must not panic on.
    rows.append(("ctrl-nospace", {"v": "x" * 100 + "\x01" + "y" * 50}))
    rows.append(("ctrl-words", {"v": "word " * 20 + "\x01" + " tail" * 10}))
    rows.append(("ctrl-bell-mid", {"v": "x" * 90 + "\x07" + "y" * 20}))
    rows.append(("ctrl-early", {"v": "\x01" + "z" * 120}))
    rows.append(("ctrl-many", {"v": ("a" * 30 + "\x02") * 4}))
    rows.append(("ctrl-double-space", {"v": "w " * 45 + "\x03" + "  tail"}))
    rows.append(("meta-shape", {
        "type": "note", "created": date(2026, 1, 15), "updated": date(2026, 1, 15),
        "draft": False, "title": "café — ß", "tags": ["alpha", "beta"],
        "x_note": "word " * 30,
    }))

    rng = random.Random(20260115)
    for i in range(600):
        m = {f"f{j}_{rng.randint(0,999)}": rand_value(rng, 0) for j in range(rng.randint(1, 6))}
        rows.append((f"fuzz-{i}", m))

    with out.open("w") as fh:
        for name, value in rows:
            try:
                r = dump(rt, value)
                s = dump(safe, value)
            except Exception as e:  # a value ruamel itself refuses is out of scope
                print(f"skip {name}: {type(e).__name__}: {e}")
                continue
            fh.write(json.dumps({"name": name, "value": tag(value), "rt": r, "safe": s},
                                ensure_ascii=False) + "\n")
    print(f"wrote {sum(1 for _ in out.open())} cases")


if __name__ == "__main__":
    main()
