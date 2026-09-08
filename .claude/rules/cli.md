# The CLI layer — thin adapter, agent-first

`internal/cli` is layer 5: zero per-type code, schema-introspecting. The
primary consumer is **an AI agent**. That design position decides these rules.

## One output gate

Every command routes through `internal/cli/render.go`. Never print from a
command body. `Emit` / `Fail` / `Guard` are the only exits.

- `WantJSON = --format json || !IsTTY()` — agents get machine output without
  knowing the flag exists. The single highest-leverage decision in the CLI.
  Do not narrow it.
- `IsTTY()` precedence is `TTY_COMPATIBLE` → `FORCE_COLOR` → real isatty.
  Never add a fourth signal — the whole fixture suite drives this gate.
- EPIPE is not a failure (`khub schema | head`); `Guard` passes it through and
  `main.go` disables the SIGPIPE kill so the write error is visible at all.
- Exit codes: 0 success · 1 a gate failed (`validate`/`check`) · 2 a refusal
  or a usage error — every `Located` failure, envelope unchanged. Three-way,
  pinned. The split is what an agent does next: on 2 the call was wrong and
  nothing was written, correct it; on 1 the workspace is wrong, fix it. New
  codes only with a stated answer to "what does an agent do differently?"

## khub never prompts

The wizard was removed in 0.9.0; every input is a flag, missing → exit 2. An
interactive prompt hangs an agent forever. Never reintroduce a prompt, pager,
or stdin-blocking confirmation.

## Load-bearing "legacy" — do not clean up

- `help.go` (853 lines of Rich emulation) stays: the alternative is cobra's
  `text/template` help, a *different* frozen output that cannot express Rich's
  column-width algorithm. `charmbracelet/fang` was evaluated — experimental,
  overwrites `SetHelpFunc` wholesale.
- `parseGlobals` stays: cobra has no eager-option hook, so Click's
  short-circuiting `--version` + root-only options need the hand parse.
- One sanctioned change exists: gate **help output only** on `IsTTY()` (it is
  currently the sole output path that never checks it, so piped help is 34–58%
  box-drawing chrome). That is a deliberate help-fixture re-record; error
  prose, tables, trees stay byte-pinned.

## Output for agents

Keep right: `query` returns minimal records (`id,type,slug,title,draft,orphan,stale`)
with `get` as the full fetch; `validate` vs `check` stay distinct; capture is
never blocked. When adding surface: verbosity costs agent *accuracy*, not just
tokens — prefer stable fields and codes over prose; errors an agent can hit on
a write should name the corrective call (as `TargetNotEmpty` does).

## No MCP server

Evaluated, rejected. ~26 verbs × 550–1,400 tok of schema = 14K–36K standing
tokens per turn vs ~900 for SKILL.md, plus a second contract the parity suite
doesn't cover, plus giving up the static-binary supply-chain position. Revisit
only if khub goes multi-tenant/remote. If ever built: generated from the
resolved schema (zero per-type code), emitting `outputSchema` +
`structuredContent`, not just inputs.

## Untrusted content drives writes

Entity bodies are attacker-influenceable text; `get`/`search` output is
untrusted by construction. khub holds private data + untrusted content and
**sends nothing outward** — keep it that way (`viz --open` and any future
`export` are the lines to think before crossing). Containment for
injected-content writes is provenance + draft, with `check` as the human
review point.

`khub serve` is not a crossing of that line and should not be argued as one: a
listener bound to `127.0.0.1` transmits to nobody. Its real exposure is the
reverse direction — anything already running on the machine can reach it — and
that is what `serve`'s guards answer. See below.

## `khub serve` — why a second contract was accepted here

The MCP verdict above rejects "a second contract the parity suite doesn't
cover." `serve` is exactly that, so the distinction has to be written down or
the two sections read as a contradiction and someone will eventually delete
the wrong one.

MCP's cost was paid by **agents, on every turn**: 14K–36K standing tokens for a
surface the CLI already gave them. `serve`'s standing cost to an agent is one
row in root help — about 20 tokens — and nothing per call, because an agent has
no reason to invoke it and the TTY gate turns an accidental invocation into an
immediate refusal rather than a hang. That gate is a guard, not a wall:
`TTY_COMPATIBLE=1` is a documented escape, so "an agent cannot run it" would be
false. What it buys is a human surface with no CLI equivalent: a graph you can
look at, and navigate, without regenerating a file. Different consumer, and a
ledger three orders of magnitude smaller.

Be precise about what "live" means here, because it is easy to overstate: the
**server** rebuilds from the live tree on every request, so it can never answer
from a stale graph. The **page** fetches once on load. A reload shows the
current tree; nothing pushes. There is no polling and no event stream, and
neither should be added without a reason better than symmetry with that
sentence.

What stays true from the MCP reasoning: the static-binary position is intact
(`net/http` is stdlib, `CGO_ENABLED=0` unchanged), and there is no per-type
code — the endpoints call `viz.Graph`, `introspect.LoadSchema` and
`entity.Get`, the same verbs the CLI calls. One caveat worth keeping in view:
`entity.Get` builds its index without the stray filter and without
`RejectMalformed`, so `/api/entity/{id}` can answer for a file `/api/graph` has
no node for. The UI only ever links from graph nodes, so nothing reaches it —
but a new caller could.

**The coverage split is deliberate.** The parity harness pins `serve`'s CLI
contract — the TTY refusal, help, arity — because those are
stdout/stderr/exit-code, which is what the harness is good at. The HTTP
contract is `httptest` in `internal/serve/`, because an HTTP body has neither
of the two properties that make the harness worth having (a whole-tree
manifest and an exact exit code).

The **port refusal is the exception, and it is not reachable from a fixture**:
the TTY gate runs before `Listen`, so `serve --port N` in a pipe-mode case
records `serve_needs_tty` and never binds. `TestListenRefusesBusyPort` covers
it instead. Do not add a fixture step expecting `port_in_use` — it cannot
produce one.

**A serve step in a `mode: human` or `mode: pty` parity case hangs the suite
forever.** Those modes do not set `TTY_COMPATIBLE=0`, so the refusal never
fires and the runner waits on a server that never exits. Default `mode: pipe`
is what makes `cli-contract/serve-refusals` terminate. Nothing in the code can
prevent this; the case file carries the warning.

**Read-only is structural, not policy.** The guard rejects every method but GET
and HEAD before routing, so a write endpoint cannot be added by accident. If
one is ever added, three things become mandatory that are not needed today: a
per-run token in the URL (same-origin currently stops a local page from
*reading* a response it is allowed to send — that argument dies the moment a
request has an effect), an `Origin` check, and a decision about whether the
HTTP contract gets harness coverage after all.

**Host-header validation is the load-bearing guard**, not the loopback bind.
DNS rebinding re-resolves an attacker's domain to `127.0.0.1`, which defeats
the bind and defeats same-origin — but the request still announces the
attacker's hostname in `Host`. `serve.AllowedHost` is therefore the whole
defence, and its test is the one not to delete. Never add a CORS header: it
would undo the layer underneath it.
