# Execution plan: selected indexed-media track modes

Status: in_progress. On 2026-09-28 the user confirmed the final implementation
review after selecting TS output and per-request track selection. The task is
active; implementation and source review are complete. Checkboxes record evidence,
not assumed product-test results. Sections 1-3 now record implemented code and
regression assertions; their compiled/runtime execution remains pending CI.

## Before activation

- [x] Obtain approval of the final PRD/design/execution summary.
- [x] Recheck worktree and session pointer; retain unrelated changes and three
  existing local bookkeeping commits. Do not create another task.
- [x] Validate real implement/check context entries and read current workflow.
- [x] Run task.py start for this task after the final review gate is satisfied.
- [x] Use configured Trellis implementation/check roles. Every dispatch starts
  with Active task: <absolute path>, then scope and role instructions. Agents
  read manifests, PRD, design, and this plan. Main owns final spec review/handoff.

## 1. Presence and selection contracts (A1, A3, A4)

- [x] Implement optional representations and strict decoding from D1, preserving
  nested unknown-field, trailing-JSON, codec/header/source validation.
- [x] Cover both public operations with three valid selections; reject neither,
  null/empty/partial/invalid companions, wrong types, and equal paired URLs.
- [x] Update fixtures mechanically without inventing URLs for hash fixtures.
  Pin the legacy paired and independently specified single-track digests.

## 2. Discovery, timeline, and identity (A2-A4)

- [x] Generalize result count and role assignment; cover cancellation, absence of
  unwanted requests, reversed completion order, and single-track completion.
- [x] Share primary-index selection across inspect/produce. Cover different
  counts/timescales, nonzero starts, segment bounds, and role-specific SAP rules.
- [x] Implement D3 single-track hashing; retain the paired preimage. Mutate actual
  raw SIDX evidence in timing-related revision tests.
- [x] Extend cache/singleflight and invalidation checks for mode isolation, access
  renewal, concurrent requests, failed discovery, and current credentials.

## 3. Short-source probe and bounded production (A2, A4)

- [x] Implement D2 EOF probing without weakening exact subsequent reads. Cover
  known short EOF, exact probe, malformed totals, short/oversized/encoded bodies,
  200 responses, stale and transient failures.
- [x] Build only selected temporary inputs, ranges, and required FFmpeg maps.
  Preserve paired overlap/trim; omit paired-only flags for singles.
- [x] Verify one requested output and expectedRevision before production
  init/segment range requests and FFmpeg, explicitly allowing needed bounded
  discovery probes. Cover current credentials, partial-input cleanup, limits,
  retry/deadline/path errors, and owner/request cancellation. Avoid blanket
  safety refactoring.

## 4. Real TS acceptance and documentation (A2, A4, A5)

- [x] Extend test fixtures with explicit per-mode selection and short-source EOF
  responses; test fixture-server changes. Keep provider policy in the shipped
  example paired rather than adding consumer selection behavior.
- [x] Extend CI's real plugin/FFmpeg path to inspect/produce/decode first, distant,
  final, and adjacent members. Verify exact streams, timestamps, HLS seek, and
  absent-track network behavior using isolated statistics.
- [x] Preserve paired cold/warm/restart/retention checks and architecture smoke.
- [x] Add README examples and limits. Update affected paired-only backend spec
  clauses using trellis-update-spec during implementation.

## 5. Validation and review

Run proportional local structural checks first:

~~~text
git diff --check
gofmt -l .
python .trellis/scripts/task.py validate .trellis/tasks/09-28-indexed-media-track-modes
~~~

Parse changed JSON/YAML and check shell syntax with available tooling. Use the
installed Python interpreter directly if the local shim fails. Keep task docs
English with LF endings. Artifact checks are not product tests.

Owning CI retains metadata/format checks plus:

~~~text
go list -m
go vet ./...
go test ./...
go test -race ./...
tests/e2e-hls-seek.sh
~~~

E2E requires candidate INDEXED_PLUGIN and checksum/ABI-verified FFMPEG_PLUGIN and
ORIGIN_PLUGIN, plus pinned runtime/FFmpeg services configured by ci.yml. Preserve
amd64/arm64 build and owner/ref smoke jobs. Runtime evidence is owned by CI;
do not substitute an unrelated local build. Push/PR and other external actions
follow applicable session authority after preparing a reviewable diff. Release
and deployment remain outside this task.

- [x] Complete independent Trellis source review and resolve findings. Rerun
  affected checks; broaden only for new changes or unresolved concerns.
- [x] Record A1-A5 in research/acceptance-evidence.md with exact source revision,
  commands and CI/artifact identity. Distinguish unavailable, failed, and passed
  checks. Actual media evidence is required for capability completion.
- [ ] Execute owning CI against the committed candidate and record compiled,
  architecture, and actual-media results. All A1-A5 runtime acceptance is pending.
- [ ] Complete spec review, commit/handoff and task completion per workflow.

## Risk and rollback checkpoints

- Decoder changes must retain nested strictness.
- Concurrent discovery/caching must not mix modes or access leases.
- HTTP probe work must not relax exact SIDX/media reads.
- Preserve paired hash/timeline/output contracts; review any necessary expansion.
- Synthetic fixtures and mocked output files cannot establish playback. Record
  real timestamp evidence and justified codec-frame tolerances.
- Keep dependencies/ABI/release versions and external task files outside the
  change. Roll back coherent validation/production behavior rather than leaving
  inconsistent support across layers.
