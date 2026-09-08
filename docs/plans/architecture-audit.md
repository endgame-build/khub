# Architecture audit implementation

Base: 49aaad6 (serve advanced immediately before branch creation). Branch: fix/architecture-audit-pr. User-approved specification.
Preserve schema-generic core, git truth, canon byte formats, ordered API maps,
validate/check separation, manual draft, missing-required capture. No new dependencies.

## Task 1: Safe writes
One workspace lock from schema load through validation and commit for mutations;
replace collection locks, acquire once for compound operations. Existing-workspace
init participates; fresh init has no cross-process serialization guarantee and
its cleanup must never remove arriving lock files. Reads/dry runs do
not lock or create files. Stable lock files. Atomic per-item/backfill writes:
unique exclusive sibling temporary files, preserve permissions/new-file umask,
write/sync/close/publish/directory-sync. New creates publish exclusively. Distinguish
pre-publication errors from published-but-durability-uncertain failures.

## Task 2: Storage ownership and scans
Normalize effective schema paths. Reject absolute/escaping/reserved paths and
cross-layout ownership collisions including aliases; folder layouts own subtrees.
Root-confined filesystem operations plus reject storage/entity/control symlinks.
Explicit external preset/output paths remain supported. Missing storage is empty;
permission/wrong-shape/I/O errors propagate. Shared layout acceptance detects
wrong-depth typed md/json/yaml as misplaced. Never report incomplete scans clean.

## Task 3: Validation and relationships
Shared existing regexp2 matcher, cached per schema, one-second timeout, invalid
patterns rejected and match errors reported. Preserve enum precedence/scalars.
Write targets use non-stray index; retain raw source access for repair. Compare
relations by resolved node identity in all write paths, deduplicate affected
relations and graph triples, preserve stored spelling, handle dangling unlink by
literal fallback and never choose ambiguous targets. Inverse identity includes
source type and predicate, precomputed per query. HTTP edges add sorted qualified
resolved_targets; CLI shape unchanged. Browser navigates resolved IDs, unresolved
text, ambiguous qualified alternatives, never first-slug match.

## Task 4: Upgrade
Preflight candidate schema/storage/templates/singletons before writes. Add --dry-run
honoring skip flags, no workspace artifacts including locks. Report core and tail
planned outcomes and removed types; JSON appends dry_run/removed_types. Actual
upgrade locks and recomputes. Stage originals/replacements, preserve backups,
publish version last. Restore files/modes/version/backups on failure; remove only
created files. External edits/failed rollback retain evidence and report
upgrade_recovery_failed with locations. Skill/wire/index tails stay nonfatal after
core commit. Preserve crash recovery material; no automatic crash recovery claim.

## Task 5: Graph and performance
Stop neighbors on empty frontier, preserve nonpositive-depth behavior, MaxInt must
terminate. Replace exhaustive cycles with gonum SCCs, one deterministic real
witness per cyclic component per predicate including self loops, same output shape
and gate, stable rotation/order. FTS insert loop uses one transaction with unchanged
bytes/scores/order/errors. Batch git dates for multiple eligible undated files in
linear history, NUL filenames, committer dates/no-follow/nested roots; merge history
and single-file fallback preserves existing helpers; no work when none eligible,
no collection attribution. Benchmark dated/undated and dense graphs.

## Task 6: Retrieval and UX
get ID... core batch: one schema/index and collection parse per file. Single output
byte-identical; multiple array argument order with duplicates; resolve all before
emit, any missing/ambiguous fails batch; multiple raw refused; edges all records.
Plain piped help via existing IsTTY/metadata, rich TTY unchanged, preserve errors,
section order, EPIPE. Fix stale current-tech/presets/MCP/goroutine docs, preserve
historical comments; document new behavior, regenerate CLI reference and guidance.

## Acceptance
Regression checks for every finding: concurrency, link/delete/create races;
atomic temp/symlink/permissions and publication failure; paths/overlap/scan errors;
regex/identity/inverse/strays; upgrade invalid/no-artifact preview/rollback/recovery;
bounded deterministic graphs; multi-get and UI. Dependency-free Node UI tests in
existing CI, outside embedded assets. Full Go tests, vet, gofmt, pinned lint,
parity+coverage, YAML, CLI smoke/reference gates; deliberate narrow fixture updates
only with rationale. Repeat scale 100/550/2000, dense and undated git benchmarks,
record timings/process counts without brittle CI timing assertions.

Deliver reviewable groups; code-review standards and spec against base, fix findings,
then open PR. No rewrite/migration/cache/database/MCP/unrelated refactor.
