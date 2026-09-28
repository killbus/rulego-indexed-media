# Planning validation: selected indexed-media track modes

Date: 2026-09-28. Status: planning; no implementation or activation performed.

## Confirmed user decisions

- Option 1: MPEG-TS output for video-only, audio-only, and paired selections.
- The selected source fields determine the mode. Each produce request emits one
  requested member, never three automatic mode outputs.
- The user authorized continued planning after clarifying this distinction.

## Convergence and review

- Rewrote and read the PRD top to bottom with R1-R6/A1-A5 mapping, concrete input,
  output, timing, compatibility and ownership boundaries, and known limitations.
- Prepared design.md and implement.md plus four real spec/research entries in
  each context manifest. No unresolved product-scope question remains.
- Research-agent review found one overstatement: revision mismatch cannot promise
  zero media bytes transferred because bounded discovery can include media bytes.
  Corrected PRD, design, and implementation plan to forbid subsequent production
  init/segment range requests and FFmpeg, while permitting needed discovery.
- Independently calculated the two single-track revision fixtures with Node
  crypto/Buffer and explicit uint64 big-endian framing; see design D3. This does
  not constitute execution of production hashing tests.

## Checks performed

- Trellis task.py validate: PASS; implement.jsonl and check.jsonl each have four
  valid entries. Invoked installed CPython 3.14 directly with -B because the
  python shim exited unsuccessfully without diagnostics.
- Node read-only artifact check: PASS for all eight then-existing task files;
  JSON syntax, context targets, local Markdown links, required planning artifacts,
  LF endings, no BOM/trailing whitespace, final newlines, and planning status.
  The first attempt had a regex syntax error in the ad hoc checker; the corrected
  checker passed. It did not modify task or product files.
- git diff --check: PASS for tracked changes. The task directory is untracked,
  so its whitespace was checked separately by the artifact check above.
- Worktree scope: only this new task directory is untracked; main is still ahead
  of origin/main by the same three prior bookkeeping commits. No new commit,
  push, task activation, product/spec/example edit, or external task edit occurred.

Go tests, race tests, builds, and actual-media CI have not been run for this task.
Their acceptance remains pending implementation. This record is planning evidence
only. The next transition requires review of the final planning summary under
the project's trellis-brainstorm workflow.
