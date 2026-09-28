#!/usr/bin/env python3
"""CI-only real-media acceptance. Uses stdlib and the pinned FFmpeg service."""

import argparse
from fractions import Fraction
import hashlib
import json
import math
from pathlib import Path, PurePosixPath
import re
import struct
import subprocess
from urllib.request import Request, urlopen

TS_TICK = Fraction(1, 90000)
SHIFT_SECONDS = 4


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def save(path, value):
    path.write_text(json.dumps(value, indent=2, default=str) + "\n", encoding="utf-8")


def boxes(data, start=0, end=None):
    end = len(data) if end is None else end
    while start < end:
        require(start + 8 <= end, "truncated MP4 box")
        size, kind = struct.unpack_from(">I4s", data, start)
        header = 8
        if size == 1:
            require(start + 16 <= end, "truncated extended MP4 box")
            size = struct.unpack_from(">Q", data, start + 8)[0]
            header = 16
        if size == 0:
            size = end - start
        require(header <= size <= end - start, "invalid MP4 box size")
        yield kind, start, start + header, start + size
        start += size


def sidx(data):
    found = [box for box in boxes(data) if box[0] == b"sidx"]
    require(len(found) == 1, "fixture must contain one global SIDX")
    _, begin, payload, end = found[0]
    version = data[payload]
    require(version in (0, 1), "unsupported fixture SIDX version")
    scale = struct.unpack_from(">I", data, payload + 8)[0]
    require(scale > 0, "zero SIDX timescale")
    fmt = "I" if version == 0 else "Q"
    earliest, offset = struct.unpack_from(">" + fmt * 2, data, payload + 12)
    cursor = payload + 12 + (8 if version == 0 else 16)
    count = struct.unpack_from(">H", data, cursor + 2)[0]
    cursor += 4
    require(count > 0 and cursor + count * 12 == end, "invalid SIDX references")
    position, timestamp = end + offset, earliest
    refs = []
    for index in range(count):
        size, duration, sap = struct.unpack_from(">III", data, cursor + index * 12)
        require(0 < size < (1 << 31) and duration > 0, "invalid direct reference")
        require(position + size <= len(data), "reference outside fixture")
        refs.append(dict(offset=position, size=size, start=timestamp, duration=duration, sap=sap))
        position += size
        timestamp += duration
    return dict(scale=scale, earliest=earliest, end=timestamp, refs=refs,
                init_size=begin, earliest_offset=payload + 12, time_format=fmt)


def shifted_fixture(data, seconds):
    """Translate actual fragment decode times and their SIDX, without remuxing."""
    data = bytearray(data)
    index = sidx(data)
    delta = seconds * index["scale"]
    struct.pack_into(">" + index["time_format"], data, index["earliest_offset"], index["earliest"] + delta)
    fragments = 0
    for kind, _, payload, end in boxes(data):
        if kind != b"moof":
            continue
        times = []
        for child, _, child_payload, child_end in boxes(data, payload, end):
            if child == b"traf":
                times.extend(box for box in boxes(data, child_payload, child_end) if box[0] == b"tfdt")
        require(len(times) == 1, "fixture needs one track/tfdt per fragment")
        _, _, cursor, _ = times[0]
        require(data[cursor] in (0, 1), "unsupported tfdt version")
        fmt = ">I" if data[cursor] == 0 else ">Q"
        timestamp = struct.unpack_from(fmt, data, cursor + 4)[0]
        struct.pack_into(fmt, data, cursor + 4, timestamp + delta)
        fragments += 1
    require(fragments == len(index["refs"]), "expected one fragment per SIDX reference")
    return data


def prepare(directory):
    for name in ("video.mp4", "audio.m4a", "short-audio.m4a"):
        original = (directory / name).read_bytes()
        shifted = shifted_fixture(original, SHIFT_SECONDS)
        (directory / ("offset-" + name)).write_bytes(shifted)
    require(0 < (directory / "offset-short-audio.m4a").stat().st_size < 65536,
            "real short AAC fixture must fit below the initial probe")
    (directory / "playlists").mkdir()


def packet_groups(probe, roles):
    streams = probe["streams"]
    require(sorted(s["codec_type"] for s in streams) == sorted(roles),
            f"unexpected actual streams: {streams}")
    groups = {}
    for stream in streams:
        role = stream["codec_type"]
        require(stream["codec_name"] == ("h264" if role == "video" else "aac"), "unexpected codec")
        scale = Fraction(stream["time_base"])
        quantum = Fraction(1, 30) if role == "video" else Fraction(1024, int(stream["sample_rate"]))
        packets = []
        for packet in probe["packets"]:
            if packet["stream_index"] == stream["index"]:
                packets.append(dict(pts=int(packet["pts"]) * scale,
                                    dts=int(packet["dts"]) * scale,
                                    duration=int(packet["duration"]) * scale,
                                    pos=int(packet.get("pos", -1))))
        require(packets, f"no {role} packets")
        require(all(p["duration"] > 0 for p in packets), "nonpositive packet duration")
        require(all(a["dts"] < b["dts"] for a, b in zip(packets, packets[1:])),
                f"nonmonotonic {role} decode timestamps")
        groups[role] = dict(packets=packets, quantum=quantum)
    return groups


def decoded_frames(output, roles):
    types, scales, frames = {}, {}, {}
    for line in output.splitlines():
        match = re.fullmatch(r"#media_type (\d+): (\w+)", line)
        if match:
            types[int(match[1])] = match[2]
        match = re.fullmatch(r"#tb (\d+): (\d+/\d+)", line)
        if match:
            scales[int(match[1])] = Fraction(match[2])
        if line and not line.startswith("#"):
            fields = [field.strip() for field in line.split(",")]
            require(len(fields) == 6, f"invalid framehash line: {line}")
            stream, dts, pts, duration = map(int, fields[:4])
            scale = scales[stream]
            require(int(fields[4]) > 0 and duration > 0, "empty decoded frame")
            frames.setdefault(types[stream], []).append(dict(
                dts=dts * scale, pts=pts * scale, duration=duration * scale))
    require(sorted(types.values()) == sorted(roles) and sorted(frames) == sorted(roles),
            "decode did not emit exactly the selected streams")
    for role, values in frames.items():
        require(all(a["dts"] < b["dts"] for a, b in zip(values, values[1:])),
                f"nonmonotonic decoded {role}")
    return frames


def check_seek(frames, roles, sources, start, duration=Fraction(1)):
    """Check the requested source position as well as decoded coverage."""
    summary = {}
    for role in roles:
        values = frames[role]
        tolerance = sources[role]["quantum"] + TS_TICK
        first = values[0]["pts"]
        end = values[-1]["pts"] + values[-1]["duration"]
        decoded_duration = sum(frame["duration"] for frame in values)
        require(abs(first - start) <= tolerance, f"{role}: HLS seek reached the wrong source position")
        require(abs(end - start - duration) <= tolerance, f"{role}: HLS seek ended at the wrong source position")
        require(abs(decoded_duration - duration) <= tolerance, f"{role}: distant HLS seek lost decoded coverage")
        require(all(abs(b["dts"] - a["dts"] - a["duration"]) <= TS_TICK
                    for a, b in zip(values, values[1:])), f"{role}: HLS seek has a decoded gap/overlap")
        summary[role] = dict(first=first, end=end, decoded_duration=decoded_duration)
    return summary


class Acceptance:
    def __init__(self, args):
        self.directory = args.directory.resolve()
        self.rulego = args.rulego
        self.fixture = args.fixture
        self.command = ["docker", "run", "--rm", "--network", args.network,
                        "-v", f"{self.directory}:/work", "--entrypoint", "/work/media-tool", args.runtime]
        self.evidence = self.directory / "track-evidence"
        self.evidence.mkdir()

    def remote(self, program, args, label):
        result = subprocess.run(self.command + ["-program", program, "--"] + args,
                                capture_output=True, text=True, timeout=150, check=False)
        (self.evidence / (label + ".stderr")).write_text(result.stderr, encoding="utf-8")
        require(result.returncode == 0, f"{program} failed ({label}): {result.stderr}")
        return result.stdout

    def probe(self, source, label):
        output = self.remote("ffprobe", ["-v", "error", "-show_streams", "-show_packets",
                                         "-show_format", "-of", "json", source], label)
        value = json.loads(output)
        save(self.evidence / (label + ".json"), value)
        return value

    def decode(self, source, roles, label, seek=None, stop=None):
        args = ["-hide_banner", "-loglevel", "error", "-xerror", "-copyts"]
        if seek is not None:
            require(stop is not None, "seek decoding needs a source-timeline stop")
            args += ["-ss", str(float(seek))]
        args += ["-i", source]
        if seek is not None:
            # copyts retains the nonzero source timeline. An output -t 1
            # would stop at timestamp 1; stop at the absolute source end.
            args += ["-to", str(float(stop))]
        for role in roles:
            args += ["-map", "0:v:0" if role == "video" else "0:a:0"]
        if "video" in roles:
            args += ["-fps_mode", "passthrough", "-c:v", "rawvideo"]
        if "audio" in roles:
            args += ["-c:a", "pcm_s16le"]
        args += ["-f", "framehash", "pipe:1"]
        output = self.remote("ffmpeg", args, label)
        (self.evidence / (label + ".framehash")).write_text(output, encoding="utf-8")
        return decoded_frames(output, roles)

    def request(self, url, body=None):
        request = Request(url, data=None if body is None else json.dumps(body).encode(),
                          headers={"Content-Type": "application/json"})
        with urlopen(request, timeout=180) as response:
            return json.load(response)

    def operation(self, node, body):
        value = self.request(self.rulego + "/e2e/" + node, body)
        require("kind" not in value and value.get("state") != "failed", f"operation failed: {value}")
        return value

    def source_evidence(self):
        sources = {}
        for name, role in (("video.mp4", "video"), ("audio.m4a", "audio"), ("short-audio.m4a", "audio")):
            original = packet_groups(self.probe("/work/" + name, "original-" + name), [role])[role]
            filename = "offset-" + name
            data = (self.directory / filename).read_bytes()
            index = sidx(data)
            probe = self.probe("/work/" + filename, "source-" + name)
            group = packet_groups(probe, [role])[role]
            require(len(original["packets"]) == len(group["packets"]), "timestamp shift changed packets")
            for before, after in zip(original["packets"], group["packets"]):
                require(after["pts"] - before["pts"] == SHIFT_SECONDS and
                        after["dts"] - before["dts"] == SHIFT_SECONDS, "fragment timestamps were not translated exactly")
            require(index["earliest"] >= SHIFT_SECONDS * index["scale"], "nonzero fixture lost SIDX start")
            decoded = self.decode("/work/" + filename, [role], "source-decode-" + name)[role]
            # Persist source skip/discard side data (in FFprobe JSON), packet
            # counts, and decoded sample coverage; AAC priming is never guessed.
            sources[name] = dict(index=index, packets=group["packets"], quantum=group["quantum"],
                                 bytes=len(data), sha256=hashlib.sha256(data).hexdigest(),
                                 generated_duration=4 if name == "short-audio.m4a" else 16,
                                 decoded_duration=sum(f["duration"] for f in decoded),
                                 packet_duration=sum(p["duration"] for p in group["packets"]),
                                 priming_and_padding=[p["side_data_list"] for p in probe["packets"]
                                                      if "side_data_list" in p])
            if role == "audio":
                sources[name]["decoded_samples"] = sources[name]["decoded_duration"] * 48000
                sources[name]["decoded_padding_samples"] = (sources[name]["decoded_duration"] -
                                                            sources[name]["generated_duration"]) * 48000
        save(self.evidence / "sources.json", sources)
        return sources

    def check_member(self, probe, decoded, roles, sources, ref, scale, label):
        groups = packet_groups(probe, roles)
        start, end = Fraction(ref["start"], scale), Fraction(ref["start"] + ref["duration"], scale)
        summary = {}
        for role in roles:
            packets = groups[role]["packets"]
            source = sources[role]
            quantum = source["quantum"]
            source_packets = source["packets"]
            # Each output packet must retain an actual source PTS/DTS. TS
            # rounding is the only timestamp matching tolerance.
            for packet in packets:
                require(any(abs(packet["pts"] - original["pts"]) <= TS_TICK and
                            abs(packet["dts"] - original["dts"]) <= TS_TICK for original in source_packets),
                        f"{label}/{role}: packet timestamp is not source-relative")
            if len(roles) == 1 or role == "video":
                expected = [p for p in source_packets if ref["offset"] <= p["pos"] < ref["offset"] + ref["size"]]
                require(len(packets) == len(expected), f"{label}/{role}: missing or extra indexed packets")
                require(all(abs(a["pts"] - b["pts"]) <= TS_TICK for a, b in zip(packets, expected)),
                        f"{label}/{role}: wrong reference packets")
            require(abs(packets[0]["pts"] - start) <= quantum + TS_TICK, f"{label}/{role}: start gap")
            last_end = packets[-1]["pts"] + packets[-1]["duration"]
            require(abs(last_end - end) <= quantum + TS_TICK, f"{label}/{role}: end gap/padding exceeds one codec frame")
            require(all(abs(b["dts"] - a["dts"] - a["duration"]) <= TS_TICK
                        for a, b in zip(packets, packets[1:])), f"{label}/{role}: internal packet gap")
            frames = decoded[role]
            if role == "video":
                require(len(frames) == len(packets), f"{label}: decode lost video frames")
            decoded_duration = sum(f["duration"] for f in frames)
            packet_duration = sum(p["duration"] for p in packets)
            require(abs(decoded_duration - packet_duration) <= quantum + TS_TICK,
                    f"{label}/{role}: decode lost coverage")
            require(abs(frames[0]["pts"] - packets[0]["pts"]) <= quantum + TS_TICK,
                    f"{label}/{role}: decoded timestamps shifted")
            summary[role] = dict(first=packets[0]["dts"], end=packets[-1]["dts"] + packets[-1]["duration"],
                                 packet_count=len(packets), decoded_duration=decoded_duration, quantum=quantum,
                                 start_delta=packets[0]["pts"] - start, end_delta=last_end - end,
                                 decoded_samples=decoded_duration * 48000 if role == "audio" else None)
        return summary

    def mode(self, selection, roles, all_sources):
        sources = {role: all_sources["short-audio.m4a" if selection == "short-audio" else
                                     ("video.mp4" if role == "video" else "audio.m4a")] for role in roles}
        lease = {"sourceKey": "fixture:track-modes"}
        for role in roles:
            name = "video.mp4" if role == "video" else "audio.m4a"
            lease[role] = dict(url=f"http://ytdlp:8080/tracks/{selection}/{name}",
                               headers={"X-Fixture-Lease": "fixture-lease"},
                               container="mp4" if role == "video" else "m4a",
                               codec="h264" if role == "video" else "aac")
        before = self.request(self.fixture + "/stats")["requests"] or []
        inspect = self.operation("indexed", dict(operation="inspect", source=lease))
        save(self.evidence / (selection + "-inspect.json"), inspect)
        primary = sources["video" if "video" in roles else "audio"]["index"]
        refs, scale = primary["refs"], primary["scale"]
        require(len(inspect["segments"]) == len(refs) >= 4, "inspect used the wrong primary segment count")
        require(inspect["duration"] == primary["end"] / scale, "inspect lost absolute end time")
        require(all(a["duration"] == b["duration"] / scale for a, b in zip(inspect["segments"], refs)),
                "inspect used the wrong primary durations")
        target = min(len(refs) * 3 // 4, len(refs) - 2)
        while sum(r["duration"] for r in refs[target:]) < scale:
            target -= 1
        require(target >= len(refs) // 2, "fixture too short for a distant one-second seek")
        selected = {0, target, target + 1, len(refs) - 1}
        summaries, urls = {}, []
        for number, ref in enumerate(refs):
            label = f"{selection}-{number}"
            pending = self.operation("origin", dict(operation="acquire", key="e2e:" + label,
                                     fingerprint=inspect["revision"], ttlMs=900000, maxBytes=33554432,
                                     productionTimeoutMs=180000))
            require(pending["state"] == "pending", "expected isolated staging reservation")
            staging = PurePosixPath(pending["stagingDir"])
            if staging.is_absolute():
                staging = staging.relative_to("/app")
            require(staging.parts[:3] == ("data", "resource-origin", "staging") and ".." not in staging.parts,
                    "unexpected origin staging directory")
            produced = self.operation("indexed", dict(operation="produce", source=lease,
                                      expectedRevision=inspect["revision"], segment=number,
                                      stagingDir=pending["stagingDir"], maxBytes=pending["maxBytes"],
                                      publishBy=pending["publishBy"]))
            member = f"{number}.ts"
            staged = json.loads(self.remote("staging", ["/work/" + staging.as_posix()], label + "-staging"))
            save(self.evidence / (label + "-produce.json"), dict(result=produced, files=staged))
            require(produced["member"] == member and [p["name"] for p in staged] == [member],
                    "produce created unrequested members or left temporary inputs")
            require(staged[0]["regular"] and produced["bytes"] == staged[0]["bytes"] > 0, "wrong output byte count")
            if number in selected:
                remote_path = "/work/" + (staging / member).as_posix()
                probe = self.probe(remote_path, label)
                require("mpegts" in probe["format"]["format_name"], "output is not MPEG-TS")
                decoded = self.decode(remote_path, roles, label + "-decode")
                summaries[number] = self.check_member(probe, decoded, roles, sources, ref, scale, label)
            ready = self.operation("origin", dict(operation="commit", resourceId=pending["resourceId"],
                                   generation=pending["generation"], entrypoint=member))
            require(ready["state"] == "ready" and ready["members"] == [member], "publication includes extra members")
            require(ready["url"].startswith("/resources/"), "unexpected publication URL")
            urls.append("http://rulego:9090" + ready["url"])
        for role in roles:
            gap = summaries[target + 1][role]["first"] - summaries[target][role]["end"]
            # Paired trimming can straddle one AAC packet at a video boundary.
            tolerance = sources[role]["quantum"] if role == "audio" and len(roles) == 2 else Fraction(0)
            require(abs(gap) <= tolerance + 2 * TS_TICK, f"{selection}/{role}: adjacent member gap/overlap {gap}")
            summaries[target + 1][role]["adjacent_gap"] = gap
        lines = ["#EXTM3U", "#EXT-X-VERSION:3", "#EXT-X-PLAYLIST-TYPE:VOD",
                 "#EXT-X-MEDIA-SEQUENCE:0", f"#EXT-X-TARGETDURATION:{math.ceil(max(r['duration'] for r in refs) / scale)}"]
        for ref, url in zip(refs, urls):
            lines += [f"#EXTINF:{ref['duration'] / scale:.9f},", url]
        lines.append("#EXT-X-ENDLIST")
        (self.directory / "playlists" / (selection + ".m3u8")).write_text("\n".join(lines) + "\n", encoding="utf-8")
        seek = Fraction(sum(r["duration"] for r in refs[:target]), scale)
        seek_start = Fraction(refs[target]["start"], scale)
        frames = self.decode(f"http://ytdlp:8080/playlists/{selection}.m3u8", roles, selection + "-seek",
                             seek, seek_start + 1)
        seek_summary = check_seek(frames, roles, sources, seek_start)
        after = self.request(self.fixture + "/stats")["requests"]
        requests = after[len(before):]
        save(self.evidence / (selection + "-requests.json"), requests)
        require(requests, "missing isolated network evidence")
        paths = {"/tracks/" + selection + ("/video.mp4" if role == "video" else "/audio.m4a"): source
                 for role, source in sources.items()}
        require(set(r["path"] for r in requests) == set(paths), "absent/unselected track was requested")
        for record in requests:
            source = paths[record["path"]]
            match = re.fullmatch(r"bytes=(\d+)-(\d+)", record["range"])
            require(match is not None and record["status"] == 206, "unexpected fixture range response")
            begin, end = map(int, match.groups())
            span = end - begin + 1
            if source["bytes"] < 65536 and record["range"] == "bytes=0-65535":
                require(record["bytes"] == source["bytes"], "short probe was not EOF clipped")
            else:
                require(record["bytes"] == span and span < source["bytes"], "nonexact or whole-source range")
        if selection == "short-audio":
            require(any(r["range"] == "bytes=0-65535" for r in requests), "short fixture never exercised discovery")
        save(self.evidence / (selection + "-timing.json"),
             dict(members=summaries, seek=seek, seek_source_start=seek_start,
                  seek_decoded=seek_summary, revision=inspect["revision"]))
        return inspect["revision"]

    def run(self):
        # The shell checks capability before starting any acceptance traversal.
        version = (self.directory / "ffprobe-version.json").read_text(encoding="utf-8")
        require("program_version" in json.loads(version), "pinned service lacks JSON FFprobe support")
        (self.evidence / "ffprobe-version.json").write_text(version, encoding="utf-8")
        sources = self.source_evidence()
        revisions = [self.mode(selection, roles, sources) for selection, roles in
                     (("video", ["video"]), ("audio", ["audio"]), ("paired", ["video", "audio"]),
                      ("short-audio", ["audio"]))]
        require(len(set(revisions)) == 4, "selected modes/evidence share a revision")
        print("Selected video/audio/paired/short-audio TS acceptance passed", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("prepare", "run"))
    parser.add_argument("directory", type=Path)
    parser.add_argument("--rulego")
    parser.add_argument("--fixture")
    parser.add_argument("--network")
    parser.add_argument("--runtime")
    args = parser.parse_args()
    if args.operation == "prepare":
        prepare(args.directory)
    else:
        require(all((args.rulego, args.fixture, args.network, args.runtime)), "run needs owning CI endpoints and pins")
        Acceptance(args).run()


if __name__ == "__main__":
    main()
