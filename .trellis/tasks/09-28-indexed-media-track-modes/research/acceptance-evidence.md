# Acceptance evidence: selected indexed-media track modes

Date: 2026-09-28. Implementation authorized by the user's final confirmation.
Task status: in_progress. This record separates source review and structural
checks from compiled/runtime acceptance.

## Source and artifact identity

- Base HEAD: d85b5ee6090384ef9f3a8a056a5766bed6ed2e6b on main.
- Candidate: uncommitted working-tree changes for this task; no candidate commit
  or plugin artifact has been produced yet.
- The three pre-existing bookkeeping commits remain intact. They are not new
  implementation commits for this task.
- Runtime, SDK, and peer-plugin selectors remain those in plugin-abi-release.json
  and .github/workflows/ci.yml. No release/deployment change is included.

## Acceptance matrix

| Criterion | Source / planned evidence | Execution status |
| --- | --- | --- |
| A1 | node.go strict decoder; node_test.go public selections and track_modes_test.go malformed-request cases | Implemented; compilation and CI pending |
| A2 | source.go/producer.go primary selection; producer_modes_test.go ranges/maps; probe_test.go EOF cases; tests/e2e/track_modes.py stream/timing checks | Implemented; actual-media CI pending |
| A3 | source_test.go paired literal; track_modes_test.go single literals; source_modes_test.go lease isolation; producer_modes_test.go renewal/gate | Implemented; CI pending |
| A4 | Existing safety/ownership tests; producer_modes_test.go retries, cancellation, limits, paths, stale invalidation and cleanup | Implemented; CI pending |
| A5 | README/spec contracts, full-scope review, all CI jobs | Docs and source review complete; CI pending |

## Structural validation

- PASS: task.py validate with installed CPython 3.14 and -B; four real entries
  each in implement.jsonl and check.jsonl, all paths valid.
- PASS: git diff --check after full-scope review. Untracked files also passed
  the raw-byte text-hygiene check described below.
- PASS: main's Node artifact check over README, backend spec, and task files:
  JSON syntax, all seven README JSON examples, local Markdown links, LF/no BOM,
  trailing whitespace, final newline, and in_progress task state.
- PASS (implementation agent): gofmt parsing/formatting and tracked/new core-test
  whitespace.
- PASS (E2E implementation agent): Bash syntax, Go formatting, Python AST, JSON
  parsing, and scoped whitespace checks. These do not execute real-media paths.
- get_context.py --mode packages reports a single-repo project; backend specs
  apply. No frontend change is planned.
- PASS (main, final code snapshot): gofmt -l . returned no paths; Git Bash -n
  accepted tests/e2e-hls-seek.sh. Installed CPython parsed both Python ASTs, all
  proposed JSON/JSONL and README JSON examples, and CI YAML with PyYAML. All 33
  proposed paths passed local-link, LF/no-BOM, final-newline, and whitespace
  checks. SHA-256 comparison after reviewer completion confirmed none of the
  checked files had changed since that aggregate pass.

## Independent full-scope review

The configured trellis-check agent reviewed core code, all tests, CI, README,
backend spec, and the test-only composition against the approved artifacts.

Two findings were fixed before the final static snapshot:

- Repeated valid track keys were rejected by code but missing from documented
  behavior and explicit regression coverage. README/spec and tests now agree.
- HLS seek validation could accept one second from the wrong source position.
  It now preserves source timestamps and asserts first/end position, decoded
  duration, and continuity, with wrong-position/truncated negative controls.

No remaining confirmed in-scope source defect was reported. Pinned client/service
compatibility, translated AAC priming, paired trim boundaries, and actual seek
results still require CI. Source review is not completed runtime acceptance.

## Runtime validation still required

Per the approved execution plan, owning CI must run go vet, unit tests, race
tests, amd64/arm64 plugin build and matching-host ABI/registration/owner-ref
smoke, and tests/e2e-hls-seek.sh with the candidate and verified peers.

No local Docker build or runtime suite is substituted for this evidence. No
current candidate CI run, playback result, or completed acceptance is claimed.
Record the tested commit, CI URL, plugin checksums/ABI, and actual media results
here when available. Prior rename CI is not evidence for this implementation.

The E2E job now retains artifact identity, FFprobe version, source and segment
probe JSON, framehash output, request statistics, timing results, and playlists
in the indexed-media-track-evidence artifact. Its early probe invocation checks
actual pinned-service support; API/protocol documentation alone is not a passed
service check. Review must preserve the AAC priming and frame-derived tolerances
documented in tests/e2e/README.md.
