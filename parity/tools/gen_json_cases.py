"""parity/corpus/json-cases.jsonl — Python json.dumps dialect expectations."""
import json, random
from datetime import date, datetime

def scalar(v):
    if isinstance(v, (date, datetime)):
        return v.isoformat()
    raise TypeError(f"Not JSON-serializable: {type(v).__name__}")

def cli_default(v):
    """The CLI dialect's fallback. Dates and datetimes render ISO (#42); any
    other unmodelled value stringifies, which is what the Go encoder's default
    branch does with fmt.Sprint (a Path must keep rendering as its path)."""
    if isinstance(v, (date, datetime)):
        return v.isoformat()
    return str(v)

def tag(v):
    if isinstance(v, datetime): return {"__datetime__": v.isoformat()}
    if isinstance(v, date): return {"__date__": v.isoformat()}
    if isinstance(v, dict): return {k: tag(x) for k, x in v.items()}
    if isinstance(v, list): return [tag(x) for x in v]
    return v

POOL = [
    "plain", "café — ß", "emoji 🎯", 'quote " back \\ slash', "tab\tnl\n",
    "\x07\x1b[0m", "ключ", "日本語", "", " ", "a/b#c",
    True, False, None, 0, 1, -7, 10**30, 0.8, 1e16, 1e-7, 1e21, -0.0,
    date(2026, 1, 15), datetime(2026, 1, 15, 12, 0, 0),
    ["a", 1, None], [], {}, {"nested": {"k": "v"}, "l": [1, 2]},
]
rng = random.Random(7)
rows = []
for i, v in enumerate(POOL):
    rows.append((f"scalar-{i}", {"v": v}))
for i in range(150):
    m = {f"k{j}": rng.choice(POOL) for j in range(rng.randint(1, 5))}
    rows.append((f"fuzz-{i}", m))

with open("parity/corpus/json-cases.jsonl", "w") as fh:
    for name, v in rows:
        rec = {
            "name": name, "value": tag(v),
            # Every surface renders a datetime as ISO "T" (#42). The CLI
            # dialect used to differ — json.dumps(default=str) reached
            # str(datetime) and emitted a SPACE — so a value read out of khub
            # and written back changed shape. Non-date values still stringify.
            "cli": json.dumps(v, default=cli_default),
            "disk": json.dumps(v, indent=2, ensure_ascii=False, default=scalar) + "\n",
        }
        if isinstance(v, dict):
            rec["jsonl"] = json.dumps({"slug": f"s-{name}", **v}, ensure_ascii=False, default=scalar)
        fh.write(json.dumps(rec, ensure_ascii=False) + "\n")
print("ok", len(rows))
