# rulego-indexed-media

`indexedMedia` (display label: Indexed Media) consumes normalized indexed
media representations and materializes one bounded, seekable MPEG-TS member
using bounded HTTP Range reads. It uses direct SIDX indexes to produce only
the requested member. The initial 64 KiB probe can encompass a short source;
there is no unbounded whole-source download fallback.

Provider resolution, HLS composition, and publication are deliberately
outside this plugin. The included YouTube example calls an existing yt-dlp
HTTP wrapper, normalizes its selected formats, and passes only the generic
lease to `indexedMedia`.

## Current support

Both `inspect` and `produce` accept the caller's selected representations:

| Supplied fields | Supported input | Timeline | Output streams |
| --- | --- | --- | --- |
| `video` | H.264 in fragmented MP4 | Video SIDX | Video only |
| `audio` | AAC in fragmented M4A or MP4 | Audio SIDX | Audio only |
| `video` and `audio` | Distinct representations of the above types | Video SIDX | Video and audio |

Each production request stream-copies the selected tracks into one requested
MPEG-TS segment. It does not generate all three combinations. MP4/M4A output,
whole-stream audio responses, transcoding, and provider mode selection are
outside this plugin's current scope.

## Install and configure

Download the plugin for your version from the
[GitHub releases](https://github.com/killbus/rulego-indexed-media/releases). Select
`indexed-media-rulego-v<VERSION>-linux-<arch>.so` (`amd64` or `arm64`) with its
matching `.so.sha256` and `.so.abi.json` sidecars. Verify the checksum and ABI
against the plugin-enabled RuleGo runtime before placing the intended producer
in `data/plugins` and restarting RuleGo. CI is configured to build, verify the
embedded Go module identity, and smoke-load with the immutable SDK/runtime pair
recorded in [`plugin-abi-release.json`](plugin-abi-release.json). Releases publish
the verified CI artifacts without rebuilding them. Installation and live-rule
cutover remain deployment steps.

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
containers, initialization lengths, and raw SIDX indexes of the selected tracks
determine the revision; temporary URLs and headers do not. Track combinations
have distinct revisions. This evidence does not detect arbitrary changes to
initialization or media contents when the recorded evidence remains identical.

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

Omit an unwanted track field entirely. Explicit `null`, empty or incomplete
objects, and invalid supplied companions are rejected; at least one track is
required. Unknown fields and repeated `video` or `audio` keys are rejected, even
when repeated values are valid. When both tracks are selected their URLs must
differ. Each URL must support HTTP byte ranges and direct SIDX references;
video references additionally require SAP type 1 boundaries.

## Operations

`inspect` returns the immutable revision and primary index timeline. Video is
primary when present; otherwise audio is primary. Segment durations come from
that index. Total duration is the last reference end divided by its timescale;
with a nonzero earliest timestamp it can differ from the sum of segment durations.

```json
{"operation":"inspect","source":{"sourceKey":"provider:asset-id","video":{"url":"https://media.example/video","headers":{},"container":"mp4","codec":"avc1.64002a"},"audio":{"url":"https://media.example/audio","headers":{},"container":"m4a","codec":"mp4a.40.2"}}}
```

```json
{"revision":"<sha256>","duration":2419.2,"segments":[{"duration":5.005}]}
```

For video-only or audio-only inspection, supply just that representation:

```json
{"operation":"inspect","source":{"sourceKey":"provider:asset-id","video":{"url":"https://media.example/video","headers":{},"container":"mp4","codec":"avc1.64002a"}}}
```

```json
{"operation":"inspect","source":{"sourceKey":"provider:asset-id","audio":{"url":"https://media.example/audio","headers":{},"container":"m4a","codec":"mp4a.40.2"}}}
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

For single-track production, use the same single-track `source` as inspection
with its returned revision and a segment number from its timeline. All other
production fields remain the same. A changed track selection requires inspection
and its own revision. A revision mismatch stops production before initialization
or segment Range reads and FFmpeg; discovery probes may already contain media bytes.

The result is `{"member":"372.ts","bytes":123456}` in every mode: one requested
member, containing only the selected streams. No manifest is accepted or written.
Source disconnects, HTTP 429, and transient 5xx responses use
bounded retries. A rejected access lease returns `source_stale`; the RuleGo
flow may resolve a fresh lease, inspect it, require the same revision, and
retry production once. FFmpeg transport/server/timeouts retry the same bounded
invocation, while deterministic exits are terminal.

[`examples/youtube-hls/chain.json`](examples/youtube-hls/chain.json) shows the
complete paired resolver → `indexedMedia` → `resourceOrigin` composition. Its manifest
route returns playlist text directly with `responseToBody`; only `.ts` members
are staged and published. The example uses `ref://:9090`, so RuleGo Server must
set `share_http_server = true`. Set `publicOrigin` in the
`manifest-public-origin` JavaScript node to the externally reachable RuleGo
origin before deployment.

The example uses only `indexed-media:manifest:` and `indexed-media:lease:`
graph-cache namespaces. They start cold after migration and follow the ordinary
resolve/inspect path on a miss; there is no old-namespace lookup. Complete-lease
hashing, paired media revisions, the `indexed-ts-v2` output profile, resource keys
and URLs, and `resourceOrigin` storage roots are unchanged. Single-track requests
use distinct revisions with the same TS profile. Callers supporting track
selection must include it in any cache key used before inspection. The shipped
YouTube example continues to select a pair. Old short-lived cache
entries may expire naturally; retained media data is not moved or deleted.

## Breaking migration in v0.3.0

The source module identity is `github.com/killbus/rulego-indexed-media`. This is
a deliberate breaking rename: there is no `indexedVod` alias, forwarding module,
old-artifact selector, or cache fallback. Rules using the old public type must
be migrated; update each shipped owner ID and every matching reference together.

The hosted repository is now
[`killbus/rulego-indexed-media`](https://github.com/killbus/rulego-indexed-media).
Version `v0.3.0` uses the new module, component, artifact and cache names and adds
caller-selected video-only, audio-only and paired TS production. Update repository
remotes and download automation to the new name. The local checkout directory
need not change.

1. Select the versioned plugin and matching sidecars from the new repository.
   Verify its checksum and Plugin ABI against the intended runtime. Do not
   relabel an older plugin or load both old and new producers together.
2. The deployment/rule owner inventories download automation, installed plugins,
   node pools, stored/exported rules, and editor templates. Stage the matched
   artifact and configuration, load only the intended producer, and verify the
   exact new-only registry, owner/ref save-readback, peer components/processors,
   bounded production, and paired-track HLS playback before completing cutover.
3. Roll back with a matched prior artifact/sidecars and prior rule/node-pool set,
   not by loading both producers or adding an alias. Preserve retained media data.
