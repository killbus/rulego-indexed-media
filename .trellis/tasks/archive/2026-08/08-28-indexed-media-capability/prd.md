# Indexed VOD capability

## Goal

Provide a generic RuleGo `indexedVod` capability that consumes an already
resolved indexed-media lease and materializes bounded, seekable VOD segments.
Resolver implementation, provider identity, HLS presentation, and resource
lifecycle are separate capabilities. YouTube through yt-dlp is the first fully
verified composition.

## Background

The released MVP proves that independent H.264/AAC fragmented MP4 streams can
be indexed and materialized as individual MPEG-TS members without downloading
either complete representation. It also proves integration with the generic
`resourceOrigin` publication boundary and `ffmpeg-over-ip` transport.

The reusable native behavior begins after source resolution: validate a
selected pair of byte-range media representations, derive its indexed timeline
and stable revision, and materialize one requested member. A permanent page URL
is resolver input and must not become an `indexedVod` concept.

## Requirements

- Expose one shared `indexedVod` owner that accepts a normalized media lease:
  a stable caller-owned source key plus selected video/audio representation
  URLs, headers, and required media facts.
- Do not resolve page URLs or invoke yt-dlp inside `indexedVod`. The RuleGo
  integration calls the existing yt-dlp HTTP wrapper and normalizes its result
  before invoking this capability.
- Validate the proven media predicate: separate direct, byte-range-addressable
  fragmented MP4 representations with H.264 video, AAC audio, top-level direct
  SIDX references, and SAP type 1 video boundaries.
- Atomically bind both representation leases, both SIDX indexes, and the stable
  revision. Concurrent inspection of the same normalized source must
  singleflight index discovery.
- Derive revision from stable source identity and immutable representation
  evidence, excluding short-lived URLs, request headers, and expiry values.
- On production, require the normalized lease and expected revision. A stale
  lease must be reported explicitly so the RuleGo flow can re-resolve,
  re-inspect, verify the same revision, and retry once.
- Keep source and FFmpeg failure domains separate. Source Range failures may
  request re-resolution; FFmpeg transport/server failures may retry the same
  bounded invocation; deterministic FFmpeg exits are terminal.
- Materialize exactly one requested MPEG-TS member from the required MP4 init
  sections and media ranges. Never download or retain a complete upstream
  representation.
- Do not accept or write caller-provided manifests. The RuleGo flow owns HLS
  manifest composition and response; `resourceOrigin` owns staging,
  publication, TTL, retention, concurrency, restart reconciliation, and HTTP
  resource URLs.
- Reuse the existing `rulego-ffmpeg-over-ip/client`; do not duplicate its wire
  protocol or absorb generic process/session ownership.
- Remove all resolver/provider leakage from the plugin contract and code:
  YouTube IDs/watch URLs, yt-dlp endpoint/cookies/arguments/output schema,
  selector/sort fields, and Googlevideo expiry interpretation.
- Breaking replacement is intentional. Do not preserve the
  `youtubeIndexedVod` contract or add compatibility aliases.
- Rename the component and repository identity to `indexedVod` and
  `rulego-indexed-vod`. Retain YouTube only in a complete integration example.

## Acceptance Criteria

- [x] Owner configuration contains only the shared staging root,
      ffmpeg-over-ip connection, and bounded execution timeouts.
- [x] `inspect` accepts a normalized media lease and returns only revision,
      duration, and segment durations.
- [x] `produce` accepts a normalized media lease, expected revision, segment,
      and the `resourceOrigin` staging lease; it returns one closed `N.ts`
      member and byte accounting without manifest fields.
- [x] Production code contains no YouTube or yt-dlp request/response semantics.
- [x] Concurrent inspection of one selected source performs one bounded index
      discovery and returns the same revision.
- [x] Refreshed representation URLs with unchanged immutable media evidence
      reproduce the same revision; changed media is rejected.
- [x] 429, transient 5xx, and network disconnects retry with bounded backoff;
      source-stale and FFmpeg failures remain distinct and tested.
- [x] SIDX, Range, staging confinement, byte limits, exact audio overlap, and
      temporary-file cleanup remain covered by focused tests.
- [x] The YouTube RuleGo example resolves through the existing yt-dlp wrapper,
      normalizes its output, and performs exactly one bounded refresh/retry on
      a stale lease.
- [x] Local integration demonstrates initial playback, distant and near-end
      seek, same-member concurrency, restart recovery, expiry recovery,
      bounded retained files, and no complete upstream download.
- [x] GitHub CI passes formatting, vet, unit/race tests, ABI-pinned amd64/arm64
      plugin builds, and owner plus `ref://` borrower smoke loading.
- [x] A verified release contains matching binaries, checksums, ABI sidecars,
      documentation, and the working YouTube integration example.

## Out of Scope

- A provider registry, resolver interface, opaque lease service, or generic
  media profile system inside `indexedVod`.
- A new yt-dlp service or changes to yt-dlp-http-wrapper,
  `resourceOrigin`, or the ffmpeg-over-ip wire protocol.
- Claiming compatibility with arbitrary containers, codecs, manifests, or
  unindexed media.
- DASH terminal delivery, transcoding profiles, live media, or Emby/Kodi
  certification.
