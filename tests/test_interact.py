"""Interactive CLI layer — the prompt gate and the wizard flows.

Prompt libraries read a real terminal, so we never drive questionary itself here.
Instead: (1) the gate ``can_prompt`` is pure, tested exhaustively; (2) the wizards
run through a ``FakePrompter`` injected at the single seam ``interact.make_prompter``,
which both forces the interactive branch (the test runner is non-TTY) and scripts
answers — so the real command logic runs with zero prompt_toolkit.
"""

from __future__ import annotations

import json
import types
from pathlib import Path

import pytest
from typer.testing import CliRunner

from khub.cli import interact
from khub.cli.interact import CliState, can_prompt
from khub.cli.main import app

runner = CliRunner()

# A minimal, self-contained preset for the fast wizard paths.
NOTE_PRESET = """
version: "9.9.9"
entities:
  note:
    layout: file
    path: notes
    attributes:
      body: { type: text }
"""


@pytest.fixture
def preset_source(tmp_path: Path) -> Path:
    """A preset-source dir holding the tiny `note` preset."""
    src = tmp_path / "presets"
    src.mkdir()
    (src / "note").mkdir()
    (src / "note" / "schema.yaml").write_text(NOTE_PRESET)
    return src


# --- Layer 1: the gate is a pure function -----------------------------------------


@pytest.mark.unit
@pytest.mark.parametrize(
    "state, fmt, tty, expected",
    [
        (CliState(None, agent=False), "text", True, True),   # human on a TTY → prompt
        (CliState(None, agent=False), "text", False, False),  # piped → no prompt
        (CliState(None, agent=True), "text", True, False),   # --agent → never prompt
        (CliState(None, agent=False), "json", True, False),  # machine output → no prompt
        (None, "text", True, True),                          # no state = not agent
    ],
)
def test_can_prompt_gate(monkeypatch, state, fmt, tty, expected) -> None:
    monkeypatch.setattr(interact, "is_tty", lambda: tty)
    assert can_prompt(state, fmt) is expected


# --- A scripted prompter, injected at the single seam -----------------------------


class FakePrompter:
    """Returns scripted answers per prompt type; no terminal, no questionary."""

    def __init__(self, *, selects=(), texts=(), confirms=(), checkboxes=(), paths=()) -> None:
        self._selects = iter(selects)
        self._texts = iter(texts)
        self._confirms = iter(confirms)
        self._checkboxes = iter(checkboxes)
        self._paths = iter(paths)

    def select(self, message, choices, **_k):
        return next(self._selects)

    def text(self, message, **_k):
        return next(self._texts)

    def confirm(self, message, **_k):
        return next(self._confirms)

    def checkbox(self, message, choices, **_k):
        return next(self._checkboxes)

    def path(self, message, **_k):
        return Path(next(self._paths))


def _inject(monkeypatch, fake: FakePrompter) -> None:
    """Force the wizard branch everywhere by returning ``fake`` from the one seam."""
    monkeypatch.setattr(interact, "make_prompter", lambda state, fmt: fake)


# --- Layer 2: the init wizard end to end ------------------------------------------


@pytest.mark.e2e
def test_init_wizard_scaffolds_and_wires(tmp_path: Path, monkeypatch) -> None:
    """Bare `khub init` on a TTY: select preset, prompt path, confirm wire, pick agent, decline skill."""
    ws = tmp_path / "hub"
    fake = FakePrompter(
        selects=["firm-ops"],           # ? Preset
        paths=[str(ws)],                # ? Directory
        confirms=[True, False],         # wire? yes ; skill? no (no npx touched)
        checkboxes=[["claude-code"]],   # ? which agents → CLAUDE.md
    )
    _inject(monkeypatch, fake)

    result = runner.invoke(app, ["init"])  # no preset, no path → wizard fills both
    assert result.exit_code == 0, result.output
    assert (ws / ".khub" / "schema.yaml").exists()   # scaffolded from the picked preset
    assert (ws / "CLAUDE.md").exists()               # wire confirmed, claude-code picked
    assert "installed khub agent skill" not in result.output  # skill declined


@pytest.mark.integration
def test_init_wizard_agent_multiselect_passes_through(
    tmp_path: Path, preset_source: Path, monkeypatch
) -> None:
    """Skill confirmed + an agent picked → npx runs with `--agent <picked>`."""
    calls: list[list[str]] = []

    def fake_run(cmd, cwd=None, capture_output=False, **_k):
        calls.append(cmd)
        return types.SimpleNamespace(returncode=0)

    monkeypatch.setattr("khub.core.skill.shutil.which", lambda _: "/opt/npx")
    monkeypatch.setattr("khub.core.skill.subprocess.run", fake_run)

    ws = tmp_path / "hub"
    fake = FakePrompter(
        paths=[str(ws)],            # ? Directory (preset supplied as an arg below)
        confirms=[False, True],     # wire? no ; skill? yes
        checkboxes=[["cursor"]],    # ? which agents
    )
    _inject(monkeypatch, fake)

    result = runner.invoke(app, ["init", "note", "--preset-source", str(preset_source)])
    assert result.exit_code == 0, result.output
    assert calls, "npx skills should have run"
    assert "--agent" in calls[0] and "cursor" in calls[0]


@pytest.mark.integration
def test_init_missing_preset_headless_errors(tmp_path: Path) -> None:
    """Headless (non-TTY runner) with no preset: no prompt, a clean error, no scaffold."""
    result = runner.invoke(app, ["-C", str(tmp_path), "init"])
    assert result.exit_code == 2
    assert "PRESET" in result.output
    assert not (tmp_path / ".khub").exists()


# --- Layer 2: schema-driven add / edit wizards ------------------------------------


class ScriptedPrompter:
    """Answers by matching a substring of the prompt message — robust to field order.

    ``select``/``text``/``checkbox`` consult their own maps (so a value that happens to
    appear in a later confirm message never leaks across methods); ``confirm`` returns a
    fixed default.
    """

    def __init__(self, *, select=None, text=None, checkbox=None, confirm=True) -> None:
        self._select = select or {}
        self._text = text or {}
        self._checkbox = checkbox or {}
        self._confirm = confirm

    def _match(self, table, message, default):
        for key, value in table.items():
            if key in message:
                return value
        return default

    def select(self, message, choices, **_k):
        return self._match(self._select, message, choices[0])

    def text(self, message, **_k):
        return self._match(self._text, message, "")

    def checkbox(self, message, choices, **_k):
        return self._match(self._checkbox, message, [])

    def confirm(self, message, **_k):
        return self._confirm

    def path(self, message, **_k):
        return Path(".")


@pytest.mark.e2e
def test_add_wizard_picks_relations(fresh_ws: Path, monkeypatch) -> None:
    """Bare-ish `khub add project` on a TTY walks the schema and picks client/owner."""
    monkeypatch.chdir(fresh_ws)
    runner.invoke(app, ["add", "client", "--name", "Acme"])
    runner.invoke(app, ["add", "person", "--name", "Dana", "--role", "partner"])

    fake = ScriptedPrompter(select={"client": "client/acme", "owner": "person/dana"}, confirm=True)
    monkeypatch.setattr(interact, "make_prompter", lambda s, f: fake)

    result = runner.invoke(app, ["add", "project", "--id", "acme-hub"])
    assert result.exit_code == 0, result.output

    got = runner.invoke(app, ["get", "project/acme-hub", "--format", "json"])
    frontmatter = json.loads(got.output)["frontmatter"]
    assert "client" in frontmatter and "owner" in frontmatter  # both relations set by the wizard


@pytest.mark.e2e
def test_add_wizard_declined_aborts(fresh_ws: Path, monkeypatch) -> None:
    """Declining the create confirm aborts with no file written."""
    monkeypatch.chdir(fresh_ws)
    fake = ScriptedPrompter(confirm=False)
    monkeypatch.setattr(interact, "make_prompter", lambda s, f: fake)
    result = runner.invoke(app, ["add", "client", "--id", "ghost"])
    assert result.exit_code != 0  # aborted
    assert not (fresh_ws / "clients" / "ghost.md").exists()


@pytest.mark.e2e
def test_edit_wizard_picks_entity_field_value(fresh_ws: Path, monkeypatch) -> None:
    """Bare `khub edit` picks the entity, the field, then the enum value."""
    monkeypatch.chdir(fresh_ws)
    runner.invoke(app, ["add", "person", "--name", "Dana", "--role", "consultant"])

    fake = ScriptedPrompter(
        select={"Entity": "person/dana", "Field to edit": "role", "role (": "partner"},
        confirm=True,
    )
    monkeypatch.setattr(interact, "make_prompter", lambda s, f: fake)

    result = runner.invoke(app, ["edit"])
    assert result.exit_code == 0, result.output
    got = runner.invoke(app, ["get", "person/dana", "--format", "json"])
    assert json.loads(got.output)["frontmatter"]["role"] == "partner"


# --- Layer 2: entity pickers (link / get / remove) --------------------------------


@pytest.mark.e2e
def test_link_wizard_picks_edge(fresh_ws: Path, monkeypatch) -> None:
    """Bare `khub link` picks the entity, a predicate from its type, then a target."""
    monkeypatch.chdir(fresh_ws)
    runner.invoke(app, ["add", "client", "--name", "Acme"])
    runner.invoke(app, ["add", "project", "--id", "hub", "--title", "Hub"])

    fake = ScriptedPrompter(
        select={"Entity": "project/hub", "Predicate": "client", "Target": "client/acme"}
    )
    monkeypatch.setattr(interact, "make_prompter", lambda s, f: fake)

    result = runner.invoke(app, ["link"])
    assert result.exit_code == 0, result.output
    assert "Linked" in result.output
    got = runner.invoke(app, ["get", "project/hub", "--format", "json"])
    assert "client" in json.loads(got.output)["frontmatter"]


@pytest.mark.e2e
def test_get_wizard_picks_entity(fresh_ws: Path, monkeypatch) -> None:
    """Bare `khub get` picks the entity to read (non-TTY runner still emits JSON)."""
    monkeypatch.chdir(fresh_ws)
    runner.invoke(app, ["add", "client", "--name", "Acme"])
    fake = ScriptedPrompter(select={"Entity": "client/acme"})
    monkeypatch.setattr(interact, "make_prompter", lambda s, f: fake)

    result = runner.invoke(app, ["get"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.output)["id"] == "client/acme"


@pytest.mark.e2e
def test_remove_wizard_declined_keeps_entity(fresh_ws: Path, monkeypatch) -> None:
    """Bare `khub remove` picks the entity and confirms; declining keeps the file."""
    monkeypatch.chdir(fresh_ws)
    runner.invoke(app, ["add", "client", "--name", "Acme"])
    fake = ScriptedPrompter(select={"Entity": "client/acme"}, confirm=False)
    monkeypatch.setattr(interact, "make_prompter", lambda s, f: fake)

    result = runner.invoke(app, ["remove"])
    assert result.exit_code != 0  # declined confirm aborts
    assert (fresh_ws / "clients" / "acme.md").exists()


# --- Layer 3: the questionary adapter is wired --------------------------------------


@pytest.mark.unit
def test_questionary_adapter_wired() -> None:
    """The lazy import resolves to questionary and a Prompter builds — proves the wiring
    without driving prompt_toolkit (the flows are covered above via the fakes)."""
    assert interact._q().__name__ == "questionary"
    assert isinstance(interact.Prompter(), interact.Prompter)
