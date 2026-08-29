# Design

## Capability boundary

```text
provider locator
  -> resolver (YouTube example: yt-dlp HTTP wrapper)
  -> normalized media lease
  -> indexedVod
       -> revision + exact segment timeline
       -> one bounded MPEG-TS member
  -> resourceOrigin publication
```

`indexedVod` begins at the normalized lease. It never receives a YouTube video
ID or a permanent page URL and never knows how that locator is resolved. This
keeps the media-indexing capability reusable without inventing resolver
interfaces or provider registries inside the plugin.

## Normalized lease

The RuleGo message contains a transient, selected source descriptor:

```json
{
  "sourceKey": "youtube:Z4tHPyZBC8g",
  "video": {
    "url": "https://<temporary-media-url>",
    "headers": {},
    "container": "mp4",
    "codec": "avc1.64002a"
  },
  "audio": {
    "url": "https://<temporary-media-url>",
    "headers": {},
    "container": "m4a",
    "codec": "mp4a.40.2"
  }
}
```

`sourceKey` is logical identity. Representation URLs are access leases and are
never treated as identity or included in the revision. The descriptor is
internal RuleGo data, not an HTTP response or persisted origin record.

## Public operations

Inspection accepts the normalized lease:

```json
{"operation":"inspect","source":{...}}
```

Production accepts a current lease and the origin-issued constraints:

```json
{
  "operation":"produce",
  "source":{...},
  "expectedRevision":"<sha256>",
  "segment":372,
  "stagingDir":"<origin-issued path>",
  "maxBytes":33554432,
  "publishBy":"<origin-issued absolute deadline>"
}
```

## Ownership

- Resolver flow: provider locator, credentials, yt-dlp invocation, format
  selection, lease refresh, and normalization.
- `indexedVod`: media-shape validation, SIDX timeline, immutable revision,
  bounded Range reads, one-member mux, and temporary-input cleanup.
- RuleGo flow: HLS playlist composition, segment URI shape, stale-lease retry,
  and HTTP response.
- `resourceOrigin`: generation lease, staging directory, byte/time policy,
  acquire/commit/fail, parent-child lifetime, retention, and static URLs.
- `ffmpeg-over-ip`: authenticated FFmpeg transport and execution semantics.

## Cache and revision

The owner caches and singleflights inspection by the complete normalized lease.
A changed URL or header therefore forces inspection through that current access
lease; cached access data is never substituted by `sourceKey`. The resulting
revision excludes those transient fields, so a refreshed lease may reproduce
the same revision only when its stable source key and immutable representation
evidence match.

No page locator or resolver callback is retained. After restart, the RuleGo
flow resolves a fresh lease and calls `inspect` before production. A stale Range
response is returned as a typed recoverable result; the flow refreshes the
lease, inspects it, requires the same revision, and retries production once.

## Failure model

- SIDX and media Range disconnect/429/5xx: bounded retry.
- Confirmed stale media lease: return `source_stale`; do not invoke a resolver.
- FFmpeg transport/server timeout: bounded retry of the same invocation and
  lease.
- Deterministic FFmpeg exit: terminal production failure.
- Deadline, byte limit, unsafe path, malformed SIDX, incompatible media, or
  revision change: terminal typed failure.

## HLS integration

The manifest route resolves the provider, normalizes the selected lease,
inspects it, acquires the parent origin resource, produces and commits segment
zero, then returns playlist text directly. Segment zero resolves from the
parent; other segment routes acquire short-lived children. Only a production
winner resolves or refreshes the lease before calling `indexedVod`.

RuleGo already supports direct playlist responses: `responseToBody` writes the
terminal RuleMsg payload and `metadataToHeaders` maps
`Content-Type: application/vnd.apple.mpegurl`. Segment routes continue using
`resourceOriginResponse` for static redirects.

## Naming and release

Use `indexedVod` for the component and `rulego-indexed-vod` for the repository
and release identity. YouTube and yt-dlp appear only in the integration example
and deployment documentation. The new contract is a breaking successor without
a compatibility alias.
