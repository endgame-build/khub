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
- Exit codes: 0 success · 1 located failure · 2 usage. Three-way, pinned. New
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
untrusted by construction. khub holds private data + untrusted content but
**no egress** — keep it that way (`viz --open` and any future `export` are the
lines to think before crossing). Containment for injected-content writes is
provenance + draft, with `check` as the human review point.
