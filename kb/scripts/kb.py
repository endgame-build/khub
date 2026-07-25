#!/usr/bin/env python3
"""kb — build-lite: a typed doc corpus for one build project.

Markdown + YAML frontmatter in git is the truth. This script is the part an
agent cannot do for itself: mint a conforming file, keep an edge honest, walk
the derived graph, and sweep the whole corpus for breakage.

Everything else (read, search, write prose) the agent already does with its own
tools, so it is not here.

Stdlib only. It reads `build.schema.yaml` and `templates/` from its own
directory — or from `<workspace>/.kb/` when a project overrides them —
and hardcodes no type, field or predicate.

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
import os
import re
import shutil
import sys
from collections.abc import Iterable, Sequence
from dataclasses import dataclass, field
from datetime import date
from pathlib import Path
from typing import Any

HERE = Path(__file__).resolve().parent
FENCE = "---"
KEY_RE = re.compile(r"^([A-Za-z_][A-Za-z0-9_-]*):(?:[ \t]+(.*))?$")
YAML_KEY_RE = re.compile(r"^([A-Za-z_][A-Za-z0-9_.-]*):[ \t]*(.*)$")
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
        if inline and inline.lstrip().startswith("{"):
            raise Bad(f"line {i + 1}: frontmatter is flat — no nested mappings ({key!r})")
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


# ---------------------------------------------------------------- yaml (read)
#
# Enough YAML to read build.schema.yaml, which is authored in khub's vocabulary
# and therefore nested: block mappings by indentation, flow mappings and lists
# (`{ enum: [a, b], required: true }`), quoted scalars, and comments both on
# their own line and trailing. Reading only — nothing rewrites this file — and
# checked in the tests against ruamel's parse of the shipped schema.


def load_yaml(text: str) -> Any:
    lines: list[tuple[int, str, int]] = []
    for number, raw in enumerate(text.split("\n"), 1):
        stripped = _strip_comment(raw)
        if stripped.strip():
            lines.append((len(stripped) - len(stripped.lstrip(" ")), stripped.strip(), number))
    if not lines:
        return {}
    cursor = [0]
    value = _block(lines, cursor, lines[0][0])
    if cursor[0] != len(lines):
        raise Bad(f"line {lines[cursor[0]][2]}: unexpected indentation")
    return value


def _strip_comment(line: str) -> str:
    quote = ""
    for i, ch in enumerate(line):
        if quote:
            if ch == quote:
                quote = ""
        elif ch in "\"'":
            quote = ch
        elif ch == "#" and (i == 0 or line[i - 1] in " \t"):
            return line[:i]
    return line


def _block(lines: list[tuple[int, str, int]], cursor: list[int], indent: int) -> Any:
    if lines[cursor[0]][1].startswith("- "):
        return _block_list(lines, cursor, indent)
    return _block_map(lines, cursor, indent)


def _block_map(lines: list[tuple[int, str, int]], cursor: list[int], indent: int) -> dict[str, Any]:
    out: dict[str, Any] = {}
    while cursor[0] < len(lines):
        col, text, number = lines[cursor[0]]
        if col < indent:
            break
        if col > indent:
            raise Bad(f"line {number}: unexpected indentation")
        m = YAML_KEY_RE.match(text)
        if not m:
            raise Bad(f"line {number}: not a `key: value` line ({text!r})")
        key, inline = m.group(1), m.group(2)
        cursor[0] += 1
        if inline:
            out[key] = _flow(inline, number)
        elif cursor[0] < len(lines) and lines[cursor[0]][0] > indent:
            out[key] = _block(lines, cursor, lines[cursor[0]][0])
        else:
            out[key] = None
    return out


def _block_list(lines: list[tuple[int, str, int]], cursor: list[int], indent: int) -> list[Any]:
    out: list[Any] = []
    while cursor[0] < len(lines):
        col, text, number = lines[cursor[0]]
        if col < indent or not text.startswith("- "):
            break
        cursor[0] += 1
        out.append(_flow(text[2:].strip(), number))
    return out


def _flow(text: str, number: int) -> Any:
    value, rest = _flow_value(text, number)
    if rest.strip():
        raise Bad(f"line {number}: trailing content after value ({rest.strip()!r})")
    return value


def _flow_value(text: str, number: int) -> tuple[Any, str]:
    s = text.lstrip()
    if s.startswith("{"):
        return _flow_pairs(s[1:], number)
    if s.startswith("["):
        return _flow_items(s[1:], number)
    if s[:1] in ("'", '"'):
        quote, i = s[0], 1
        while i < len(s) and s[i] != quote:
            i += 2 if s[i] == "\\" else 1
        if i >= len(s):
            raise Bad(f"line {number}: unterminated quote")
        return s[1:i].replace('\\"', '"').replace("\\\\", "\\"), s[i + 1 :]
    end = len(s)
    for i, ch in enumerate(s):
        if ch in ",}]":
            end = i
            break
    return _bare(s[:end].strip()), s[end:]


def _flow_pairs(text: str, number: int) -> tuple[dict[str, Any], str]:
    out: dict[str, Any] = {}
    rest = text
    while True:
        rest = rest.lstrip()
        if rest.startswith("}"):
            return out, rest[1:]
        if not rest:
            raise Bad(f"line {number}: unterminated flow mapping")
        key, _, remainder = rest.partition(":")
        if not _:
            raise Bad(f"line {number}: flow mapping entry without a ':' ({rest!r})")
        value, rest = _flow_value(remainder, number)
        out[key.strip()] = value
        rest = rest.lstrip()
        rest = rest.removeprefix(",")


def _flow_items(text: str, number: int) -> tuple[list[Any], str]:
    out: list[Any] = []
    rest = text
    while True:
        rest = rest.lstrip()
        if rest.startswith("]"):
            return out, rest[1:]
        if not rest:
            raise Bad(f"line {number}: unterminated flow sequence")
        value, rest = _flow_value(rest, number)
        out.append(value)
        rest = rest.lstrip()
        rest = rest.removeprefix(",")


def _bare(token: str) -> Any:
    if token in ("true", "false"):
        return token == "true"
    if token in ("null", "~", ""):
        return None
    if re.fullmatch(r"-?\d+", token):
        return int(token)
    return token


# -------------------------------------------------------------------- schema


class Schema:
    """The resolved contract. khub's vocabulary, read straight from the YAML."""

    def __init__(self, data: dict[str, Any]) -> None:
        self.data = data
        self.base = data.get("base") or {}
        entities = data.get("entities")
        if not isinstance(entities, dict) or not entities:
            raise Bad("schema has no `entities` block")
        self.types: dict[str, dict[str, Any]] = entities

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
        return self.types[type_].get("layout") == "singleton"

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


SCHEMA_FILE = "build.schema.yaml"


CONFIG_DIR = ".kb"


def _config(root: Path, name: str) -> Path:
    """The workspace override if it has one, else the copy beside this script."""
    local = root / CONFIG_DIR / name
    return local if local.is_file() else HERE / name


def load_schema(root: Path) -> Schema:
    path = _config(root, SCHEMA_FILE)
    if not path.is_file():
        raise Bad(f"no {SCHEMA_FILE} beside {HERE / 'kb.py'} or under {root}/{CONFIG_DIR}/")
    return Schema(load_yaml(path.read_text()))


def template_for(root: Path, type_: str) -> str | None:
    local = root / CONFIG_DIR / "templates" / f"{type_}.md"
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
    """Ids follow the schema: `<prefix>-NNN-slug`, or `NNN-slug` with no prefix."""
    prefixes = schema.prefixes(e.type)
    if not prefixes:
        if not re.match(r"^\d{3,}-[a-z0-9-]+$", e.slug):
            yield Finding(ERROR, "bad_id", e.slug, "filename must be NNN-slug")
        return
    expected = schema.prefix_for(e.type, e.fm)
    if expected is None:
        # The prefix is chosen by an attribute this entity has not set yet, so no
        # id can be right. `incomplete` already names that cause; do not say it twice.
        return
    m = re.match(r"^([a-z]+)-(\d{3,})-([a-z0-9-]+)$", e.slug)
    if not m or m.group(1) not in prefixes:
        yield Finding(ERROR, "bad_id", e.slug, f"filename must be {'|'.join(prefixes)}-NNN-slug")
        return
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


VALIDATE_CODES = {
    "malformed", "duplicate_id", "type_mismatch", "unknown_field", "bad_value",
    "bad_id", "cardinality", "self_link", "dangling", "wrong_type",
}


def _emit(args: argparse.Namespace, payload: Any, render_text: Any) -> None:
    """khub's output contract: --format json for the agent, text for a human."""
    if args.format == "json":
        print(json.dumps(payload, indent=2))
    else:
        render_text()


def _record(corpus: Corpus, e: Entity) -> dict[str, Any]:
    """The uniform JSON record. `id` is the bare slug: kb ids carry a type prefix,
    so unlike khub they are never ambiguous and never need `type/slug`."""
    return {"id": e.slug, "type": e.type, "slug": e.slug,
            "path": rel(corpus.root, e.path), **e.fm}


def dynamic_fields(extra: Sequence[str], known: Iterable[str]) -> dict[str, str]:
    """khub's `--<field> <value>` options, which argparse cannot declare ahead."""
    fields = set(known)
    out: dict[str, str] = {}
    i = 0
    while i < len(extra):
        token = extra[i]
        if not token.startswith("--"):
            raise Bad(f"unexpected argument {token!r}")
        if "=" in token:
            name, value = token[2:].split("=", 1)
            i += 1
        else:
            name = token[2:]
            if i + 1 >= len(extra) or extra[i + 1].startswith("--"):
                raise Bad(f"--{name} needs a value")
            value = extra[i + 1]
            i += 2
        if name not in fields and name.replace("-", "_") in fields:
            name = name.replace("-", "_")
        out[name] = value
    return out


def _declared_edges(schema: Schema, type_: str, fm: dict[str, Any]) -> list[tuple[str, str]]:
    return [(p, t) for p in schema.rels(type_) for t in as_list(fm.get(p)) if isinstance(t, str)]


def _order_keys(schema: Schema, type_: str, fm: dict[str, Any]) -> list[tuple[str, Any]]:
    """type, title, the type's own required fields, created, then the rest."""
    own = [k for k in schema.types[type_].get("attributes", {}) if k in fm]
    # dict.fromkeys dedupes: a type may redeclare a base attribute (every type
    # here redeclares `title` to make it required), and it must stay one key.
    lead = list(dict.fromkeys(["type", "title", *own, "created"]))
    return [(k, fm[k]) for k in lead if k in fm] + [(k, v) for k, v in fm.items() if k not in lead]


def next_number(corpus: Corpus, prefix: str | None) -> int:
    """One past the highest ordinal in use for this prefix — khub counts the same way."""
    pattern = re.compile(rf"^{re.escape(prefix)}-(\d+)-" if prefix else r"^(\d+)-")
    used = [int(m.group(1)) for slug in corpus.entities if (m := pattern.match(slug))]
    return max(used, default=0) + 1


def slugify(text: str, limit: int = 40) -> str:
    words = re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-").split("-")
    out: list[str] = []
    for word in words:
        if word and len("-".join([*out, word])) <= limit:
            out.append(word)
    return "-".join(out) or "untitled"


def _type_of(corpus: Corpus, slug: str) -> str | None:
    hit = corpus.entities.get(slug)
    return hit.type if hit else None


# ------------------------------------------------------------------ workspace


def cmd_init(args: argparse.Namespace) -> int:
    root = Path(args.workspace or ".").resolve()
    schema = load_schema(root)
    made: list[str] = []
    for type_, cfg in schema.types.items():
        path = root / cfg["path"]
        if schema.is_singleton(type_):
            if path.is_file():
                continue
            path.parent.mkdir(parents=True, exist_ok=True)
            fm = {"type": type_, "title": type_.upper() if type_ == "prd" else "Architecture",
                  "created": date.today().isoformat()}
            path.write_text(render([l for k, v in fm.items() for l in emit_key(k, v)],
                                   template_for(root, type_) or ""))
            made.append(cfg["path"])
        elif not path.is_dir():
            path.mkdir(parents=True, exist_ok=True)
            (path / ".gitkeep").touch()
            made.append(cfg["path"] + "/")
    print("\n".join(f"created {m}" for m in made) or "nothing to do — already scaffolded")
    print("\nnext: write knowledge/prd.md, then `kb check`")
    return 0


def cmd_schema(args: argparse.Namespace) -> int:
    schema = load_schema(find_root(args.workspace))
    view = args.view or "all"
    if view == "show" and args.type not in schema.types:
        raise Bad(f"unknown type {args.type!r} — one of {', '.join(schema.types)}")
    types = [args.type] if view == "show" else list(schema.types)

    if args.format == "json":
        if view == "types":
            print(json.dumps(list(schema.types), indent=2))
        elif view == "edges":
            print(json.dumps({t: schema.rels(t) for t in types}, indent=2))
        elif view == "show":
            print(json.dumps({args.type: schema.types[args.type]}, indent=2))
        else:
            print(json.dumps(schema.data, indent=2))
        return 0

    if view == "types":
        for type_ in types:
            print(type_)
        return 0
    for type_ in types:
        cfg = schema.types[type_]
        where = (f"{cfg['path']}, one document" if schema.is_singleton(type_)
                 else f"{cfg['path']}/{'|'.join(schema.prefixes(type_)) or 'slug'}-NNN-slug.md")
        print(f"\n{type_}  ({where})")
        if view != "edges":
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


def cmd_status(args: argparse.Namespace) -> int:
    corpus = load(args)
    findings = check(corpus)
    counts = {t: len(corpus.by_type(t)) for t in corpus.schema.types}
    errors = sum(1 for f in findings if f.severity == ERROR)
    gaps = sum(1 for f in findings if f.severity == GAP)
    orphans = sum(1 for f in findings if f.code == "orphan")
    payload = {"counts": counts, "total": len(corpus.entities), "orphan": orphans,
               "errors": errors, "gaps": gaps}

    def text() -> None:
        for type_, n in counts.items():
            print(f"{type_:<14} {n}")
        print(f"\n{len(corpus.entities)} entities, {orphans} orphan, "
              f"{errors} errors, {gaps} gaps")

    _emit(args, payload, text)
    return 0


# ---------------------------------------------------------------------- write


def cmd_add(args: argparse.Namespace) -> int:
    corpus = load(args)
    schema = corpus.schema
    if args.type not in schema.types:
        raise Bad(f"unknown type {args.type!r} — one of {', '.join(schema.types)}")
    declared = schema.fields(args.type)
    fields = dynamic_fields(args.extra, declared)
    unknown = set(fields) - set(declared)
    if unknown:
        raise Bad(f"unknown field(s) {', '.join(sorted(unknown))} for {args.type}")
    title = fields.pop("title", None)
    if not title:
        raise Bad("--title is required: it is the slug source")

    fm: dict[str, Any] = {"type": args.type, "title": title}
    for key, raw in fields.items():
        fm[key] = [v.strip() for v in raw.split(",")] if declared[key].get("many") else raw
    fm["created"] = date.today().isoformat()

    if schema.is_singleton(args.type):
        path, slug = corpus.root / schema.types[args.type]["path"], args.type
    else:
        slug = args.id or _mint(corpus, schema, args.type, fm, title)
        path = type_root(corpus.root, schema, args.type) / f"{slug}.md"
    if path.exists():
        raise Bad(f"{rel(corpus.root, path)} already exists")

    for target in (t for _, t in _declared_edges(schema, args.type, fm)):
        if target not in corpus.entities:
            raise Bad(f"{target!r} resolves to nothing — link it after the target exists")

    body = args.body if args.body is not None else (template_for(corpus.root, args.type) or "")
    if args.body_file:
        body = sys.stdin.read() if args.body_file == "-" else Path(args.body_file).read_text()
    ordered = _order_keys(schema, args.type, fm)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(render([l for k, v in ordered for l in emit_key(k, v)], body))
    _emit(args, {"id": slug, "type": args.type, "slug": slug, "path": rel(corpus.root, path)},
          lambda: print(f"{slug}  {rel(corpus.root, path)}"))
    return 0


def _mint(corpus: Corpus, schema: Schema, type_: str, fm: dict[str, Any], title: str) -> str:
    """khub's rule: `<prefix>-NNN-<slug>`, or `NNN-<slug>` where no prefix is declared."""
    spec = schema.types[type_].get("id_prefix")
    prefix = schema.prefix_for(type_, fm)
    if prefix is None and isinstance(spec, dict):
        raise Bad(f"{type_} needs --{spec['by']} <{'|'.join(spec['map'])}> to mint an id")
    stem = f"{prefix}-" if prefix else ""
    return f"{stem}{next_number(corpus, prefix):03d}-{slugify(title)}"


def cmd_edit(args: argparse.Namespace) -> int:
    corpus = load(args)
    entity = _need(corpus, args.id)
    declared = corpus.schema.fields(entity.type)
    if args.field not in declared:
        raise Bad(f"{entity.type} has no {args.field!r} — one of {', '.join(declared)}")
    if args.field in corpus.schema.rels(entity.type):
        raise Bad(f"{args.field} is a relation — use `kb link {args.id} {args.field} <target>`")
    problem = _attr_problem(declared[args.field], args.value)
    if problem:
        raise Bad(f"{args.field}: {problem}")
    lines = splice(entity.front_lines, args.field, args.value)
    if "updated" in declared:
        lines = splice(lines, "updated", date.today().isoformat())
    entity.path.write_text(render(lines, entity.body))
    _emit(args, {"id": entity.slug, "type": entity.type, args.field: args.value},
          lambda: print(f"{entity.slug} {args.field} = {args.value}"))
    return 0


def cmd_remove(args: argparse.Namespace) -> int:
    corpus = load(args)
    entity = _need(corpus, args.id)
    inbound = corpus.in_edges(entity.slug)
    if inbound and not args.force:
        who = ", ".join(f"{s} ({p})" for p, s in inbound)
        raise Bad(f"{entity.slug} still has inbound edges from {who} — --force to delete anyway")
    entity.path.unlink()
    _emit(args, {"id": entity.slug, "removed": rel(corpus.root, entity.path)},
          lambda: print(f"removed {rel(corpus.root, entity.path)}"))
    return 0


def cmd_link(args: argparse.Namespace) -> int:
    corpus = load(args)
    entity = _need(corpus, args.id)
    rels = corpus.schema.rels(entity.type)
    if args.predicate not in rels:
        raise Bad(f"{entity.type} has no {args.predicate!r} — one of {', '.join(rels)}")
    spec = rels[args.predicate]
    unlinking = args.remove
    if not unlinking:
        target = _need(corpus, args.target)
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
    print(f"{entity.slug} {args.predicate} {'x' if unlinking else '->'} {args.target}")
    return 0


# ----------------------------------------------------------------- read/walk


def cmd_get(args: argparse.Namespace) -> int:
    corpus = load(args)
    entity = _need(corpus, args.id)
    payload = _record(corpus, entity)
    if args.edges:
        payload["edges"] = (
            [{"direction": "out", "predicate": p, "id": t} for p, t in corpus.out_edges(entity.slug)]
            + [{"direction": "in", "predicate": corpus.schema.inverse(p) or p, "id": s}
               for p, s in corpus.in_edges(entity.slug)]
        )

    def text() -> None:
        print(f"{entity.slug}  ({entity.type})  {rel(corpus.root, entity.path)}")
        for key, value in entity.fm.items():
            print(f"  {key:<14} {value}")
        for edge in payload.get("edges", []):
            arrow = "->" if edge["direction"] == "out" else "<-"
            print(f"  {edge['direction']:<3} {edge['predicate']:<14} {arrow} {edge['id']}")

    _emit(args, payload, text)
    return 0


def cmd_query(args: argparse.Namespace) -> int:
    corpus = load(args)
    schema = corpus.schema
    known = {k for t in schema.types for k in schema.fields(t)}
    wanted = dynamic_fields(args.extra, known)
    unknown = set(wanted) - known
    if unknown:
        raise Bad(f"unknown field(s) {', '.join(sorted(unknown))}")

    rows: list[Entity] = []
    for e in sorted(corpus.entities.values(), key=lambda x: (x.type, x.slug)):
        if args.type and e.type != args.type:
            continue
        if any(str(e.fm.get(k)) != v for k, v in wanted.items()):
            continue
        if args.tag and args.tag not in as_list(e.fm.get("tags")):
            continue
        preds = {p for p, _ in corpus.out_edges(e.slug)} | {
            corpus.schema.inverse(p) or p for p, _ in corpus.in_edges(e.slug)
        }
        if args.has and args.has not in preds:
            continue
        if args.missing and args.missing in preds:
            continue
        rows.append(e)
    if args.limit:
        rows = rows[: args.limit]

    def text() -> None:
        for e in rows:
            facet = e.fm.get("kind") or e.fm.get("status") or ""
            print(f"{e.slug:<26} {e.type:<13} {facet!s:<12} {e.title}")

    _emit(args, [_record(corpus, e) for e in rows], text)
    return 0


def _walk(corpus: Corpus, start: str, *, predicate: str | None,
          direction: str, depth: int) -> list[tuple[int, str, str, str]]:
    """One BFS behind neighbors, impact and history — they differ only in defaults."""
    rows: list[tuple[int, str, str, str]] = []
    seen, frontier = {start}, [start]
    for level in range(1, depth + 1):
        nxt: list[str] = []
        for slug in frontier:
            pairs: list[tuple[str, str, str]] = []
            if direction in ("out", "both"):
                pairs += [("out", p, t) for p, t in corpus.out_edges(slug)]
            if direction in ("in", "both"):
                pairs += [("in", p, s) for p, s in corpus.in_edges(slug)]
            for way, pred, other in pairs:
                if predicate and pred != predicate:
                    continue
                label = corpus.schema.inverse(pred) if way == "in" else pred
                rows.append((level, way, label or pred, other))
                if other not in seen:
                    seen.add(other)
                    nxt.append(other)
        frontier = nxt
    return rows


def _render_walk(corpus: Corpus, entity: Entity, rows: list[tuple[int, str, str, str]],
                 args: argparse.Namespace) -> int:
    def text() -> None:
        print(f"{entity.slug}  ({entity.type})  {entity.title}")
        for level, way, pred, other in rows:
            hit = corpus.entities.get(other)
            arrow = "->" if way == "out" else "<-"
            print(f"{'  ' * level}{way:<3} {pred:<14} {arrow} {other:<24} "
                  f"{hit.title if hit else '(missing)'}")

    _emit(args, [{"depth": d, "direction": w, "predicate": p, "id": o,
                  "type": _type_of(corpus, o)} for d, w, p, o in rows], text)
    return 0


def cmd_neighbors(args: argparse.Namespace) -> int:
    corpus = load(args)
    entity = _need(corpus, args.id)
    direction = "in" if args.in_ else "out" if args.out else "both"
    rows = _walk(corpus, entity.slug, predicate=args.predicate,
                 direction=direction, depth=args.depth)
    return _render_walk(corpus, entity, rows, args)


def cmd_impact(args: argparse.Namespace) -> int:
    corpus = load(args)
    entity = _need(corpus, args.id)
    rows = _walk(corpus, entity.slug, predicate=args.predicate,
                 direction="in" if args.reverse else "out", depth=len(corpus.entities) or 1)
    return _render_walk(corpus, entity, rows, args)


def cmd_history(args: argparse.Namespace) -> int:
    corpus = load(args)
    entity = _need(corpus, args.id)
    rows = _walk(corpus, entity.slug, predicate=args.predicate,
                 direction="both", depth=args.limit or len(corpus.entities) or 1)
    return _render_walk(corpus, entity, rows, args)


# ------------------------------------------------------------------ integrity


def _report(args: argparse.Namespace, corpus: Corpus, findings: list[Finding]) -> int:
    errors = [f for f in findings if f.severity == ERROR]
    gaps = [f for f in findings if f.severity == GAP]

    def text() -> None:
        print(f"{len(corpus.entities)} entities, {len(errors)} errors, {len(gaps)} gaps")
        if errors:
            print("\nerrors — the corpus is broken here")
            for f in errors:
                print(f.line())
        if gaps:
            print("\ngaps — legal, but unfinished")
            for f in gaps:
                print(f.line())

    _emit(args, {"entities": len(corpus.entities), "strict": bool(getattr(args, "strict", False)),
                 "errors": [f.__dict__ for f in errors], "gaps": [f.__dict__ for f in gaps]}, text)
    if errors:
        return 1
    return 1 if (gaps and getattr(args, "strict", False)) else 0


def cmd_validate(args: argparse.Namespace) -> int:
    """Per-entity well-formedness and referential integrity, khub's split."""
    corpus = load(args)
    findings = [f for f in check(corpus) if f.code in VALIDATE_CODES]
    if args.target:
        findings = [f for f in findings
                    if f.where == args.target or f.where.startswith(f"{args.target}/")
                    or _type_of(corpus, f.where) == args.target]
    return _report(args, corpus, findings)


def cmd_check(args: argparse.Namespace) -> int:
    """Everything validate covers, plus the graph-wide gates."""
    corpus = load(args)
    return _report(args, corpus, check(corpus))


# ------------------------------------------------------------------- install


SKILL_TARGETS = {"opencode": ".opencode/skills", "claude": ".claude/skills",
                 "agents": ".agents/skills"}


def cmd_install_skills(args: argparse.Namespace) -> int:
    """Copy the skill where a host will find it. khub's verb, khub's flags."""
    source = HERE.parent / "skills" / "kb"
    if not source.is_dir():
        raise Bad(f"no skill directory at {source}")
    targets = args.target or ["opencode"]
    root = find_root(args.workspace)
    written: list[str] = []
    for name in targets:
        if name not in SKILL_TARGETS:
            raise Bad(f"unknown target {name!r} — one of {', '.join(SKILL_TARGETS)}")
        dest = _skill_dir(name, root, args.global_) / "kb"
        written.append(str(dest))
        if args.dry_run:
            continue
        shutil.copytree(source, dest, dirs_exist_ok=True,
                        ignore=shutil.ignore_patterns("__pycache__"))
    if args.bin and not args.dry_run:
        binary = Path(args.bin).expanduser()
        binary.mkdir(parents=True, exist_ok=True)
        link = binary / "kb"
        link.unlink(missing_ok=True)
        link.symlink_to(HERE / "kb.py")
        written.append(str(link))

    def text() -> None:
        for path in written:
            print(f"{'would install' if args.dry_run else 'installed'} {path}")
        print('\nallow it — the catch-all goes FIRST, opencode takes the last match:')
        print('  { "permission": { "bash": { "*": "ask", "kb *": "allow" } } }')

    _emit(args, {"installed": written, "dry_run": args.dry_run}, text)
    return 0


def _skill_dir(target: str, root: Path, is_global: bool) -> Path:
    if not is_global:
        return root / SKILL_TARGETS[target]
    if target == "opencode":  # opencode reads global skills under the XDG root
        xdg = os.environ.get("XDG_CONFIG_HOME")
        return (Path(xdg) if xdg else Path.home() / ".config") / "opencode" / "skills"
    return Path.home() / SKILL_TARGETS[target]


def _need(corpus: Corpus, slug: str) -> Entity:
    hit = corpus.entities.get(slug)
    if hit is None:
        raise Bad(f"no entity {slug!r} in {corpus.root}")
    return hit


def load(args: argparse.Namespace) -> Corpus:
    root = find_root(args.workspace)
    return scan(root, load_schema(root))


# --------------------------------------------------------------------- main
#
# The verbs and flags are khub's, so what an agent learns on one transfers to the
# other. kb implements a subset: khub's `search`, `stale`, `reindex`, `viz`,
# `backfill` and `wire` are deliberately absent (see the README).


def _add_format(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--format", choices=["text", "json"], default="text")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="kb", description=__doc__.split("\n")[0])
    parser.add_argument("-C", "--workspace",
                        help="operate on this workspace (default: nearest knowledge/ above cwd)")
    # Not required: a bare `kb` prints the verb list rather than an argparse error,
    # because that listing is how a reader (and an agent) discovers the surface.
    sub = parser.add_subparsers(dest="command", metavar="<command>")

    p = sub.add_parser("init", help="scaffold the corpus directories and the two documents")
    p.set_defaults(fn=cmd_init)

    p = sub.add_parser("schema", help="the ontology: types, fields, enums, edges")
    p.add_argument("view", nargs="?", choices=["types", "show", "edges"])
    p.add_argument("type", nargs="?")
    _add_format(p)
    p.set_defaults(fn=cmd_schema)

    p = sub.add_parser("status", help="counts per type, orphans, errors, gaps")
    _add_format(p)
    p.set_defaults(fn=cmd_status)

    p = sub.add_parser("add", help="mint one conforming entity file; --<field> <value>")
    p.add_argument("type")
    p.add_argument("--id", help="explicit slug instead of a minted one")
    p.add_argument("--body")
    p.add_argument("--body-file", help="- for stdin")
    _add_format(p)
    p.set_defaults(fn=cmd_add)

    p = sub.add_parser("get", help="one entity; --edges adds the derived inverses")
    p.add_argument("id")
    p.add_argument("--edges", action="store_true")
    _add_format(p)
    p.set_defaults(fn=cmd_get)

    p = sub.add_parser("edit", help="change one attribute and bump updated")
    p.add_argument("id")
    p.add_argument("field")
    p.add_argument("value")
    _add_format(p)
    p.set_defaults(fn=cmd_edit)

    p = sub.add_parser("remove", help="delete an entity; refuses while an edge points at it")
    p.add_argument("id")
    p.add_argument("--force", action="store_true")
    _add_format(p)
    p.set_defaults(fn=cmd_remove)

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

    p = sub.add_parser("query", help="filter by frontmatter; --<field> <value>")
    p.add_argument("--type")
    p.add_argument("--tag")
    p.add_argument("--has", metavar="PREDICATE")
    p.add_argument("--missing", metavar="PREDICATE", help="the gap query")
    p.add_argument("--limit", type=int)
    _add_format(p)
    p.set_defaults(fn=cmd_query)

    p = sub.add_parser("neighbors", help="one-hop adjacency, both directions by default")
    p.add_argument("id")
    p.add_argument("--predicate")
    p.add_argument("--depth", type=int, default=1)
    p.add_argument("--in", dest="in_", action="store_true")
    p.add_argument("--out", action="store_true")
    _add_format(p)
    p.set_defaults(fn=cmd_neighbors)

    p = sub.add_parser("impact", help="blast radius: the transitive closure over one predicate")
    p.add_argument("id")
    p.add_argument("--predicate", default="depends_on")
    p.add_argument("--reverse", action="store_true", help="ancestors instead of descendants")
    _add_format(p)
    p.set_defaults(fn=cmd_impact)

    p = sub.add_parser("history", help="the supersession chain")
    p.add_argument("id")
    p.add_argument("--predicate", default="supersedes")
    p.add_argument("--limit", type=int)
    _add_format(p)
    p.set_defaults(fn=cmd_history)

    p = sub.add_parser("validate", help="per-entity well-formedness and referential integrity")
    p.add_argument("target", nargs="?", help="a type or an id (default: the whole workspace)")
    _add_format(p)
    p.set_defaults(fn=cmd_validate)

    p = sub.add_parser("check", help="graph-wide: errors break the build, gaps do not")
    p.add_argument("--strict", action="store_true", help="fail on gaps too")
    _add_format(p)
    p.set_defaults(fn=cmd_check)

    p = sub.add_parser("install-skills", help="copy the skill into the agent skill directories")
    p.add_argument("--target", action="append", choices=sorted(SKILL_TARGETS),
                   help="repeatable (default: opencode)")
    p.add_argument("--global", dest="global_", action="store_true")
    p.add_argument("--bin", metavar="DIR", help="also symlink `kb` onto PATH")
    p.add_argument("--dry-run", action="store_true")
    _add_format(p)
    p.set_defaults(fn=cmd_install_skills)
    return parser


# `add` and `query` take schema-driven --<field> options argparse cannot declare.
DYNAMIC = {"add", "query"}


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    argv = list(sys.argv[1:] if argv is None else argv)
    if any(a in DYNAMIC for a in argv):
        args, extra = parser.parse_known_args(argv)
        args.extra = extra
    else:
        args, args.extra = parser.parse_args(argv), []
    if not getattr(args, "fn", None):
        parser.print_help()
        return 0
    try:
        return int(args.fn(args))
    except Bad as exc:
        print(f"kb: {exc}", file=sys.stderr)
        return 2
    except BrokenPipeError:  # piped into `head`
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
