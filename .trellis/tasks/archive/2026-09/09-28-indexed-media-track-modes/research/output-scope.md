# Research: track combinations and output scope

- Query: Which existing output and composition contracts can three track modes
  reuse, and which first-scope decision needs user input?
- Scope: source/docs/CI inspection in this repository; read-only inspection of
  the engineering audio task and capability guidance.
- Date: 2026-09-28.
- Baseline: `d85b5ee6090384ef9f3a8a056a5766bed6ed2e6b`, following merged source
  rename `9e8a9af461b486370962624fe16d874400851f72`.

## Current output contract

- `README.md:3` describes one bounded, seekable MPEG-TS member per request.
  `README.md:15` restricts the current input to paired H.264/AAC representations.
- `README.md:86` documents inspect as revision, duration, and segment durations.
  `README.md:111` documents produce as member filename and byte count, with
  no plugin-owned manifest. Publication and HTTP serving are external owners.
- `examples/youtube-hls/chain.json:161` turns the inspected timeline into an HLS
  playlist with `.ts` member URLs and an `indexed-ts-v2:<revision>` resource
  fingerprint. The source-selection/cache policy at lines 58 and 186 is paired.
- `examples/youtube-hls/chain.json:225` binds a produced child to the parent
  resource and the `indexed-ts-v2` output profile. A new output or selection
  policy must not accidentally reuse an incompatible resource/cache identity.

## Existing audio task has a different delivery contract

- `D:/Repositories/rulego-engineering/.trellis/tasks/09-11-indexed-audio/prd.md:14`
  specifies a streamed fragmented-MP4 HTTP response at `/youtube/:videoId/audio.m4a`.
- That PRD's line 21 explicitly requires zero plugin changes; lines 23-25
  exclude full-track publication and a second consumer cache on the RuleGo side.
- `research/indexed-audio-design-evidence.md:39` in that task records historical
  ffmpeg remux experiments with `-f mp4 pipe:1`; line 72 rejects adding
  `produceAudio` for that particular full-response composition.
- The old note partly justifies placement using the old repository name. The
  current authority is the owned transformation: the engineering capability
  guide (`.trellis/spec/guides/rulego-capability-design.md:53`) assigns bounded
  indexed output members to this plugin and presentation elsewhere.
- Therefore the earlier rejection is not proof that bounded single-track
  production belongs outside indexedMedia. Equally, implementing such production
  does not complete the independent streaming endpoint or its consumer checks.

## First-scope options and user decision

Track selection and output container are separate decisions. Both options below
retain bounded indexed production, H.264/AAC support, and external publication.

| Option | Observable result | Design/verification impact |
| --- | --- | --- |
| A: TS for all three track combinations (recommended first scope) | Each requested member contains only video, only audio, or both; member/result shape stays `N.ts` plus byte count. | Concentrates change on track presence, timeline, identity, and FFmpeg maps; still needs real single-track TS decode and seek evidence. |
| B: Also support MP4/fMP4 output for single-track media | Clients can consume an approved MP4/M4A fragment contract. | Must define self-contained member vs shared initialization/media fragments, timestamp rules, filenames/MIME, profiles, manifest implications, and client acceptance in addition to track-mode support. |

An entire streamed `audio.m4a` response is a further lifecycle/HTTP composition
decision, not an automatic consequence of option B. The existing audio task
continues to own that proposal unless its scope is explicitly revised.

Recommendation: option A for the first implementation because it matches the
current bounded-member contract while exercising all three track combinations.
The user selected option A on 2026-09-28 after comparing the playback/file
scenarios, then confirmed that each request produces only its selected track
combination and requested member. All three outputs are never generated
automatically. This resolves output scope; final design review is a separate gate.

## Planned acceptance evidence

- `tests/e2e-hls-seek.sh:121` already generates separate local H.264 and AAC
  fragmented-MP4 fixtures. Reuse that fixture mechanism instead of depending on
  live provider credentials for native track-mode verification.
- `tests/e2e-hls-seek.sh:84` checks bounded ranges; extend observation so a
  single-track run proves no reads from the absent representation.
- `tests/e2e-hls-seek.sh:261` executes FFmpeg playback with both maps at a
  distant seek. Retain this paired regression and add the corresponding single
  track playback assertions, including exact stream-type/count checks.
- Cover first, distant, and final indexed members and continuity between
  adjacent members; mocked FFmpeg argument assertions alone cannot establish
  playable timestamps or the intended absence of the other track.
- Keep source verification, compiled pinned-runtime CI, release, and live
  deployment evidence distinct. This planning step establishes no runtime result.

## Caveats

- No tests, containers, remote workflows, releases, or live endpoint probes were
  run during this research. Historical audio evidence is not fresh verification.
- Only planning artifacts in the new task are being written. No product,
  existing example, engineering task, spec, or runtime configuration was changed.
- Input/timeline/revision details are recorded separately in `track-contracts.md`.
  The resolved contracts and execution sequence are in ../design.md and
  ../implement.md. Their choices supersede the earlier open research options.
