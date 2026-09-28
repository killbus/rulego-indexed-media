# Source acceptance evidence

## Snapshot and scope

- Resumed on 2026-09-28 after the user requested `继续推进`.
- Source checkout: `D:/Repositories/rulego-indexed-vod`, branch `main`.
- Baseline HEAD: `ab2217bccb46b8ca854b778ef82cd805e6941cb9`. Evidence below
  concerns the uncommitted candidate diff, not a built or released artifact.
- The task remains `in_progress`. Compiled tests and final runtime acceptance
  belong to owning CI under the reviewed execution plan.

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
  Independently recomputed during implementation; Go execution remains pending.

These tests are authored and formatted, not reported as passing. Existing
lease/singleflight, Range, producer, cleanup, and error tests remain in place.

## Independent Trellis review

The check sub-agent completed the full production/docs/config/CI/E2E and Go-test
source review on 2026-09-28 with no concrete defects and no reviewer edits. It
independently reran the local verifier, production formatting/diff checks, context
validation, and workflow YAML/Bash checks. No local review work remains; the
candidate is ready for owning CI.

Compatibility with the pinned RuleGo API is not verified locally because the
dependency is not cached. Compiled and runtime checks remain pending CI; the
source-review result does not substitute for them.

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

Still pending, with no successful run or candidate artifact claimed:

- `go list -m`, `go vet ./...`, `go test ./...`, `go test -race ./...`.
- Pinned SDK builds for linux/amd64 and linux/arm64, checksums/ABI sidecars,
  embedded `path`/`mod` identity, and matching-runtime shared-owner smoke.
- Peer-plugin coexistence and real paired HLS playback/seek.

There has been no commit, push, CI dispatch, hosted rename, release, installation,
or deployment during this continuation. A1/A6 are accepted from source
review/handoff; A2-A5 remain open until the required compiled/runtime evidence
exists. No archive or completion transition is appropriate yet.
