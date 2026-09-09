# Issues and the board — where the backlog lives and how it moves

The backlog is GitHub issues plus the org project board
[#15 "khub"](https://github.com/endgame-build/khub/issues). Nothing else
is a backlog: the `docs/*.md` design documents hold *rationale*; a new
candidate becomes an issue first. An issue holds the *decision and its
acceptance*.

## Standing verdicts — do not reopen without new evidence

- **Board, not milestones, not priority labels.** Priority (`P0 P1 P2 Hold`)
  and Size (`S M L`) are project fields; the board is the order. One place to
  sort. Changing what is P0 is a board edit, never a rule edit.
- **Native sub-issues, not body checklists,** for anything with a sequence.
  The three epics — #137 federation, #140 schema evolution, #141 OKF
  provenance — roll progress up and keep the parent short.
- **Labels: exactly one area, optional kind.** Areas: `schema write-path
  retrieval integrity agent-surface interop ingestion federation viz perf
  testing`. Kinds: `design` (a decision or note gates code), `epic`, `hold`
  (parked deliberately — say why in the body). GitHub's defaults mean what
  they say; `critical` means "blocks a shipped feature from reaching users"
  and nothing softer; `okf` is the OKF-spec theme and rides beside an area.
- **P0 today** is the federation contracts plus OKF provenance, because both
  are cheaper to design in than to retrofit. That is the board's statement,
  restated here only so a session does not start elsewhere by default.

## Decisions live in the issue body

Options with trade-offs, a recommendation, then a dated `Decided:` line at
the top once it is made — the shape #25–#28 carry. Comments are conversation;
the body is the record. Closing as decided-against is `wontfix` with the
reason in the closing comment. Closing as shipped is `Closes #n` in the PR.

## Workflow for an agent

1. Pick from the top of P0 on the board unless told otherwise. Read the body,
   its parent epic, and anything it says it depends on.
2. Status → In Progress. Branch `<area>/<slug>` from `origin/main`.
3. One issue per commit when a PR closes several. The commit says which
   fixture bytes moved and why — `testing.md`'s record discipline.
4. PR body carries `Closes #n` per issue; linking moves Status to In Review.
5. Tick the issue's Acceptance boxes, or name the miss in the PR.
6. Work discovered mid-task becomes a new issue with an area label — the
   `project-add` workflow puts it on the board — never scope creep in the PR.

## Machinery

- New issues land on the board through `.github/workflows/project-add.yml`
  (`actions/add-to-project`), which needs the repo secret
  `ADD_TO_PROJECT_PAT` — `GITHUB_TOKEN` cannot write an org project. If the
  PAT becomes a burden, the board's own "Auto-add to project" workflow is the
  no-secret fallback; org-plan-limited.
- Issue templates set the body shape and the kind label; the **Area** dropdown
  lands in the body and whoever triages applies the area label (a form cannot
  map a dropdown to a label without another workflow, which is not worth it).
- In a worktree session the isolation guard refuses any Bash command whose
  text contains `git` — `.github/…` paths included. Read and write those files
  with the file tools; pass issue and PR bodies with `--body-file`, never
  inline; run `gh` from a directory inside the repo.
