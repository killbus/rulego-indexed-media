"""Negative controls for the acceptance oracle; executed by owning CI."""

import copy
from fractions import Fraction
import struct
import unittest

from track_modes import Acceptance, check_seek, decoded_frames, packet_groups, shifted_fixture


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
