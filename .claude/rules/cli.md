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

- `WantJSON = --format json || !IsTTY()` — agents get machine output without
  knowing the flag exists. Do not narrow it.
- `IsTTY()` precedence is `TTY_COMPATIBLE` → `FORCE_COLOR` → real isatty.
  Never add a fourth signal — the whole fixture suite drives this gate.
- EPIPE is not a failure (`khub schema | head`); `Guard` passes it through and
  `main.go` disables the SIGPIPE kill so the write error is visible at all.
- Exit codes: 0 success · 1 a gate failed (`validate`/`check`) · 2 a refusal
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

Keep right: `query` returns minimal records (`id,type,slug,title,draft,orphan,stale`)
with `get` as the full fetch; `validate` vs `check` stay distinct; capture is
never blocked. When adding surface: verbosity costs agent *accuracy*, not just
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

A read-only loopback view of the graph for a human. Its exposure is inbound —
anything already running on the machine can reach it — and that is what the
guards answer.

- **TTY gate.** `serve` refuses a non-terminal stdout (`serve_needs_tty`) so an
  agent that invokes it by accident gets an immediate refusal, not a hang.
  `TTY_COMPATIBLE=1` is the documented escape.
- **The server rebuilds from the live tree on every request; the page fetches
  once on load.** No polling, no event stream; do not add either without a
  reason better than symmetry.
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
  can answer for a file `/api/graph` has no node for. The UI only links from
  graph nodes; a new caller must not assume otherwise.
