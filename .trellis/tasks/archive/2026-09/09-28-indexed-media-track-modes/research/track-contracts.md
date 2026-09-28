# Research: Indexed-media track contracts

- Query: Which existing validation, index discovery, timeline, revision, lease-cache, and production track-selection contracts must change to support video-only, audio-only, and paired input, while preserving paired behavior and its literal revision fixture?
- Scope: Internal repository research and planning only. Output/container selection and downstream integration belong to the main session and are not evaluated here.
- Date: 2026-09-28
- Method: Read the task PRD, project workflow, backend quality spec, relevant production code, and existing tests. No code edits, test execution, git operations, or task activation. All anchors below are repository-relative.

## Findings

### Files found

| File | Relevant responsibility |
| --- | --- |
| `.trellis/tasks/09-28-indexed-media-track-modes/prd.md` | Requirements for three selected track combinations, identity, bounded production, and paired compatibility; output choice remains open. |
| `.trellis/workflow.md` | Planning/research phases and persistence requirement; research procedure begins at line 352. |
| `.trellis/spec/backend/quality-guidelines.md` | Existing paired-only input contract and lease/revision/cache invariants, beginning at line 109. |
| `.trellis/spec/backend/index.md` | Backend guideline index; documentation language is English. |
| `.trellis/spec/backend/error-handling.md` | Template only; concrete error behavior must be read from code. |
| `node.go` | Lease/request structs, strict decoding, representation validation, shared owner, typed response errors. |
| `source.go` | Cached inspection bundles, concurrent discovery, video-primary inspection, immutable revision hash. |
| `sidx.go` | Bounded index discovery, SIDX parser, timestamps, index Range requests and header checks. |
| `producer.go` | Revision gate, selected input ranges, audio overlap, FFmpeg input mapping, limits and cleanup. |
| `source_test.go` | Paired helper fixtures, lease cache/singleflight tests, revision digest fixture, decoding and HTTP failures. |
| `sidx_test.go` | Synthetic SIDX builder, direct-reference checks, overlap selection and overflow comparison. |
| `producer_test.go` | Bounded production, refreshed credentials, revision/stale errors, retry/path/deadline regressions. |
| `node_test.go` | Real RuleGo node validation and shared-owner coverage; current missing-track rejection at line 834. |
| `README.md` | Public paired lease and video timeline description at lines 59 and 86. |
| `go.mod` | Module and Go/RuleGo/FFmpeg client dependency versions. |

### Facts: input presence and validation

1. `mediaLease.Video` and `.Audio` are value structs, not optional pointers (`node.go:47`). There is no explicit track-set or mode field. Missing fields, `null`, and empty objects can all leave a zero-valued representation when decoding a fresh request. Current mandatory validation masks that distinction.
2. Both operations enter the same `Source.validate()` through `decodeRequest` (`node.go:222`). It requires a nonempty, unpadded source key of at most 4096 bytes without NUL, valid video **and** audio, and unequal URL strings (`node.go:241`). Distinctness is a literal URL comparison, not a proof of distinct underlying streams.
3. Per-representation validation requires HTTP(S), a host, a nonempty URL no longer than 64 KiB, and no `|`. Video accepts MP4 with a case-insensitive `avc1` or `h264` prefix; audio accepts M4A/MP4 with a `mp4a.40.` prefix or exactly `aac` (`node.go:251`). These are declared string checks, not parsing/probing of the actual codec or stream kind. Prefix checks are broader than a complete codec identifier validator.
4. At most 128 headers are allowed; values are bounded at 16 KiB; token-style header names and no CR/LF in values are required (`node.go:267`, `sidx.go:209`). Range and Accept-Encoding supplied by a caller are ignored in favor of internally generated values for both discovery and media reads (`sidx.go:195`, `producer.go:225`).
5. Decoding caps the request at 8 MiB, rejects unknown fields and extra JSON values, and validates production revision syntax, nonnegative segment, staging path presence, positive bounded maxBytes, and a nonzero publishBy (`node.go:195`, `node.go:214`, `node.go:232`). Public decode failures become `invalid_input` via `OnMsg` (`node.go:139`). Internal manager methods do not independently call `Source.validate()` (`source.go:73`, `producer.go:21`).
6. Two existing suites intentionally reject each omitted track in both operations: `source_test.go:285` and `node_test.go:834`. Their paired controls must survive; their omission expectations must change for the new contract. Do not replace these with only tests of a private presence helper.

### Facts: discovery and immutable bundles

1. A bundle contains the complete lease and two value indexes (`source.go:21`). `inspectSource` unconditionally launches two goroutines, passes `requireSAP=true` for video and false for audio, receives exactly two results, and restores video/audio role order independently of completion order (`source.go:162`). Simply omitting one launch would leave the fixed two-result receive blocked.
2. Discovery begins with an exact 64 KiB Range read, walks at most 1024 top-level MP4 boxes, and caps a fetched SIDX at 1 MiB (`sidx.go:13`, `sidx.go:41`). It reads box headers outside the probe in 16-byte ranges (`sidx.go:79`). These are bounded index reads, not full-file downloads.
3. The first top-level SIDX is used. Its version must be 0 or 1, timescale positive, reference list nonempty and exact-length, references direct with nonzero size/duration, and offsets/times free of uint64 overflow (`sidx.go:109`, `sidx.go:123`, `sidx.go:127`, `sidx.go:151`, `sidx.go:165`). Video additionally requires starts-with-SAP and SAP type 1; audio does not (`sidx.go:168`). Making audio the primary timeline does not make it video or introduce a video SAP requirement.
4. `InitSize` is the absolute byte offset of SIDX. Media reference offsets start after SIDX plus first_offset, and times start at earliest_presentation_time (`sidx.go:154`, `sidx.go:175`, `sidx.go:179`). References advance contiguously in index time by their durations. Actual fragment timestamps and init-box contents are not validated here.
5. The discovery function waits for both results before examining errors (`source.go:179`); the first received error wins if both fail. Single-track support needs correct variable work counting and cancellation, but deterministic simultaneous-error precedence is not an existing guarantee.

### Facts: primary timeline and paired alignment

1. `Inspect` uses video reference count, video reference durations, and video timescale only (`source.go:82`). `Produce` bounds the requested segment against the same video list and selects exactly one video reference (`producer.go:54`). There is no mode-neutral primary-index helper today.
2. Each returned segment duration is `ref.Duration / Timescale`. Total duration is `last.Start + last.Duration`, divided by timescale (`sidx.go:33`, `source.go:88`). It is the final absolute index time, not necessarily the sum of segment durations. With earliest=5, durations=2000+3000, and timescale=1000, the parser fixture pins durationTicks=5005 (`sidx_test.go:59`); the durations sum to 5 seconds while the absolute end is 5.005 seconds.
3. Paired production includes every audio reference with a strict intersection with the video half-open interval. It uses 128-bit cross multiplication for rational comparisons, preserving distinct timescales without float rounding (`producer.go:140`, `producer.go:165`). Exact boundary-touching references are excluded. The only coverage rejection is zero overlapping references (`producer.go:159`); full start/end coverage is not verified.
4. Audio trim is video start time minus the first selected audio start time. Only a tiny negative value strictly between -1e-9 and zero is clamped (`producer.go:96`). Audio that begins materially later than video can still be selected and produce a negative trim. Partial end coverage can also pass. These are existing limitations, not newly approved alignment policies.
5. Existing overlap tests cover one minimum-reference case using equal timescales and one large-integer fraction comparison (`sidx_test.go:93`, `sidx_test.go:103`). They do not establish all required boundary, cross-timescale, partial-coverage, or audio-primary behavior.

### Facts: source revision and the compatibility fixture

The exact current hash preimage is (`source.go:249`):

```text
JSON({sourceKey, video:{container,codec}, audio:{container,codec}})
|| BE64(video.InitSize) || BE64(len(video.Raw)) || video.Raw
|| BE64(audio.InitSize) || BE64(len(audio.Raw)) || audio.Raw
```

- The JSON field order is supplied by the local facts structs; codecs/containers are lowercased (`source.go:250`, `source.go:273`). Access URLs and headers are absent. SIDX raw bytes, including encoded timing/offset/SAP fields, are evidence; independently mutated parsed `Refs` or `Timescale` fields are not separately hashed.
- `TestSourceRevisionFixedDigest` uses sourceKey `fixture:paired-index-v1`, mixed-case codec/container metadata, distinct init lengths, and distinct binary SIDX stand-ins. The required literal digest is `d6e815a371172793a89d537156d03db8394870361c9ac6624ff3510a1ed657f8` (`source_test.go:161`, `source_test.go:176`). Its comment pins pre-rename framing/order; no git history was read.
- **The fixture intentionally has no URLs.** It also omits parsed reference/timescale fields. Inferring track presence from a nonempty URL or successful request validation inside the hash helper would misclassify this fixture. Adding a universal mode/version prefix, adding fields to the paired facts JSON, switching absent/present encodings globally, or iterating tracks in goroutine completion order would change paired hashes.
- Current immutable evidence does **not** include initialization bytes themselves, media fragment bytes, an ETag, or a content digest (`source.go:249`). An init payload changed in place without changing its length or SIDX is not distinguished. R4 must be interpreted against this evidence envelope or explicitly expanded with a migration decision; preserving all paired digests rules out silently adding init-content hashing to the paired preimage.

### Facts: lease cache and production revision gate

1. Cache/singleflight key is SHA-256 of `json.Marshal(source)` for the complete decoded lease (`source.go:147`), not sourceKey or revision. It includes URLs, header maps, track slots, and the supplied codec/container strings. There is no separate lowercasing or header canonicalization in this helper. For example, nil versus empty header maps can create different lease keys even though they behave similarly on HTTP requests.
2. Cache hits return the atomic lease-plus-index bundle. Identical inflight leases share one discovery; changed access fields create a new discovery (`source.go:105`). At 256 entries an arbitrary map entry is evicted; the manager cache has no TTL (`source.go:19`, `source.go:132`). This is separate from composition-level resolver-lease TTL caching described by the backend spec; that downstream mechanism is outside this research scope.
3. Production checks deadline and staging confinement, obtains the bundle for the current complete lease, and checks expectedRevision before selected media reads and FFmpeg (`producer.go:21`, `producer.go:34`, `producer.go:38`, `producer.go:42`). A new lease may require index reads before this comparison; “before source read” in a test name must not be interpreted as “before any network discovery.”
4. `writeInput` receives URLs/headers from `bundle.source`, which is safe only while complete-lease lookup remains intact (`producer.go:76`, `producer.go:85`). On a media `source_stale` error, the exact matching cached bundle is invalidated; replacement bundles are protected by pointer comparison (`producer.go:47`, `source.go:153`). Failed discoveries are not cached (`source.go:131`).
5. Existing coverage: identical cold-lease singleflight (`source_test.go:47`), changed URL/header cache separation (`source_test.go:93`), owner cancellation (`source_test.go:119`), URL/header-independent revision (`source_test.go:136`), refreshed production credentials (`producer_test.go:101`), revision mismatch (`producer_test.go:147`), and stale invalidation (`producer_test.go:164`). Most use stubbed discovery, so they do not prove absent-track HTTP behavior or real discovery-plus-hash identity.

### Facts: production track mapping and bounds

1. Production always creates a video temp file and an audio temp file, copies init plus one video reference and all overlapping audio references, and closes/removes the temps (`producer.go:62`, `producer.go:76`, `producer.go:85`). Input limits are applied per temp input, not to the sum of both inputs (`producer.go:178`). No missing track may consume a temp file or source request under R3.
2. FFmpeg inputs are video at index 0 and trimmed audio at index 1. Required maps are `0:v:0` and `1:a:0`; the shared flags include copyts, stream copy, and shortest (`producer.go:100`). The existing paired result is `<segment>.ts`; this observation does not choose a single-track output contract.
3. FFmpeg runs through the existing shared manager's remote client with the configured credentials, dial timeout, deadline context, MaxOutputBytes, and a nil output callback (`producer.go:109`, `producer.go:116`). A change in selected tracks does not require a new owner, resolver, or production backend.
4. Preserve path resolution/confinement (`producer.go:303`), source-range validation and retries (`producer.go:205`), typed errors and sanitization (`node.go:297`, `producer.go:282`), output file checks (`producer.go:128`), and deferred temp-input removal. Temp inputs are cleaned on error, but `produceOnce` does not have a universal deferred removal of a partial output after terminal FFmpeg failure (`producer.go:114`); avoid claiming stronger existing cleanup coverage than the code provides.
5. Unit production fixtures contain `initdataVID` and stub FFmpeg by writing `mpeg-ts` (`producer_test.go:18`, `producer_test.go:76`). These prove control flow, not stream composition or playback. Actual-media acceptance remains required by PRD R6 and belongs with the main session's output work.

### Design options, not implementation decisions

#### Presence and validated selection

- Recommended direction: derive a validated selected track set once, then use that selection consistently in discovery, primary-index selection, revision dispatch, and production. Validate every supplied representation; do not use `URL != empty` or `valid()` as an instruction to silently skip a malformed supplied object. Keep same-URL rejection when both tracks are selected.
- Option A: pointer representation fields distinguish an omitted track from a supplied `{}`. A supplied empty/partial object must fail validation. Ordinary pointer decoding also treats `null` as absence; choose and document whether this is permitted.
- Option B: preserve explicit field presence/raw JSON alongside the representation to distinguish omission from `null` as well. If R1 means every supplied value must be an object, this enables rejecting `null`. Any custom decoder must preserve nested unknown-field rejection; an inner plain `json.Unmarshal` would bypass the outer decoder's DisallowUnknownFields policy.
- A zero-value-only presence test with the existing structs cannot distinguish omitted input from a malformed `{}` and is insufficient for the stated malformed-supplied requirement.
- No additional caller-supplied mode is necessary to infer the three supported combinations. If a public mode is introduced for other reasons, it needs an explicit consistency contract with supplied tracks.

#### Discovery, timeline, and input mapping

| Selected tracks | Proposed discovery | Proposed primary index | Proposed input selection and required map |
| --- | --- | --- | --- |
| Video + audio | Both; video requires SAP1, audio does not | Video, preserving existing segment numbering | One video reference plus existing overlapping audio selection; inputs 0 video and 1 audio, maps `0:v:0`, `1:a:0`. |
| Video only | Video only, requires SAP1 | Video | Init plus selected video reference; one input, map `0:v:0`. |
| Audio only | Audio only, no video SAP requirement | Audio | Init plus selected audio reference; one input, map `0:a:0`; no cross-track overlap or audio-to-video trim. |

- Drive goroutine count and result collection from selected tracks while retaining role labels, bounded requests, and owner/request cancellation. Zero tracks must already be rejected.
- Use the same primary index for inspect durations, segment count, range checks, and segment selection. Single-track production should consume exactly the chosen primary reference. Do not create an empty video index merely to pass an audio track through video-specific branches.
- A compatibility-oriented option uses existing absolute-end duration semantics for all modes. Another option gives new single-track modes duration equal to the sum of reference durations while retaining timestamps for media reads/production. The difference matters for nonzero earliest time and must be settled/documented; do not silently change paired duration during this task.
- Preserve paired overlap and FFmpeg argument behavior as the baseline. Stronger full-audio-coverage validation or normalization of negative trim would be an additional paired behavior change requiring its own explicit decision and regression evidence.
- The table concerns input assembly and required stream maps only. Member extension, muxer flags, timestamp output policy, and consumer integration for single-track results are intentionally left to the main session.

#### Revision compatibility and mode identity

- Preferred compatibility option: retain the exact current paired hash implementation as a legacy paired helper and keep its direct literal fixture. Dispatch validated single-track requests to a separately framed, versioned preimage that explicitly includes the selected role, sourceKey, stable metadata, and length-framed selected index evidence. A byte prefix or distinct schema can domain-separate it from the legacy paired JSON preimage. A concrete single-track schema/digest must be pinned when designed.
- Presence for this dispatch comes from validated selection or explicit optional fields, not transient URL content. If the lease struct changes to pointers, the fixture may need a mechanical pointer construction update while retaining the exact same facts, binary evidence, and literal expected digest. Do not add fake URLs merely to satisfy a new hash-side presence heuristic.
- Alternative: retain the existing two role-labelled JSON slots and binary slots for every mode, representing an absent track with canonical empty metadata plus zero init/raw lengths. This can distinguish valid modes without changing paired bytes, but it must explicitly require zero evidence for absent slots and forbid empty supplied objects. It creates a less explicit absent-slot convention that needs its own fixed fixtures.
- In either option, video-only, audio-only, and paired forms of the same sourceKey must differ; selected index/init-size/sourceKey/codec/container changes must differ; URLs and headers must remain excluded. Discovery completion order must never affect the preimage.
- Keep complete-lease cache keys and current-lease production reads. Pointer/omitempty changes should retain present paired serialization where feasible; there is no reason to replace the cache key with the stable revision. If absence has multiple accepted wire forms, choose a canonical internal representation so those forms cannot carry hidden stale access fields.

### Required regression inventory (planned, not written or run)

| Area | Cases and expected evidence | Existing anchor / gap |
| --- | --- | --- |
| Presence: success | Both operations accept valid paired, omitted-audio video-only, and omitted-video audio-only. Keep paired output and Success relation controls. | Update `source_test.go:285`, `node_test.go:834`. |
| Presence: zero tracks | Neither supplied; source absent/null/empty; both explicitly absent under the chosen null policy -> `invalid_input`, no discovery or FFmpeg. | Not covered by current omission loops. |
| Presence: malformed companion | Valid video plus `{}`/partial/invalid audio, and the symmetric case -> `invalid_input`, never successful single-track fallback. Include headers-only, codec-only, empty URL, and wrong JSON types. | Value structs currently conceal the empty-object/absence distinction (`node.go:47`). |
| Presence: null | Pin omitted vs explicit null for either/both fields according to the chosen policy. If null is absent, a remaining invalid object still fails. | Explicit design decision required. |
| Malformed request | Wrong operation, unknown top-level and nested fields, extra JSON value, invalid sourceKey, revision/segment/limits/deadline metadata; retain public typed Failure behavior. | `node.go:195`, `node.go:232`; partial coverage at `source_test.go:285`. |
| Malformed representation | Wrong URL scheme/host, overlong URL, pipe, unsupported codec/container for each role, excessive/invalid headers, paired identical URL. Cover both operations and singles as well as pairs. | `node.go:241`, `node.go:251`, `sidx.go:209`. |
| Absence: discovery | Real discovery through a recording HTTP transport: only selected endpoints requested; one or two correctly labelled indexes; reversed paired completion order; single-track completion without waiting for a nonexistent result. | `source.go:162`; current singleflight test stubs discovery. |
| Malformed index | Each selected role: zero timescale/count/duration/size, indirect reference, truncation, unsupported version, offset/time overflow, oversized/missing SIDX, bad box size. Reject malformed selected audio in a pair even if video is valid. | `sidx.go:41`, `sidx.go:109`; limited cases in `sidx_test.go:75`. |
| Role-specific SAP | Non-SAP or non-type-1 video rejected in both video modes; valid direct audio without video SAP flags accepted as audio-only and paired audio. | `sidx.go:168`; no positive audio-only parser/discovery case. |
| Small/bounded sources | Exact initial probe contract, SIDX inside and beyond probe, extended box headers, range ignoring/mismatched/oversized/short responses. Include real small audio files: a file shorter than the fixed 64 KiB probe currently cannot satisfy the exact requested range. | `sidx.go:13`, `sidx.go:79`, `source.go:203`, `producer.go:264`. |
| Identity: legacy | Retain the exact paired digest and URL-less fixture, distinct init lengths/raw bytes and canonical video-before-audio order. Keep URL/header refresh invariance and case normalization of stable metadata. | `source_test.go:136`, `source_test.go:161`. |
| Identity: selected set | Same sourceKey with video-only/audio-only/paired -> three distinct revisions. Add fixed single-track fixtures once framing is settled. An absent role contributes no accidental stale index/header/URL state. | New coverage required. |
| Identity: immutable changes | Change sourceKey, selected codec/container, init length, and raw SIDX evidence for each selected role -> changed revision. Change encoded timing/index bytes, not only a disconnected parsed Timescale/Refs field. | `source.go:249`; existing tests mutate only selected examples. |
| Identity: evidence limitation | Record that same-length changed init/media bytes with identical facts/SIDX are not detected today. If stronger detection is required, add evidence and migration coverage as an explicit scope change. | Existing preimage does not hash content (`source.go:249`). |
| Cache: isolation | Each mode: repeated identical lease reuses discovery; selected URL or headers change -> fresh inspection; same sourceKey/revision across modes cannot reuse a different selected set. Exercise concurrent identical and different-mode requests. | Extend `source_test.go:47`, `source_test.go:93`. |
| Cache: current credentials | Renew only video, only audio, or the sole selected track. Same index evidence reproduces revision; production sends refreshed URL/headers, never old credentials, and no absent-track requests. | Extend `producer_test.go:101` with real discovery/range instrumentation. |
| Cache: failures | A selected track's stale response invalidates the exact cached lease; unrelated modes/newer bundles survive. Failed discovery is not cached; owner close and waiting-request cancellation terminate for one or two tracks. | `source.go:119`, `source.go:131`, `source.go:153`; `source_test.go:119`, `producer_test.go:164`. |
| Revision gate | Switching mode or selected immutable evidence while retaining expectedRevision -> `revision_changed` after any needed index discovery, before media ranges/FFmpeg. Refreshed credentials with equal evidence succeed. | `producer.go:38`, `producer.go:42`; strengthen assertions beyond run count in `producer_test.go:147`. |
| Timing: primary | Audio-only inspect count/durations/segment bounds come from audio; video and paired remain video-primary. Cover first/middle/last segment, differing track counts/scales, and out-of-range/negative segment through public decode. | `source.go:82`, `producer.go:54`; new audio-primary cases. |
| Timing: earliest/precision | Zero and nonzero earliest timestamps, SIDX v1 large timestamps/offsets, positive durations, total duration vs sum under chosen policy, no overflow or accidental float-based overlap comparison. | `sidx_test.go:59`, `sidx_test.go:103`; v1 builder at `sidx_test.go:15`. |
| Timing: paired overlap | Different timescales (e.g. video 1000/audio 48000), exact shared boundaries, multiple audio refs, no overlap, audio starting earlier/later, audio ending early, and tiny negative trim. Distinguish preserving baseline behavior from approving stricter coverage. | `producer.go:96`, `producer.go:140`; limited baseline at `sidx_test.go:93`. |
| Production: selection | Only selected init/reference ranges and temp inputs; single audio maps `0:a:0`, single video `0:v:0`, paired maps unchanged. Required maps must not use optional-map syntax that hides malformed supplied media. | `producer.go:62`, `producer.go:100`; current fake runner does not inspect maps. |
| Production: safety | Per-input and output byte limits, current headers, bounded transient retries, terminal stale response, expired deadline/cancellation, confined/symlink-resolved paths, and input cleanup in each selected mode. Inspect temp cleanup after a selected input fails. | `producer_test.go:68`, `producer_test.go:129`, `producer_test.go:164`, `producer_test.go:188`, `producer_test.go:223`, `producer_test.go:241`. |

Real-media acceptance should verify actual selected stream presence and timing using the main session's chosen output contract; fake index bundles and a fake `mpeg-ts` file are insufficient. This research does not specify muxers, manifests, HTTP endpoints, or downstream CI architecture.

### Related specs and requirements

- PRD R1/R3 require rejecting malformed supplied tracks and never fetching absent representations (`.trellis/tasks/09-28-indexed-media-track-modes/prd.md:23`). R4/R5 require selected-set identity, fresh credentials, paired compatibility and retained limits (`.trellis/tasks/09-28-indexed-media-track-modes/prd.md:29`).
- The current backend spec explicitly requires both tracks and expects missing-track failures (`.trellis/spec/backend/quality-guidelines.md:134`, `.trellis/spec/backend/quality-guidelines.md:156`, `.trellis/spec/backend/quality-guidelines.md:178`). These clauses describe the old behavior and need an authorized spec update alongside any implementation; this research does not modify them.
- Retain complete-lease cache and revision/access separation (`.trellis/spec/backend/quality-guidelines.md:136`, `.trellis/spec/backend/quality-guidelines.md:148`), current-request credentials (`.trellis/spec/backend/quality-guidelines.md:151`), and the literal digest guard (`.trellis/spec/backend/quality-guidelines.md:187`).
- Workflow requires research persisted under the supplied task (`.trellis/workflow.md:352`) and distinguishes reviewed planning from task activation (`.trellis/workflow.md:436`). This file is research, not approval to implement.

### External references and versions

- Local dependency evidence: module `github.com/killbus/rulego-indexed-media`, Go `1.25.0`, FFmpeg-over-IP Go module `v0.5.0`, RuleGo `v0.36.1-0.20260802040353-2ec085f29027` (`go.mod:1`, `go.mod:3`, `go.mod:6`). These are declared versions, not runtime versions verified by this research.
- Relevant Go API reference for a future decoder design: <https://pkg.go.dev/encoding/json> (`Unmarshal`, `Marshal`, `Decoder.DisallowUnknownFields`). No external documentation was fetched during this internal research.
- No external FFmpeg/container recommendations or downstream integration research are included; the main session owns those choices.

## Caveats / Not Found

- Missing vs `null` semantics, single-track duration semantics for nonzero earliest time, and exact single-track revision framing remain design choices. They must not be accidentally determined by an `if URL != empty` shortcut.
- Preserving the paired literal digest is feasible, but it preserves the existing evidence envelope: codec/container metadata, sourceKey, SIDX bytes, and init length. It cannot simultaneously detect every same-size init/media mutation without an explicit compatibility strategy.
- Parser validation does not prove actual stream type, codec compatibility, SAP delta zero, or complete paired audio coverage. Video SAP validation checks starts-with-SAP and type only (`sidx.go:168`). Audio-primary real-media timing needs evidence beyond synthetic references.
- Discovery requires an exact initial 64 KiB response. Supporting smaller valid indexed files may require a separately reviewed probe adjustment; this is especially relevant to short audio-only fixtures.
- No absent-track discovery/production implementation or real single-track acceptance evidence was found in the target code. Existing synthetic helpers always construct paired bundles and must not mask this gap.
- All statements describe files read in a shared workspace on the stated date; other agents may subsequently move anchors. Only this research file was written. No tests were written or run, and no implementation approval is asserted.
