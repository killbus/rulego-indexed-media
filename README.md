# rulego-indexed-media

`indexedMedia` (display label: Indexed Media) consumes normalized indexed
media representations and materializes one bounded, seekable MPEG-TS member
without downloading a complete representation. It uses direct SIDX indexes
to produce only the requested member.

Provider resolution, HLS composition, and publication are deliberately
outside this plugin. The included YouTube example calls an existing yt-dlp
HTTP wrapper, normalizes its selected formats, and passes only the generic
lease to `indexedMedia`.

## Current support

Both `inspect` and `produce` require distinct, paired representations:

- H.264 video in fragmented MP4.
- AAC audio in fragmented M4A or MP4.

The video index supplies the timeline, and production stream-copies both
tracks into MPEG-TS. Missing either track is invalid. Audio-only, video-only,
and optional-track operation require a separate future design and implementation;
this rename does not add codecs, containers, protocols, or track modes.

## Install and configure

For a separately authorized cutover, select
`indexed-media-rulego-v<VERSION>-linux-<arch>.so` (`amd64` or `arm64`) with its
matching `.so.sha256` and `.so.abi.json` sidecars. Verify the checksum and ABI
against the plugin-enabled RuleGo runtime before placing the intended producer
in `data/plugins` and restarting RuleGo. CI is configured to build, verify the
embedded Go module identity, and smoke-load with the immutable SDK/runtime pair
recorded in [`plugin-abi-release.json`](plugin-abi-release.json). This source
preparation is not evidence of a published new-name release or an installation.

Configure one shared owner in `node_pool.json`:

```json
{
  "id": "indexed-media",
  "type": "indexedMedia",
  "configuration": {
    "root": "./data/resource-origin/staging",
    "ffmpegAddress": "ffmpeg-over-ip:15050",
    "ffmpegSecret": "replace-me",
    "indexTimeoutMs": 30000,
    "ffmpegDialTimeoutMs": 5000,
    "produceTimeoutMs": 300000
  }
}
```

Rule chains borrow the same owner with `{"root":"ref://indexed-media"}`.
The owner ID is a caller-selected graph key, not a component-type alias or a
storage path; keep the `resourceOrigin` staging root unchanged.

## Normalized media lease

Both operations receive the current access lease. `sourceKey`, codecs,
containers, and the two SIDX indexes determine the revision; temporary URLs
and headers do not.

```json
{
  "sourceKey": "provider:asset-id",
  "video": {
    "url": "https://media.example/video",
    "headers": {"Authorization": "temporary value"},
    "container": "mp4",
    "codec": "avc1.64002a"
  },
  "audio": {
    "url": "https://media.example/audio",
    "headers": {"Authorization": "temporary value"},
    "container": "m4a",
    "codec": "mp4a.40.2"
  }
}
```

The URLs must support HTTP byte ranges. Video SIDX references must be direct
SAP type 1 boundaries.

## Operations

`inspect` returns only the immutable revision and exact video timeline:

```json
{"operation":"inspect","source":{"sourceKey":"provider:asset-id","video":{"url":"https://media.example/video","headers":{},"container":"mp4","codec":"avc1.64002a"},"audio":{"url":"https://media.example/audio","headers":{},"container":"m4a","codec":"mp4a.40.2"}}}
```

```json
{"revision":"<sha256>","duration":2419.2,"segments":[{"duration":5.005}]}
```

`produce` requires a freshly resolved lease, the expected revision, and the
limits issued by `resourceOrigin`:

```json
{
  "operation": "produce",
  "source": {"sourceKey":"provider:asset-id","video":{"url":"https://media.example/video","headers":{},"container":"mp4","codec":"avc1.64002a"},"audio":{"url":"https://media.example/audio","headers":{},"container":"m4a","codec":"mp4a.40.2"}},
  "expectedRevision": "<sha256>",
  "segment": 372,
  "stagingDir": "/app/data/resource-origin/staging/<generation>",
  "maxBytes": 33554432,
  "publishBy": "2026-08-28T18:00:00Z"
}
```

The result is `{"member":"372.ts","bytes":123456}`. No manifest is accepted
or written. Source disconnects, HTTP 429, and transient 5xx responses use
bounded retries. A rejected access lease returns `source_stale`; the RuleGo
flow may resolve a fresh lease, inspect it, require the same revision, and
retry production once. FFmpeg transport/server/timeouts retry the same bounded
invocation, while deterministic exits are terminal.

[`examples/youtube-hls/chain.json`](examples/youtube-hls/chain.json) shows the
complete resolver → `indexedMedia` → `resourceOrigin` composition. Its manifest
route returns playlist text directly with `responseToBody`; only `.ts` members
are staged and published. The example uses `ref://:9090`, so RuleGo Server must
set `share_http_server = true`. Set `publicOrigin` in the
`manifest-public-origin` JavaScript node to the externally reachable RuleGo
origin before deployment.

The example uses only `indexed-media:manifest:` and `indexed-media:lease:`
graph-cache namespaces. They start cold after migration and follow the ordinary
resolve/inspect path on a miss; there is no old-namespace lookup. Complete-lease
hashing, media revisions, the `indexed-ts-v2` output profile, resource keys and
URLs, and `resourceOrigin` storage roots are unchanged. Old short-lived cache
entries may expire naturally; retained media data is not moved or deleted.

## Breaking migration and delivery status

The source module identity is `github.com/killbus/rulego-indexed-media`. This is
a deliberate breaking rename: there is no `indexedVod` alias, forwarding module,
old-artifact selector, or cache fallback. Rules using the old public type must
be migrated; update each shipped owner ID and every matching reference together.

Local source preparation does not rename or verify the hosted repository. The
hosted rename to `killbus/rulego-indexed-media`, a fresh release version and
publication, installation, and live-rule cutover remain separately authorized
work. The local checkout directory need not change.

1. The repository/release owner confirms the target name, performs and verifies
   the hosted rename, and reconciles remotes and authority routes. Select a fresh
   release version and publish only the exact successful-CI artifacts and
   sidecars; do not relabel or overwrite an older release.
2. The deployment/rule owner inventories download automation, installed plugins,
   node pools, stored/exported rules, and editor templates. Stage the matched
   artifact and configuration, load only the intended producer, and verify the
   exact new-only registry, owner/ref save-readback, peer components/processors,
   bounded production, and paired-track HLS playback before completing cutover.
3. Roll back with a matched prior artifact/sidecars and prior rule/node-pool set,
   not by loading both producers or adding an alias. Preserve retained media data.
