# Design: breaking indexed-media rename

## Objective and boundary

Implement the naming map in the PRD without changing the indexed-representation
to bounded playable-segment transformation. This is a coordinated breaking rename,
not a compatibility shim or single-track feature. Source implementation can be
reviewed before any separately authorized GitHub, release, or deployment action.

The existing data flow is unchanged:

1. Resolver/rule composition selects and normalizes the current video/audio lease.
2. indexedMedia inspects immutable indexes and reports revision/segment metadata.
3. resourceOrigin owns acquisition, staging limits, publication, and retention.
4. indexedMedia produces the requested bounded paired-track TS member using the
   existing ffmpeg client/service, then returns control to the composition.
5. Resource/HTTP owners and the consumer retain serving/playback responsibility.

No new registry owner, cache service, provider adapter, or data model is introduced.

## Public and internal naming

- Change go.mod to github.com/killbus/rulego-indexed-media. Keep package main,
  the exported plugin entry point, dependencies, ABI pins, and version metadata
  unchanged for this source-only stage.
- Register exactly indexedMedia, use the internal indexedMediaNode type, and
  display Indexed Media. Preserve configuration/message fields, operations,
  Success/Failure relations, and typed failure kinds.
- Do not register indexedVod, publish a forwarding module, translate old types,
  or add old-name fallback branches. A rule using indexedVod must fail to load
  with the renamed plugin alone.
- Change shipped owner IDs from indexed-vod to indexed-media and all matching
  ref:// references together. IDs are caller-selected graph keys, not aliases
  for component types; generic IDs such as owner remain valid.
- Rename plugin-specific diagnostic, scratch, and temporary-file labels. Update
  example scratch directories only where the source owns their creation; do not
  rename resourceOrigin roots, staging paths, or existing user storage.
- Update current project docs to separate capability from current support. Keep
  HLS protocol terminology, historical task/journal evidence, and LICENSE
  contributor attribution intact. Migration explanations and negative tests may
  intentionally contain old names; active old-name configuration may not.

## Cache and identity decisions

| Layer | Decision and reason |
| --- | --- |
| Example graph cache | Rename indexed-vod: to indexed-media: in every manifest/lease reader, writer, and invalidator. These short-lived hints can start cold; adding legacy reads would contradict the chosen migration policy. |
| Complete-lease bundle cache | Keep leaseKey unchanged. Access URLs/headers and their verified indexes must remain atomic; this is not the graph namespace. |
| Media revision | Keep sourceRevision byte-for-byte equivalent, including track order, normalized format facts, lengths, and index bytes. The algorithm has no project-name salt. |
| Resource/output identity | Keep indexed-ts-v2, resource/source keys, IDs, segment numbering, URL shapes, and publication limits unchanged. Renaming must not invalidate durable media identity. |

Do not flush, move, or migrate retained data. Old graph-cache entries may expire
naturally; their existence must not affect correctness under the new namespace.

## Media behavior invariants

- Inspect and produce still require distinct valid H.264/MP4 video and AAC/M4A
  or MP4 audio representations; a missing track remains invalid.
- The video index remains the primary timeline. Audio range overlap selection,
  bounded index/Range reads, and remote stream-copy MPEG-TS production are unchanged.
- Lease normalization, retries, source_stale invalidation, revision_changed,
  request cancellation/deadlines, path/byte limits, cleanup, and error redaction
  retain the contracts recorded in source-impact.md and backend quality guidance.
- A future single-track task must separately design validation, timeline choice,
  revision evidence, and output mapping. It is not unblocked merely by this rename.

## Artifacts, loading, and CI

- Candidate artifacts become indexed-media-rulego-v<VERSION>-linux-<arch>.so,
  with matching .sha256 and .abi.json sidecars for amd64 and arm64.
- Keep the existing pinned SDK/runtime and peer-plugin versions. Verify the
  candidate's embedded Go module metadata in addition to checksum/ABI sidecars;
  changing the filename alone is insufficient.
- The existing CI smoke copies the versioned artifact into /app/data/plugins
  without renaming its basename. Keep that installation shape with the new name,
  and assert the exact unprefixed indexedMedia registry type, absence of indexedVod,
  the indexed-media node-pool owner, and successful ref borrower save/readback.
- The existing E2E harness copies the producer to 01-indexed.so and peers to
  02-origin.so/03-ffmpeg.so. Those neutral load-order names need not change. Assert
  the same exact indexedMedia type there and retain peer component/processor and
  paired-track HLS seek checks. These tests validate loading; filenames do not
  themselves define the expected public registry type.
- Update all candidate artifact selectors together, including the E2E environment
  variable and release metadata examples. release.yml already uses the dynamic
  repository, exact successful-CI revision/tag checks, and no-rebuild publication;
  preserve this mechanism rather than invent a second publisher.
- Local planning/source checks are not CI or live-runtime evidence. Record source
  revision, CI result, platform, ABI, and artifact identity when those checks run.

## Downstream ownership and staged cutover

### Stage 1: source readiness after implementation approval

The source repository owns code, examples, tests, build naming, and source specs.
Main aligns the engineering capability phrase at
D:/Repositories/rulego-engineering/.trellis/spec/guides/rulego-capability-design.md:53
through the normal spec-review phase. The bounded sibling scan found no direct
host/resource-origin/ffmpeg-over-ip naming edits to make.

Keep the live ownership route at
D:/Repositories/rulego-engineering/.trellis/spec/guides/rulego-ownership.md:14
pointing to the actual existing repository until Stage 2 is authorized and complete.
The local checkout directory may retain its old name; Go module identity does not
require a filesystem move. Document that the source target name and hosted
repository state can differ during this preparation stage.

### Stage 2: separately authorized repository and release work

The repository/release owner must confirm the target repository name is available,
rename the hosted repository, verify the actual new URL, reconcile relevant remotes
and automation, and then update the engineering authority route. Do not rely on
host redirects as intentional compatibility or rewrite history. Select a fresh
release version and publish only exact tested candidate artifacts; never relabel
or overwrite an old release to pretend it implements the rename.

### Stage 3: separately authorized installation and rule cutover

The deployment/rule-chain owner inventories real plugin files, download automation,
node pools, stored/exported rules, and editor templates. Stage the matched new
artifact/sidecars/configuration, ensure only the intended producer binary is loaded,
and verify registry, rules, references, bounded invocation, and paired playback.
Replacing installed files or restarting a runtime requires actual target checks
and authorization. No persistent media data is deleted as part of this cutover.

The existing indexed-audio plan remains independent with its zero-plugin-change
constraint; historical deployment observations are inventory leads, not fresh proof.

## Validation and rollback

PRD A1-A6 map to the execution checklist. Main/test review must cover both new-name
success and old-type rejection, not merely search-and-replace completeness.
Snapshot a known revision digest before naming edits and use a fixed regression
fixture. Test cache cold/warm/stale behavior under the new namespace and ensure
populated old namespace keys do not become fallback inputs.

Before a source commit, use ordinary scoped edits and preserve user changes; do
not use destructive Git resets. An authorized release/deployment rollback is a
matched prior artifact plus prior rule/node-pool configuration, not dual loading
or an alias layer. Because identity/storage do not change, no schema/data rollback
is designed here. Record any CI or external-delivery work still awaiting authority
without reporting the overall deployed migration as complete.
