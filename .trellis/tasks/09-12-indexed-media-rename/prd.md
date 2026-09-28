# Rename indexed VOD to indexed media

## Goal

Align the project's name with its owned capability: consume indexed media
representations and produce requested playable media segments with bounded work.
This lets users understand the responsibility independently of today's paired
audio/video implementation and leaves a coherent boundary for future video-only,
audio-only, or combined compositions.

This is a naming change, not single-track implementation. The rename must preserve
the current media behavior and make its support limits explicit.

## Status and Artifacts

- Status is in progress. On 2026-09-12 the user confirmed no compatibility and
  explicitly approved implementation with `开始`; the reviewed existing task was
  activated with `task.py start`. Local source, example, CI-definition, test, and
  related guidance edits are authorized. On 2026-09-28 the user additionally
  approved the specific commit/push/draft-PR and CI follow-up proposal with `授权`.
  Remote rename, merge, release, installation, and deployment remain separate.
- [design.md](design.md) defines naming, invariants, migration, and rollback.
- [implement.md](implement.md) defines ordered execution and verification.
- implement.jsonl and check.jsonl contain curated spec/research context.
- Evidence: [source impact](research/source-impact.md),
  [downstream impact](research/downstream-impact.md), and
  [session recovery](research/session-recovery.md).
- Continuation evidence (2026-09-28): [local acceptance](research/acceptance-evidence.md)
  and [proposed commit/CI handoff](research/commit-plan.md). Source docs and
  delivery boundaries are reviewed; compiled/runtime acceptance passed in
  [CI run 36389841942](https://github.com/killbus/rulego-indexed-vod/actions/runs/36389841942).
  [Draft PR #1](https://github.com/killbus/rulego-indexed-vod/pull/1) is ready for review.

## Background

- Both `inspect` and `produce` require separate valid video and audio leases.
- The declared formats are H.264 in MP4 and AAC in M4A/MP4, with indexed Range
  access; the timeline is video-primary and production maps both streams into TS.
- The single-representation index/range primitives do not inherently require a
  pair, but the current public contract and production path do.
- These are source-level findings, not evidence of a deployed version or of
  existing audio-only/video-only support.
- The downstream search found two current references in engineering guidance and
  no direct naming matches in the current host, resource-origin, or ffmpeg-over-ip
  sibling source/configuration. Installed artifacts, saved rules, external
  installers, and remote repository state were not inspected.

## Naming Decision

| Surface | Current | Planned |
| --- | --- | --- |
| Repository | `rulego-indexed-vod` | `rulego-indexed-media` |
| Go module | `github.com/killbus/rulego-indexed-vod` | `github.com/killbus/rulego-indexed-media` |
| RuleGo node type | `indexedVod` | `indexedMedia` |
| Display label | `Indexed VOD` | `Indexed Media` |
| Shipped shared-instance ID | indexed-vod | indexed-media |
| Artifact prefix | indexed-vod-rulego-v | indexed-media-rulego-v |

Retain `indexed` to describe the mechanism enabling bounded access. Use `media`
to avoid making a video-plus-audio implementation the permanent capability
boundary. These names do not promise arbitrary codecs, containers, or protocols.

The user confirmed on 2026-09-12 that no old-name compatibility is required.
Use a coordinated breaking rename: no indexedVod alias, forwarding module,
dual-name configuration lookup, or legacy artifact/cache fallback. Old callers
must update. The user approved this complete naming map for implementation.

## Requirements

1. R1 - Describe responsibility by the indexed-to-playable-segment transformation.
   Current documentation must distinguish the supported paired-track input from
   future single-track extensions.
2. R2 - Apply the naming map consistently across source/registration, module,
   docs, examples, tests, temporary labels, CI artifacts, and active local guidance.
   Use the recorded source/downstream inventory and classify remaining matches as
   migration explanation, negative tests, protocol terms, or historical evidence.
3. R3 - Keep public type, shared-instance ID, and storage path distinct. Update
   every shipped reference when an instance ID changes. Arbitrary valid instance
   IDs remain supported; retained resource/staging roots must not be migrated.
4. R4 - Enforce the confirmed no-compatibility policy at plugin registration,
   graph loading, configuration, and artifact selection. Identify actual deployed
   callers before a separately authorized cutover; do not assume all are known.
5. R5 - Preserve bounded paired H.264/AAC production, complete-lease caching,
   revision evidence, typed errors, and resource lifecycle. Rename the example's
   non-durable graph cache namespaces coherently, accepting a cold cache; preserve
   its output-profile/resource keys and all internal identity algorithms.
6. R6 - Keep provider resolution/format selection in the resolver/composition;
   resource acquisition/publication in resourceOrigin; remote execution in the
   ffmpeg client/service; HTTP serving and consumer interpretation in their owners.
7. R7 - Verify new registration and old-type rejection, shared-owner graphs,
   module/build identity, pinned host ABI loading, peer coexistence, and paired
   HLS behavior. Preserve current dependencies and release integrity gates.
8. R8 - Separate local source readiness from remote repository rename, publication,
   installation, and live-rule cutover. Each external stage requires its normal
   explicit authorization and current evidence. Update the engineering authority
   URL only once the new repository identity actually exists.

## In Scope

- One coherent source-owned rename, regression tests, current documentation,
  shipped configuration, and CI/release artifact naming.
- Engineering capability wording alignment; the ownership-route update is
  conditional on the separately authorized repository-host rename.
- A migration/rollback checklist assigning external work to the relevant owners,
  without performing those actions during source implementation approval alone.

## Out of Scope

- Implementing optional tracks, changing the timeline model or media identity,
  or adding single-track ffmpeg/output handling in this naming task.
- Adding codecs, containers, protocols, transparent original-byte Range proxying,
  a streaming `audio.m4a` endpoint, or a new service/session owner.
- Rewriting the existing `09-11-indexed-audio` task's zero-plugin-change constraint
  without a separate decision in that task's owning planning context.
- Dependency/ABI upgrades, unrelated host/peer-plugin edits, persistent storage
  migration, or historical task/journal/legal-attribution rewriting.
- Physical workspace moves, GitHub changes, commits, releases, installations, or
  live deployments without separate explicit authorization.

## Acceptance Criteria

- [x] A0 (planning gate): PRD, design, execution plan, impact/owner inventory, and
  both real context manifests are present and validated; the user explicitly
  approves the latest final summary before activation.
- [x] A1 (R1, R2): Current docs state the new capability/name and current support
  matrix without claiming single-track support. Remaining old-name matches are
  explained by an explicit historical/protocol/migration/negative-test exception.
- [x] A2 (R2, R4, R7): Module metadata identifies the new module; the plugin
  exports exactly one indexedMedia component with the new label and unchanged
  relations. The old indexedVod type is absent and an old-type rule fails to load.
- [x] A3 (R3, R5): All shipped owner/ref pairs load correctly. New graph cache
  readers/writers/invalidation agree, old namespaces are not fallback inputs, and
  cold-cache recovery still follows ordinary resolution/inspection.
- [x] A4 (R5, R6): Revision fixtures and existing lease, Range, production,
  deadline/path/cleanup/error tests pass; missing either track is still rejected.
  No resource identity, output-profile tag, or owned data root changes.
- [x] A5 (R2, R7): Owning CI passes formatting/vet/unit/race checks, builds both
  architectures with aligned new-name artifacts and verified sidecars, loads the
  expected unprefixed type in the pinned runtime, and passes the paired-track HLS
  seek/peer-coexistence test. Evidence identifies the tested source revision.
- [x] A6 (R8): Handoff clearly separates local source/CI evidence from deferred
  remote rename, release version, installers, saved rules, and deployment checks.
  No external action or unexecuted check is reported as completed.

## Risks and Deferred Work

- This is deliberately breaking: plugin/configuration mismatches fail to load.
  A later cutover and rollback must move a matched artifact and rule set together.
- Renamed graph cache namespaces start cold. This affects reuse only, not durable
  media identity, because missing cache entries already resolve a fresh lease.
- A broader name may be mistaken for broader format/track support; A1 and A4 guard
  that boundary. Follow-up single-track work needs its own input, timeline,
  revision, and output-format/stream-mapping design and approval.
- Remote repository availability, installed filenames/registry, external
  download automation, saved rules, release numbering, and deployed playback are
  deferred to separately authorized delivery. Local source work does not prove
  that the ecosystem or deployed consumers have migrated.
