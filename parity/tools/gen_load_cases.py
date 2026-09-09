"""parity/corpus/load-cases.jsonl — scalar resolution expectations for BOTH
Python load paths: python-frontmatter/PyYAML (md frontmatter, YAML 1.1) and
ruamel safe (yaml entities, YAML 1.2)."""
import json
import frontmatter
from io import StringIO
from datetime import date, datetime
from ruamel.yaml import YAML

ruamel_safe = YAML(typ="safe")

ZOO = [
    "yes", "no", "on", "off", "Yes", "NO", "On", "y", "n", "Y", "N",
    "true", "True", "TRUE", "false", "null", "Null", "NULL", "~",
    "017", "0o17", "0x1f", "0b101", "1_000", "1:30", "1:30:30",
    "2026-01-15", "2026-01-15T10:00:00", "2026-01-15 10:00:00",
    "2026-01-15T10:00:00Z", "2026-01-15T10:00:00+02:00", "2026-1-5",
    "1", "-1", "+1", "1.5", ".5", "5.", "1e5", "1E5", "-.inf", ".inf", ".NaN",
    ".nan", "1.5e-3", "0", "-0", "017x", "=", "<<",
    "café", "a b", "", "100000000000000000000000000",
]

def tag(v):
    if isinstance(v, bool): return {"t": "bool", "v": v}
    if isinstance(v, int): return {"t": "int", "v": str(v)}
    if isinstance(v, float):
        if v != v: return {"t": "float", "v": "nan"}
        if v == float("inf"): return {"t": "float", "v": "inf"}
        if v == float("-inf"): return {"t": "float", "v": "-inf"}
        return {"t": "float", "v": repr(v)}
    if isinstance(v, datetime): return {"t": "datetime", "v": v.isoformat()}
    if isinstance(v, date): return {"t": "date", "v": v.isoformat()}
    if v is None: return {"t": "null"}
    if isinstance(v, str): return {"t": "str", "v": v}
    return {"t": type(v).__name__, "v": str(v)}

with open("parity/corpus/load-cases.jsonl", "w") as fh:
    for s in ZOO:
        doc = f"k: {s}\n" if s else "k:\n"
        try:
            md = frontmatter.loads(f"---\n{doc}---\n").metadata.get("k")
            md_t = tag(md)
        except Exception as e:
            md_t = {"t": "error", "v": type(e).__name__}
        try:
            ru = (ruamel_safe.load(doc) or {}).get("k")
            ru_t = tag(ru)
        except Exception as e:
            ru_t = {"t": "error", "v": type(e).__name__}
        fh.write(json.dumps({"scalar": s, "pyyaml": md_t, "ruamel": ru_t}, ensure_ascii=False) + "\n")
print("ok", len(ZOO))
