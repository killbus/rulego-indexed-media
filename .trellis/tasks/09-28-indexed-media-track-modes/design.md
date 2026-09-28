# Design: selected indexed-media track modes

## Boundary and data flow

`current lease -> strict validation -> selected track set -> cached discovery
-> primary timeline/revision -> requested input ranges -> FFmpeg -> one N.ts`.

The caller selects representations. The plugin infers selection from presence,
so no public mode or output-format field is needed. Inspect returns the existing
revision/duration/segments shape. Produce uses the current lease and existing
expectedRevision/staging limits and returns the existing filename/byte-count
shape. There is no fan-out. Provider resolution, manifests, resource origin,
and HTTP responses remain composition concerns.

## D1. Presence and strict decoding (R1)

Use explicit optional representations, preferably pointers with `omitempty`.
A strict source decoder using raw per-track fields distinguishes omission from
explicit null. Omitted members become nil; supplied values must be valid objects.
Preserve DisallowUnknownFields at every custom-decoder level, request-size limits,
and EOF validation. A custom UnmarshalJSON must not bypass nested strictness.

Derive a validated role-labelled selection in video-then-audio order. Validate
all present tracks using existing role-specific rules. Reject no tracks and equal
paired URLs. Null, empty/partial objects, and wrong types remain invalid_input.
Programmatic fixtures may need pointer construction changes; never infer presence
from URLs. Preserve paired present-field JSON serialization where possible.

## D2. Discovery and primary timeline (R2, R3)

| Selected fields | Discovery | Primary index | Production inputs/maps |
| --- | --- | --- | --- |
| video | Video only, SAP1 required | Video | Init + reference N; `0:v:0` |
| audio | Audio only, no video SAP rule | Audio | Init + reference N; `0:a:0` |
| video and audio | Both, role-specific SAP rules | Video | Video reference N plus existing overlapping audio; `0:v:0`, `1:a:0` |

Spawn and collect exactly the selected number of discovery results. Assign by
role rather than completion order. Preserve owner/request cancellation and
buffered delivery so early errors cannot strand workers. Do not create a pretend
video index for audio-only input.

Share primary-index selection across inspection and production. Per-segment
duration remains reference.Duration / Timescale. Total duration remains last end
/ Timescale (`sidx.go:33`), not a normalized sum. Document that these can differ
when earliest presentation time is nonzero, and retain source timestamps.

Preserve paired overlap selection, rational comparisons, partial-overlap
acceptance, and trim semantics (`producer.go:140`). Singles bypass overlap and
cross-track trimming.

### Initial probe for short sources

The current exact 64 KiB probe (`sidx.go:42`, `source.go:203`) rejects short valid
sources before parsing. Add a probe-specific EOF rule: request bytes 0-65535 with
identity encoding; accept normal exact 206 responses or a clipped 206 only when
fully parsed Content-Range proves start=0, a positive known total below 65536,
and end=total-1. Require the exact declared body length and the existing read cap.
Reject 200 responses, malformed/inconsistent headers, compressed or oversized
bodies, and short bodies without proven EOF. Retain retries and stale errors.

Only the initial probe permits EOF clipping. Subsequent SIDX/header and production
init/media reads remain exact ranges. Use an explicit response-length policy in
the retry helper or a small probe helper; never globally relax exact reads to
'at most'. A bounded probe may encompass a small source, but there is no unbounded
whole-source fallback. Update fixture-server over-end range behavior, currently
rejected at `tests/e2e/fixture-server.go:294`, with corresponding fixture tests.

## D3. Revision and access identity (R4)

Retain paired framing byte for byte (`source.go:249`): JSON facts in sourceKey,
video, audio order followed by video then audio init-size/raw-length/raw bytes.
Preserve the URL-less fixture (`source_test.go:161`) and its literal digest:

`d6e815a371172793a89d537156d03db8394870361c9ac6624ff3510a1ed657f8`.

Dispatch singles by selected roles, or explicit pointer presence in direct hash
fixtures, to this domain-separated SHA-256 preimage:

~~~text
ASCII("indexed-media-single-track-v1") || 0x00
|| BE64(len(factsJSON)) || factsJSON
|| BE64(selectedIndex.InitSize)
|| BE64(len(selectedIndex.Raw)) || selectedIndex.Raw
~~~

factsJSON uses Go encoding/json on ordered struct fields sourceKey, role (video
or audio), representation; representation fields are container then codec,
lowercased as stableFacts does today. URLs, headers, absent-role evidence, and
parsed timestamps outside raw SIDX do not enter the preimage.

Independent fixtures use sourceKey `fixture:single-index-v1` and corresponding
facts/init-size/raw bytes from the legacy paired fixture:

| Role | Expected digest |
| --- | --- |
| video | `4a2b69d8836874c9300b5f8cf1d4c7401f1768930a531878056a4f6e0113992b` |
| audio | `284385c83651ee8e63a104915dc1f9ee57afd0837f7893fae67e8e4dcca7b52d` |

These were calculated with Node crypto/Buffer independently of production code;
they are planned expectations, not passed Go tests.

Keep cache/singleflight keyed by the complete decoded lease, including selected
fields and current URLs/headers. Preserve atomic access data/index bundles,
changed-lease discovery, exact-bundle invalidation, and the revision gate before
production init/segment range requests and FFmpeg. Bounded discovery probes can
already contain media bytes, including the whole source for short files; tests
must distinguish discovery from subsequent production requests. Keep existing
access-lease case treatment; revision normalization must not accidentally make
access leases equivalent.

This preserves the existing evidence envelope, not full content hashing. Same
init length, raw SIDX, and stable facts cannot detect arbitrary content changes.
Paired resource revisions remain stable; singles get distinct revisions under
the same TS profile. Future callers must include selection in any cache lookup
performed before inspection instead of caching solely by sourceKey.

## D4. One-member TS production (R2, R3, R5)

Build an input plan from selected roles and the primary reference. Create, close,
and remove only selected temporary inputs, including after partial assembly
failure. Preserve current Range credentials, per-input/output limits, confined
paths, retries/deadlines, and the nil FFmpeg output callback.

Keep paired FFmpeg arguments as the baseline. Singles use one input and its
required map, stream copy, -copyts, -mpegts_copyts 1, +initial_discontinuity,
-muxdelay 0, and MPEG-TS. Singles need no cross-track -ss or -shortest. Never use
optional '?' maps to conceal mismatched supplied media.

Return decimal segment number + .ts after successful output validation. Bounded
retries may repeat production of that same member; there are no unrequested
members, modes, manifests, or init outputs. Invalid/failed output cannot yield
Success. Preserve temporary-input cleanup; do not claim that existing terminal
FFmpeg failures always remove partial output files. Staging ownership is unchanged.

## D5. Real-media evidence and integration (R6)

Extend hermetic owning CI using its pinned runtime, candidate plugin, verified
peers, and FFmpeg service. Keep the shipped provider-selection policy paired.
Use test-only source selection/composition to drive each mode through actual
indexedMedia inspect/produce and externally assembled TS playlists. Isolate
per-mode staging/cache identities and request statistics.

Verify exact stream count/types, decoding, first/distant/final and adjacent
members, and distant HLS seek. Compare packet timestamps/decoded coverage with
fixture source/SIDX timing. Check monotonic decode timestamps per stream; allow
presentation reordering where applicable. Derive boundary tolerances from fixture
frame/sample duration and TS rounding. Record AAC priming/padding explicitly;
arbitrary seconds of tolerance must not hide gaps or overlaps. Include nonzero
starts and a real audio source smaller than 64 KiB.

Measure absent-track requests across discovery and production using isolated
statistics. Large fixtures retain bounded-range checks; short probes have the
explicit EOF exception. Keep paired cold/warm/restart/stale/retention checks,
both architecture builds, ABI sidecars, registration, and shared-owner smoke.
Mocks establish internal contracts; actual media establishes playback capability.

## Compatibility, spec updates, and rollback

- Update paired-only clauses in backend quality-guidelines.md alongside code:
  Contracts, Validation & Error Matrix, and Tests Required. The reviewed PRD
  replaces those clauses only; other safety/ownership guidance still applies.
- README gains request examples, omission rules, timeline/evidence limits, and
  one-output behavior. No consumer UI, public mode route, release/dependency/ABI
  change, or legacy alias is added.
- Rollback restores prior plugin behavior; single-mode callers must stop sending
  those requests or explicitly select a supported pair. Paired resources need
  no revision migration. Deployment/retention decisions remain external.
- Return to scope review if real-media evidence requires altering paired identity,
  paired timing, or the public output contract.
