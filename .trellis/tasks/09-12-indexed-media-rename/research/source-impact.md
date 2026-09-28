# Source impact: indexed media naming

## Evidence boundary

Inspected on 2026-09-12 at source commit
ab2217bccb46b8ca854b778ef82cd805e6941cb9 in
D:/Repositories/rulego-indexed-vod. These are source/configuration observations,
not a build, deployment, playback result, or assertion about unknown consumers.
The user confirmed that old-name compatibility is not required.

## Naming surfaces and intended treatment

| Surface and evidence | Planned treatment |
| --- | --- |
| go.mod:1: github.com/killbus/rulego-indexed-vod | Change module identity to github.com/killbus/rulego-indexed-media; no forwarding module. |
| node.go:22, node.go:42, node.go:102, plugin.go:13 | Register only indexedMedia, use indexedMediaNode, display Indexed Media; update naming in initialization errors. |
| README.md:24, README.md:37, testdata/smoke-node-pool.json:10, testdata/smoke-borrower-chain.json:13, tests/e2e/node-pool.json:22 | Rename shipped owner IDs to indexed-media and every matching ref://indexed-media; arbitrary user-selected instance IDs remain valid. |
| examples/youtube-hls/chain.json:141, examples/youtube-hls/chain.json:282, examples/youtube-hls/chain.json:358, examples/youtube-hls/chain.json:374 | Change all node types and their owner references together. Preserve message shapes and graph topology. |
| examples/youtube-hls/chain.json:58, examples/youtube-hls/chain.json:161, examples/youtube-hls/chain.json:186, examples/youtube-hls/chain.json:250, examples/youtube-hls/chain.json:259, examples/youtube-hls/chain.json:268, examples/youtube-hls/chain.json:277, examples/youtube-hls/chain.json:302, examples/youtube-hls/chain.json:369, examples/youtube-hls/chain.json:408 | Rename every indexed-vod: graph cache read/write/invalidation prefix to indexed-media:; no legacy cache read. Preserve TTLs and key suffixes. |
| node_test.go:17, node_test.go:414, node_test.go:415, node_test.go:575, node_test.go:671 | Update contract, graph/cache, and shared-owner tests; add explicit rejection of the old public type. |
| .github/workflows/ci.yml:71, .github/workflows/ci.yml:131, .github/workflows/ci.yml:132, .github/workflows/ci.yml:138, .github/workflows/ci.yml:201 | Rename artifact prefix to indexed-media-rulego-v<VERSION>-linux-<arch> and update registration, owner lookup, and E2E artifact selection together. Keep .so, .sha256, and .abi.json aligned. |
| tests/e2e-hls-seek.sh:10, tests/e2e-hls-seek.sh:24, tests/e2e-hls-seek.sh:177 | Rename candidate/scratch labels and registration checks, retaining the hermetic paired-track test. |
| producer.go:62, producer.go:68 | Rename temporary-file prefixes only; preserve same-directory cleanup, permissions, and bounded IO. |
| .trellis/spec/backend/quality-guidelines.md, README and example docs | Update active naming and document the current paired-track limitation; preserve contract meaning. |

Inventory coordinates describe the inspected baseline and may shift during later
edits. Search all tracked source, examples, tests, CI, and current docs again at
implementation time. Do not rewrite task/workspace history or the historical
contributor attribution at LICENSE:3. The HLS protocol value
#EXT-X-PLAYLIST-TYPE:VOD is not a plugin name.

## Media and ownership invariants

- node.go:195 routes both operations through decoding and source validation.
  node.go:241 declares func (source mediaLease) validate() error; its required
  pair check at node.go:245 is:

  ~~~go
  if !source.Video.valid(true) || !source.Audio.valid(false) || source.Video.URL == source.Audio.URL {
      return errors.New("invalid indexed media lease")
  }
  ~~~

- node.go:251 validates declared H.264/MP4 video and AAC/M4A or MP4 audio. This
  is not evidence of support for arbitrary formats or of probing actual bytes.
- source.go:73: Inspect derives the segment timeline from the video index.
  source.go:162: inspectSource discovers both indexes.
- producer.go:53: produceOnce selects video plus overlapping audio ranges;
  producer.go:104 maps 0:v:0 and 1:a:0, with copy/shortest MPEG-TS output.
  Renaming or relaxing validation alone cannot provide single-track support.
- source.go:147: func leaseKey(source mediaLease) string hashes JSON of the
  complete normalized lease, including URLs and headers. Keep this internal
  access cache unchanged; graph cache namespace strings are a different layer.
- source.go:249: func sourceRevision(source mediaLease, videoIndex, audioIndex mediaIndex) string
  hashes JSON of sourceKey and lowercased container/codec facts, then for video
  followed by audio hashes big-endian uint64 InitSize, uint64 raw-index length,
  and raw index bytes. No project-name salt is present. Preserve the entire
  algorithm, order, and URL/header exclusion.
- Preserve the example's indexed-ts-v2 output-profile/resource-key suffix,
  request URLs, resolver format policy, source keys, resource IDs, and publication
  limits. resource-origin/staging belongs to the resource owner; do not migrate
  retained data roots or storage when changing an example owner ID.
- Provider resolution, resource acquisition/publication, remote ffmpeg execution,
  HTTP serving, and consumer interpretation retain their existing owners.

## Build and release boundaries

- go.mod uses Go 1.25.0, ffmpeg client v0.5.0, and RuleGo
  v0.36.1-0.20260802040353-2ec085f29027; do not combine the rename with upgrades.
- VERSION and compatibility.json.pluginVersion are 0.2.0;
  compatibility.json.rulegoRelease is v0.37.0. Release numbering is a later
  release decision, not permission to overwrite existing assets.
- CI already owns Go formatting/vet/unit/race checks, pinned-ABI plugin builds
  for amd64/arm64, isolated registration/ref smoke, and a hermetic HLS seek test
  with ffmpeg-over-ip v0.5.1 and resource-origin v0.1.1 peer plugins.
- .github/workflows/release.yml uses dynamic GITHUB_REPOSITORY, verifies
  CI:success, exact CI head SHA and release tag/VERSION, validates checksums,
  and publishes the tested artifacts without rebuilding. Preserve these gates.
- The new artifact must identify the new Go module and retain the existing host
  ABI evidence. Merely changing a .so filename does not establish module identity.
- Updating local source does not rename GitHub, change remotes, publish artifacts,
  modify installed plugins, or prove live consumers have migrated. Those actions
  need separate authorization and verified current targets.

## Planned regression evidence

- Extend TestPluginContract to assert exactly one new type/label and exercise
  real registry/graph rejection of indexedVod without registering an alias.
- Extend TestStrictGenericRequestDecoding with missing-video and missing-audio
  cases for both inspect and produce; valid paired leases remain accepted.
- Preserve TestRevisionExcludesLeaseURLsAndHeaders and add a fixed revision
  fixture to guard exact hash stability across the rename.
- Preserve complete-lease singleflight/cache, refreshed credentials, stale-source
  invalidation, bounded Range/production, deadline/path/output cleanup, and typed
  error tests. Update graph cache tests coherently, covering cold miss, warm hit,
  stale invalidation, and no fallback to populated old namespaces.
- Preserve TestExampleScriptsCompile, public-origin and signed-URL tests,
  TestRuleGoSharedOwnerAndBorrower, and full paired-track HLS seek CI.
- These are planned checks. None was executed during this planning session.
