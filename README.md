# rulego-indexed-vod

`indexedVod` consumes a normalized lease for separate H.264 video and AAC
audio fragmented-MP4 representations. It inspects their direct SIDX indexes
and materializes one bounded, seekable MPEG-TS member without downloading a
complete representation.

Provider resolution, HLS composition, and publication are deliberately
outside this plugin. The included YouTube example calls an existing yt-dlp
HTTP wrapper, normalizes its selected formats, and passes only the generic
lease to `indexedVod`.

## Install and configure

Use the `.so` whose architecture and ABI sidecar match the plugin-enabled
RuleGo runtime. Place it in `data/plugins` and restart RuleGo. Releases are
built and smoke-loaded with the immutable SDK/runtime pair recorded in
[`plugin-abi-release.json`](plugin-abi-release.json).

Configure one shared owner in `node_pool.json`:

```json
{
  "id": "indexed-vod",
  "type": "indexedVod",
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

Rule chains borrow the same owner with `{"root":"ref://indexed-vod"}`.

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
complete resolver → `indexedVod` → `resourceOrigin` composition. Its manifest
route returns playlist text directly with `responseToBody`; only `.ts` members
are staged and published. The example uses `ref://:9090`, so RuleGo Server must
set `share_http_server = true`.
