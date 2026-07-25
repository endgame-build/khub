"""build-lite standalone: contract drift against khub, and the graduation claim.

`kb/` is a separate implementation with its own reader and its own gate,
so no code is shared with khub. Two things are, and this pins both:

1. **The contract.** `build.schema.yaml` is khub's `core.yaml` base plus the
   build-lite preset, so it must equal what `khub init build-lite` scaffolds —
   modulo exactly two deltas the file documents (`id_prefix` per type, no
   `draft`). Any third delta is drift, and it fails here.
2. **The output.** Every file `kb` writes must be a khub entity. A corpus
   authored entirely through `kb` has to validate and check clean under khub
   with nothing modified — that is the whole graduation path.
"""

from __future__ import annotations

import contextlib
import importlib.util
import io
import sys
from pathlib import Path
from typing import Any

import pytest
from ruamel.yaml import YAML

from khub.core.entity import create
from khub.core.integrity import check as khub_check
from khub.core.integrity import validate as khub_validate
from khub.core.template import load_template
from khub.core.workspace import init_workspace

REPO = Path(__file__).resolve().parents[1]
STANDALONE = REPO / "kb"
SCRIPTS = STANDALONE / "scripts"
PRESETS = REPO / "src" / "khub" / "presets"
PRESET = "build-lite"




@pytest.fixture(scope="module")
def kb() -> Any:
    """The standalone tool, imported as a module — it is not a package."""
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("kb_standalone", SCRIPTS / "kb.py")
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules["kb_standalone"] = module
    spec.loader.exec_module(module)
    return module


@pytest.fixture(scope="module")
def scaffold(tmp_path_factory: pytest.TempPathFactory) -> Path:
    """A stock `khub init build-lite` workspace — the canonical contract."""
    root = tmp_path_factory.mktemp("canonical")
    init_workspace(PRESET, root)
    return root


def _load(path: Path) -> Any:
    with path.open() as fh:
        return YAML(typ="safe").load(fh)


@pytest.mark.integration
def test_base_block_matches_khub(scaffold: Path) -> None:
    canonical = _load(scaffold / ".khub" / "schema.yaml")["base"]
    shipped = _load(SCRIPTS / "build.schema.yaml")["base"]

    assert shipped == canonical, "the base block is khub's, verbatim"


@pytest.mark.integration
def test_entities_match_khub(scaffold: Path) -> None:
    canonical = _load(scaffold / ".khub" / "schema.yaml")["entities"]
    shipped = _load(SCRIPTS / "build.schema.yaml")["entities"]

    assert set(shipped) == set(canonical), "the six types are the contract"
    for type_, cfg in shipped.items():
        assert cfg == canonical[type_], f"{type_} drifted from the preset"


def _diff(khub: Any, shipped: Any, path: str = "") -> list[str]:
    """Every leaf difference between two loaded documents, deepest key named."""
    if isinstance(khub, dict) and isinstance(shipped, dict):
        out: list[str] = []
        for key in sorted(set(khub) | set(shipped)):
            where = f"{path}{key}"
            if key not in khub:
                out.append(f"+ {where} (only in the drop-in)")
            elif key not in shipped:
                out.append(f"- {where} (missing from the drop-in)")
            else:
                out += _diff(khub[key], shipped[key], f"{where}.")
        return out
    if khub != shipped:
        return [f"~ {path.rstrip('.')}: khub={khub!r} drop-in={shipped!r}"]
    return []


@pytest.mark.integration
def test_shipped_schema_is_core_plus_preset_verbatim() -> None:
    """1:1 against the preset SOURCES, not just the scaffold they flatten into.

    Everything — key sets at every depth, enum member order, `acyclic`, `inverse`,
    paths, `required`, `orphan` — must be identical, and `id_prefix` is the only
    key the drop-in is allowed to add.
    """
    combined = {
        "base": _load(PRESETS / "core.yaml")["base"],
        **_load(PRESETS / PRESET / "schema.yaml"),
    }
    shipped = _load(SCRIPTS / "build.schema.yaml")

    assert set(shipped) == set(combined), "top-level keys must match core.yaml + the preset"
    assert _diff(combined, shipped) == [], "\n".join(_diff(combined, shipped))


@pytest.mark.integration
def test_id_prefixes_cover_every_stored_type(kb: Any, scaffold: Path) -> None:
    """The one added key has to be complete, or `kb new` cannot mint an id."""
    schema = kb.load_schema(Path("/nonexistent"))
    for type_ in schema.types:
        if schema.is_singleton(type_):
            assert not schema.prefixes(type_)
            continue
        assert schema.prefixes(type_), f"{type_} has no id_prefix"
        enum = schema.attrs(type_).get("kind", {}).get("enum")
        spec = schema.types[type_]["id_prefix"]
        if isinstance(spec, dict):
            assert sorted(spec["map"]) == sorted(enum or []), f"{type_} prefix map != kind enum"


@pytest.mark.e2e
def test_both_tools_mint_the_same_id(kb: Any, tmp_path: Path) -> None:
    """The point of putting id_prefix in the preset: one rule, two implementations."""
    theirs, ours = tmp_path / "khub", tmp_path / "kb"
    for root in (theirs, ours):
        root.mkdir()
    init_workspace(PRESET, theirs)
    create(theirs, "adr", {"title": "Use Postgres", "status": "proposed"})
    create(theirs, "requirement", {"title": "Settle in 2s", "kind": "constraint"})

    with contextlib.redirect_stdout(io.StringIO()):
        kb.main(["-C", str(ours), "init"])
        kb.main(["-C", str(ours), "add", "adr", "--title", "Use Postgres",
                 "--status", "proposed"])
        kb.main(["-C", str(ours), "add", "requirement", "--title", "Settle in 2s",
                 "--kind", "constraint"])

    minted = lambda root: sorted(p.stem for p in (root / "knowledge").rglob("*.md"))
    assert minted(theirs) == minted(ours) == ["ad-001-use-postgres", "arc42",
                                              "cst-001-settle-in-2s", "prd"]


@pytest.mark.integration
def test_body_templates_match_the_preset(scaffold: Path) -> None:
    """kb's Markdown templates must render what khub's YAML section lists do."""
    shipped = sorted(p.stem for p in (SCRIPTS / "templates").glob("*.md"))
    canonical = sorted(p.stem for p in (scaffold / ".khub" / "templates").glob("*.yaml"))
    assert shipped == canonical

    for type_ in shipped:
        template = load_template(scaffold, type_)
        assert template is not None
        rendered = template.render().rstrip("\n")
        on_disk = (SCRIPTS / "templates" / f"{type_}.md").read_text().rstrip("\n")
        assert on_disk == rendered, f"{type_}.md drifted from the preset template"


@pytest.mark.e2e
def test_a_kb_authored_corpus_graduates_to_khub(kb: Any, tmp_path: Path) -> None:
    """The graduation claim, run rather than asserted."""
    root = tmp_path / "hub"
    root.mkdir()

    def run(*argv: str) -> int:
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            return int(kb.main(["-C", str(root), *argv]))

    assert run("init") == 0
    assert run("add", "component", "--title", "Public API", "--kind", "service") == 0
    assert run("add", "component", "--title", "Stripe", "--kind", "external") == 0
    assert run("add", "requirement", "--title", "Payments settle in 2s", "--kind", "constraint") == 0
    assert run("add", "adr", "--title", "Use Stripe", "--status", "accepted", "--affects", "cmp-001-public-api") == 0
    assert run("add", "feature-spec", "--title", "Checkout", "--status", "planned", "--requirements", "cst-001-payments-settle-in-2s") == 0
    assert run("link", "cst-001-payments-settle-in-2s", "realized_in", "cmp-001-public-api") == 0
    assert run("link", "cmp-001-public-api", "depends_on", "cmp-002-stripe") == 0
    assert run("check", "--strict") == 0

    before = {p: p.read_bytes() for p in sorted(root.rglob("*.md"))}
    init_workspace(PRESET, root, force=True)
    assert {p: p.read_bytes() for p in sorted(root.rglob("*.md"))} == before, (
        "khub init rewrote entity files — the corpus is not khub-shaped"
    )

    assert not khub_validate(root).errors
    report = khub_check(root)
    assert report.passed, report
    assert not report.orphans and not report.malformed and not report.strays


@pytest.mark.e2e
def test_khub_written_entities_pass_the_kb_gate(kb: Any, tmp_path: Path) -> None:
    """The other direction: khub's own writes must clear kb's stricter reader."""
    root = tmp_path / "hub"
    root.mkdir()
    init_workspace(PRESET, root)
    create(root, "component", {"title": "Ledger", "kind": "service"}, id_="cmp-001-ledger")

    corpus = kb.scan(root, kb.load_schema(Path("/nonexistent")))
    assert not corpus.malformed, corpus.malformed
    # kb closes the schema, so every attribute khub writes — `draft` included —
    # has to be declared in build.schema.yaml or this reports on every entity.
    errors = [f for f in kb.check(corpus) if f.severity == kb.ERROR]
    assert errors == [], errors
