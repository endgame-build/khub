#!/usr/bin/env python3
"""bl — build-lite: a typed doc corpus for one build project.

Markdown + YAML frontmatter in git is the truth. This script is the part an
agent cannot do for itself: mint a conforming file, keep an edge honest, walk
the derived graph, and sweep the whole corpus for breakage.

Everything else (read, search, write prose) the agent already does with its own
tools, so it is not here. Stdlib only, one file: copy it in and run it.

Frontmatter profile
-------------------
A deliberately small subset of YAML, so no parser dependency is needed and the
corpus keeps one shape: a flat mapping of `key: scalar`, `key: [a, b]`, or a
`key:` followed by `- item` lines. No nesting, no multi-line scalars, no
anchors, no trailing comments. Anything outside the profile is a `malformed`
finding, not a silent misread.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections.abc import Iterable, Sequence
from dataclasses import dataclass, field
from datetime import date
from pathlib import Path
from typing import Any

HERE = Path(__file__).resolve().parent
FENCE = "---"
KEY_RE = re.compile(r"^([A-Za-z_][A-Za-z0-9_-]*):(?:[ \t]+(.*))?$")
ITEM_RE = re.compile(r"^[ \t]*-[ \t]+(.*)$")
H2_RE = re.compile(r"^##\s+(.*)$", re.MULTILINE)
CODE_FENCE_RE = re.compile(r"^(```|~~~).*?^\1[^\S\n]*$", re.MULTILINE | re.DOTALL)
DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
NEEDS_QUOTE = set("-?:,[]{}#&*!|>'\"%@`")

ERROR = "error"
GAP = "gap"


class Bad(Exception):
    """A usage or input error worth one line on stderr."""


@dataclass(frozen=True)
class Finding:
    severity: str
    code: str
    where: str
    message: str

    def line(self) -> str:
        mark = "E" if self.severity == ERROR else "-"
        return f"  {mark} {self.code:<14} {self.where:<28} {self.message}"


@dataclass
class Entity:
    slug: str
    type: str
    path: Path
    fm: dict[str, Any]
    body: str
    front_lines: list[str]

    @property
    def title(self) -> str:
        t = self.fm.get("title")
        return t if isinstance(t, str) else ""


# --------------------------------------------------------------- frontmatter


def split_front(text: str) -> tuple[list[str], str]:
    """The frontmatter's raw lines and the body. Raises Bad if there is none."""
    if not text.startswith(FENCE + "\n"):
        raise Bad("no frontmatter: the file must start with a --- fence")
    rest = text[len(FENCE) + 1 :]
    end = rest.find("\n" + FENCE)
    if end < 0:
        raise Bad("unterminated frontmatter: no closing --- fence")
    front = rest[:end].split("\n") if rest[:end] else []
    after = rest[end + len(FENCE) + 1 :]
    return front, after.lstrip("\n")


def parse_front(lines: Sequence[str]) -> dict[str, Any]:
    """The frontmatter profile, parsed. Raises Bad on anything outside it."""
    out: dict[str, Any] = {}
    i = 0
    while i < len(lines):
        raw = lines[i]
        if not raw.strip() or raw.lstrip().startswith("#"):
            i += 1
            continue
        m = KEY_RE.match(raw)
        if not m:
            raise Bad(f"line {i + 1}: not a `key: value` line ({raw.strip()!r})")
        key, inline = m.group(1), m.group(2)
        if key in out:
            raise Bad(f"line {i + 1}: duplicate key {key!r}")
        if inline is None or not inline.strip():
            items: list[Any] = []
            i += 1
            while i < len(lines) and ITEM_RE.match(lines[i]):
                item = ITEM_RE.match(lines[i])
                assert item is not None
                items.append(scalar(item.group(1).strip()))
                i += 1
            out[key] = items
        else:
            out[key] = scalar(inline.strip())
            i += 1
    return out


def scalar(text: str) -> Any:
    if text.startswith("[") and text.endswith("]"):
        inner = text[1:-1].strip()
        return [scalar(p.strip()) for p in inner.split(",")] if inner else []
    if len(text) >= 2 and text[0] == text[-1] and text[0] in "\"'":
        return text[1:-1].replace('\\"', '"').replace("\\\\", "\\")
    if text in ("true", "false"):
        return text == "true"
    if text in ("null", "~", ""):
        return None
    return text


def emit_scalar(value: Any) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if value is None:
        return "null"
    text = str(value)
    risky = (
        not text
        or text[0] in NEEDS_QUOTE
        or text != text.strip()
        or ": " in text
        or " #" in text
        or text in ("true", "false", "null", "~")
    )
    if risky:
        return '"' + text.replace("\\", "\\\\").replace('"', '\\"') + '"'
    return text


def emit_key(key: str, value: Any) -> list[str]:
    """One frontmatter key as lines. Lists are block style — one edge per line
    keeps a link's git diff to a single line."""
    if isinstance(value, list):
        if not value:
            return []
        return [f"{key}:"] + [f"  - {emit_scalar(v)}" for v in value]
    return [f"{key}: {emit_scalar(value)}"]


def key_span(lines: Sequence[str], key: str) -> tuple[int, int] | None:
    """Half-open line span a key occupies, including its `- item` lines."""
    for i, raw in enumerate(lines):
        m = KEY_RE.match(raw)
        if m and m.group(1) == key:
            j = i + 1
            while j < len(lines) and (ITEM_RE.match(lines[j]) or not lines[j].strip()):
                if not lines[j].strip():
                    break
                j += 1
            return i, j
    return None


def splice(lines: list[str], key: str, value: Any) -> list[str]:
    """Replace (or append) one key, leaving every other line byte-identical."""
    new = emit_key(key, value)
    span = key_span(lines, key)
    if span is None:
        return lines + new
    start, end = span
    return lines[:start] + new + lines[end:]


def render(front: Sequence[str], body: str) -> str:
    head = "\n".join(front)
    return f"{FENCE}\n{head}\n{FENCE}\n\n{body.lstrip(chr(10))}"


# -------------------------------------------------------------------- schema


class Schema:
    def __init__(self, data: dict[str, Any]) -> None:
        self.data = data
        self.base = data.get("base", {})
        self.types: dict[str, dict[str, Any]] = data["types"]

    def attrs(self, type_: str) -> dict[str, Any]:
        merged = dict(self.base.get("attributes", {}))
        merged.update(self.types[type_].get("attributes", {}))
        return merged

    def rels(self, type_: str) -> dict[str, Any]:
        merged = dict(self.base.get("relations", {}))
        merged.update(self.types[type_].get("relations", {}))
        return merged

    def fields(self, type_: str) -> dict[str, Any]:
        return {**self.attrs(type_), **self.rels(type_)}

    def is_singleton(self, type_: str) -> bool:
        return bool(self.types[type_].get("singleton"))

    def prefixes(self, type_: str) -> list[str]:
        spec = self.types[type_].get("id_prefix")
        if spec is None:
            return []
        if isinstance(spec, str):
            return [spec]
        return list(dict.fromkeys(spec["map"].values()))

    def prefix_for(self, type_: str, fm: dict[str, Any]) -> str | None:
        spec = self.types[type_].get("id_prefix")
        if spec is None:
            return None
        if isinstance(spec, str):
            return spec
        value = fm.get(spec["by"])
        return spec["map"].get(value) if isinstance(value, str) else None

    def inverse(self, predicate: str) -> str | None:
        for cfg in self.types.values():
            spec = cfg.get("relations", {}).get(predicate)
            if spec and spec.get("inverse"):
                return str(spec["inverse"])
        return None


def load_schema(root: Path) -> Schema:
    """The workspace override if it has one, else the copy beside this script."""
    local = root / ".build-lite" / "schema.json"
    path = local if local.is_file() else HERE / "schema.json"
    return Schema(json.loads(path.read_text()))


def template_for(root: Path, type_: str) -> str | None:
    local = root / ".build-lite" / "templates" / f"{type_}.md"
    path = local if local.is_file() else HERE / "templates" / f"{type_}.md"
    return path.read_text() if path.is_file() else None


# ------------------------------------------------------------------ corpus


@dataclass
class Corpus:
    root: Path
    schema: Schema
    entities: dict[str, Entity] = field(default_factory=dict)
    malformed: list[tuple[Path, str]] = field(default_factory=list)
    strays: list[Path] = field(default_factory=list)
    collisions: list[tuple[str, Path]] = field(default_factory=list)

    def by_type(self, type_: str) -> list[Entity]:
        return [e for e in self.entities.values() if e.type == type_]

    def out_edges(self, slug: str) -> list[tuple[str, str]]:
        """(predicate, target) pairs declared on this entity, in schema order."""
        e = self.entities.get(slug)
        if e is None:
            return []
        pairs: list[tuple[str, str]] = []
        for pred in self.schema.rels(e.type):
            for target in as_list(e.fm.get(pred)):
                if isinstance(target, str):
                    pairs.append((pred, target))
        return pairs

    def in_edges(self, slug: str) -> list[tuple[str, str]]:
        """(predicate, source) pairs pointing here — computed, never stored."""
        found: list[tuple[str, str]] = []
        for other in self.entities:
            for pred, target in self.out_edges(other):
                if target == slug:
                    found.append((pred, other))
        return found


def as_list(value: Any) -> list[Any]:
    if value is None:
        return []
    return value if isinstance(value, list) else [value]


def type_root(root: Path, schema: Schema, type_: str) -> Path:
    return root / schema.types[type_]["path"]


def scan(root: Path, schema: Schema) -> Corpus:
    corpus = Corpus(root=root, schema=schema)
    for type_, cfg in schema.types.items():
        target = root / cfg["path"]
        if schema.is_singleton(type_):
            files = [target] if target.is_file() else []
        else:
            if not target.is_dir():
                continue
            visible = [p for p in target.iterdir() if not p.name.startswith(".")]
            files = sorted(p for p in visible if p.is_file() and p.suffix == ".md")
            corpus.strays.extend(sorted(p for p in visible if p not in files))
        for path in files:
            slug = type_ if schema.is_singleton(type_) else path.stem
            try:
                front, body = split_front(path.read_text())
                fm = parse_front(front)
            except Bad as exc:
                corpus.malformed.append((path, str(exc)))
                continue
            if slug in corpus.entities:
                corpus.collisions.append((slug, path))
                continue
            corpus.entities[slug] = Entity(slug, type_, path, fm, body, front)
    return corpus


def find_root(explicit: str | None) -> Path:
    if explicit:
        return Path(explicit).resolve()
    here = Path.cwd().resolve()
    for candidate in [here, *here.parents]:
        if (candidate / "knowledge").is_dir():
            return candidate
    for candidate in [here, *here.parents]:
        if (candidate / ".git").exists():
            return candidate
    return here


def rel(root: Path, path: Path) -> str:
    try:
        return str(path.relative_to(root))
    except ValueError:
        return str(path)


# -------------------------------------------------------------------- check


def check(corpus: Corpus) -> list[Finding]:
    schema, out = corpus.schema, []
    for path, why in corpus.malformed:
        out.append(Finding(ERROR, "malformed", rel(corpus.root, path), why))
    for slug, path in corpus.collisions:
        out.append(Finding(ERROR, "duplicate_id", rel(corpus.root, path), f"{slug} already taken"))
    for path in corpus.strays:
        out.append(Finding(ERROR, "stray", rel(corpus.root, path), "not an entity of this type"))

    for type_, cfg in schema.types.items():
        if schema.is_singleton(type_) and not (corpus.root / cfg["path"]).is_file():
            severity = ERROR if cfg.get("required") else GAP
            out.append(Finding(severity, "missing", cfg["path"], f"the {type_} has not been written"))

    for entity in corpus.entities.values():
        out.extend(_entity_findings(corpus, entity))
    out.extend(_cycle_findings(corpus))
    order = {ERROR: 0, GAP: 1}
    return sorted(out, key=lambda f: (order[f.severity], f.code, f.where))


def _entity_findings(corpus: Corpus, e: Entity) -> Iterable[Finding]:
    schema = corpus.schema
    attrs, rels = schema.attrs(e.type), schema.rels(e.type)
    known = {**attrs, **rels}

    def err(code: str, msg: str) -> Finding:
        return Finding(ERROR, code, e.slug, msg)

    if e.fm.get("type") != e.type:
        yield err("type_mismatch", f"type: {e.fm.get('type')!r} in the {e.type} directory")
    for key, value in e.fm.items():
        if key not in known:
            yield err("unknown_field", f"{key!r} is not in the schema (typo, or drop it)")
            continue
        if key in attrs:
            problem = _attr_problem(attrs[key], value)
            if problem:
                yield err("bad_value", f"{key}: {problem}")

    for pred, spec in rels.items():
        values = as_list(e.fm.get(pred))
        if not spec.get("many") and len(values) > 1:
            yield err("cardinality", f"{pred} takes one target, got {len(values)}")
        for target in values:
            if not isinstance(target, str):
                yield err("bad_value", f"{pred}: {target!r} is not a slug")
                continue
            if target == e.slug:
                yield err("self_link", f"{pred} points at itself")
                continue
            hit = corpus.entities.get(target)
            if hit is None:
                yield err("dangling", f"{pred} -> {target} resolves to nothing")
            elif spec["to"] != "any" and hit.type != spec["to"]:
                yield err("wrong_type", f"{pred} -> {target} is a {hit.type}, wants {spec['to']}")

    if not schema.is_singleton(e.type):
        yield from _id_findings(schema, e)

    missing = [k for k, spec in known.items() if spec.get("required") and not e.fm.get(k)]
    if missing:
        yield Finding(GAP, "incomplete", e.slug, "missing " + ", ".join(sorted(missing)))

    template = template_for(corpus.root, e.type)
    if template:
        absent = _missing_heading(template, e.body)
        if absent:
            yield Finding(GAP, "body_shape", e.slug, f"no '## {absent}' section")

    swept = not corpus.schema.types[e.type].get("orphan")
    if swept and not corpus.out_edges(e.slug) and not corpus.in_edges(e.slug):
        yield Finding(GAP, "orphan", e.slug, "no relation in or out")


def _id_findings(schema: Schema, e: Entity) -> Iterable[Finding]:
    prefixes = schema.prefixes(e.type)
    if not prefixes:
        return
    m = re.match(r"^([a-z]+)-(\d{3})-([a-z0-9-]+)$", e.slug)
    if not m or m.group(1) not in prefixes:
        shape = "|".join(prefixes)
        yield Finding(ERROR, "bad_id", e.slug, f"filename must be {shape}-NNN-slug")
        return
    expected = schema.prefix_for(e.type, e.fm)
    if expected and m.group(1) != expected:
        yield Finding(ERROR, "bad_id", e.slug, f"prefix disagrees with its kind (want {expected}-)")


def _attr_problem(spec: dict[str, Any], value: Any) -> str | None:
    if value is None:
        return None
    kind = spec.get("type", "text")
    if spec.get("enum"):
        return None if value in spec["enum"] else f"{value!r} not in {'|'.join(spec['enum'])}"
    if kind == "date":
        return None if isinstance(value, str) and DATE_RE.match(value) else "not an ISO date"
    if kind == "list":
        return None if isinstance(value, list) else "must be a list"
    if kind == "bool":
        return None if isinstance(value, bool) else "must be true or false"
    return None if isinstance(value, str) and value.strip() else "must be non-empty text"


def _missing_heading(template: str, body: str) -> str | None:
    """The first template H2 not present, in order. Extra headings are fine."""
    required = H2_RE.findall(CODE_FENCE_RE.sub("", template))
    present = [h.strip() for h in H2_RE.findall(CODE_FENCE_RE.sub("", body))]
    pos = 0
    for want in (h.strip() for h in required):
        while pos < len(present) and not present[pos].startswith(want):
            pos += 1
        if pos == len(present):
            return want
        pos += 1
    return None


def _cycle_findings(corpus: Corpus) -> Iterable[Finding]:
    acyclic = {
        pred
        for type_ in corpus.schema.types
        for pred, spec in corpus.schema.rels(type_).items()
        if spec.get("acyclic")
    }
    for pred in sorted(acyclic):
        graph: dict[str, list[str]] = {}
        for slug in corpus.entities:
            graph[slug] = [t for p, t in corpus.out_edges(slug) if p == pred]
        seen: set[str] = set()
        for start in sorted(graph):
            if start in seen:
                continue
            stack = [(start, [start])]
            while stack:
                node, trail = stack.pop()
                for nxt in graph.get(node, []):
                    if nxt in trail:
                        cycle = " -> ".join(trail[trail.index(nxt) :] + [nxt])
                        yield Finding(ERROR, "cycle", start, f"{pred}: {cycle}")
                        seen.update(trail)
                        stack = []
                        break
                    if nxt not in seen:
                        stack.append((nxt, trail + [nxt]))
            seen.add(start)


# ----------------------------------------------------------------- commands


def cmd_check(args: argparse.Namespace) -> int:
    corpus = load(args)
    findings = check(corpus)
    errors = [f for f in findings if f.severity == ERROR]
    gaps = [f for f in findings if f.severity == GAP]
    if args.json:
        print(json.dumps({
            "entities": len(corpus.entities),
            "errors": [f.__dict__ for f in errors],
            "gaps": [f.__dict__ for f in gaps],
        }, indent=2))
    else:
        print(f"{len(corpus.entities)} entities, {len(errors)} errors, {len(gaps)} gaps")
        if errors:
            print("\nerrors — the corpus is broken here")
            for f in errors:
                print(f.line())
        if gaps:
            print("\ngaps — legal, but unfinished")
            for f in gaps:
                print(f.line())
    if errors:
        return 1
    return 1 if (gaps and args.strict) else 0


def cmd_new(args: argparse.Namespace) -> int:
    corpus = load(args)
    schema = corpus.schema
    if args.type not in schema.types:
        raise Bad(f"unknown type {args.type!r} — one of {', '.join(schema.types)}")
    fields = dict(pair.split("=", 1) for pair in args.set) if args.set else {}
    unknown = set(fields) - set(schema.fields(args.type))
    if unknown:
        raise Bad(f"unknown field(s) {', '.join(sorted(unknown))} for {args.type}")

    fm: dict[str, Any] = {"type": args.type, "title": args.title}
    for key, raw in fields.items():
        spec = schema.fields(args.type)[key]
        fm[key] = [v.strip() for v in raw.split(",")] if spec.get("many") else raw
    fm["created"] = date.today().isoformat()

    if schema.is_singleton(args.type):
        path = corpus.root / schema.types[args.type]["path"]
        slug = args.type
    else:
        prefix = schema.prefix_for(args.type, fm)
        if prefix is None:
            spec = schema.types[args.type]["id_prefix"]
            raise Bad(f"{args.type} needs --set {spec['by']}=<{'|'.join(spec['map'])}> to mint an id")
        slug = f"{prefix}-{next_number(corpus, prefix):03d}-{slugify(args.title)}"
        path = type_root(corpus.root, schema, args.type) / f"{slug}.md"
    if path.exists():
        raise Bad(f"{rel(corpus.root, path)} already exists")

    for target in (t for _, t in _declared_edges(schema, args.type, fm)):
        if target not in corpus.entities:
            raise Bad(f"{target!r} resolves to nothing — link it after the target exists")

    ordered = _order_keys(schema, args.type, fm)
    body = template_for(corpus.root, args.type) or ""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(render([l for k, v in ordered for l in emit_key(k, v)], body))
    print(f"{slug}  {rel(corpus.root, path)}")
    return 0


def _declared_edges(schema: Schema, type_: str, fm: dict[str, Any]) -> list[tuple[str, str]]:
    return [(p, t) for p in schema.rels(type_) for t in as_list(fm.get(p)) if isinstance(t, str)]


def _order_keys(schema: Schema, type_: str, fm: dict[str, Any]) -> list[tuple[str, Any]]:
    """type, title, the type's own required fields, created, then the rest."""
    own = [k for k in schema.types[type_].get("attributes", {}) if k in fm]
    lead = ["type", "title", *own, "created"]
    return [(k, fm[k]) for k in lead if k in fm] + [(k, v) for k, v in fm.items() if k not in lead]


def next_number(corpus: Corpus, prefix: str) -> int:
    used = [
        int(m.group(1))
        for slug in corpus.entities
        if (m := re.match(rf"^{re.escape(prefix)}-(\d+)-", slug))
    ]
    return max(used, default=0) + 1


def slugify(text: str, limit: int = 40) -> str:
    words = re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-").split("-")
    out: list[str] = []
    for word in words:
        if word and len("-".join([*out, word])) <= limit:
            out.append(word)
    return "-".join(out) or "untitled"


def cmd_link(args: argparse.Namespace) -> int:
    corpus = load(args)
    entity = _need(corpus, args.id)
    rels = corpus.schema.rels(entity.type)
    if args.predicate not in rels:
        raise Bad(f"{entity.type} has no {args.predicate!r} — one of {', '.join(rels)}")
    spec = rels[args.predicate]
    unlinking = args.__dict__.get("remove", False)
    target = _need(corpus, args.target) if not unlinking else corpus.entities.get(args.target)
    if not unlinking:
        assert target is not None
        if target.slug == entity.slug:
            raise Bad("an entity cannot link to itself")
        if spec["to"] != "any" and target.type != spec["to"]:
            raise Bad(f"{args.predicate} wants {spec['to']}, {target.slug} is a {target.type}")

    current = [v for v in as_list(entity.fm.get(args.predicate)) if isinstance(v, str)]
    if unlinking:
        if args.target not in current:
            print(f"no {args.predicate} -> {args.target} on {entity.slug}")
            return 0
        updated = [v for v in current if v != args.target]
    else:
        if args.target in current:
            print(f"{entity.slug} already has {args.predicate} -> {args.target}")
            return 0
        updated = [*current, args.target] if spec.get("many") else [args.target]

    value: Any = updated if spec.get("many") else (updated[0] if updated else None)
    lines = entity.front_lines
    if value in (None, []):
        span = key_span(lines, args.predicate)
        lines = lines[: span[0]] + lines[span[1] :] if span else lines
    else:
        lines = splice(lines, args.predicate, value)
    entity.path.write_text(render(lines, entity.body))
    arrow = "x" if unlinking else "->"
    print(f"{entity.slug} {args.predicate} {arrow} {args.target}")
    return 0


def cmd_links(args: argparse.Namespace) -> int:
    corpus = load(args)
    entity = _need(corpus, args.id)
    rows: list[tuple[int, str, str, str]] = []
    seen = {entity.slug}
    frontier = [entity.slug]
    for depth in range(1, args.depth + 1):
        nxt: list[str] = []
        for slug in frontier:
            pairs = []
            if args.direction in ("out", "both"):
                pairs += [("out", p, t) for p, t in corpus.out_edges(slug)]
            if args.direction in ("in", "both"):
                pairs += [("in", p, s) for p, s in corpus.in_edges(slug)]
            for way, pred, other in pairs:
                if args.predicate and pred != args.predicate:
                    continue
                label = corpus.schema.inverse(pred) if way == "in" else pred
                rows.append((depth, way, label or pred, other))
                if other not in seen:
                    seen.add(other)
                    nxt.append(other)
        frontier = nxt
    if args.json:
        print(json.dumps([
            {"depth": d, "direction": w, "predicate": p, "id": o, "type": _type_of(corpus, o)}
            for d, w, p, o in rows
        ], indent=2))
        return 0
    print(f"{entity.slug}  ({entity.type})  {entity.title}")
    for depth, way, pred, other in rows:
        hit = corpus.entities.get(other)
        arrow = "->" if way == "out" else "<-"
        pad = "  " * depth
        print(f"{pad}{way:<3} {pred:<14} {arrow} {other:<24} {hit.title if hit else '(missing)'}")
    return 0


def _type_of(corpus: Corpus, slug: str) -> str | None:
    hit = corpus.entities.get(slug)
    return hit.type if hit else None


def cmd_ls(args: argparse.Namespace) -> int:
    corpus = load(args)
    wanted = dict(pair.split("=", 1) for pair in args.where) if args.where else {}
    rows = []
    for e in sorted(corpus.entities.values(), key=lambda x: (x.type, x.slug)):
        if args.type and e.type != args.type:
            continue
        if any(str(e.fm.get(k)) != v for k, v in wanted.items()):
            continue
        if args.tag and args.tag not in as_list(e.fm.get("tags")):
            continue
        preds = {p for p, _ in corpus.out_edges(e.slug)}
        if args.has and args.has not in preds:
            continue
        if args.missing and args.missing in preds:
            continue
        rows.append(e)
    if args.json:
        print(json.dumps(
            [{"id": e.slug, "type": e.type, "path": rel(corpus.root, e.path), **e.fm} for e in rows],
            indent=2,
        ))
        return 0
    for e in rows:
        facet = e.fm.get("kind") or e.fm.get("status") or ""
        print(f"{e.slug:<26} {e.type:<13} {facet!s:<12} {e.title}")
    return 0


def cmd_schema(args: argparse.Namespace) -> int:
    schema = load_schema(find_root(args.root))
    if args.json:
        print(json.dumps(schema.data, indent=2))
        return 0
    for type_, cfg in schema.types.items():
        if schema.is_singleton(type_):
            where = f"{cfg['path']}, one document"
        else:
            where = f"{cfg['path']}/{'|'.join(schema.prefixes(type_))}-NNN-slug.md"
        print(f"\n{type_}  ({where})")
        for name, spec in schema.attrs(type_).items():
            if name == "type":
                continue
            shape = "|".join(spec["enum"]) if spec.get("enum") else spec.get("type", "text")
            print(f"    {name:<14} {shape}{'  (required)' if spec.get('required') else ''}")
        for name, spec in schema.rels(type_).items():
            many = "[]" if spec.get("many") else ""
            inv = f"  (inverse: {spec['inverse']})" if spec.get("inverse") else ""
            print(f"    {name:<14} -> {spec['to']}{many}{inv}")
    return 0


def cmd_init(args: argparse.Namespace) -> int:
    root = Path(args.root or ".").resolve()
    schema = load_schema(root)
    made: list[str] = []
    for type_, cfg in schema.types.items():
        path = root / cfg["path"]
        if schema.is_singleton(type_):
            if path.is_file():
                continue
            path.parent.mkdir(parents=True, exist_ok=True)
            fm = {"type": type_, "title": type_.upper() if type_ == "prd" else "Architecture"}
            fm["created"] = date.today().isoformat()
            lines = [l for k, v in fm.items() for l in emit_key(k, v)]
            path.write_text(render(lines, template_for(root, type_) or ""))
            made.append(cfg["path"])
        elif not path.is_dir():
            path.mkdir(parents=True, exist_ok=True)
            (path / ".gitkeep").touch()
            made.append(cfg["path"] + "/")
    print("\n".join(f"created {m}" for m in made) or "nothing to do — already scaffolded")
    print("\nnext: write knowledge/prd.md, then `bl check`")
    return 0


def _need(corpus: Corpus, slug: str) -> Entity:
    hit = corpus.entities.get(slug)
    if hit is None:
        raise Bad(f"no entity {slug!r} in {corpus.root}")
    return hit


def load(args: argparse.Namespace) -> Corpus:
    root = find_root(args.root)
    return scan(root, load_schema(root))


# --------------------------------------------------------------------- main


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="bl", description=__doc__.split("\n")[0])
    parser.add_argument("-C", "--root", help="workspace root (default: nearest knowledge/ above cwd)")
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("init", help="scaffold the corpus directories and the two documents")
    p.set_defaults(fn=cmd_init)

    p = sub.add_parser("schema", help="the ontology: types, fields, enums, edges")
    p.add_argument("--json", action="store_true")
    p.set_defaults(fn=cmd_schema)

    p = sub.add_parser("new", help="mint one conforming entity file and print its id")
    p.add_argument("type")
    p.add_argument("title")
    p.add_argument("--set", action="append", metavar="FIELD=VALUE", help="repeatable")
    p.set_defaults(fn=cmd_new)

    p = sub.add_parser("link", help="add a schema-checked relation")
    p.add_argument("id")
    p.add_argument("predicate")
    p.add_argument("target")
    p.set_defaults(fn=cmd_link, remove=False)

    p = sub.add_parser("unlink", help="remove a relation")
    p.add_argument("id")
    p.add_argument("predicate")
    p.add_argument("target")
    p.set_defaults(fn=cmd_link, remove=True)

    p = sub.add_parser("ls", help="list entities, filtered")
    p.add_argument("type", nargs="?")
    p.add_argument("--where", action="append", metavar="FIELD=VALUE", help="repeatable")
    p.add_argument("--tag")
    p.add_argument("--has", metavar="PREDICATE")
    p.add_argument("--missing", metavar="PREDICATE", help="the gap query")
    p.add_argument("--json", action="store_true")
    p.set_defaults(fn=cmd_ls)

    p = sub.add_parser("links", help="edges in and out, including the derived inverses")
    p.add_argument("id")
    p.add_argument("--predicate")
    p.add_argument("--depth", type=int, default=1, help="walk transitively (blast radius)")
    p.add_argument("--direction", choices=["in", "out", "both"], default="both")
    p.add_argument("--json", action="store_true")
    p.set_defaults(fn=cmd_links)

    p = sub.add_parser("check", help="sweep the whole corpus: errors break the build, gaps do not")
    p.add_argument("--strict", action="store_true", help="fail on gaps too")
    p.add_argument("--json", action="store_true")
    p.set_defaults(fn=cmd_check)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        return int(args.fn(args))
    except Bad as exc:
        print(f"bl: {exc}", file=sys.stderr)
        return 2
    except BrokenPipeError:  # piped into `head`
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
