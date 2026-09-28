# Source acceptance evidence

## Snapshot and scope

- Resumed on 2026-09-28 after the user requested `继续推进`.
- Source checkout: `D:/Repositories/rulego-indexed-vod`; initial review on `main`,
  delivered on `feat/indexed-media-rename`.
- Baseline HEAD: `ab2217bccb46b8ca854b778ef82cd805e6941cb9`. Source commit:
  `d388152eb2feea64a04b32f413fa750c42425289`.
- The local checks below preceded the source commit. Owning CI subsequently
  passed; see the run and artifact evidence below. The task remains open for
  draft PR review and Trellis wrap-up, without implying merge or deployment.

## Local verification

| Command / check | Result and limit |
| --- | --- |
| `node .trellis/tasks/09-12-indexed-media-rename/research/verify-local.cjs` | Passed. Production Go files equal the baseline with only the approved naming map; source/index logic, dependency lock, version/ABI records, release workflow, and existing producer/SIDX tests are unchanged. |
| Same script: JSON and JavaScript checks | Passed. Four shipped graph/pool JSON files match the naming map, and all 31 example scripts parse. |
| Same script: cache scenarios | Passed. Real example scripts cover cold resolution, ignored populated old namespaces, new lease/manifest writes and warm reads, unchanged TTL/output identity, paired produce payload, and stale eviction. The JavaScript VM cache fixture is not the RuleGo runtime. |
| `python .trellis/scripts/task.py validate .trellis/tasks/09-12-indexed-media-rename` | Passed. Both context manifests contain four valid entries. |
| YAML load plus Bash syntax checks | Passed in independent review after the source-module assertion: both workflows and all 20 run blocks, plus `tests/e2e-hls-seek.sh`. Syntax checks execute no workflow commands. |

Final `gofmt -l node.go plugin.go producer.go node_test.go source_test.go` and
`git diff --check` passed after the implementation worker's handoff. A scoped
old-name search found only the intentional exceptions classified below.
All 14 task artifacts were also checked for trailing whitespace; `task.json` and
the eight JSONL context entries parse successfully.

## Prepared Go regressions

- Sole `indexedMedia` export, `Indexed Media` label, unchanged relations, and
  new-type loading / old-type rejection using an isolated component registry.
- Shared owner/borrower loading with an arbitrary owner ID.
- Real RuleGo cache fixtures for cold/warm/stale paths, old namespace isolation,
  mixed old/current manifest and lease keys, and complete-lease equality.
- Missing video/audio rejection for both operations at decoding and node-message
  boundaries, including `Failure` / `invalid_input` and valid paired controls.
- A literal source revision digest against the unchanged pre-rename algorithm:
  `d6e815a371172793a89d537156d03db8394870361c9ac6624ff3510a1ed657f8`.
  Independently recomputed during implementation, then verified by Go tests in CI.

These tests passed in the owning CI unit and race runs, together with the retained
lease/singleflight, Range, producer, cleanup, and error tests.

## Independent Trellis review

The check sub-agent completed the full production/docs/config/CI/E2E and Go-test
source review on 2026-09-28 with no concrete defects and no reviewer edits. It
independently reran the local verifier, production formatting/diff checks, context
validation, and workflow YAML/Bash checks. No local review work remains; the
candidate was ready for owning CI, which subsequently passed.

The pinned RuleGo dependency was unavailable locally. Its API compatibility and
runtime behavior were verified by the successful owning CI run below.

## Downstream guidance

Read-only inspection of the engineering checkout found an existing one-line
change in `.trellis/spec/guides/rulego-capability-design.md`: `Indexed VOD
production` is already `Indexed media production`. Its bounded-output and
composition boundaries are unchanged. This session preserves that existing edit
and the unrelated indexed-audio research file.

The ownership guide still names the existing `killbus/rulego-indexed-vod`
repository. This is the intended Stage 1 state; updating that authority requires
the separately verified hosted rename.

## Remaining name classifications

- README migration explanations: intentional old public-type mention.
- Go/CI/E2E negative assertions and legacy cache fixtures: intentional rejection
  evidence; no fallback registration, lookup, or artifact selector.
- Backend quality guidance: old type appears only as a rejection assertion.
- HLS `#EXT-X-PLAYLIST-TYPE:VOD`: unchanged protocol value.
- LICENSE attribution, archived tasks/journal, and dated research: historical
  evidence preserved. The physical checkout path is unchanged.

## CI and external acceptance

- Draft PR: https://github.com/killbus/rulego-indexed-vod/pull/1
- CI: https://github.com/killbus/rulego-indexed-vod/actions/runs/36389841942
- Event: `pull_request`; reported head SHA:
  `d388152eb2feea64a04b32f413fa750c42425289`.
- `gh run watch 36389841942 --repo killbus/rulego-indexed-vod --exit-status`
  confirmed overall `success` on 2026-09-28.

| Check | Result |
| --- | --- |
| Source module identity and formatting | Passed |
| `go vet ./...`, `go test ./...`, `go test -race ./...` | Passed |
| linux/amd64 and linux/arm64 pinned SDK builds | Passed |
| Plugin checksums and ABI sidecars | Passed on both platforms |
| Embedded new module `path` / `mod`, old module absence | Passed on both platforms |
| Pinned runtime owner/ref smoke and old-type absence | Passed on both platforms |
| Peer-plugin coexistence and real paired HLS playback/seek | Passed |
| Release metadata collection | Passed; no release published |

Candidate filenames are `indexed-media-rulego-v0.2.0-linux-amd64.so` and
`indexed-media-rulego-v0.2.0-linux-arm64.so`, with `.sha256` and `.abi.json`
sidecars. Build jobs verified ABI
`abi-d4fc741b72b9dba1573b61029d6deba9d17c8c22f2805546a5a92764b5c404bf` and lock
`sha256:5f9501666c46871d6acd84ab259f5ce72bf11ae2d566a51f649d2c6acd289c98`
against the unchanged `plugin-abi-release.json`. That file also pins the SDK,
runtime, and packaging revision used by these jobs.

The GitHub artifact API reported these unexpired archives. Digests identify
uploaded archives, not individual plugin files; plugin hashes were verified
inside their owning build jobs.

| Archive | Artifact ID | Archive SHA-256 |
| --- | --- | --- |
| plugin-linux-amd64 | 10956310578 | `18d86aead1f164ce716bb59b337bbd4ed790c80a48b07522f005cbbebc0e3652` |
| plugin-linux-arm64 | 10956450495 | `b76f6078bae4be7a5bf53f2ed5525c80e74615a4bc3ceecb0e05e5080261e7e7` |
| release-metadata | 10956370667 | `de88f90b7c16e44df3d95595ca5bb9d2ba2d281220c0322fb002bad0b71a0d54` |

A0-A6 are accepted for this source stage. The source commit is pushed and the
draft PR is ready for review. Merge, hosted rename, release, installation, and
deployment have not been performed. Task archival and journal wrap-up remain
separate from this draft-PR handoff.
