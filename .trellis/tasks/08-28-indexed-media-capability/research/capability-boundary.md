# Indexed VOD capability boundary

## Stable capability

`indexedVod` consumes a normalized selected-media lease and exposes two
behaviors:

1. inspect the indexed media and return its stable revision and exact segment
   timeline;
2. materialize one requested MPEG-TS member within an origin-issued staging
   lease and byte/deadline bound.

A permanent page URL belongs to a resolver. It is neither stable media identity
nor an indexed representation. For the YouTube integration, the existing
yt-dlp HTTP wrapper turns the watch URL into temporary video/audio access URLs;
the RuleGo flow normalizes that result before calling `indexedVod`.

## Atomic state

Both representations and both indexes are validated as one revision. The owner
singleflights and caches inspection by the complete normalized lease, so a
changed URL or header is inspected and never replaced with cached access data.
Signed URLs and headers remain transient inputs rather than revision evidence
or persisted origin data.

On stale media access, `indexedVod` reports a typed recoverable result. The
RuleGo flow owns resolver re-entry, registers the refreshed lease, verifies the
same revision, and retries production once.

## External owners

- Resolver flow owns permanent locators, provider credentials, yt-dlp request
  construction, selected-format normalization, and lease refresh.
- RuleGo flow owns HLS playlist composition, segment URI shape, and retry
  routing.
- `resourceOrigin` owns generation leases, staging paths, publication,
  retention, parent-child lifetime, and static resource URLs.
- `ffmpeg-over-ip` owns authenticated FFmpeg transport and process execution.

## Runtime evidence

- RuleGo `responseToBody` writes the terminal RuleMsg payload directly.
- RuleGo `metadataToHeaders` maps message metadata to HTTP response headers.
- Therefore the manifest route can return playlist text with
  `Content-Type: application/vnd.apple.mpegurl` without storing a manifest or
  adding a file-writer component.
- Segment resources remain ordinary `resourceOrigin` publications and use its
  response processor for redirects.

## Non-negotiable omissions

`indexedVod` contains no page URL resolver, YouTube video ID, yt-dlp endpoint,
yt-dlp output schema, selector/sort, cookie path, Googlevideo expiry parser,
caller-provided manifest, provider registry, or compatibility alias.

## Verified integration state

- The RuleGo composition retains the normalized source lease in native
  `ChainCache` for 10 minutes, keyed by YouTube ID and verified revision. The
  first manifest took 25.66 seconds; the next unseen member completed in 10.20
  seconds without another yt-dlp startup.
- mpv reported a 2528.97-second VOD and continued playback after exact seeks to
  600.00 seconds and 2513.97 seconds.
- Two concurrent requests for the same unseen member completed from one
  publication and produced the same SHA-256 digest.
- A RuleGo restart preserved a published member (4.2 ms response) and produced
  a new member after resolving a fresh lease (28.07 seconds).
- A deterministic 404 representation lease exercised `source_stale`; the flow
  deleted the cached lease, resolved and reinspected it, required the same
  revision, and published the requested member in 17.96 seconds.
- The resulting origin snapshot contained four requested MPEG-TS members,
  7,135,164 retained bytes, no staging files, and no MP4, M4A, WebM, partial, or
  complete upstream representation.
- `go test ./...`, `go vet ./...`, JSON validation, gofmt, and diff checks pass.
  Race and ABI-pinned plugin builds remain GitHub CI responsibilities.
