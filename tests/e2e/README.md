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

The existing paired concurrency, retry, restart, stale-parent/TTL, static range,
and broken-mapping checks remain in the shell harness. Python negative controls
run in CI with `python3 -m unittest discover -s tests/e2e -p 'test_*.py'`; fixture
server tests run with `go test ./...`. Static parsing/format checks alone do not
establish real-media support. CI must execute the compiled and media checks.
