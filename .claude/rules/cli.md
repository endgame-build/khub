# The CLI layer — thin adapter, agent-first

`internal/cli` is layer 5: zero per-type code, schema-introspecting. The
primary consumer is **an AI agent**. That design position decides these rules.

## One output gate

Every command routes through `internal/cli/render.go`. Never print outside an
`Emit` render closure — the `fmt.Print*` calls in command files all sit inside
the human-render `func()` handed to `Emit`. `Emit` / `Fail` / `Guard` are the
only exits. An advisory line a caller must see whatever the format (search's
dropped-rows and thin-result note) goes through `Note`, to stderr, before
`Emit`.

The one exception is cobra's hidden `__complete`, which the scripts from
`khub completion` call at TAB time: a fixed text protocol (candidates, then
`:<directive>`) written by cobra outside `Emit`, with no `--format`, reading no
workspace. Its completers live in `completion.go` and stay workspace-free.

- `WantJSON = --format json || !IsTTY()` — agents get machine output without
  knowing the flag exists. Do not narrow it.
- `IsTTY()` precedence is `TTY_COMPATIBLE` → `FORCE_COLOR` → real isatty.
  Never add a fourth signal — the whole fixture suite drives this gate.
  On Windows the third signal also recognises a Cygwin/MSYS2 pty (mintty,
  the standalone Git Bash window), which reaches khub as a named pipe; that
  check is the real isatty read the only way such a pty allows, not a fourth
  signal.
- Every path khub prints goes through `filepath.ToSlash`, so Windows prints
  what darwin does. The Windows parity leg is the check.
- EPIPE is not a failure (`khub schema | head`); `Guard` passes it through and
  `main.go` disables the SIGPIPE kill so the write error is visible at all.
- Exit codes: 0 success · 1 a gate failed (`validate`/`check`/`reindex --check`/`doctor`) · 2 a refusal
  or a usage error — every `Located` failure, envelope unchanged. The split is
  what an agent does next: on 2 the call was wrong and nothing was written,
  correct it; on 1 the workspace is wrong, fix it. New codes only with a
  stated answer to "what does an agent do differently?"

## khub never prompts

Every input is a flag; a missing one is exit 2. An interactive prompt hangs an
agent forever. Never introduce a prompt, pager, or stdin-blocking confirmation.

## Pinned rendering — do not clean up

- `help.go` (Rich-style help emulation) stays: cobra's `text/template` help is
  a *different* frozen output that cannot express its column-width algorithm.
- `parseGlobals` stays: cobra has no eager-option hook, so the short-circuiting
  `--version` and root-only options need the hand parse.
- Help output is gated on `IsTTY()`: rich terminal help stays byte-pinned,
  piped help is plain text with the same section order. Error prose, tables
  and trees keep their own pinned bytes.

## Output for agents

Keep right: `query` returns minimal records
(`id,type,slug,title,draft,orphan,stale,trust`) with `get` as the full fetch;
`trust` is derived on read and rides last, and no lifecycle `status` joins the
rows (`draft` is derived from it); `validate` vs `check` stay distinct; capture
is never blocked. When adding surface: verbosity costs agent *accuracy*, not just
tokens — prefer stable fields and codes over prose; errors an agent can hit on
a write should name the corrective call (as `TargetNotEmpty` does).

## No MCP server

The CLI plus `SKILL.md` is the agent surface. An MCP server would be a second
contract the parity suite does not cover, with a standing per-turn schema cost
the CLI never charges. If one is ever built: generated from the resolved
schema (zero per-type code), emitting `outputSchema` + `structuredContent`.

## Untrusted content drives writes

Entity bodies are attacker-influenceable text; `get`/`search` output is
untrusted by construction. khub holds private data plus untrusted content and
**sends nothing outward** — keep it that way (`viz --open` and any future
`export` are the lines to think before crossing). Containment for
injected-content writes is provenance + draft, with `check` as the human
review point.

## `khub serve`

A read-only loopback page for a human reviewer: what needs attention, one type's
rows, one entity, the ontology diagram. Its exposure is inbound — anything
already running on the machine can reach it — and that is what the guards
answer.

- **TTY gate.** `serve` refuses a non-terminal stdout (`serve_needs_tty`) so an
  agent that invokes it by accident gets an immediate refusal, not a hang.
  `TTY_COMPATIBLE=1` is the documented escape.
- **The server rebuilds from the live tree on every request; the page fetches
  when a view loads, when the window comes back to the front, and on `r`.**
  Views are routed by the URL fragment; a sort or filter change re-renders from
  the data in hand. A refresh compares the reading with the one on screen
  (`same` in `ui/widgets.js`) and leaves an unchanged view alone, so its DOM,
  scroll and focus stay. No polling, no event stream; do not add either without
  a reason better than symmetry.
- **Every fact on the page comes from a core package.** Totals, coverage,
  findings, rows, counts, markers, history and the body tree are
  `internal/explore`, built on the functions `query`, `check`, `get`, `recent`,
  `search`, `run` and `neighbors` call, each held to them by a test; `serve` routes
  and encodes. What `check` fails on that only the command tree knows
  (`flag_shadowed`) is handed to `serve` as `Shadows`. A marker the page judged
  itself would be a second definition. The page arranges what it is given: it sums
  types, picks the largest gap, and counts how many of the rows in hand a facet's
  value keeps (`ui/listmodel.js`), since the filters are the page's own.
- **A file that does not parse does not stop the page.** `explore.load` keeps
  going and every payload read from a scan carries `malformed`, which the page
  shows as a banner (`/api/queries` carries none; the home's overview does).
  `viz`, `reindex` and `export` still refuse.
- **The overview's picture is computed, never simulated.** `ui/layout.js`
  places every box, tag, relation label and group frame with dagre (vendored
  in `internal/assets`) from text-derived sizes; `cy.js` draws those positions
  with no layout run and no dragging. One type and its neighbors (`?focus=`) is
  the same layout over a smaller graph. `tests/layout.test.mjs` holds the
  no-overlap property over the presets and 300 generated schemas, whole and
  focused — keep a new diagram feature inside that model (a node the layout
  places) rather than adding force-directed physics or renderer-placed
  decorations such as loops.
- **What the page remembers lives with the history entry.** `app.js` stamps
  each entry in `history.state` and keeps its scroll, its drawn rows, the
  diagram's viewport and the list an entity page was opened from in memory under
  that stamp, so back and forward restore a
  place and a new visit starts at the top. Two memories sit beside it: each
  type's last list address, which an entity's breadcrumb returns to, and the
  reviewer's name, the only thing in local storage (`ui/reviewer.js`). What a
  view shows stays in the fragment; a place is never in the address.
- **Text a reader pastes into a terminal goes through `ui/shell.js`.** An id is
  a file's name and can hold anything a shell would run. Quoting does not stop a
  control character, which the terminal acts on first, so such a value has no
  word and its copy button is off.
- **A path handed to git is a name, and git fetches nothing.**
  `gitlog.FileVersions` passes `--literal-pathspecs`, overrules `log.follow` and
  sets `GIT_NO_LAZY_FETCH`; a side git cannot show is unknown, never absent.
- **Bodies reach the page as a block tree, never as markup.** `explore`
  parses Markdown (goldmark, confined there by depguard) into a dozen node
  kinds the page builds through `dom.js`; raw HTML and comments arrive as text
  (an agent reading the entity sees a comment, so the reviewer must) and only
  `http`/`https`/`mailto` hrefs survive. That is what keeps
  `TestUIHasNoMarkupSinks` satisfiable — do not add a renderer that needs a
  sink.
- **Read-only is structural.** Every method but GET and HEAD is rejected
  before routing. A write endpoint would make three things mandatory: a
  per-run token in the URL, an `Origin` check, and harness coverage of the
  HTTP contract.
- **Host-header validation is the load-bearing guard**, not the loopback bind:
  DNS rebinding defeats the bind and same-origin, but the request still
  announces the attacker's hostname. `serve.AllowedHost` is the whole defence;
  its test is the one not to delete. Never add a CORS header.
- **Coverage split.** The parity harness pins the CLI contract (the TTY
  refusal, help, arity); the HTTP contract is `httptest` in `internal/serve/`.
  The port refusal is not fixture-reachable — the TTY gate runs before
  `Listen` — and `TestListenRefusesBusyPort` covers it.
- **A serve step in a `mode: human` or `mode: pty` parity case hangs the suite
  forever**: those modes do not set `TTY_COMPATIBLE=0`. Keep such cases in the
  default `mode: pipe`.
- `entity.Get` builds its index without the stray filter, so `/api/entity/{id}`
  can answer for a file `/api/rows` has no node for (it then carries no markers
  and no neighbours). The UI links only to entities of the filtered
  index; a new caller must not assume otherwise.
