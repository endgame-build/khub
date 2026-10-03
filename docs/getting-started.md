# Getting started with khub

This walkthrough builds a tiny firm-ops hub from nothing (one client, one person, one project), then queries it, walks its graph, and gates it. Everything lands in git as Markdown; the schema checks every write. By the end you have seen the two rules that matter most: a relation to a missing target is rejected on the spot, while a missing field is captured anyway and flagged later.

## Install

khub is a single static binary for macOS and Linux (amd64, arm64),
distributed through npm as `@endgame-build/khub` — one package carrying a
prebuilt binary per platform, with a launcher that picks the matching one.
Install it as a pinned dev dependency of the repo you are working in, so
everyone (and every agent) touching that repo runs the same khub:

```bash
npm install -D @endgame-build/khub
npx @endgame-build/khub --version
```

For a machine-global install instead, `npm install -g @endgame-build/khub`,
optionally pinned with `@X.Y.Z`. The package has no install scripts; the
launcher picks the binary out of the package at run time, so
`--ignore-scripts` changes nothing.

Or build from a checkout, which needs read access to the repository:

```bash
git clone https://github.com/endgame-build/khub.git
cd khub
go build -o khub ./cmd/khub
./khub --help    # prefix every command below with `./`
```

The rest of this guide writes `khub …`. With the per-repo install read that as `npx @endgame-build/khub …`; with a global install, as `khub …`; if you built from source, as `./khub …`. The output is the same.

This walkthrough passes every value as a flag, which is the only way khub takes input: it never prompts, so a missing argument is a usage error rather than a question. That is also exactly how an agent drives it — pipe the output or add `--format json` to get machine-readable records.

To hand the setup to an agent instead, install the `setup` skill with `npx skills add endgame-build/khub -s setup` and ask it to set khub up; see [Agent skills](../README.md#agent-skills) for the Claude Code plugin and the other agents.

## Seed a workspace

`init` scaffolds a workspace from a preset — a directory holding the preset's three schema layer files (`ontology.yaml`, `policy.yaml`, `storage.yaml`) and optional body `templates/`. The `firm-ops` preset is the operating graph of a consulting and delivery firm: nine entity types (client, project, person, opportunity, meeting, and more) with typed relations between them. For software work there is `build-hub`: eleven types (capability, actor, use-case, requirement, adr, system, component, api, repo, and the `prd` and `arc42` narratives); `build-lite` is an alias that resolves to it. A preset with templates also gets them flattened to `.khub/templates/`, and every md `layout: singleton` type with a template is created on the spot (the `build-hub` preset seeds `prd.md` and `arc42.md` this way — creations only, an existing file is never touched).

```bash
khub init firm-ops ./my-hub
```

```
Initialized firm-ops workspace at my-hub
created CLAUDE.md
created AGENTS.md
index.md created

Agent skill not installed. To install:
  cd my-hub && npx skills add endgame-build/khub
In Claude Code: /plugin marketplace add endgame-build/khub, then /plugin install khub@khub
```

```bash
cd my-hub
```

`init` does three things: it scaffolds the tree, wires the schema into your agent files (`CLAUDE.md` and `AGENTS.md`; next section), and writes the first `index.md` — the one-file view of the corpus an agent reads before anything else. Pass `--no-wire` to skip the wire tail. Installing the agent skills is a separate step — scaffolding a workspace and populating your agent directories are different decisions, so `init` names the commands rather than running them.

init wrote a `.khub/` control directory and one folder per entity type:

```
my-hub/
├── .khub/
│   ├── config.yaml            # workspace name, preset, stale_days
│   ├── ontology.yaml          # the domain: types, attributes, relations, capture cues
│   ├── policy.yaml            # workspace gates: required singletons, orphan exemptions
│   └── storage.yaml           # layouts, inventory paths, id prefixes, template links
├── clients/
├── identity/team/             # person entities live here
├── projects/
├── opportunities/  partnerships/  meetings/  transcripts/  fragments/  case-studies/
```

`ontology.yaml` is the operational setup. It declares what a client, a person, and a project *are*: their fields, which are required, and the legal relations between types — `policy.yaml` and `storage.yaml` carry this workspace's gates and file layout beside it, and the base block every type inherits ships inside the khub binary (`khub schema base` prints it). Extend the files as the work demands; for now the preset defaults are enough.

## Wire it into your agent's context

`init` already wrote a managed block to `my-hub/CLAUDE.md` and `my-hub/AGENTS.md`. In `CLAUDE.md` the block imports the schema (`@.khub/ontology.yaml` and its two siblings, a Claude Code directive); in `AGENTS.md` (the cross-agent standard, which has no import) it points at the schema files to read. Both list the command surface, so any agent working in this repo loads the ontology into context before it runs a single khub command. When the schema changes, re-run the wiring in place:

```bash
khub wire
```

```
updated CLAUDE.md
updated AGENTS.md
```

Bare `khub wire` updates whichever agent files already exist. `khub wire --target claude|agents|both` creates a specific one (`CLAUDE.md` gets the import, `AGENTS.md` the pointer).

Now install khub's agent skills. They ship apart from the binary.

In Claude Code, install the plugin. It carries both skills (`khub` and `setup`) and a session mod; see [the plugin guide](plugin.md):

```text
/plugin marketplace add endgame-build/khub
/plugin install khub@khub
```

For opencode, Cursor, Codex and the rest, [`npx skills`](https://skills.sh) copies the two skills into each agent's skill folder (Node and repo access required):

```bash
npx skills add endgame-build/khub
```

After upgrading khub, run `khub upgrade --dry-run`, inspect the candidate outcomes, then run `khub upgrade`. The preview creates no workspace artifacts. The real run refreshes `.khub/` from the preset (an edited file is kept in `<name>.bak`), validates and publishes the core changes transactionally, then removes the skill copies earlier khub versions installed into the workspace, re-wires agent files, and rebuilds `index.md`.

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
