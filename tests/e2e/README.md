# Real selected-track acceptance

`tests/e2e-hls-seek.sh` runs only in the owning CI environment. It keeps the
ABI-pinned runtime, candidate plugin, verified peers, and digest-pinned FFmpeg
service/client used by the paired provider example. The example remains paired.

The additional `track-modes-chain.json` exposes test-only raw indexed-media and
resource-origin operations. The Python harness requests origin-issued staging
limits, calls inspect and produce with omitted absent fields, and commits one
requested TS member per reservation. It builds the playlists externally. These
routes are acceptance infrastructure and are not shipped example endpoints.

`media-tool` uses the existing Go protocol client to explicitly invoke `ffprobe`
or `ffmpeg`. The peer API/protocol supports both programs; the shell checks JSON
FFprobe support against the actual pinned service before starting acceptance.
Each invocation has a deadline and propagates remote exit failures. No host
FFmpeg installation or assumed FFprobe CLI-image entrypoint is used.

For video, audio, paired, and short audio selections the harness verifies:

- Inspection matches the primary SIDX, including its absolute end time.
- Each request creates exactly its requested member, with no temporary inputs
  left behind; publication contains that member only.
- First, distant, adjacent, and final members have exactly the requested stream
  types/codecs and decode using required maps. Packet PTS/DTS match the source,
  decode timestamps increase, and adjacent member boundaries are checked.
- Distant HLS seeking retains source timestamps and decodes one second of all
  selected streams at the requested source position, with continuous coverage.
- Isolated request statistics include only selected representations, bounded
  exact ranges, and the explicit EOF-clipped discovery probe for short AAC.

Fixtures are generated with fixed video GOPs and AAC sample rate. Copies shift
both SIDX and fragment `tfdt` by four seconds. FFprobe verifies the actual packet
translation against the originals before using these fixtures. A four-second
32-kbit AAC source must be smaller than 64 KiB. Source hashes, SIDX facts,
FFprobe JSON, framehash output, and request/timing evidence are uploaded even on
failure. Source AAC skip/discard metadata and decoded padding sample counts are
recorded explicitly. Matching packet timestamps permits only one 90-kHz TS tick;
boundary coverage permits one codec frame (video 1/30 s, AAC 1024/48000 s). Only
paired audio adjacency permits one AAC frame of trimming overlap; single-track
adjacency permits two TS rounding ticks. There are no arbitrary second-based
timing allowances.

Seeking starts from the immediately preceding primary-index member, still in
the latter half of the fixture, to let the demuxer and decoder read ahead to
the requested boundary. In CI run `36405654696`, seeking exactly to a paired
video boundary decoded video at 16..17 s but began audio at 16.053333 s; the
full member contained audio from 15.989333 s. Its first three AAC packets
preceded the selected video keyframe in demux order, even though two had later
timestamps. The harness uses the preceding member's playlist
offset for input `-ss`, then `trim`/`atrim` at the requested absolute source
time and `-to` at its one-second end. These filters preserve timestamps; no
PTS reset is applied. Preroll is bounded to one indexed member, using its
actual duration. Full member decodes remain untrimmed. The unchanged seek
checks reject late audio, missing coverage, gaps, and wrong source positions.
Each `*-seek-request.json` saves both playlist offsets, both source positions,
member numbers, preroll duration, and the exact FFmpeg arguments before remote
decoding or validation, so failures retain the requested seek geometry. The
corrected command passed actual-media CI run `36408640108` for all selections,
including short audio; paired audio began exactly at source time 16 seconds.

The pinned FFprobe in CI run `36402240660` omitted `duration` only for the first
packet of `original-audio.m4a`. Its next DTS was 1024 samples later; all 750
remaining packets declared 1024 samples, with the same DTS cadence and PTS=DTS.
The harness permits that first duration to be derived from the next DTS only
for 48-kHz AAC-LC with a 1/48000 time base, when every remaining packet declares
one 1024-sample frame and every DTS step matches exactly. It also requires PTS
to equal DTS throughout. This works with zero or shifted source timestamps.
Missing video, interior, final, multiple, or uncorroborated durations fail;
existing durations are never replaced, and timing tolerances remain unchanged.
The raw probe JSON retains the omission. Each successful probe check writes a
separate `*-duration-derivations.json` recording the method, packet/stream index,
DTS pair, duration ticks, sample clock, and corroborating packet count (empty
lists when no inference was needed). These files are saved before decoding or
later acceptance checks, so a subsequent failure retains the derivation evidence.

The existing paired concurrency, retry, restart, stale-parent/TTL, static range,
and broken-mapping checks remain in the shell harness. Python negative controls
run in CI with `python3 -m unittest discover -s tests/e2e -p 'test_*.py'`; fixture
server tests run with `go test ./...`. Static parsing/format checks alone do not
establish real-media support. CI must execute the compiled and media checks.
