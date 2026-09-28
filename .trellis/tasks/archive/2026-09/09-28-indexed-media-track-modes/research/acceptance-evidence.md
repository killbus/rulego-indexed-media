# Acceptance evidence: selected indexed-media track modes

Date: 2026-09-28. Implementation authorized by the user's final confirmation.
All A1-A5 acceptance criteria passed in owning CI 36408640108. Final task
archival and PR squash merge follow this evidence record.

## Source and artifact identity

- Base HEAD: d85b5ee6090384ef9f3a8a056a5766bed6ed2e6b on main.
- Accepted candidate: `9a1abc76b85808a1bea9e48d1b371160ea62e02a` on
  `feat/indexed-media-track-modes`, pushed after user confirmation.
- PR: https://github.com/killbus/rulego-indexed-vod/pull/2 (base `main`).
- The three pre-existing bookkeeping commits remain intact. They are not new
  implementation commits for this task.
- Runtime, SDK, and peer-plugin selectors remain those in plugin-abi-release.json
  and .github/workflows/ci.yml. No release/deployment change is included.

## Acceptance matrix

| Criterion | Source / planned evidence | Execution status |
| --- | --- | --- |
| A1 | node.go strict decoder; node_test.go public selections and track_modes_test.go malformed-request cases | PASS: unit/race in CI 36408640108 |
| A2 | source.go/producer.go primary selection; producer_modes_test.go ranges/maps; probe_test.go EOF cases; tests/e2e/track_modes.py stream/timing checks | PASS: all four real-media fixture selections and bounded range checks |
| A3 | source_test.go paired literal; track_modes_test.go single literals; source_modes_test.go lease isolation; producer_modes_test.go renewal/gate | PASS: unit/race plus distinct real-media revisions |
| A4 | Existing safety/ownership tests; producer_modes_test.go retries, cancellation, limits, paths, stale invalidation and cleanup | PASS: unit/race, both-architecture smoke, and complete paired lifecycle E2E |
| A5 | README/spec contracts, full-scope review, all CI jobs | PASS: source review, compiled checks, architecture builds/smoke, and real TS/HLS acceptance |

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

## First owning-CI execution

Run: https://github.com/killbus/rulego-indexed-vod/actions/runs/36402240660

- PR head: `8b60a40bbfe84f4f29bc2b40f0a99d78871df1bd`. The PR merge checkout
  recorded by the runtime harness is `a2ef2b5a3aefc14cc5ef9c229a963adff3bfbc20`.
- PASS: formatting, `go vet ./...`, `go test ./...`, `go test -race ./...`,
  Python acceptance controls, amd64/arm64 plugin builds and matching-runtime
  owner/ref smoke, and release metadata.
- PASS: pinned service JSON FFprobe capability check and the paired seek
  invocation reached before the new selected-track harness. This does not
  establish complete all-mode playback, timing, or lifecycle acceptance.
- FAIL: `packet_groups` indexed a missing `duration` on the original AAC
  fixture's first packet. Its PTS/DTS are 0; the next packet is at 1024 ticks
  with a 1/48000 time base. The other 750 packets report duration 1024. The raw
  `original-audio.m4a.json` is retained in `indexed-media-track-evidence`.
- FAIL: the separate debug artifact upload recursively traversed
  `data/resource-origin/catalog` and received EACCES. The explicitly scoped
  `indexed-media-track-evidence` artifact uploaded successfully.
- Tested amd64 plugin SHA-256:
  `3c2e8d711392eabcf08349c570d11411f494fd9aaa6aaa9b48aebd2cc6fa21dc`.
  Runtime/FFmpeg image digests and verified peer checksums are recorded in the
  uploaded `artifact-identity.txt`; ABI verification passed in the build job.

These failures require focused harness fixes and a new owning-CI run. No
production core defect has been established by this failed run. The task remains
in_progress, with A2/A4/A5 runtime acceptance incomplete.

## CI repair validation

- Missing first AAC durations are now derived only from an exact, fully
  corroborated 1024-sample AAC-LC cadence. Raw probes stay unchanged; separate
  derivation records retain the evidence used. Timing tolerances are unchanged.
- PASS: 25 pure Python controls, including the observed omission and rejected
  ambiguous cases. Replaying the downloaded original audio probe derives one
  duration across 751 packets; the original/shifted video probes need no
  derivation and retain their exact four-second translation.
- Failure diagnostics are saved before cleanup. Debug uploads use a readable
  direct-child snapshot instead of walking private runtime data.
- PASS: Bash syntax, CI YAML parsing, simulated inaccessible-child collection,
  original failure-status preservation (including failed log writes), and
  scoped whitespace checks.
- PASS: independent Trellis repair review found no additional defects. It
  checked all changed harness/CI/spec/task files and replayed authentic probes
  and the captured video framehash, plus debug-collector negative controls.
- These local repair checks do not replace the next actual-media CI run.

## Second owning-CI execution

Run: https://github.com/killbus/rulego-indexed-vod/actions/runs/36405654696

- PR head: `ee6df47f861599d2bc921106a4c49762ab54376f`; runtime merge checkout:
  `fb12c550a29b5d55d7364adac86db3d9c7ea43a5`. The amd64 plugin checksum remains
  `3c2e8d711392eabcf08349c570d11411f494fd9aaa6aaa9b48aebd2cc6fa21dc`.
- PASS: formatting, vet, unit/race, 25 Python controls, both architecture
  builds/ABI/owner-ref smoke, release metadata, media evidence upload, readable
  debug collection, and debug upload. Both first-run failures are resolved.
- PASS: video-only and audio-only member, timestamp, distant HLS seek, and
  isolated network checks. Video seek decodes 30 frames at source time 16..17;
  audio seek decodes 47 frames at 16.032..17.034666667.
- PASS before failure: paired first/distant/final member checks and adjacency.
  Member 6 decodes all 60 video frames at 16..18 and 94 audio frames at
  15.989333333..17.994666667; member 7 audio starts at 17.994666667.
- FAIL: direct paired HLS seek at playlist offset 12 decodes video at 16..17,
  but only 45 audio frames at 16.053333333..17.013333333 (0.96 seconds).
  `paired-6.json` reports the first three audio packets before the video
  keyframe in demux order; those packets are present in the member decode but
  absent after the direct seek. The assertion correctly rejects this loss.
- Short-audio and the final broken-mapping check have not yet executed. Paired
  cache/restart/retention checks precede the selected-track harness. This run
  does not establish complete A2/A4/A5 acceptance.

The next harness correction must use bounded distant pre-roll before the
requested source interval, preserve timestamps, and retain the strict
position/coverage/continuity assertions. It must not relax tolerances or decode
the entire playlist from its beginning. Actual behavior still needs owning CI.

Second repair local validation:

- PASS: 32 Python controls, including bounded pre-roll with actual unequal
  member durations, nonzero/zero source clocks, short audio, selected maps and
  filters, saved request evidence before remote work, and untrimmed full-member
  decoding. The observed late paired audio still fails the strict oracle.
- PASS: geometry replay against every saved source index, Python AST/JSON and
  changed-file text hygiene, task context validation, and `git diff --check`.
- Production code and packet/seek timing tolerances are unchanged. The corrected
  FFmpeg command still requires actual-media CI; local controls are not runtime
  acceptance.
- PASS: independent Trellis review of all nine changed files found no confirmed
  defects. It replayed authentic seeks and short-audio geometry: target member
  5, input member 4 at playlist offset 2.048, source interval 6.56..7.56, and
  0.512 seconds of pre-roll. The saved command establishes requested seek
  geometry; fixture statistics do not establish HLS member-fetch order.

## Accepted owning-CI execution

Run: https://github.com/killbus/rulego-indexed-vod/actions/runs/36408640108

- PR head: `9a1abc76b85808a1bea9e48d1b371160ea62e02a`; runtime merge checkout:
  `f51b33cf082f6276d1bb1b809fa50c81e5574d78`. All five CI jobs passed.
- PASS: formatting, vet, unit/race, 32 Python controls, release metadata, both
  architecture builds/ABI/owner-ref smoke, real TS/HLS, and artifact uploads.
- PASS: exact selected streams, first/distant/final/adjacent members, source
  timestamps, nonzero starts, single-member staging/publication, and range
  isolation for video-only, audio-only, paired, and short audio. Each selection
  has a distinct revision. Network records total 17/17/41/17 respectively;
  single modes contain only their selected representation path.
- PASS: short AAC remains below 64 KiB with a proven clipped initial probe and
  exact later reads. Its distant seek starts at source time 6.56 and decodes
  1.002666667 seconds.
- PASS: distant video seek covers 16..17 exactly; audio-only covers
  16.032..17.034666667; paired video covers 16..17 and paired audio covers
  16..17.013333333. All pass the unchanged codec-frame tolerances.
- PASS: paired concurrent/warm requests, bounded retry/ranges, restart with
  stable manifest/member hashes and no renewed media production, retention/
  parent expiry, HTTP 206/416/304, CORS, and final broken mapping check.
  The log ends with both selected-track and hermetic-HLS success markers.
- Downloaded source probes, timing records, framehashes, requests, and seek
  geometry were replayed locally against the strict oracle. Downloaded plugin
  bytes match checksum and ABI sidecars for both architectures:
  - amd64: `3c2e8d711392eabcf08349c570d11411f494fd9aaa6aaa9b48aebd2cc6fa21dc`
  - arm64: `15f233a7a62af90a60c7b47afb9986eea4f901c193227adb843b42f88c3256a9`
- ABI: `abi-d4fc741b72b9dba1573b61029d6deba9d17c8c22f2805546a5a92764b5c404bf`.
  Both lock digests match `plugin-abi-release.json`. Runtime and peer identities
  are retained in `artifact-identity.txt`; no local runtime substitutes for CI.

The remaining changes are acceptance records and Trellis archival/journal only.
They stay on the feature branch and receive final PR checks before squash merge.

The E2E job now retains artifact identity, FFprobe version, source and segment
probe JSON, framehash output, request statistics, timing results, and playlists
in the indexed-media-track-evidence artifact. Its early probe invocation checks
actual pinned-service support; API/protocol documentation alone is not a passed
service check. Review must preserve the AAC priming and frame-derived tolerances
documented in tests/e2e/README.md.
