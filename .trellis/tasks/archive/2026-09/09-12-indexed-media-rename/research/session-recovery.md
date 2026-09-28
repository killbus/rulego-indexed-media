# Session recovery and naming evidence

- Date: 2026-09-12.
- Resumed Codex session: `01a0902e-eb89-7bc3-83d6-c2ff12a9db6a`.
- Source owner: `D:/Repositories/rulego-indexed-vod`.
- Source snapshot: `main`, `ab2217bccb46b8ca854b778ef82cd805e6941cb9`.
- Scope: recover the authorized planning task and verify current source facts.
  No implementation, task activation, remote rename, release, or deployment.

## Recovered intent and current task state

- The preceding discussion separated the current mandatory video/audio pair from
  the more general indexed-media transformation. The user's next requested step
  was to follow Trellis and create a planning task for capability-aligned naming.
- The old session reported approval-service failures. Current disk inspection
  found `.trellis/tasks/09-12-indexed-media-rename/task.json` already in `planning`,
  with a generated PRD and example-only context manifests. Reuse this scaffold;
  the old failure report is not evidence that a new task must be created.
- No current task was selected in the source repository. The engineering
  workspace's stale `09-11-songloft-audio-chain` pointer is not this request.
- Proposed repository/module/node/label names remain subject to review. The
  compatibility decision was not answered in the recovered conversation.
- The existing audio task retains its zero-plugin-change premise. No silent
  expansion of that task or approval to implement single-track support follows
  from the rename discussion.

## Verified source evidence

- `go.mod:1` declares `github.com/killbus/rulego-indexed-vod`.
- `node.go:22` defines `componentType = "indexedVod"`; `node.go:102` labels it
  `Indexed VOD`. `plugin.go` registers `indexedVodNode`.
- `node.go:195` routes `inspect` and `produce` through source validation.
  `node.go:245` requires both representations and distinct URLs:

  ```go
  if !source.Video.valid(true) || !source.Audio.valid(false) || source.Video.URL == source.Audio.URL {
      return errors.New("invalid indexed media lease")
  }
  ```

- `node.go:251` validates declared video as H.264/MP4 and audio as AAC/M4A or MP4.
  This validation does not independently verify encoded bytes.
- `source.go:82` builds the inspection timeline from `bundle.videoIdx.Refs`.
- `producer.go:53` selects video and overlapping audio; `producer.go:104` requires
  `"-map", "0:v:0", "-map", "1:a:0"` and outputs MPEG-TS. Relaxing only validation
  would not implement a valid single-track path.
- `README.md:24` and `README.md:37` use instance ID `indexed-vod` and
  `ref://indexed-vod`. These are distinct from the node's registered type.
- `.github/workflows/ci.yml:71` names release artifacts
  `indexed-vod-rulego-v<VERSION>-linux-<arch>.so`; the workflow also checks
  `indexedVod` registration and shared-instance configuration.
- `examples/youtube-hls/chain.json` uses the old node type, shared-instance
  references, and `indexed-vod` cache prefixes. `tests/e2e/node-pool.json` and
  `tests/e2e-hls-seek.sh` depend on the old naming as well.

The earlier single-representation index/range analysis is preserved in
`D:/Repositories/rulego-engineering/.trellis/tasks/09-11-indexed-audio/research/indexed-vod-contract-review.md`.
The validation, video-primary timeline, mandatory ffmpeg maps, and naming facts
above were checked again against the current source snapshot.

## Next planning boundary

The user confirmed on 2026-09-12 that no old-name compatibility is required:
use a coordinated breaking migration with no `indexedVod` alias. Source/downstream
research, the converged PRD, migration design, execution plan, and both real context
manifests are now prepared. The remaining boundary is final user review followed
by explicit implementation approval; do not activate the task before that response.
This recovery does not establish runtime, CI, host-loading, or consumer evidence;
no such tests were run. See planning-validation.md for the planning-only checks.
