# Quality Guidelines

> Code quality standards for backend development.

---

## Overview

<!--
Document your project's quality standards here.

Questions to answer:
- What patterns are forbidden?
- What linting rules do you enforce?
- What are your testing requirements?
- What code review standards apply?
-->

(To be filled by the team)

---

## Forbidden Patterns

<!-- Patterns that should never be used and why -->

(To be filled by the team)

---

## Required Patterns

<!-- Patterns that must always be used -->

(To be filled by the team)

---

## Testing Requirements

<!-- What level of testing is expected -->

(To be filled by the team)

---

## Code Review Checklist

<!-- What reviewers should check -->

(To be filled by the team)

## Scenario: RuleGo routing-node handoff

### 1. Scope / Trigger

Apply this contract when a rule chain routes a `resourceOrigin` descriptor
through `jsSwitch` and then builds another origin request.

### 2. Signatures

- `resourceOrigin.acquire` returns a descriptor containing `resourceId` in the
  message body.
- `jsSwitch` returns relation names; it does not publish mutations made to its
  local `metadata` value.
- The next `jsTransform` must read the parent ID from `msg.resourceId`.

### 3. Contracts

- Keep request context such as video ID, revision, and segment in metadata.
- Keep resource lifecycle identifiers in the current message until a transform
  deliberately copies them for a later asynchronous production branch.
- Manifest and member routes must have distinguishable static shapes, for
  example `/youtube/:videoId/index.m3u8` and
  `/youtube/:videoId/segments/:revision/:segment`.

### 4. Validation & Error Matrix

- Empty parent `resourceId` -> `resourceOrigin.resolve` returns `invalid_input`.
- Missing parent resource -> child acquisition returns `parent_unavailable`.
- Segment `0` -> resolve `0.ts` from the manifest resource.
- Segment `N > 0` -> acquire a child with the current parent `resourceId`.

### 5. Good / Base / Bad Cases

- Good: initial and child branches read the descriptor from `msg.resourceId`.
- Base: request metadata survives routing unchanged.
- Bad: a `jsSwitch` mutates metadata and a downstream node depends on that
  mutation.

### 6. Tests Required

- Run the switch and downstream transform in a real in-memory RuleGo graph and
  assert the emitted request contains the input parent ID.
- Load the complete example chain to catch HTTP-router path conflicts.
- Verify segment `0` and one demand segment return a ready resource at runtime.

### 7. Wrong vs Correct

```javascript
// Wrong: the switch mutation is not an output contract.
metadata.parentResourceId = String(msg.resourceId)
return ['Initial']

// Correct: consume the descriptor in the downstream transform.
return {msg: {operation: 'resolve', resourceId: String(msg.resourceId), member: '0.ts'}, metadata: metadata}
```

## Scenario: Indexed-media access leases

### 1. Scope / Trigger

Apply this contract when maintaining indexed-media registration, input validation,
or immutable index caching while callers refresh representation URLs or headers.

### 2. Signatures

- RuleGo type: `indexedMedia`, label `Indexed Media`, operations `inspect` and
  `produce`, and relations `Success` / `Failure`.
- Go module: `github.com/killbus/rulego-indexed-media`. Candidate artifacts use
  `indexed-media-rulego-v<VERSION>-linux-<arch>.so` with matching checksum and ABI
  sidecars; a renamed filename does not establish the embedded module identity.
- Input lease: `sourceKey`, plus video/audio `url`, `headers`, `container`, and
  `codec`.
- Inspection result: stable `revision`, `duration`, and segment durations.
- Production input: the complete current lease, `expectedRevision`, segment,
  and origin-issued staging limits.

### 3. Contracts

- Export only the current registered type. Component type, caller-selected node
  ID, and storage root are separate identities; shipped owners use `indexed-media`
  and borrowers use `ref://indexed-media`, while other valid caller IDs work.
- Both operations require distinct valid H.264/MP4 video and AAC/M4A-or-MP4 audio
  representations. The media-wide name does not make either track optional.
- Cache and singleflight inspection by the complete normalized lease.
- A RuleGo composition may memoize the complete normalized lease as a short
  lived performance hint. The YouTube example uses
  `indexed-media:lease:<videoId>:<revision>` in `ChainCache`; a hit bypasses the
  resolver, while a miss still follows the ordinary resolve and inspect path.
- All manifest/lease cache readers, writers, and invalidators share the
  `indexed-media:` namespace. Other namespaces are not fallback inputs; a cold
  cache follows normal resolution without moving retained resource data.
- Resolver-lease cache state is neither identity nor durable state. Its TTL
  must be shorter than the expected provider lease, and a RuleGo restart must
  remain correct by resolving a fresh lease on the resulting cache miss.
- Evict a cached lease after `source_stale` so refresh re-inspects it.
- Exclude URLs and headers from revision evidence.
- Reinspect a changed lease, then require its revision to match before
  production.
- Use only the current request's URLs and headers for media Range reads.

### 4. Validation & Error Matrix

- Unregistered component type -> graph-load failure, not an alias lookup.
- Missing video or audio representation -> `invalid_input` for either operation.
- 401/403/404/410 from a representation -> `source_stale`.
- Missing, expired, or restart-lost RuleGo lease cache -> resolve and inspect.
- Refreshed lease with different immutable evidence -> `revision_changed`.
- Disconnect, 429, or transient 5xx -> bounded retry.
- Invalid SIDX, codec, container, range, path, deadline, or byte bound ->
  terminal typed failure.

### 5. Good / Base / Bad Cases

- Good: a refreshed URL is inspected and reproduces the same revision; later
  member requests reuse that exact lease for its short TTL.
- Base: a cache miss resolves again, and repeated use of an identical lease
  reuses its inspected bundle.
- Bad: a new session/plugin owner is introduced only to retain resolver output,
  or a cache lookup by `sourceKey` returns old signed URLs for a new lease.

### 6. Tests Required

- Assert exactly one exported `indexedMedia` type with the expected label and
  relations. In an isolated registry, load the new type and reject `indexedVod`.
- Load shipped owner/ref graphs and retain coverage for arbitrary valid owner IDs.
- Reject each missing track for inspect and produce, with a valid paired control.
- Assert concurrent identical leases perform one inspection.
- Execute the RuleGo cache writer and reader in separate traversals and assert
  the second traversal emits `produce` without entering the resolver branch.
- Prepopulate old manifest/lease namespaces and assert normal resolution. Cover
  cold miss, current-namespace write/warm hit, and stale eviction of both keys.
- Assert changed URLs/headers cause inspection and are used by production.
- Assert transient access fields do not alter revision, while changed indexes
  do.
- Pin a literal revision digest using distinct video/audio indexes and init sizes
  to guard the exact hash framing and track order, not only relative equality.
- Assert stale access and revision change remain distinct failures.
- CI verifies embedded module identity, new-only registration, shared references,
  matching-host ABI on both architectures, peer coexistence, and paired HLS seek.

### 7. Wrong vs Correct

```go
// Wrong: stable identity accidentally selects stale credentials.
bundle := bundles[lease.SourceKey]

// Correct: access data and its verified indexes remain atomic.
bundle := bundles[hashCompleteLease(lease)]
```
