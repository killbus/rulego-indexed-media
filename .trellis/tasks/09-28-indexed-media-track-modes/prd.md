# Support independent video, audio and paired indexed media

## Goal

Let callers choose video-only, audio-only, or paired video/audio indexed media
through `indexedMedia.inspect` and `indexedMedia.produce`. Each produce request
creates exactly one MPEG-TS member for the supplied track set and segment. This
enables audio listening and silent-video use without fetching an unwanted track.

## Background and confirmed scope

- On 2026-09-28 the user requested progression after the completed source rename,
  selected option 1 (TS for all modes), and confirmed that modes are alternatives
  selected per request, not three automatically generated outputs.
- Existing validation requires both tracks (`node.go:241`); inspection uses the
  video index (`source.go:82`); production maps both tracks (`producer.go:100`).
  Public result shapes are at `README.md:86` and `README.md:111`.
- The separate engineering task retains its streamed `audio.m4a` endpoint and
  zero-plugin-change boundary:
  `D:/Repositories/rulego-engineering/.trellis/tasks/09-11-indexed-audio/prd.md:14`
  and line 21. This task does not complete or amend that composition.
- Evidence: [track contracts](research/track-contracts.md) and
  [output ownership](research/output-scope.md). Implementation mechanics and
  validation sequence are in [design.md](design.md) and [implement.md](implement.md).

## Requirements

- R1: Both operations accept video alone, audio alone, or a valid pair. Omission
  is the only absent-track wire form: reject explicit `null`, empty/partial or
  malformed supplied objects, and no tracks. Validate every supplied track; an
  invalid companion must not silently become single-track success. Retain strict
  unknown-field/trailing-JSON rejection and distinct URLs for paired tracks.
- R2: Accept indexed H.264/MP4 video and AAC/M4A-or-MP4 audio using bounded reads,
  including valid sources smaller than the initial 64 KiB probe. Return
  `{member: "N.ts", bytes: ...}` for the requested member only, using stream-copy
  remuxing. No automatic production of other modes, members, or whole files.
- R3: Use video as the primary timeline whenever selected; use audio otherwise.
  Segment count, durations, and bounds come from that primary index. Keep the
  existing total-duration meaning (last reference end / timescale), including
  nonzero earliest timestamps. Fetch and output only selected tracks. Preserve
  paired audio-overlap behavior; require SAP1 for video only.
- R4: Distinguish selected track sets in revision and complete-lease caching.
  Preserve the paired revision byte contract and literal fixture. Selected
  sourceKey, normalized codec/container, raw SIDX, and initialization length
  determine revision; URLs/headers do not. Reinspect changed access leases and
  use current credentials. A mode or evidence mismatch against expectedRevision
  fails before production init/segment range requests or FFmpeg. Bounded
  discovery probes may already contain media bytes, especially for short files.
- R5: Preserve indexedMedia registration, shared ownership, typed errors,
  bounded retries/deadlines, per-input/output byte limits, path confinement,
  temporary-input cleanup, and stale-cache invalidation. A failed output is
  never returned as successful. Resource publication and HTTP remain external.
- R6: Document request examples and verify actual stream composition, decoding,
  timing, and absence of unwanted requests for all modes in owning CI, while
  retaining existing paired HLS seek and plugin compatibility checks.

## Acceptance Criteria

- [ ] A1 (R1): Both public operations accept the three valid selections; zero
  tracks, null/empty/invalid companions, nested unknown fields, and malformed
  requests fail as invalid_input before network/FFmpeg work.
- [ ] A2 (R2, R3): Real first, distant, final, and adjacent members contain
  exactly the selected stream types and decode successfully. Inspect/produce
  agree on the primary timeline, including nonzero earliest timestamps. Single
  modes issue no absent-track requests and produce only the requested member.
  A valid audio fixture below 64 KiB succeeds; invalid or oversized probe
  responses fail without relaxing exact subsequent index/media ranges.
- [ ] A3 (R4): All modes have distinct revisions; the paired literal digest
  remains unchanged; single-track literals are pinned. Evidence changes alter
  revision. URL/header renewal triggers discovery and current-credential reads
  without altering revision. Cache/singleflight and stale invalidation remain
  isolated by complete lease.
- [ ] A4 (R5): Existing paired playback/seek, overlap behavior, registration,
  owner/ref, deadline, retry, byte-bound, safe-path, temporary-input cleanup,
  cancellation, and error-sanitization regressions pass with single-mode coverage
  for affected paths. Revision mismatch permits only needed bounded discovery;
  it prevents production init/segment range requests and FFmpeg.
- [ ] A5 (R6): English docs/specs show per-request selection and result contracts;
  the provider example remains a paired caller. CI passes formatting, vet, unit
  and race tests, both architecture build/smoke checks, and real TS playback.
  Evidence records the tested source/artifact and any unavailable checks.

## Out of scope and known limits

- MP4/M4A/fMP4 output, entire-stream HTTP responses, downloads, synthesized
  tracks, transcoding, new codecs, unindexed inputs, multiple tracks of one kind,
  muxed input files, and automatic provider or playback-mode selection.
- Plugin-owned manifests, HTTP serving, publication/retention, and consumer UX;
  hosted repository rename, release publication, installation, or runtime cutover.
- Stronger full-content identity: equal-length changed initialization/media
  contents with identical existing evidence are not detected by current revision.
- General paired alignment repairs or changes to total-duration semantics.
  Existing partial-output cleanup limitations are not a new successful-output
  guarantee; staging/publication ownership remains unchanged.

## Execution status

The user confirmed the final PRD/design/execution review on 2026-09-28, and
task.py start activated this task. Implementation, documentation, independent
source review, and local static checks are complete. Commit `8b60a40` was pushed
and PR #2 opened against main. The first owning-CI run passed compiled tests and
both-architecture build/smoke; real-media acceptance stopped on an AAC probe
duration assumption, and debug upload hit a private-directory permission error.
Harness fixes and a new CI run remain required. See research/acceptance-evidence.md;
this task is not completed or archived.
