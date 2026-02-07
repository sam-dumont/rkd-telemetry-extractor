"""Tests for rkd_parser.py — 100% branch coverage required."""

import csv
import io
import math
import struct
import sys
import textwrap
from pathlib import Path
from unittest import mock

import pytest

import rkd_parser
from rkd_parser import (
    GPS_EPOCH,
    GPS_LEAP_SECONDS,
    GPSFix,
    IMUFrame,
    META_HEADER_SIZE,
    RECORD_ACCEL,
    RECORD_GPS,
    RECORD_GYRO,
    RECORD_HEADER,
    RECORD_HEADER_SIZE,
    RECORD_NAMES,
    RECORD_PERIODIC,
    RECORD_TERMINATOR,
    RECORD_TIMESTAMP,
    RKD_MAGIC,
    RKDParser,
    RKDSession,
    TRAILING_CRC_SIZE,
    _gps_to_utc_ms,
    _haversine,
    _lerp,
    _lerp_angle,
    _xml_escape,
    create_sample_rkd,
    export_csv,
    export_gpx,
    find_rkd_files,
    main,
    print_session_info,
    process_file,
)

SAMPLE_RKD = Path(__file__).resolve().parent.parent.parent / "samples" / "sample_mettet.rkd"


# ─────────────────────────────────────────────────────────────────────────────
# Helpers — build minimal RKD binary data
# ─────────────────────────────────────────────────────────────────────────────

def _meta_header(car_id=11098, timestamp=1617532800):
    """Build a 28-byte meta header."""
    return struct.pack("<7I", 0x00148000, 0, 1, 0, car_id, timestamp, 0)


def _record(rtype, payload, frame=0):
    """Build a single RKD record (10-byte header + payload)."""
    frame_lo = frame & 0xFFFF
    frame_hi = (frame >> 16) & 0xFFFF
    header = struct.pack("<5H", 0, rtype, len(payload), frame_lo, frame_hi)
    return header + payload


def _minimal_rkd(*records):
    """Build a minimal RKD file: magic + meta header + records + trailing CRC."""
    data = bytearray(RKD_MAGIC + _meta_header())
    for rec in records:
        data.extend(rec)
    data.extend(b"\x00\x00")  # trailing CRC
    return bytes(data)


def _gps_payload(gps_ts=1302000000, sats=18, lat_raw=503000000, lon_raw=46500000,
                 speed_raw=1000, heading_raw=18000000, alt_raw=250000, vspeed=10):
    """Build a 36-byte GPS record payload."""
    return struct.pack("<2I2h6i", 3, gps_ts, sats, 0,
                       lat_raw, lon_raw, speed_raw, heading_raw, alt_raw, vspeed)


def _accel_payload(ax=0, ay=0, az=1000):
    """Build a 12-byte accelerometer payload (values in milli-g)."""
    return struct.pack("<3i", ax, ay, az)


def _gyro_payload(gx=0, gy=0, gz=0):
    """Build a 12-byte gyroscope payload."""
    return struct.pack("<3i", gx, gy, gz)


def _terminator_payload(timestamp=1617533000):
    """Build a 12-byte terminator payload."""
    return struct.pack("<I", timestamp) + b"\x00" * 8


def _make_session_with_data(num_gps=3, num_imu=18, braking_frame=None):
    """Build an RKDSession with GPS and IMU data for export testing."""
    session = RKDSession(
        file_path="test.rkd",
        file_size=1000,
        car_id=11098,
        session_timestamp=1617532800,
    )
    base_gps_ts = 1302000000
    for i in range(num_gps):
        session.gps_fixes.append(GPSFix(
            frame=i * 6,
            gps_timestamp=base_gps_ts + i,
            utc_ms=_gps_to_utc_ms(base_gps_ts + i),
            satellites=18,
            latitude=50.3 + i * 0.0001,
            longitude=4.65 + i * 0.0001,
            speed_ms=10.0 + i,
            heading_deg=90.0 + i * 10,
            altitude_m=250.0 + i,
            vertical_speed_cms=5,
        ))
    first_frame = 0
    last_frame = (num_gps - 1) * 6 if num_gps > 0 else 0
    for i in range(num_imu):
        frame = first_frame + i
        if frame > last_frame:
            break
        ax = -1.0 if (braking_frame is not None and frame == braking_frame) else 1.0
        session.imu_frames.append(IMUFrame(
            frame=frame,
            accel_x=ax,
            accel_y=0.5,
            accel_z=9.81,
            gyro_x=0.1,
            gyro_y=0.2,
            gyro_z=5.0,
        ))
    return session


# ─────────────────────────────────────────────────────────────────────────────
# Pure utility functions
# ─────────────────────────────────────────────────────────────────────────────

class TestHaversine:
    def test_same_point_returns_zero(self):
        assert _haversine(50.0, 4.0, 50.0, 4.0) == 0.0

    def test_known_distance(self):
        # ~111 km between 1 degree latitude at equator
        dist = _haversine(0.0, 0.0, 1.0, 0.0)
        assert 110.0 < dist < 112.0


class TestGpsToUtcMs:
    def test_known_conversion(self):
        # GPS epoch + 0 seconds - leap seconds = 1980-01-06 minus 18s
        result = _gps_to_utc_ms(GPS_LEAP_SECONDS)
        # Should be GPS epoch in unix ms
        expected = int(GPS_EPOCH.timestamp() * 1000)
        assert result == expected

    def test_positive_value(self):
        result = _gps_to_utc_ms(1302000000)
        assert isinstance(result, int)
        assert result > 0


class TestLerp:
    def test_midpoint(self):
        assert _lerp(0.0, 10.0, 0.5) == 5.0

    def test_endpoints(self):
        assert _lerp(3.0, 7.0, 0.0) == 3.0
        assert _lerp(3.0, 7.0, 1.0) == 7.0


class TestLerpAngle:
    def test_normal(self):
        assert _lerp_angle(10.0, 20.0, 0.5) == pytest.approx(15.0)

    def test_wraparound(self):
        result = _lerp_angle(350.0, 10.0, 0.5)
        assert result == pytest.approx(0.0, abs=0.01)


class TestXmlEscape:
    def test_all_special_chars(self):
        assert _xml_escape('A & B <C> "D"') == 'A &amp; B &lt;C&gt; &quot;D&quot;'

    def test_no_special_chars(self):
        assert _xml_escape("hello") == "hello"


# ─────────────────────────────────────────────────────────────────────────────
# RKDSession properties
# ─────────────────────────────────────────────────────────────────────────────

class TestRKDSessionProperties:
    def test_duration_seconds_empty(self):
        s = RKDSession()
        assert s.duration_seconds == 0.0

    def test_duration_seconds_one_fix(self):
        s = RKDSession()
        s.gps_fixes.append(GPSFix(0, 100, 0, 18, 50.0, 4.0, 10.0, 90.0, 250.0, 0))
        assert s.duration_seconds == 0.0

    def test_duration_seconds_two_fixes(self):
        s = RKDSession()
        s.gps_fixes.append(GPSFix(0, 100, 0, 18, 50.0, 4.0, 10.0, 90.0, 250.0, 0))
        s.gps_fixes.append(GPSFix(6, 110, 0, 18, 50.0, 4.0, 10.0, 90.0, 250.0, 0))
        assert s.duration_seconds == 10

    def test_max_speed_kmh_empty(self):
        s = RKDSession()
        assert s.max_speed_kmh == 0.0

    def test_max_speed_kmh_with_data(self):
        s = RKDSession()
        s.gps_fixes.append(GPSFix(0, 100, 0, 18, 50.0, 4.0, 10.0, 90.0, 250.0, 0))
        assert s.max_speed_kmh == pytest.approx(36.0)

    def test_total_distance_km_empty(self):
        s = RKDSession()
        assert s.total_distance_km == 0.0

    def test_total_distance_km_one_fix(self):
        s = RKDSession()
        s.gps_fixes.append(GPSFix(0, 100, 0, 18, 50.0, 4.0, 10.0, 90.0, 250.0, 0))
        assert s.total_distance_km == 0.0

    def test_total_distance_km_two_fixes(self):
        s = RKDSession()
        s.gps_fixes.append(GPSFix(0, 100, 0, 18, 50.0, 4.0, 10.0, 90.0, 250.0, 0))
        s.gps_fixes.append(GPSFix(6, 110, 0, 18, 50.001, 4.001, 10.0, 90.0, 250.0, 0))
        assert s.total_distance_km > 0


# ─────────────────────────────────────────────────────────────────────────────
# Parser validation
# ─────────────────────────────────────────────────────────────────────────────

class TestValidateMagic:
    def test_too_small(self):
        parser = RKDParser()
        with pytest.raises(ValueError, match="File too small"):
            parser._validate_magic(b"short")

    def test_wrong_magic(self):
        parser = RKDParser()
        with pytest.raises(ValueError, match="Invalid RKD magic"):
            parser._validate_magic(b"\x00" * 40)

    def test_valid_magic(self):
        parser = RKDParser()
        parser._validate_magic(RKD_MAGIC + b"\x00" * 30)  # no exception


class TestParseMetaHeader:
    def test_too_small(self):
        parser = RKDParser()
        session = RKDSession()
        # Data with valid magic but not enough for meta header
        data = RKD_MAGIC + b"\x00" * 10
        with pytest.raises(ValueError, match="File too small for meta header"):
            parser._parse_meta_header(data, session)

    def test_valid_meta(self):
        parser = RKDParser()
        session = RKDSession()
        data = RKD_MAGIC + _meta_header(car_id=12345, timestamp=1617532800)
        parser._parse_meta_header(data, session)
        assert session.car_id == 12345
        assert session.session_timestamp == 1617532800


# ─────────────────────────────────────────────────────────────────────────────
# Record parsing
# ─────────────────────────────────────────────────────────────────────────────

class TestParseHeaderRecord:
    def test_valid_key_value(self):
        parser = RKDParser()
        session = RKDSession()
        payload = b"HW_ID\x00RK1234\x00"
        parser._parse_header_record(payload, session)
        assert session.config["HW_ID"] == "RK1234"

    def test_no_null_separator(self):
        parser = RKDParser()
        session = RKDSession()
        payload = b"NOVALUE"
        parser._parse_header_record(payload, session)
        assert len(session.config) == 0


class TestParseGpsRecord:
    def test_valid_gps(self):
        parser = RKDParser()
        session = RKDSession()
        payload = _gps_payload()
        parser._parse_gps_record(payload, 0, session)
        assert len(session.gps_fixes) == 1
        fix = session.gps_fixes[0]
        assert fix.latitude == pytest.approx(50.3)
        assert fix.longitude == pytest.approx(4.65)

    def test_malformed_gps(self):
        parser = RKDParser()
        session = RKDSession()
        parser._parse_gps_record(b"\x00" * 10, 0, session)
        assert len(session.gps_fixes) == 0


class TestParseAccelRecord:
    def test_valid_accel(self):
        parser = RKDParser()
        parser._accel_by_frame = {}
        parser._parse_accel_record(_accel_payload(0, 0, 1000), 100)
        assert 100 in parser._accel_by_frame
        assert parser._accel_by_frame[100][2] == pytest.approx(9.81)

    def test_malformed_accel(self):
        parser = RKDParser()
        parser._accel_by_frame = {}
        parser._parse_accel_record(b"\x00" * 5, 100)
        assert 100 not in parser._accel_by_frame


class TestParseGyroRecord:
    def test_valid_gyro(self):
        parser = RKDParser()
        parser._gyro_by_frame = {}
        parser._parse_gyro_record(_gyro_payload(280, 0, 0), 100)
        assert 100 in parser._gyro_by_frame
        assert parser._gyro_by_frame[100][0] == pytest.approx(10.0)

    def test_malformed_gyro(self):
        parser = RKDParser()
        parser._gyro_by_frame = {}
        parser._parse_gyro_record(b"\x00" * 5, 100)
        assert 100 not in parser._gyro_by_frame


class TestParseTerminatorRecord:
    def test_valid_terminator(self):
        parser = RKDParser()
        session = RKDSession()
        payload = struct.pack("<I", 1617533000) + b"\x00" * 8
        parser._parse_terminator_record(payload, session)
        assert session.terminator_timestamp == 1617533000

    def test_short_terminator(self):
        parser = RKDParser()
        session = RKDSession()
        parser._parse_terminator_record(b"\x00\x00", session)
        assert session.terminator_timestamp == 0


class TestMergeImu:
    def test_empty(self):
        parser = RKDParser()
        parser._accel_by_frame = {}
        parser._gyro_by_frame = {}
        session = RKDSession()
        parser._merge_imu(session)
        assert len(session.imu_frames) == 0

    def test_both_sensors(self):
        parser = RKDParser()
        parser._accel_by_frame = {100: (1.0, 2.0, 9.81)}
        parser._gyro_by_frame = {100: (0.1, 0.2, 0.3)}
        session = RKDSession()
        parser._merge_imu(session)
        assert len(session.imu_frames) == 1
        assert session.imu_frames[0].accel_x == 1.0
        assert session.imu_frames[0].gyro_z == 0.3

    def test_accel_only_frame(self):
        parser = RKDParser()
        parser._accel_by_frame = {100: (1.0, 2.0, 9.81)}
        parser._gyro_by_frame = {}
        session = RKDSession()
        parser._merge_imu(session)
        assert len(session.imu_frames) == 1
        assert session.imu_frames[0].gyro_x == 0.0

    def test_gyro_only_frame(self):
        parser = RKDParser()
        parser._accel_by_frame = {}
        parser._gyro_by_frame = {100: (0.1, 0.2, 0.3)}
        session = RKDSession()
        parser._merge_imu(session)
        assert len(session.imu_frames) == 1
        assert session.imu_frames[0].accel_x == 0.0


# ─────────────────────────────────────────────────────────────────────────────
# Parse records dispatch
# ─────────────────────────────────────────────────────────────────────────────

class TestParseRecords:
    def test_no_records(self):
        """File with magic + meta header + trailing CRC but zero records."""
        parser = RKDParser()
        session = RKDSession()
        raw = RKD_MAGIC + _meta_header() + b"\x00\x00"
        parser._accel_by_frame = {}
        parser._gyro_by_frame = {}
        parser._parse_records(raw, session)
        assert len(session.gps_fixes) == 0

    def test_truncated_record(self):
        """Record header claims payload larger than remaining data."""
        # Build a record header that says payload is 100 bytes but file ends
        bad_record = struct.pack("<5H", 0, RECORD_GPS, 100, 0, 0)
        raw = RKD_MAGIC + _meta_header() + bad_record + b"\x00\x00"
        parser = RKDParser()
        parser._accel_by_frame = {}
        parser._gyro_by_frame = {}
        session = RKDSession()
        parser._parse_records(raw, session)
        assert len(session.gps_fixes) == 0

    def test_all_record_types(self):
        """Dispatch to all record type handlers."""
        header_rec = _record(RECORD_HEADER, b"KEY\x00VAL\x00")
        gps_rec = _record(RECORD_GPS, _gps_payload(), frame=0)
        accel_rec = _record(RECORD_ACCEL, _accel_payload(), frame=0)
        gyro_rec = _record(RECORD_GYRO, _gyro_payload(), frame=0)
        periodic_rec = _record(RECORD_PERIODIC, b"\x00\x00\x00\x00")
        timestamp_rec = _record(RECORD_TIMESTAMP, b"\x00\x00\x00\x00")
        term_rec = _record(RECORD_TERMINATOR, _terminator_payload())

        raw = RKD_MAGIC + _meta_header()
        raw += header_rec + gps_rec + accel_rec + gyro_rec
        raw += periodic_rec + timestamp_rec + term_rec
        raw += b"\x00\x00"  # trailing CRC

        parser = RKDParser()
        parser._accel_by_frame = {}
        parser._gyro_by_frame = {}
        session = RKDSession()
        parser._parse_records(raw, session)

        assert session.config.get("KEY") == "VAL"
        assert len(session.gps_fixes) == 1
        assert 0 in parser._accel_by_frame
        assert 0 in parser._gyro_by_frame
        assert session.record_counts[RECORD_PERIODIC] == 1
        assert session.record_counts[RECORD_TIMESTAMP] == 1
        assert session.record_counts[RECORD_TERMINATOR] == 1
        assert session.terminator_timestamp == 1617533000

    def test_loop_exhaustion_without_terminator(self):
        """Records run out without a TERMINATOR."""
        gps_rec = _record(RECORD_GPS, _gps_payload(), frame=0)
        raw = RKD_MAGIC + _meta_header() + gps_rec + b"\x00\x00"
        parser = RKDParser()
        parser._accel_by_frame = {}
        parser._gyro_by_frame = {}
        session = RKDSession()
        parser._parse_records(raw, session)
        assert len(session.gps_fixes) == 1


# ─────────────────────────────────────────────────────────────────────────────
# Full parse integration
# ─────────────────────────────────────────────────────────────────────────────

class TestParseIntegration:
    def test_parse_sample_file(self):
        """Parse the real sample file end-to-end."""
        parser = RKDParser()
        session = parser.parse(SAMPLE_RKD)
        assert session.car_id == 11098
        assert len(session.gps_fixes) == 50
        assert len(session.imu_frames) > 0
        assert session.max_speed_kmh > 0
        assert session.total_distance_km > 0
        assert session.duration_seconds > 0

    def test_parse_crafted_file(self, tmp_path):
        """Parse a crafted RKD file from disk."""
        gps1 = _record(RECORD_GPS, _gps_payload(gps_ts=1302000000), frame=0)
        accel1 = _record(RECORD_ACCEL, _accel_payload(), frame=0)
        gyro1 = _record(RECORD_GYRO, _gyro_payload(), frame=0)
        gps2 = _record(RECORD_GPS, _gps_payload(gps_ts=1302000001), frame=6)
        accel2 = _record(RECORD_ACCEL, _accel_payload(), frame=6)
        gyro2 = _record(RECORD_GYRO, _gyro_payload(), frame=6)

        raw = _minimal_rkd(gps1, accel1, gyro1, gps2, accel2, gyro2)
        rkd_file = tmp_path / "test.rkd"
        rkd_file.write_bytes(raw)

        parser = RKDParser()
        session = parser.parse(rkd_file)
        assert len(session.gps_fixes) == 2
        assert len(session.imu_frames) == 2


# ─────────────────────────────────────────────────────────────────────────────
# CSV export
# ─────────────────────────────────────────────────────────────────────────────

class TestExportCsv:
    def test_no_imu_frames(self, tmp_path, capsys):
        """Early return when imu_frames is empty."""
        session = RKDSession(file_path="test.rkd")
        session.gps_fixes.append(GPSFix(0, 100, 0, 18, 50.0, 4.0, 10.0, 90.0, 250.0, 0))
        export_csv(session, tmp_path / "out.csv")
        assert "Warning" in capsys.readouterr().err

    def test_no_gps_fixes(self, tmp_path, capsys):
        """Early return when gps_fixes is empty."""
        session = RKDSession(file_path="test.rkd")
        session.imu_frames.append(IMUFrame(0, 1.0, 0.5, 9.81, 0.1, 0.2, 0.3))
        export_csv(session, tmp_path / "out.csv")
        assert "Warning" in capsys.readouterr().err

    def test_normal_export(self, tmp_path):
        """Normal CSV export with interpolation."""
        session = _make_session_with_data(num_gps=3, num_imu=18, braking_frame=3)
        out = tmp_path / "out.csv"
        export_csv(session, out)
        assert out.exists()
        with open(out) as f:
            reader = csv.reader(f)
            headers = next(reader)
            rows = list(reader)
        assert "utc (ms)" in headers
        assert len(rows) > 0
        # Check braking column — find the braking frame row
        braking_idx = headers.index("braking")
        has_braking = any(row[braking_idx] == "1" for row in rows)
        has_not_braking = any(row[braking_idx] == "0" for row in rows)
        assert has_braking
        assert has_not_braking

    def test_single_gps_fix_fallback(self, tmp_path):
        """total_frames == 0 triggers ms_per_frame fallback."""
        session = RKDSession(file_path="test.rkd", file_size=100)
        fix = GPSFix(0, 1302000000, _gps_to_utc_ms(1302000000), 18,
                     50.3, 4.65, 10.0, 90.0, 250.0, 5)
        session.gps_fixes.append(fix)
        session.imu_frames.append(
            IMUFrame(0, 1.0, 0.5, 9.81, 0.1, 0.2, 5.0))
        out = tmp_path / "out.csv"
        export_csv(session, out)
        assert out.exists()

    def test_imu_outside_gps_range(self, tmp_path):
        """IMU frames before first and after last GPS fix are skipped."""
        session = RKDSession(file_path="test.rkd", file_size=100)
        session.gps_fixes.append(
            GPSFix(10, 1302000000, _gps_to_utc_ms(1302000000), 18,
                   50.3, 4.65, 10.0, 90.0, 250.0, 5))
        session.gps_fixes.append(
            GPSFix(20, 1302000001, _gps_to_utc_ms(1302000001), 18,
                   50.3001, 4.6501, 11.0, 100.0, 251.0, 5))
        # IMU frames: before, inside, and after GPS range
        session.imu_frames.append(IMUFrame(5, 1.0, 0.5, 9.81, 0.1, 0.2, 5.0))
        session.imu_frames.append(IMUFrame(15, 1.0, 0.5, 9.81, 0.1, 0.2, 5.0))
        session.imu_frames.append(IMUFrame(25, 1.0, 0.5, 9.81, 0.1, 0.2, 5.0))
        out = tmp_path / "out.csv"
        export_csv(session, out)
        with open(out) as f:
            rows = list(csv.reader(f))
        # Header + 1 data row (only frame 15 is in range)
        assert len(rows) == 2

    def test_frame_span_zero(self, tmp_path):
        """Two consecutive GPS fixes at the same frame triggers t=0.0 (line 532).

        Need 3 GPS fixes so that the middle pair shares a frame, but there's
        still a next GPS fix (gps_idx < len-1 is True), entering the
        interpolation branch where frame_span == 0.
        """
        session = RKDSession(file_path="test.rkd", file_size=100)
        session.gps_fixes.append(
            GPSFix(0, 1302000000, _gps_to_utc_ms(1302000000), 18,
                   50.3, 4.65, 10.0, 90.0, 250.0, 5))
        # Two fixes at the same frame
        session.gps_fixes.append(
            GPSFix(6, 1302000001, _gps_to_utc_ms(1302000001), 18,
                   50.3001, 4.6501, 11.0, 100.0, 251.0, 5))
        session.gps_fixes.append(
            GPSFix(6, 1302000002, _gps_to_utc_ms(1302000002), 18,
                   50.3002, 4.6502, 12.0, 110.0, 252.0, 5))
        # IMU frame at frame 6 — between gps_fixes[1] and [2] which share frame 6
        session.imu_frames.append(IMUFrame(0, 1.0, 0.5, 9.81, 0.1, 0.2, 5.0))
        session.imu_frames.append(IMUFrame(3, 1.0, 0.5, 9.81, 0.1, 0.2, 5.0))
        session.imu_frames.append(IMUFrame(6, 1.0, 0.5, 9.81, 0.1, 0.2, 5.0))
        out = tmp_path / "out.csv"
        export_csv(session, out)
        assert out.exists()

    def test_imu_at_last_gps_fix(self, tmp_path):
        """IMU frame at the last GPS fix uses the direct (non-interpolation) path."""
        session = RKDSession(file_path="test.rkd", file_size=100)
        session.gps_fixes.append(
            GPSFix(0, 1302000000, _gps_to_utc_ms(1302000000), 18,
                   50.3, 4.65, 10.0, 90.0, 250.0, 5))
        session.gps_fixes.append(
            GPSFix(6, 1302000001, _gps_to_utc_ms(1302000001), 18,
                   50.3001, 4.6501, 11.0, 100.0, 251.0, 5))
        # IMU frame exactly at frame 6 (last GPS fix)
        session.imu_frames.append(IMUFrame(0, 1.0, 0.5, 9.81, 0.1, 0.2, 5.0))
        session.imu_frames.append(IMUFrame(6, 1.0, 0.5, 9.81, 0.1, 0.2, 5.0))
        out = tmp_path / "out.csv"
        export_csv(session, out)
        with open(out) as f:
            rows = list(csv.reader(f))
        assert len(rows) == 3  # header + 2 data rows

    def test_sample_file_csv(self, tmp_path):
        """Export the real sample file to CSV."""
        parser = RKDParser()
        session = parser.parse(SAMPLE_RKD)
        out = tmp_path / "sample.csv"
        export_csv(session, out)
        assert out.exists()
        with open(out) as f:
            reader = csv.reader(f)
            headers = next(reader)
            rows = list(reader)
        assert len(headers) == 24
        assert len(rows) > 0


# ─────────────────────────────────────────────────────────────────────────────
# GPX export
# ─────────────────────────────────────────────────────────────────────────────

class TestExportGpx:
    def test_no_gps_fixes(self, tmp_path, capsys):
        session = RKDSession(file_path="test.rkd")
        export_gpx(session, tmp_path / "out.gpx")
        assert "Warning" in capsys.readouterr().err

    def test_normal_export(self, tmp_path):
        session = _make_session_with_data(num_gps=3)
        out = tmp_path / "out.gpx"
        export_gpx(session, out)
        content = out.read_text()
        assert '<?xml version="1.0"' in content
        assert "<trkpt" in content
        assert content.count("<trkpt") == 3

    def test_sample_file_gpx(self, tmp_path):
        parser = RKDParser()
        session = parser.parse(SAMPLE_RKD)
        out = tmp_path / "sample.gpx"
        export_gpx(session, out)
        assert out.exists()


# ─────────────────────────────────────────────────────────────────────────────
# Print session info
# ─────────────────────────────────────────────────────────────────────────────

class TestPrintSessionInfo:
    def test_full_session(self, capsys):
        session = _make_session_with_data(num_gps=3, num_imu=18)
        session.config["HW_ID"] = "RK1234"
        session.record_counts = {RECORD_GPS: 3, RECORD_ACCEL: 18, 999: 1}
        print_session_info(session)
        out = capsys.readouterr().out
        assert "RKD Session" in out
        assert "Car ID" in out
        assert "GPS data" in out
        assert "IMU data" in out
        assert "HW_ID" in out
        # Unknown record type
        assert "UNKNOWN(0x03e7)" in out

    def test_empty_session(self, capsys):
        session = RKDSession()
        print_session_info(session)
        out = capsys.readouterr().out
        assert "RKD Session" in out
        # No GPS or IMU sections
        assert "GPS data" not in out
        assert "IMU data" not in out

    def test_no_timestamp(self, capsys):
        session = RKDSession()
        session.session_timestamp = 0
        print_session_info(session)
        out = capsys.readouterr().out
        assert "Session start" not in out


# ─────────────────────────────────────────────────────────────────────────────
# create_sample_rkd
# ─────────────────────────────────────────────────────────────────────────────

class TestCreateSampleRkd:
    def test_from_sample_file(self, tmp_path):
        """Truncate sample file at 5 GPS fixes."""
        out = tmp_path / "truncated.rkd"
        create_sample_rkd(SAMPLE_RKD, out, max_gps_fixes=5)
        assert out.exists()
        # Parse the truncated file
        parser = RKDParser()
        session = parser.parse(out)
        assert len(session.gps_fixes) <= 5

    def test_with_terminator(self, tmp_path):
        """File with a TERMINATOR record — stops before GPS limit."""
        gps_rec = _record(RECORD_GPS, _gps_payload(), frame=0)
        term_rec = _record(RECORD_TERMINATOR, _terminator_payload())
        raw = _minimal_rkd(gps_rec, term_rec)
        src = tmp_path / "with_term.rkd"
        src.write_bytes(raw)
        out = tmp_path / "sample_with_term.rkd"
        create_sample_rkd(src, out, max_gps_fixes=100)
        assert out.exists()

    def test_truncated_record_in_source(self, tmp_path):
        """Source file has a truncated record at the end."""
        gps_rec = _record(RECORD_GPS, _gps_payload(), frame=0)
        # Build file with a record header claiming 100 bytes but no payload
        bad_header = struct.pack("<5H", 0, RECORD_GPS, 100, 0, 0)
        raw = RKD_MAGIC + _meta_header() + gps_rec + bad_header + b"\x00\x00"
        src = tmp_path / "truncated_src.rkd"
        src.write_bytes(raw)
        out = tmp_path / "sample_trunc.rkd"
        create_sample_rkd(src, out, max_gps_fixes=100)
        assert out.exists()

    def test_no_records_in_source(self, tmp_path):
        """Source file with only magic + meta header + CRC."""
        raw = RKD_MAGIC + _meta_header() + b"\x00\x00"
        src = tmp_path / "empty.rkd"
        src.write_bytes(raw)
        out = tmp_path / "sample_empty.rkd"
        create_sample_rkd(src, out, max_gps_fixes=10)
        assert out.exists()


# ─────────────────────────────────────────────────────────────────────────────
# find_rkd_files
# ─────────────────────────────────────────────────────────────────────────────

class TestFindRkdFiles:
    def test_finds_files(self):
        result = find_rkd_files(SAMPLE_RKD.parent)
        assert len(result) >= 1
        assert any(p.suffix == ".rkd" for p in result)

    def test_empty_directory(self, tmp_path):
        result = find_rkd_files(tmp_path)
        assert result == []


# ─────────────────────────────────────────────────────────────────────────────
# process_file
# ─────────────────────────────────────────────────────────────────────────────

class TestProcessFile:
    def test_info_only(self, capsys):
        session = process_file(SAMPLE_RKD, info_only=True)
        assert session.car_id == 11098
        out = capsys.readouterr().out
        assert "RKD Session" in out

    def test_full_export_default_dir(self, tmp_path):
        # Copy sample to tmp_path so exports go there
        src = tmp_path / "test.rkd"
        src.write_bytes(SAMPLE_RKD.read_bytes())
        session = process_file(src)
        assert (tmp_path / "test.csv").exists()
        assert (tmp_path / "test.gpx").exists()

    def test_custom_output_dir(self, tmp_path):
        out_dir = tmp_path / "exports"
        session = process_file(SAMPLE_RKD, output_dir=out_dir)
        assert (out_dir / "sample_mettet.csv").exists()
        assert (out_dir / "sample_mettet.gpx").exists()

    def test_no_csv(self, tmp_path):
        out_dir = tmp_path / "no_csv"
        session = process_file(SAMPLE_RKD, output_dir=out_dir, no_csv=True)
        assert not (out_dir / "sample_mettet.csv").exists()
        assert (out_dir / "sample_mettet.gpx").exists()

    def test_no_gpx(self, tmp_path):
        out_dir = tmp_path / "no_gpx"
        session = process_file(SAMPLE_RKD, output_dir=out_dir, no_gpx=True)
        assert (out_dir / "sample_mettet.csv").exists()
        assert not (out_dir / "sample_mettet.gpx").exists()


# ─────────────────────────────────────────────────────────────────────────────
# CLI main()
# ─────────────────────────────────────────────────────────────────────────────

class TestMain:
    def test_no_args(self):
        """No arguments → prints help and exits."""
        with mock.patch("sys.argv", ["rkd_parser.py"]):
            with pytest.raises(SystemExit) as exc_info:
                main()
            assert exc_info.value.code == 1

    def test_nonexistent_file(self):
        """Single file that doesn't exist → error exit."""
        with mock.patch("sys.argv", ["rkd_parser.py", "/nonexistent/file.rkd"]):
            with pytest.raises(SystemExit) as exc_info:
                main()
            assert exc_info.value.code == 1

    def test_single_file(self, tmp_path):
        """Single file mode — processes and exports."""
        src = tmp_path / "test.rkd"
        src.write_bytes(SAMPLE_RKD.read_bytes())
        with mock.patch("sys.argv", ["rkd_parser.py", str(src)]):
            main()
        assert (tmp_path / "test.csv").exists()

    def test_single_file_info(self, capsys):
        """Single file mode with --info."""
        with mock.patch("sys.argv", ["rkd_parser.py", str(SAMPLE_RKD), "--info"]):
            main()
        assert "RKD Session" in capsys.readouterr().out

    def test_single_file_with_sample(self, tmp_path):
        """Single file + --sample flag."""
        src = tmp_path / "test.rkd"
        src.write_bytes(SAMPLE_RKD.read_bytes())
        with mock.patch("sys.argv", ["rkd_parser.py", str(src), "--sample", "5"]):
            main()
        assert (tmp_path / "sample_test.rkd").exists()

    def test_single_file_with_sample_and_output_dir(self, tmp_path):
        """Single file + --sample + --output-dir."""
        src = tmp_path / "test.rkd"
        src.write_bytes(SAMPLE_RKD.read_bytes())
        out_dir = tmp_path / "out"
        out_dir.mkdir()
        with mock.patch("sys.argv", [
            "rkd_parser.py", str(src), "--sample", "5", "--output-dir", str(out_dir)
        ]):
            main()
        assert (out_dir / "sample_test.rkd").exists()

    def test_all_in_with_files(self, tmp_path):
        """Batch mode — processes all .rkd files in directory."""
        src = tmp_path / "test.rkd"
        src.write_bytes(SAMPLE_RKD.read_bytes())
        with mock.patch("sys.argv", ["rkd_parser.py", "--all-in", str(tmp_path)]):
            main()
        assert (tmp_path / "test.csv").exists()

    def test_all_in_empty_dir(self, tmp_path):
        """Batch mode — no .rkd files found → error exit."""
        with mock.patch("sys.argv", ["rkd_parser.py", "--all-in", str(tmp_path)]):
            with pytest.raises(SystemExit) as exc_info:
                main()
            assert exc_info.value.code == 1

    def test_all_in_with_options(self, tmp_path):
        """Batch mode with --info, --no-csv, --no-gpx, --output-dir."""
        src = tmp_path / "test.rkd"
        src.write_bytes(SAMPLE_RKD.read_bytes())
        out_dir = tmp_path / "batch_out"
        with mock.patch("sys.argv", [
            "rkd_parser.py", "--all-in", str(tmp_path),
            "--output-dir", str(out_dir), "--no-gpx"
        ]):
            main()
        assert (out_dir / "test.csv").exists()
        assert not (out_dir / "test.gpx").exists()
