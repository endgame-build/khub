# Getting started with khub

This walkthrough builds a tiny firm-ops hub from nothing (one client, one person, one project), then queries it, walks its graph, and gates it. Everything lands in git as Markdown; the schema checks every write. By the end you have seen the two rules that matter most: a relation to a missing target is rejected on the spot, while a missing field is captured anyway and flagged later.

## Install

khub is a `khub` console script and needs Python 3.11+. Two ways in.

Install it as a tool (the repo is private, so this needs SSH access to the org; HTTPS with a token works too):

```bash
uv tool install git+ssh://git@github.com/endgame-build/khub@v0.8.0
```

Or clone and run from the checkout:

```bash
git clone git@github.com:endgame-build/khub.git
cd khub
uv sync
uv run khub --help    # prefix every command below with `uv run`
```

The rest of this guide writes `khub …`. If you cloned, read that as `uv run khub …`; the output is the same.

This walkthrough passes every value as a flag, which is the only way khub takes input: it never prompts, so a missing argument is a usage error rather than a question. That is also exactly how an agent drives it — pipe the output or add `--format json` to get machine-readable records.

From inside Claude Code, `/plugin install khub@khub` then `/khub:setup` installs and wires khub for you; see the [README](../README.md#use-it-from-claude-code).

## Seed a workspace

`init` scaffolds a workspace from a preset — a directory holding the preset's `schema.yaml` and optional body `templates/`. The `firm-ops` preset is the HQ operations ontology: nine entity types (client, project, person, opportunity, meeting, and more) with typed relations between them. A preset with templates also gets them flattened to `.khub/templates/`, and every md `layout: singleton` type with a template is created on the spot (the `build-hub` preset seeds `prd.md`, `roadmap.md`, `glossary.md`, `arc42.md`, `erd.md` this way — creations only, an existing file is never touched).

```bash
khub init firm-ops ./my-hub
```

```
Initialized firm-ops workspace at my-hub
created CLAUDE.md
created AGENTS.md

Agent skill not installed. To install:
  khub install-skills
```

```bash
cd my-hub
```

`init` does two things: it scaffolds the tree and wires the schema into your agent files (`CLAUDE.md` and `AGENTS.md`; next section). Pass `--no-wire` to skip the wire tail. Installing the agent skill is a separate step, `khub install-skills` — it needs `npx` and SSH access to the skill repo, and a scaffold should not depend on either.

init wrote a `.khub/` control directory and one folder per entity type:

```
my-hub/
├── .khub/
│   ├── config.yaml            # workspace name, preset, stale_days
│   └── schema.yaml            # the effective schema: the contract every write is checked against
├── clients/
├── identity/team/             # person entities live here
├── projects/
├── opportunities/  partnerships/  meetings/  transcripts/  fragments/  case-studies/
```

`schema.yaml` is the operational setup. It declares what a client, a person, and a project *are*: their fields, which are required, and the legal relations between types. Extend it as the work demands; for now the preset defaults are enough.

## Wire it into your agent's context

`init` already wrote a managed block to `my-hub/CLAUDE.md` and `my-hub/AGENTS.md`. In `CLAUDE.md` the block imports the schema (`@.khub/schema.yaml`, a Claude Code directive); in `AGENTS.md` (the cross-agent standard, which has no import) it points at the schema file to read. Both list the command surface, so any agent working in this repo loads the ontology into context before it runs a single khub command. When the schema changes, re-run the wiring in place:

```bash
khub wire
```

```
updated CLAUDE.md
updated AGENTS.md
```

Bare `khub wire` updates whichever agent files already exist. `khub wire --target claude|agents|both` creates a specific one (`CLAUDE.md` gets the import, `AGENTS.md` the pointer).

Now install the agent skill (Cursor, Codex, and others land in `.agents/skills/`; Claude Code in `.claude/skills/`):

```bash
khub install-skills
```

Add `--agent claude-code` (repeatable) to target specific agents instead of letting npx auto-detect, or `--dry-run` to see the command it would run. Without khub on PATH, the same install is [`npx skills`](https://skills.sh) directly:

```bash
npx skills add git@github.com:endgame-build/khub.git -s khub -s setup
```

Either way writes a `skills-lock.json` at the project root. Commit it so teammates install the same skill version. The installed skill directories are per-machine, and `khub install-skills` adds `.claude/skills/` and `.agents/skills/` to `.gitignore` for you; after a bare `npx skills add` you add those lines yourself.

## Author your first entities

Each `add` mints a slug from `--name` or `--title` ("Acme Corp" becomes `acme-corp`), writes a well-formed Markdown file, and prints its id. A client needs a name; a person needs a name and a role from the set `consultant | engineer | manager | partner`.

```bash
khub add client --name "Acme Corp" --industry manufacturing
```

```
clients/acme-corp.md
Created client 'acme-corp' (active)
```

```bash
khub add person --name "Dana Lee" --role partner
```

```
identity/team/dana-lee.md
Created person 'dana-lee' (active)
```

A project requires two relations: `client` points at a client, `owner` points at a person. Set them inline with `--client` and `--owner`, naming the target slugs. khub resolves them against what already exists.

```bash
khub add project --title "Acme Diagnostic" --client acme-corp --owner dana-lee
```

```
projects/acme-diagnostic/_index.md
Created project 'acme-diagnostic' (active)
```

Read the project back as an agent-facing record. Every JSON entity carries a qualified `id` of `type/slug`, its on-disk `path`, and its frontmatter:

```bash
khub get acme-diagnostic --format json
```

```
{"id": "project/acme-diagnostic", "type": "project", "slug": "acme-diagnostic", "path": "projects/acme-diagnostic/_index.md", "frontmatter": {"type": "project", "created": "2026-07-08", "updated": "2026-07-08", "draft": false, "title": "Acme Diagnostic", "client": "acme-corp", "owner": "dana-lee"}, "body": ""}
```

That JSON is a projection. The file on disk is the truth, plain Markdown with YAML frontmatter, ready to commit:

```bash
cat projects/acme-diagnostic/_index.md
```

```
---
type: project
created: 2026-07-08
updated: 2026-07-08
draft: false
title: Acme Diagnostic
client: acme-corp
owner: dana-lee
---
```

## Two behaviors worth seeing

### A broken relation fails the write

Point the project's owner at a person who does not exist. khub checks the target before it touches a file, rejects the write, and exits non-zero. Nothing lands.

```bash
khub add project --title "Broken Project" --client acme-corp --owner nobody
```

```
No person 'nobody' to satisfy relation 'owner'
```

The graph never carries a dangling edge, because a dangling edge never gets written.

### A missing field never blocks capture

Add a person without a `--role`, even though the schema marks role required. The write still succeeds and the entity is active:

```bash
khub add person --name "Sam Rivera"
```

```
identity/team/sam-rivera.md
Created person 'sam-rivera' (active)
```

Per-entity `validate` passes; the record is well-formed as far as it goes:

```bash
khub validate person/sam-rivera
```

```
{"count": 1, "errors": [], "fixed": []}
```

The gap surfaces at graph level. `check` reads required-completeness across the active graph and names the hole:

```bash
khub check
```

```
active-but-incomplete person/sam-rivera: missing role
orphan person/sam-rivera
```

Capture first, complete later. You record what you know now; the gate tells you what is still owed.

## Query and walk the graph

Filter entities by type. Read commands print a Rich table on a terminal and JSON when piped; add `--format json` to force the record shape.

```bash
khub query --type project
```

```
                            query
┏━━━━━━━━━━━━━━━━━━━━━━━━━┳━━━━━━━━━┳━━━━━━━┳━━━━━━━━┳━━━━━━━┓
┃ id                      ┃ type    ┃ draft ┃ orphan ┃ stale ┃
┡━━━━━━━━━━━━━━━━━━━━━━━━━╇━━━━━━━━━╇━━━━━━━╇━━━━━━━━╇━━━━━━━┩
│ project/acme-diagnostic │ project │ False │ False  │ False │
└─────────────────────────┴─────────┴───────┴────────┴───────┘
```

Walk one hop out from the project to see its edges, the client and the owner you set inline:

```bash
khub neighbors acme-diagnostic
```

```
                     neighbors
┏━━━━━━━━━━━━━━━━━━┳━━━━━━━━━━━┳━━━━━━━━━━━┳━━━━━━━┓
┃ id               ┃ predicate ┃ direction ┃ depth ┃
┡━━━━━━━━━━━━━━━━━━╇━━━━━━━━━━━╇━━━━━━━━━━━╇━━━━━━━┩
│ client/acme-corp │ client    │ out       │ 1     │
│ person/dana-lee  │ owner     │ out       │ 1     │
└──────────────────┴───────────┴───────────┴───────┘
```

`status` counts the whole workspace at a glance, per type, draft versus active, and the orphan Sam still trips:

```bash
khub status
```

```
          status
┏━━━━━━━━━━━━━━━━┳━━━━━━━┓
┃ type           ┃ count ┃
┡━━━━━━━━━━━━━━━━╇━━━━━━━┩
│ opportunity    │     0 │
│ project        │     1 │
│ meeting        │     0 │
│ transcript     │     0 │
│ fragment       │     0 │
│ case-study     │     0 │
│ partnership    │     0 │
│ person         │     2 │
│ client         │     1 │
├────────────────┼───────┤
│ draft / active │ 0 / 4 │
│ orphan         │     1 │
│ stale          │     0 │
│ stray          │     0 │
│ malformed      │     0 │
│ OKF-conformant │   yes │
└────────────────┴───────┘
```

## Gate it

Close the one gap. Give Sam a role, then run the gate again:

```bash
khub edit person/sam-rivera role consultant
khub check
```

```
orphan person/sam-rivera (informational)
Graph check passed
```

`check` passes. Sam is still an orphan (zero relations), but an orphan is informational: a valid entity that no edge touches is allowed (a dormant contact, a note not yet linked). It fails the gate only under `--strict`.

That is the division of labor. `validate` judges one entity's well-formedness; `check` judges the whole graph: relations resolve, required fields are filled across the active set, no strays, no cycles. Run `validate` as you write and `check` before you commit.

## Next steps

- [`cli.md`](cli.md) — the full command reference and JSON contracts.
- [`concepts.md`](concepts.md) — the mental model: entities, the schema, the graph projection.
- [`schema.md`](schema.md) — extend the schema with your own types and relations.
- [`firm-ops-preset.md`](firm-ops-preset.md) — every type, field, and relation in the preset you just used.
