# Quality Guidelines

> Code quality standards for backend development.

---

## Overview

<!--
Document your project's quality standards here.

Questions to answer:
- What patterns are forbidden?
- What linting rules do you enforce?
- What are your testing requirements?
- What code review standards apply?
-->

(To be filled by the team)

---

## Forbidden Patterns

<!-- Patterns that should never be used and why -->

(To be filled by the team)

---

## Required Patterns

<!-- Patterns that must always be used -->

(To be filled by the team)

---

## Testing Requirements

<!-- What level of testing is expected -->

(To be filled by the team)

---

## Code Review Checklist

<!-- What reviewers should check -->

(To be filled by the team)

## Scenario: RuleGo routing-node handoff

### 1. Scope / Trigger

Apply this contract when a rule chain routes a `resourceOrigin` descriptor
through `jsSwitch` and then builds another origin request.

### 2. Signatures

- `resourceOrigin.acquire` returns a descriptor containing `resourceId` in the
  message body.
- `jsSwitch` returns relation names; it does not publish mutations made to its
  local `metadata` value.
- The next `jsTransform` must read the parent ID from `msg.resourceId`.

### 3. Contracts

- Keep request context such as video ID, revision, and segment in metadata.
- Keep resource lifecycle identifiers in the current message until a transform
  deliberately copies them for a later asynchronous production branch.
- Manifest and member routes must have distinguishable static shapes, for
  example `/youtube/:videoId/index.m3u8` and
  `/youtube/:videoId/segments/:revision/:segment`.

### 4. Validation & Error Matrix

- Empty parent `resourceId` -> `resourceOrigin.resolve` returns `invalid_input`.
- Missing parent resource -> child acquisition returns `parent_unavailable`.
- Segment `0` -> resolve `0.ts` from the manifest resource.
- Segment `N > 0` -> acquire a child with the current parent `resourceId`.

### 5. Good / Base / Bad Cases

- Good: initial and child branches read the descriptor from `msg.resourceId`.
- Base: request metadata survives routing unchanged.
- Bad: a `jsSwitch` mutates metadata and a downstream node depends on that
  mutation.

### 6. Tests Required

- Run the switch and downstream transform in a real in-memory RuleGo graph and
  assert the emitted request contains the input parent ID.
- Load the complete example chain to catch HTTP-router path conflicts.
- Verify segment `0` and one demand segment return a ready resource at runtime.

### 7. Wrong vs Correct

```javascript
// Wrong: the switch mutation is not an output contract.
metadata.parentResourceId = String(msg.resourceId)
return ['Initial']

// Correct: consume the descriptor in the downstream transform.
return {msg: {operation: 'resolve', resourceId: String(msg.resourceId), member: '0.ts'}, metadata: metadata}
```

## Scenario: Indexed-media access leases

### 1. Scope / Trigger

Apply this contract when maintaining indexed-media registration, track selection,
input validation, bounded production, or immutable index caching while callers
refresh representation URLs or headers.

### 2. Signatures

- RuleGo type: `indexedMedia`, label `Indexed Media`, operations `inspect` and
  `produce`, and relations `Success` / `Failure`.
- Go module: `github.com/killbus/rulego-indexed-media`. Candidate artifacts use
  `indexed-media-rulego-v<VERSION>-linux-<arch>.so` with matching checksum and ABI
  sidecars; a renamed filename does not establish the embedded module identity.
- Input lease: `sourceKey` and at least one of `video` / `audio`, each containing
  `url`, `headers`, `container`, and `codec`. No mode or format field.
- Inspection result: stable `revision`, `duration`, and segment durations.
- Production input: the complete current lease, `expectedRevision`, segment,
  and origin-issued staging limits.
- Production result: `{"member":"N.ts","bytes":123456}` for one requested segment
  containing only the selected tracks.

### 3. Contracts

- Export only the current registered type. Component type, caller-selected node
  ID, and storage root are separate identities; shipped owners use `indexed-media`
  and borrowers use `ref://indexed-media`, while other valid caller IDs work.
- Both operations accept H.264/MP4 video alone, AAC/M4A-or-MP4 audio alone, or a
  valid pair with distinct URLs. Omission alone denotes absence; validate every
  supplied track and preserve nested unknown-field and trailing-JSON rejection.
  Reject repeated `video` or `audio` keys even when both values are valid, so
  later values cannot conceal an earlier malformed companion.
- Discover and fetch only selected tracks. Video is primary when present; audio
  is primary otherwise. Segment count, durations, and bounds use the primary
  index. Total duration remains last reference end / timescale, including nonzero
  earliest timestamps. Require direct references; SAP1 is required for video only.
- Preserve paired overlap and trimming. Single-track production selects primary
  reference N, uses the required `0:v:0` or `0:a:0` map, and skips cross-track
  trimming. All modes retain timestamp-preserving TS stream copy, bounded inputs
  and output, staging confinement, retries, deadlines, and temporary-input cleanup.
- The CI fixture's FFprobe may omit the first AAC packet's `duration`. Only
  infer that value for AAC-LC, 48000 Hz, time base 1/48000, exactly one omission
  at packet zero, and a following packet. Require PTS=DTS, every following
  duration to equal 1024 samples, and every DTS step to equal 1024 samples.
  Derive from the next DTS; preserve the raw probe and record the derivation
  separately in `*-duration-derivations.json`. Never enlarge timing tolerances.
- HLS acceptance seeks to the preceding primary member, still in the latter
  half of the fixture, to preserve audio emitted before the sought video
  keyframe in demux order. Bound pre-roll to that member's actual duration.
  Use input `-ss` in playlist time, `-copyts`, output `trim`/`atrim` at the
  requested absolute source start, and `-to` at source start + 1 second. Do not
  reset PTS or widen position/coverage/continuity tolerances. Full member
  decodes remain untrimmed. Save `*-seek-request.json` with both clocks, member
  numbers, pre-roll duration, and exact arguments before remote decoding.
- On E2E failure, save diagnostics before container cleanup and retain the
  original exit status even if log writing fails. Upload debug files through
  the readable `tmp/e2e-hls-debug/` snapshot, containing only allowed regular
  direct children of each run directory. Do not follow symlinks, recursively
  traverse private runtime data, or change runtime permissions for uploads.
- The initial `bytes=0-65535` probe uses identity encoding. Permit a clipped 206
  only with a fully parsed Content-Range proving start=0, positive total<65536,
  end=total-1, and exact body length. All subsequent ranges remain exact. A probe
  can encompass a short source; never fall back to an unbounded download.
- Cache and singleflight inspection by the complete normalized lease.
- A RuleGo composition may memoize the complete normalized lease as a short
  lived performance hint. The YouTube example uses
  `indexed-media:lease:<videoId>:<revision>` in `ChainCache`; a hit bypasses the
  resolver, while a miss still follows the ordinary resolve and inspect path.
- All manifest/lease cache readers, writers, and invalidators share the
  `indexed-media:` namespace. Other namespaces are not fallback inputs; a cold
  cache follows normal resolution without moving retained resource data.
- Resolver-lease cache state is neither identity nor durable state. Its TTL
  must be shorter than the expected provider lease, and a RuleGo restart must
  remain correct by resolving a fresh lease on the resulting cache miss.
- Evict a cached lease after `source_stale` so refresh re-inspects it.
- Exclude URLs and headers from revision evidence. Use selected stable facts,
  initialization lengths, and raw SIDX bytes; this is not full-content identity.
- Preserve the paired revision preimage byte for byte. Single-track SHA-256 uses
  `indexed-media-single-track-v1` plus NUL, BE64 facts-JSON length, facts JSON,
  BE64 init size, BE64 raw SIDX length, then raw SIDX. Ordered JSON fields are
  `sourceKey`, `role`, `representation`; representation fields are lowercased
  `container` then `codec`. Do not infer role presence from URL contents.
- Reinspect a changed lease, then require its revision to match before production
  initialization/segment Range reads and FFmpeg. Discovery probes may already
  contain media bytes. Pre-inspection caller cache keys must include selection.
- Use only the current request's URLs and headers for media Range reads.

### 4. Validation & Error Matrix

- Unregistered component type -> graph-load failure, not an alias lookup.
- Neither track, explicit null, empty/partial/malformed supplied track, unknown
  fields, repeated track keys, or equal paired URLs -> `invalid_input` before
  network/FFmpeg work.
- Valid omitted companion -> inspect/produce only the selected track.
- Missing video/interior/final/multiple packet durations, unsupported sample
  clock/profile, or inconsistent AAC cadence -> fail CI evidence validation.
- Seek input before the latter half, no preceding member, or less than one
  second after the target -> reject the fixture; wrong source position or
  missing decoded coverage still fails after bounded pre-roll.
- Unreadable debug file -> log and skip that file; a private runtime directory
  is never traversed. Debug collection must not turn the test failure into a pass.
- HTTP 200, invalid Content-Range, encoded/oversized body, or short response without
  proven EOF on the initial probe -> terminal typed range failure.
- 401/403/404/410 from a representation -> `source_stale`.
- Missing, expired, or restart-lost RuleGo lease cache -> resolve and inspect.
- Refreshed lease with different immutable evidence -> `revision_changed`.
- Disconnect, 429, or transient 5xx -> bounded retry.
- Invalid SIDX, codec, container, range, path, deadline, or byte bound ->
  terminal typed failure.

### 5. Good / Base / Bad Cases

- Good: a refreshed URL is inspected and reproduces the same revision; later
  member requests reuse that exact lease for its short TTL.
- Base: a cache miss resolves again, and repeated use of an identical lease
  reuses its inspected bundle.
- Good: an audio-only source omits video, uses the audio timeline, and produces
  one requested TS member with no video requests.
- Bad: treating an invalid supplied companion as absent, or generating all three
  combinations when the caller requested one.
- Good: a first AAC duration omission is corroborated by the entire fixture's
  sample clock and recorded without rewriting raw FFprobe output.
- Bad: filling every absent packet duration with a codec constant or zero.
- Good: for source start 4 and two-second members, seek to playlist time 10,
  trim retained timestamps at source time 16, and stop at source time 17.
- Bad: accepting late audio after a video-keyframe seek by enlarging the
  tolerance, or replacing a distant seek with decoding from the beginning.
- Bad: a new session/plugin owner is introduced only to retain resolver output,
  or a cache lookup by `sourceKey` returns old signed URLs for a new lease.

### 6. Tests Required

- Assert exactly one exported `indexedMedia` type with the expected label and
  relations. In an isolated registry, load the new type and reject `indexedVod`.
- Load shipped owner/ref graphs and retain coverage for arbitrary valid owner IDs.
- Accept all three selections for inspect and produce. Reject neither track,
  null/empty/partial/invalid companions, wrong types, nested unknown fields,
  repeated track keys (including valid values), and trailing JSON before
  network/FFmpeg work.
- Assert primary timeline behavior with differing counts/timescales and nonzero
  starts; preserve paired overlap and role-specific SAP checks. Assert only the
  selected input maps, files, and Range requests occur.
- Cover proven short-source EOF and malformed/short/oversized probe responses,
  retaining exact subsequent reads and per-input/output limits.
- Assert concurrent identical leases perform one inspection.
- Execute the RuleGo cache writer and reader in separate traversals and assert
  the second traversal emits `produce` without entering the resolver branch.
- Prepopulate old manifest/lease namespaces and assert normal resolution. Cover
  cold miss, current-namespace write/warm hit, and stale eviction of both keys.
- Assert changed URLs/headers cause inspection and are used by production.
- Assert transient access fields do not alter revision, while changed indexes
  do.
- Pin a literal revision digest using distinct video/audio indexes and init sizes
  to guard the exact hash framing and track order, not only relative equality.
- Pin independent single-track literal digests and mode isolation. Cover changed
  raw SIDX, stable facts and init lengths, discovery order/cancellation, and exact
  stale-bundle invalidation across modes.
- Assert stale access and revision change remain distinct failures.
- CI verifies embedded module identity, new-only registration, shared references,
  matching-host ABI on both architectures, peer coexistence, and all-mode TS
  stream types, decoding, timing, and HLS seek. Include first/distant/final and
  adjacent members, nonzero starts, valid audio below 64 KiB, and isolated
  absent-track request counts. Preserve paired cache/restart/retention checks.
- Exercise the observed first AAC duration omission and reject ambiguous
  omissions, missing/reordered timestamps, cadence gaps, and wrong profiles or
  clocks. Assert raw probe preservation and separately persisted derivations.
- Check debug collection with inaccessible runtime children and symlinks, and
  preserve the failed command's exit status when diagnostic writing fails.
- Verify seek pre-roll for unequal member durations, zero/nonzero starts, and
  short audio. Assert selected maps/filters, distinct clocks, untrimmed full
  decodes, early request evidence, and rejection of the observed paired audio
  loss. The constructed command must also pass the owning real-media CI.

### 7. Wrong vs Correct

```go
// Wrong: stable identity accidentally selects stale credentials.
bundle := bundles[lease.SourceKey]

// Correct: access data and its verified indexes remain atomic.
bundle := bundles[hashCompleteLease(lease)]
```
