"""Negative controls for the acceptance oracle; executed by owning CI."""

import copy
from fractions import Fraction
import json
from pathlib import Path
import struct
import tempfile
import unittest
from unittest.mock import patch

from track_modes import Acceptance, check_seek, decoded_frames, packet_groups, seek_geometry, shifted_fixture


class MediaEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.probe = {
            "streams": [{"index": 0, "codec_type": "video", "codec_name": "h264", "time_base": "1/90000"}],
            "packets": [
                {"stream_index": 0, "pts": 360000, "dts": 360000, "duration": 3000},
                {"stream_index": 0, "pts": 363000, "dts": 363000, "duration": 3000},
            ],
        }
        self.source = {"video": {"quantum": Fraction(1, 30), "packets": [
            dict(pts=Fraction(4), dts=Fraction(4), duration=Fraction(1, 30), pos=100),
            dict(pts=Fraction(121, 30), dts=Fraction(121, 30), duration=Fraction(1, 30), pos=200),
        ]}}
        self.decoded = {"video": copy.deepcopy(self.source["video"]["packets"])}
        self.ref = dict(start=120, duration=2, offset=100, size=200)

    def check(self, probe=None, decoded=None):
        # The timing oracle has no Docker/HTTP dependency. Constructing the CI
        # runner is unnecessary for these negative controls.
        return Acceptance.check_member(None, self.probe if probe is None else probe,
                                       self.decoded if decoded is None else decoded, ["video"],
                                       self.source, self.ref, 30, "control")

    def test_source_relative_control(self):
        self.check()

    def test_timestamp_rebase_is_rejected(self):
        for packet in self.probe["packets"]:
            packet["pts"] -= 360000
            packet["dts"] -= 360000
        with self.assertRaisesRegex(AssertionError, "source-relative"):
            self.check()

    def test_missing_packet_is_rejected(self):
        self.probe["packets"].pop()
        with self.assertRaisesRegex(AssertionError, "indexed packets"):
            self.check()

    def test_undecoded_packet_is_rejected(self):
        self.decoded["video"].pop()
        with self.assertRaisesRegex(AssertionError, "decode lost video"):
            self.check()

    def test_extra_stream_is_rejected_even_without_packets(self):
        self.probe["streams"].append({"index": 1, "codec_type": "audio"})
        with self.assertRaisesRegex(AssertionError, "unexpected actual streams"):
            self.check()

    def test_reordered_decode_timestamps_are_rejected(self):
        self.probe["packets"].reverse()
        with self.assertRaisesRegex(AssertionError, "nonmonotonic"):
            packet_groups(self.probe, ["video"])

    def test_missing_video_duration_is_rejected_despite_regular_cadence(self):
        del self.probe["packets"][0]["duration"]
        with self.assertRaisesRegex(AssertionError, "unsupported missing video packet duration"):
            packet_groups(self.probe, ["video"])

    def test_ts_rounding_does_not_allow_frame_sized_drift(self):
        rounded = copy.deepcopy(self.probe)
        for packet in rounded["packets"]:
            packet["pts"] += 1
            packet["dts"] += 1
        self.check(rounded)
        for packet in rounded["packets"]:
            packet["pts"] += 1
            packet["dts"] += 1
        with self.assertRaisesRegex(AssertionError, "source-relative"):
            self.check(rounded)

    def test_empty_decode_output_is_rejected(self):
        with self.assertRaises(AssertionError):
            decoded_frames("#media_type 0: audio\n#tb 0: 1/48000\n", ["audio"])

    def test_framehash_counts_actual_decoded_samples(self):
        output = "#media_type 0: audio\n#tb 0: 1/48000\n0, 192000, 192000, 1024, 2048, abc\n"
        frames = decoded_frames(output, ["audio"])["audio"]
        self.assertEqual(frames[0]["pts"], 4)
        self.assertEqual(frames[0]["duration"] * 48000, 1024)


class AACDurationEvidenceTests(unittest.TestCase):
    def setUp(self):
        # Minimal form of CI 36402240660 original-audio.m4a: the first
        # duration is absent; all following packets declare 1024 samples.
        self.probe = {
            "streams": [{"index": 0, "codec_type": "audio", "codec_name": "aac",
                         "profile": "LC", "sample_rate": "48000", "time_base": "1/48000"}],
            "packets": [
                {"stream_index": 0, "pts": 0, "dts": 0, "pos": "1332"},
                {"stream_index": 0, "pts": 1024, "dts": 1024, "duration": 1024},
                {"stream_index": 0, "pts": 2048, "dts": 2048, "duration": 1024},
            ],
        }

    def test_first_duration_uses_next_dts_and_preserves_zero_or_nonzero_start(self):
        for start in (0, 192000):
            with self.subTest(start=start):
                probe = copy.deepcopy(self.probe)
                for packet in probe["packets"]:
                    packet["pts"] += start
                    packet["dts"] += start
                before = copy.deepcopy(probe)
                group = packet_groups(probe, ["audio"])["audio"]
                self.assertEqual(probe, before)
                self.assertEqual([p["duration"] for p in group["packets"]], [Fraction(8, 375)] * 3)
                self.assertEqual(group["packets"][0]["pts"], Fraction(start, 48000))
                self.assertEqual(group["duration_derivations"], [dict(
                    packet_index=0, stream_index=0, method="next_dts_with_aac_lc_frame_consistency",
                    dts=start, next_dts=start + 1024, duration_ticks=1024, time_base="1/48000",
                    sample_rate=48000, frame_samples=1024, corroborating_packets=2)])

    def test_present_duration_is_never_replaced_by_cadence(self):
        for duration in (512, 1024):
            with self.subTest(duration=duration):
                self.probe["packets"][0]["duration"] = duration
                group = packet_groups(self.probe, ["audio"])["audio"]
                self.assertEqual(group["packets"][0]["duration"], Fraction(duration, 48000))
                self.assertEqual(group["duration_derivations"], [])
        for duration in (0, -1024):
            with self.subTest(duration=duration):
                self.probe["packets"][0]["duration"] = duration
                with self.assertRaisesRegex(AssertionError, "nonpositive packet duration"):
                    packet_groups(self.probe, ["audio"])

    def test_missing_duration_without_next_packet_is_rejected(self):
        self.probe["packets"] = self.probe["packets"][:1]
        with self.assertRaisesRegex(AssertionError, "unsupported missing audio packet duration"):
            packet_groups(self.probe, ["audio"])

    def test_interior_final_or_multiple_missing_durations_are_rejected(self):
        for missing in ([1], [2], [0, 1], [0, 2]):
            with self.subTest(missing=missing):
                probe = copy.deepcopy(self.probe)
                probe["packets"][0]["duration"] = 1024
                for index in missing:
                    del probe["packets"][index]["duration"]
                with self.assertRaisesRegex(AssertionError, "unsupported missing audio packet duration"):
                    packet_groups(probe, ["audio"])

    def test_unconfirmed_profile_or_sample_clock_is_rejected(self):
        for field, value in (("profile", None), ("profile", "HE-AAC"),
                             ("sample_rate", "44100"), ("time_base", "1/90000")):
            with self.subTest(field=field, value=value):
                probe = copy.deepcopy(self.probe)
                probe["streams"][0][field] = value
                with self.assertRaisesRegex(AssertionError, "unsupported missing audio packet duration"):
                    packet_groups(probe, ["audio"])

    def test_next_dts_must_be_exactly_one_frame_later(self):
        for next_dts in (-1024, 0, 1023, 1025, 2048):
            with self.subTest(next_dts=next_dts):
                probe = copy.deepcopy(self.probe)
                probe["packets"][1].update(dts=next_dts, pts=next_dts)
                with self.assertRaisesRegex(AssertionError, "ambiguous AAC packet duration"):
                    packet_groups(probe, ["audio"])

    def test_remaining_stream_must_corroborate_frame_cadence(self):
        for change in (dict(duration=960), dict(duration=2048), dict(pts=2049),
                       dict(dts=3072, pts=3072), dict(dts=1024, pts=1024)):
            with self.subTest(change=change):
                probe = copy.deepcopy(self.probe)
                probe["packets"][2].update(change)
                with self.assertRaisesRegex(AssertionError, "ambiguous AAC packet duration"):
                    packet_groups(probe, ["audio"])

    def test_reordered_or_missing_first_timestamps_are_rejected(self):
        self.probe["packets"][0]["pts"] = -1024
        with self.assertRaisesRegex(AssertionError, "ambiguous AAC packet duration"):
            packet_groups(self.probe, ["audio"])
        for field in ("pts", "dts"):
            with self.subTest(field=field):
                probe = copy.deepcopy(self.probe)
                del probe["packets"][0][field]
                with self.assertRaisesRegex(AssertionError, "missing audio packet timestamp"):
                    packet_groups(probe, ["audio"])

    def test_next_packet_must_belong_to_same_stream(self):
        self.probe["streams"].append(dict(index=1, codec_type="video", codec_name="h264",
                                          time_base="1/15360"))
        self.probe["packets"].insert(1, dict(stream_index=1, pts=0, dts=0, duration=512))
        groups = packet_groups(self.probe, ["video", "audio"])
        self.assertEqual(groups["audio"]["packets"][0]["duration"], Fraction(8, 375))
        self.assertEqual(groups["video"]["duration_derivations"], [])

    def test_probe_saves_raw_evidence_and_separate_derivation(self):
        runner = object.__new__(Acceptance)
        with tempfile.TemporaryDirectory() as directory:
            runner.evidence = Path(directory)
            with patch.object(runner, "remote", return_value=json.dumps(self.probe)):
                result = runner.probe("/work/audio.m4a", "original-audio.m4a", ["audio"])
            raw = json.loads((runner.evidence / "original-audio.m4a.json").read_text())
            derived = json.loads((runner.evidence / "original-audio.m4a-duration-derivations.json").read_text())
            self.assertEqual(raw, self.probe)
            self.assertEqual(result, self.probe)
            self.assertNotIn("duration", raw["packets"][0])
            self.assertEqual(derived["audio"][0]["duration_ticks"], 1024)


class SeekCommandTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.runner = object.__new__(Acceptance)
        self.runner.evidence = Path(directory.name)
        self.refs = [dict(start=4000 + 2000 * i, duration=2000) for i in range(8)]

    def test_one_preceding_member_stays_distant_and_uses_distinct_clocks(self):
        geometry = seek_geometry(self.refs, 1000, 6)
        self.assertEqual(geometry, dict(target_member=6, input_member=5,
                                       requested_playlist_seek=12, input_playlist_seek=10,
                                       input_source_start=14, source_start=16, source_stop=17,
                                       preroll_duration=2))

    def test_preroll_uses_actual_member_duration_including_short_audio(self):
        for durations, target, input_seek, requested_seek, preroll in (
                ([1000, 2000, 1500, 500, 2500, 3000, 1000, 1000], 6,
                 Fraction(15, 2), Fraction(21, 2), Fraction(3)),
                ([512] * 7 + [437], 5, Fraction(256, 125), Fraction(64, 25), Fraction(64, 125))):
            with self.subTest(durations=durations):
                refs, start = [], 4000
                for duration in durations:
                    refs.append(dict(start=start, duration=duration))
                    start += duration
                geometry = seek_geometry(refs, 1000, target)
                self.assertEqual(geometry["input_playlist_seek"], input_seek)
                self.assertEqual(geometry["requested_playlist_seek"], requested_seek)
                self.assertEqual(geometry["source_start"], requested_seek + 4)
                self.assertEqual(geometry["preroll_duration"], preroll)
                self.assertEqual(geometry["input_member"], target - 1)

    def test_early_or_truncated_seek_is_rejected(self):
        for target in (0, 1, 4, 8):
            with self.subTest(target=target), self.assertRaises(AssertionError):
                seek_geometry(self.refs, 1000, target)
        self.refs[-1]["duration"] = 400
        with self.assertRaisesRegex(AssertionError, "lacks one second"):
            seek_geometry(self.refs, 1000, 7)

    def test_seek_command_prerolls_then_trims_on_source_clock_without_rebasing(self):
        for roles in (["video"], ["audio"], ["video", "audio"]):
            for origin in (0, 4000):
                with self.subTest(roles=roles, origin=origin):
                    refs = [dict(start=origin + 2000 * i, duration=2000) for i in range(8)]
                    geometry = seek_geometry(refs, 1000, 6)
                    start = geometry["source_start"]
                    output = "".join(f"#media_type {i}: {role}\n#tb {i}: 1/48000\n"
                                     f"{i}, {start * 48000}, {start * 48000}, 1024, 2048, abc\n"
                                     for i, role in enumerate(roles))
                    with patch.object(self.runner, "remote", return_value=output) as remote:
                        frames = self.runner.decode("http://fixture/paired.m3u8", roles, "seek", seek=geometry)
                    program, args, _ = remote.call_args.args
                    self.assertEqual(program, "ffmpeg")
                    self.assertIn("-copyts", args)
                    self.assertEqual(args.count("-ss"), 1)
                    self.assertLess(args.index("-ss"), args.index("-i"))
                    self.assertEqual(args[args.index("-ss") + 1], "10.0")
                    self.assertGreater(args.index("-to"), args.index("-i"))
                    self.assertEqual(args[args.index("-to") + 1], str(float(start + 1)))
                    self.assertNotIn("-t", args)
                    self.assertNotIn("-start_at_zero", args)
                    self.assertNotIn("setpts", " ".join(args))
                    self.assertEqual([args[i + 1] for i, arg in enumerate(args) if arg == "-map"],
                                     ["0:v:0" if role == "video" else "0:a:0" for role in roles])
                    for role, option, name in (("video", "-vf", "trim"), ("audio", "-af", "atrim")):
                        if role in roles:
                            self.assertEqual(args[args.index(option) + 1], f"{name}=start={float(start)}")
                            self.assertEqual(frames[role][0]["pts"], start)
                        else:
                            self.assertNotIn(option, args)

    def test_seek_request_is_saved_before_remote_work_or_decode_validation(self):
        geometry = seek_geometry(self.refs, 1000, 6)

        def empty_decode(program, args, label):
            saved = json.loads((self.runner.evidence / (label + "-request.json")).read_text())
            self.assertEqual(saved["geometry"]["source_start"], "16")
            self.assertEqual(saved["geometry"]["input_playlist_seek"], "10")
            self.assertEqual(saved["geometry"]["target_member"], 6)
            self.assertEqual(saved["args"], args)
            return ""

        with patch.object(self.runner, "remote", side_effect=empty_decode):
            with self.assertRaisesRegex(AssertionError, "decode did not emit"):
                self.runner.decode("http://fixture/paired.m3u8", ["video", "audio"], "seek", seek=geometry)

    def test_full_member_decode_has_no_seek_or_trim(self):
        output = "#media_type 0: audio\n#tb 0: 1/48000\n0, 192000, 192000, 1024, 2048, abc\n"
        with patch.object(self.runner, "remote", return_value=output) as remote:
            self.runner.decode("/work/6.ts", ["audio"], "member")
        args = remote.call_args.args[1]
        for option in ("-ss", "-to", "-af", "-vf"):
            self.assertNotIn(option, args)


class SeekEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.sources = {"video": {"quantum": Fraction(1, 30)},
                        "audio": {"quantum": Fraction(1024, 48000)}}
        self.frames = {}
        for role, count in (("video", 30), ("audio", 47)):
            quantum = self.sources[role]["quantum"]
            self.frames[role] = [dict(pts=16 + i * quantum, dts=16 + i * quantum, duration=quantum)
                                 for i in range(count)]

    def check(self):
        return check_seek(self.frames, ["video", "audio"], self.sources, Fraction(16))

    def test_source_position_and_codec_rounding_control(self):
        self.check()

    def test_one_second_from_wrong_member_is_rejected(self):
        for shift in (Fraction(-4), Fraction(-16)):
            with self.subTest(shift=shift):
                frames = copy.deepcopy(self.frames)
                for values in frames.values():
                    for frame in values:
                        frame["pts"] += shift
                        frame["dts"] += shift
                with self.assertRaisesRegex(AssertionError, "wrong source position"):
                    check_seek(frames, ["video", "audio"], self.sources, Fraction(16))

    def test_truncated_seek_is_rejected(self):
        del self.frames["audio"][-3:]
        with self.assertRaisesRegex(AssertionError, "ended at the wrong source position"):
            self.check()

    def test_paired_audio_demux_seek_loss_is_rejected_even_when_video_is_correct(self):
        # CI 36405654696: audio starts at 16.053333 and covers only 0.96 s,
        # although the video covers the requested 16..17 s exactly.
        quantum = self.sources["audio"]["quantum"]
        self.frames["audio"] = [dict(pts=16 + Fraction(4, 75) + i * quantum,
                                     dts=16 + Fraction(4, 75) + i * quantum, duration=quantum)
                                for i in range(45)]
        with self.assertRaisesRegex(AssertionError, "audio: HLS seek reached the wrong source position"):
            self.check()

    def test_internal_gap_is_rejected_with_valid_endpoints(self):
        del self.frames["video"][15]
        with self.assertRaisesRegex(AssertionError, "decoded gap/overlap"):
            self.check()


class FixtureTranslationTests(unittest.TestCase):
    def test_sidx_and_tfdt_shift_together_for_both_versions(self):
        def box(kind, payload):
            return struct.pack(">I4s", len(payload) + 8, kind) + payload

        for version in (0, 1):
            with self.subTest(version=version):
                fmt = ">I" if version == 0 else ">Q"
                tfdt = box(b"tfdt", bytes([version, 0, 0, 0]) + struct.pack(fmt, 1024))
                fragment = box(b"moof", box(b"traf", tfdt)) + box(b"mdat", b"media")
                payload = bytes([version, 0, 0, 0]) + struct.pack(">II", 1, 48000)
                payload += struct.pack(fmt, 1024) + struct.pack(fmt, 0)
                payload += struct.pack(">HHIII", 0, 1, len(fragment), 1024, 0)
                original = box(b"sidx", payload) + fragment
                shifted = shifted_fixture(original, 4)
                self.assertEqual(len(shifted), len(original))
                self.assertEqual(struct.unpack_from(fmt, shifted, 20)[0], 193024)
                tfdt_time = original.index(b"tfdt") + 8
                self.assertEqual(struct.unpack_from(fmt, shifted, tfdt_time)[0], 193024)
                self.assertTrue(shifted.endswith(b"media"))


if __name__ == "__main__":
    unittest.main()
