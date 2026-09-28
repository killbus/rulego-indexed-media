# Proposed implementation commit

## Execution and CI follow-up

The initial proposal below was confirmed and executed as `8b60a40` on
`feat/indexed-media-track-modes`, then pushed at the user's request. PR #2 now
targets `main`. The user requested continued progression; fixes remain on this
feature branch, with a squash merge planned after checks and review pass.

First-run CI repairs form one additional commit:
`fix: validate AAC probe evidence and collect readable CI logs`.

- .github/workflows/ci.yml
- tests/e2e-hls-seek.sh
- tests/e2e/track_modes.py
- tests/e2e/test_track_modes.py
- tests/e2e/README.md
- .trellis/spec/backend/quality-guidelines.md
- .trellis/tasks/09-28-indexed-media-track-modes/task.json
- .trellis/tasks/09-28-indexed-media-track-modes/prd.md
- .trellis/tasks/09-28-indexed-media-track-modes/implement.md
- .trellis/tasks/09-28-indexed-media-track-modes/research/acceptance-evidence.md
- .trellis/tasks/09-28-indexed-media-track-modes/research/commit-plan.md

This repair preserves production code and timing tolerances. Recheck the staged
list before committing; do not include downloaded CI artifacts. Push the repair
to the existing PR and rerun owning CI. Runtime acceptance remains incomplete.

The first repair was committed and pushed as `ee6df47`. Second-run CI repairs
form one additional commit under the existing continuation/push authorization:
`fix: preserve audio coverage in distant HLS seek checks`.

- tests/e2e/track_modes.py
- tests/e2e/test_track_modes.py
- tests/e2e/README.md
- .trellis/spec/backend/quality-guidelines.md
- .trellis/tasks/09-28-indexed-media-track-modes/task.json
- .trellis/tasks/09-28-indexed-media-track-modes/prd.md
- .trellis/tasks/09-28-indexed-media-track-modes/implement.md
- .trellis/tasks/09-28-indexed-media-track-modes/research/acceptance-evidence.md
- .trellis/tasks/09-28-indexed-media-track-modes/research/commit-plan.md

This correction adds one indexed member of distant pre-roll and timestamp-
preserving trim, retaining strict timing assertions. Stage only these reviewed
paths after the checks; do not include downloaded evidence. Push to the same
feature branch and verify actual-media CI before the planned squash merge.

## Initial proposal (historical)

Prepared for the workflow Phase 3.4 review. This file is a proposal, not approval
or evidence of a commit. Runtime acceptance remains pending owning CI, as agreed
in implement.md; the task must stay in_progress until that evidence is recorded.

## Branch and scope

Create feat/indexed-media-track-modes from current HEAD
d85b5ee6090384ef9f3a8a056a5766bed6ed2e6b, preserving the three existing bookkeeping
commits, then make one coherent implementation commit. No amend or push in this
step. Release and deployment are outside this task.

## Commit 1

Message: feat: support selected indexed media tracks

Product and regression files:

- node.go
- source.go
- sidx.go
- producer.go
- node_test.go
- source_test.go
- sidx_test.go
- track_modes_test.go
- probe_test.go
- source_modes_test.go
- producer_modes_test.go

Real-media acceptance and CI:

- tests/e2e-hls-seek.sh
- tests/e2e/fixture-server.go
- tests/e2e/fixture-server_test.go
- tests/e2e/track-modes-chain.json
- tests/e2e/track_modes.py
- tests/e2e/test_track_modes.py
- tests/e2e/media-tool/main.go
- tests/e2e/README.md
- .github/workflows/ci.yml

Documentation and task artifacts:

- README.md
- .trellis/spec/backend/quality-guidelines.md
- .trellis/tasks/09-28-indexed-media-track-modes/task.json
- .trellis/tasks/09-28-indexed-media-track-modes/prd.md
- .trellis/tasks/09-28-indexed-media-track-modes/design.md
- .trellis/tasks/09-28-indexed-media-track-modes/implement.md
- .trellis/tasks/09-28-indexed-media-track-modes/implement.jsonl
- .trellis/tasks/09-28-indexed-media-track-modes/check.jsonl
- .trellis/tasks/09-28-indexed-media-track-modes/research/track-contracts.md
- .trellis/tasks/09-28-indexed-media-track-modes/research/output-scope.md
- .trellis/tasks/09-28-indexed-media-track-modes/research/planning-validation.md
- .trellis/tasks/09-28-indexed-media-track-modes/research/acceptance-evidence.md
- .trellis/tasks/09-28-indexed-media-track-modes/research/commit-plan.md

Unrecognized dirty files: none identified. Recheck the worktree immediately
before staging and keep any newly unrecognized files out of this batch.

## Validation boundary

Final static checks and independent review are recorded in acceptance-evidence.md.
Compiled Go checks, both-architecture ABI smoke, and actual TS/HLS acceptance
must run against the committed candidate in owning CI. This proposal does not
claim those results and does not complete or archive the task.
