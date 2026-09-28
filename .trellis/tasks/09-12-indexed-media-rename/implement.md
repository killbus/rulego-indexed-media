# Execution plan: indexed-media rename

Status: in progress following the user's explicit `开始` approval on 2026-09-12.
The initial approval covered local implementation and verification preparation.
On 2026-09-28 the user approved the concrete commit/push/draft-PR and CI follow-up
proposal with `授权`; see research/commit-plan.md. No checklist item grants
additional repository-host, release, installation, or deployment authority.

## Before task activation

- [x] Obtain a subsequent explicit approval of the latest final planning summary.
- [x] Read PRD/design, source-impact and downstream-impact; recheck current worktree
  and active-task pointers without replacing unrelated work or a different task.
- [x] Load the current workflow transition and relevant Trellis backend guidance.
  Validate both real context manifests, then activate this existing task through
  the normal task command; do not create a duplicate.
- [x] In auto mode, main coordinates and uses the Trellis implementation/check
  roles with the absolute active task path, role boundaries, and native context
  injection (child-side loading only as fallback). Main owns final specs and handoff.
- [x] Confirm approved writable targets. Source is in the sibling checkout; writing
  there requires the appropriate filesystem approval. Use apply_patch for edits.

Continuation on 2026-09-28 runs in the source checkout itself. Existing source
changes are retained and reviewed; the engineering capability wording is already
aligned in its separate working tree. Checked implementation items below mean
the code or configuration is prepared, not that compiled/runtime checks passed.
See [acceptance evidence](research/acceptance-evidence.md) for verification limits.

## 1. Pin regression boundaries (A2, A4)

- [x] Record the source revision/worktree baseline and pin a fixed sourceRevision
  fixture against the pre-rename algorithm. Confirm the algorithm is unchanged.
- [x] Extend plugin contract tests: exactly one new public type, new label,
  unchanged relations, successful new-type graph loading, and rejected old type
  in an isolated registry with only the candidate plugin registered.
- [x] Add missing-video and missing-audio rejection cases for inspect and produce,
  with valid paired requests still accepted. Keep existing codec/schema cases.
- [x] Retain complete-lease cache/singleflight, refreshed credentials, Range limits,
  stale/revision distinction, deadline/path/cleanup, and error-redaction tests.

## 2. Rename source and shipped graphs together (A1-A4)

- [x] Update go.mod, node registration/internal type/display/diagnostic naming,
  plugin component construction, and producer temporary prefixes.
- [x] Update README/example docs, smoke pools/rules, E2E pool, and every shipped
  owner/ref pair. Do not alter arbitrary IDs, resource-origin/staging roots,
  resource IDs, request shapes, output profile indexed-ts-v2, or HLS protocol VOD.
- [x] Rename all example graph-cache readers/writers/invalidation to indexed-media:.
  Update tests for new-key cold miss/warm hit/stale eviction and ignored old keys;
  do not add alias or fallback paths. Keep internal complete-lease hashing intact.
- [x] Keep the graph topology, routing/metadata handoff, resolver format policy,
  public-origin handling, TTLs, and paired-track timeline/ffmpeg maps unchanged.
- [x] Classify remaining old-name matches. Preserve history, legal attribution,
  migration notes, and explicit negative tests; fix live naming inconsistencies.

## 3. Align build and runtime verification (A2, A5)

- [x] Update the CI artifact prefix, smoke data/scratch labels, public-type and
  owner/ref readback assertions, candidate E2E artifact path, and E2E registry check.
- [x] Add explicit old-type absence in the isolated smoke and inspect embedded Go
  module metadata for the new module. Keep neutral E2E load-order basenames.
- [x] Keep Go/dependency/ABI pins and source-stage version metadata unchanged.
  Verify release artifact selection remains consistent with .so/.sha256/.abi.json
  and release.yml retains exact successful-CI revision/tag/no-rebuild gates.
- [x] Preserve both architecture builds and the real peer-plugin HLS seek path.
  Do not substitute registry presence for producer/playback verification.

## 4. Run checks and record proportional evidence (A1-A6)

Fast local checks, adapted to the available shell/tooling:

~~~text
git diff --check
gofmt -l node.go plugin.go producer.go node_test.go source_test.go
rg -n --hidden -i 'rulego-indexed-vod|indexed[ _-]?vod' README.md go.mod *.go examples testdata tests .github .trellis/spec
python .trellis/scripts/task.py validate .trellis/tasks/09-12-indexed-media-rename
~~~

Use rg -g '*.go' or an explicit tracked-file list where the shell does not expand
the example wildcard. Parse changed JSON with the available native JSON parser;
check shell/workflow syntax with available project tooling. Review the entire diff
for unintended semantic, dependency, version, and identity changes. No speculative
dependency installation is required for planning checks.

Compiled, container, and multi-architecture checks belong to the owning GitHub CI:

~~~text
go list -m
go vet ./...
go test ./...
go test -race ./...
go version -m <candidate-plugin.so>
~~~

Also require the existing CI build/sidecar, exact new-type/old-type-absence,
shared-owner/ref smoke, and tests/e2e-hls-seek.sh jobs. The expected go list -m
result is github.com/killbus/rulego-indexed-media. Do not run local Docker builds
to replace owning CI. Any push/dispatch beyond already granted authority needs
separate approval. Until CI runs, report those checks as pending, not passed.

Record per-criterion results under research/ with source revision, exact commands,
CI run/platform/ABI/artifact identity where applicable, and any limitations. Tests
added for the rename are not evidence until executed successfully.

## 5. Quality/spec gate and handoff (A0-A6)

- [x] Complete the independent Trellis source review and local checks. No concrete
  defects remain; the compiled/runtime quality gate is still pending owning CI.
- [x] Main performs the required spec review through trellis-update-spec: update
  the source quality guide's example cache naming and engineering capability
  wording without widening support. Do not point the live ownership guide at a
  repository that has not yet been renamed and verified.
- [x] Summarize actual changed files, acceptance evidence, and remaining authority
  boundaries. Follow the workflow's explicit commit-batch approval; do not commit,
  push, publish, or finish the task merely because editing is complete.
- [x] Keep remote-name preflight/rename, release version/publication, actual
  installer/rule inventory, installation, and playback readback in the separately
  authorized stages defined by design.md. No deployed-migration completion claim
  is allowed from local source checks or historical research alone.

## Risk and rollback checkpoints

- Node/plugin/graph and cache changes are one coherent review unit; partially
  migrated owner/ref or cache branches are invalid intermediate delivery states.
- Retained media storage and immutable identity are unchanged. If that assumption
  stops holding, stop and re-plan rather than expanding this naming task.
- Before authorized deployment, retain a known prior artifact/sidecar and matching
  prior rule/node-pool set. Roll back that pair together, never keep old/new plugins
  loaded as an implicit compatibility strategy.
