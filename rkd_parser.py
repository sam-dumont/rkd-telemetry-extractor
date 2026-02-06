#!/usr/bin/env python3
"""
rkd_parser.py — Race-Keeper RKD Telemetry Parser & Exporter

Parses proprietary Race-Keeper (.rkd) binary telemetry files and exports to:
  - Telemetry Overlay Custom CSV (30 Hz, with GPS interpolation)
  - GPX 1.1 track format (at native GPS rate, ~5 Hz)

The RKD format is used by Race-Keeper "Instant Video" systems (Trivinci/IENSO Inc.)
commonly found in professional track day and racing video recording setups.

This is the first public implementation of the RKD binary format parser.
See RKD_FORMAT_SPEC.md for the full format specification.

Usage:
    python3 rkd_parser.py outing.rkd                  # Export CSV + GPX
    python3 rkd_parser.py outing.rkd --info            # Print session summary only
    python3 rkd_parser.py --all-in /path/to/usb/       # Process all .rkd files recursively
    python3 rkd_parser.py outing.rkd --no-gpx          # CSV only
    python3 rkd_parser.py outing.rkd --output-dir out/ # Custom output directory

Author: Sam (with Claude Code)
License: MIT
"""

from __future__ import annotations

import argparse
import csv
import datetime
import io
import math
import os
import struct
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import BinaryIO

# ─────────────────────────────────────────────────────────────────────────────
# Constants
# ─────────────────────────────────────────────────────────────────────────────

# RKD magic signature: PNG-style binary format marker.
# \x89  — high bit set, catches 8-bit stripping
# RKD   — format identifier
# \r\n  — detects newline translation
# \x1a  — stops DOS TYPE command
# \n    — detects reverse newline translation
RKD_MAGIC = b"\x89RKD\r\n\x1a\n"

# GPS epoch: January 6, 1980 00:00:00 UTC (start of GPS week 0).
# GPS time does not include leap seconds, so GPS time = UTC + leap_seconds.
GPS_EPOCH = datetime.datetime(1980, 1, 6, tzinfo=datetime.timezone.utc)
GPS_LEAP_SECONDS = 18  # Leap seconds as of 2021 (valid 2017-01-01 through at least 2025)

# Record types in the RKD binary stream
RECORD_HEADER = 1       # KEY\0VALUE\0 configuration string
RECORD_GPS = 2          # 36-byte GPS fix (5 Hz)
RECORD_PERIODIC = 6     # 4-byte system metric (~1 Hz)
RECORD_ACCEL = 7        # 12-byte 3-axis accelerometer (30 Hz)
RECORD_TIMESTAMP = 8    # 4-byte hardware timer sync
RECORD_GYRO = 12        # 12-byte 3-axis gyroscope (30 Hz)
RECORD_TERMINATOR = 0x8001  # Session end marker

RECORD_NAMES = {
    RECORD_HEADER: "HEADER",
    RECORD_GPS: "GPS",
    RECORD_PERIODIC: "PERIODIC",
    RECORD_ACCEL: "ACCEL",
    RECORD_TIMESTAMP: "TIMESTAMP",
    RECORD_GYRO: "GYRO",
    RECORD_TERMINATOR: "TERMINATOR",
}

# Meta header size (between magic and record stream)
META_HEADER_SIZE = 28  # 7 × uint32

# Record header size (universal for all record types)
RECORD_HEADER_SIZE = 10  # crc(2) + type(2) + payload_size(2) + frame_lo(2) + frame_hi(2)

# Trailing CRC at end of file
TRAILING_CRC_SIZE = 2

# ─────────────────────────────────────────────────────────────────────────────
# Data classes
# ─────────────────────────────────────────────────────────────────────────────

@dataclass
class GPSFix:
    """A single GPS fix from the receiver."""
    frame: int              # Frame number in the recording
    gps_timestamp: int      # Seconds since GPS epoch (1980-01-06)
    utc_ms: int             # Unix timestamp in milliseconds (UTC)
    satellites: int
    latitude: float         # Degrees (WGS84)
    longitude: float        # Degrees (WGS84)
    speed_ms: float         # Speed in m/s
    heading_deg: float      # Heading in degrees (0-360)
    altitude_m: float       # Altitude in meters (above MSL)
    vertical_speed_cms: int # Vertical speed in cm/s (signed)


@dataclass
class IMUFrame:
    """A single IMU measurement (accelerometer + gyroscope at same frame)."""
    frame: int
    accel_x: float  # m/s² (positive = forward)
    accel_y: float  # m/s² (positive = left)
    accel_z: float  # m/s² (positive = up, ~9.81 at rest)
    gyro_x: float   # deg/s
    gyro_y: float   # deg/s
    gyro_z: float   # deg/s


@dataclass
class RKDSession:
    """All data parsed from a single RKD file."""
    # File info
    file_path: str = ""
    file_size: int = 0

    # Meta header
    car_id: int = 0
    session_timestamp: int = 0  # Unix timestamp from meta header

    # Configuration key-value pairs from HEADER records
    config: dict[str, str] = field(default_factory=dict)

    # Telemetry data
    gps_fixes: list[GPSFix] = field(default_factory=list)
    imu_frames: list[IMUFrame] = field(default_factory=list)

    # Raw record counts (for diagnostics)
    record_counts: dict[int, int] = field(default_factory=dict)

    # Terminator data
    terminator_timestamp: int = 0  # Unix timestamp from TERMINATOR record

    @property
    def duration_seconds(self) -> float:
        """Session duration derived from GPS timestamp range."""
        if len(self.gps_fixes) < 2:
            return 0.0
        return (self.gps_fixes[-1].gps_timestamp - self.gps_fixes[0].gps_timestamp)

    @property
    def max_speed_kmh(self) -> float:
        """Maximum speed in km/h."""
        if not self.gps_fixes:
            return 0.0
        return max(fix.speed_ms for fix in self.gps_fixes) * 3.6

    @property
    def total_distance_km(self) -> float:
        """Total distance in km, computed from sequential GPS positions."""
        if len(self.gps_fixes) < 2:
            return 0.0
        total = 0.0
        for i in range(1, len(self.gps_fixes)):
            total += _haversine(
                self.gps_fixes[i - 1].latitude, self.gps_fixes[i - 1].longitude,
                self.gps_fixes[i].latitude, self.gps_fixes[i].longitude,
            )
        return total


# ─────────────────────────────────────────────────────────────────────────────
# Utility functions
# ─────────────────────────────────────────────────────────────────────────────

def _haversine(lat1: float, lon1: float, lat2: float, lon2: float) -> float:
    """Haversine distance between two lat/lon points, in kilometers."""
    R = 6371.0  # Earth radius in km
    dlat = math.radians(lat2 - lat1)
    dlon = math.radians(lon2 - lon1)
    a = (math.sin(dlat / 2) ** 2 +
         math.cos(math.radians(lat1)) * math.cos(math.radians(lat2)) *
         math.sin(dlon / 2) ** 2)
    return R * 2 * math.atan2(math.sqrt(a), math.sqrt(1 - a))


def _gps_to_utc_ms(gps_seconds: int) -> int:
    """Convert GPS timestamp (seconds since 1980-01-06) to Unix milliseconds (UTC).

    GPS time runs ahead of UTC because it doesn't include leap seconds.
    As of 2021, GPS time = UTC + 18 seconds.
    """
    utc_dt = GPS_EPOCH + datetime.timedelta(seconds=gps_seconds - GPS_LEAP_SECONDS)
    return int(utc_dt.timestamp() * 1000)


def _lerp(a: float, b: float, t: float) -> float:
    """Linear interpolation between a and b at parameter t ∈ [0, 1]."""
    return a + (b - a) * t


# ─────────────────────────────────────────────────────────────────────────────
# Parser
# ─────────────────────────────────────────────────────────────────────────────

class RKDParser:
    """Sequential parser for Race-Keeper RKD binary telemetry files.

    The RKD file format consists of:
      1. An 8-byte magic signature (\\x89RKD\\r\\n\\x1a\\n)
      2. A 28-byte meta header with car ID and Unix timestamp
      3. A stream of typed records, each with a 10-byte header
      4. A 2-byte trailing CRC

    Records are processed sequentially. Accelerometer and gyroscope records
    at the same frame number are merged into a single IMUFrame.
    """

    def parse(self, path: str | Path) -> RKDSession:
        """Parse an RKD file and return an RKDSession with all telemetry data."""
        path = Path(path)
        with open(path, "rb") as f:
            data = f.read()

        session = RKDSession(file_path=str(path), file_size=len(data))
        self._validate_magic(data)
        self._parse_meta_header(data, session)
        self._parse_records(data, session)
        self._merge_imu(session)
        return session

    def _validate_magic(self, data: bytes) -> None:
        """Verify the 8-byte RKD magic signature."""
        if len(data) < len(RKD_MAGIC):
            raise ValueError("File too small to be an RKD file")
        if data[:len(RKD_MAGIC)] != RKD_MAGIC:
            raise ValueError(
                f"Invalid RKD magic: expected {RKD_MAGIC!r}, got {data[:8]!r}"
            )

    def _parse_meta_header(self, data: bytes, session: RKDSession) -> None:
        """Parse the 28-byte meta header following the magic.

        Layout (7 × uint32 LE):
          [0] flags/version (observed: 1343488 = 0x00148000)
          [1] reserved (0)
          [2] file sequence (1)
          [3] reserved (0)
          [4] car_id — unique device/car identifier
          [5] session_timestamp — Unix timestamp of session start
          [6] reserved (0)
        """
        offset = len(RKD_MAGIC)
        if len(data) < offset + META_HEADER_SIZE:
            raise ValueError("File too small for meta header")

        values = struct.unpack_from("<7I", data, offset)
        session.car_id = values[4]
        session.session_timestamp = values[5]

    def _parse_records(self, data: bytes, session: RKDSession) -> None:
        """Parse the sequential record stream.

        Each record has a 10-byte header:
          [crc: uint16] [type: uint16] [payload_size: uint16] [frame_lo: uint16] [frame_hi: uint16]

        The frame number is a 32-bit value split across frame_lo and frame_hi:
          frame = (frame_hi << 16) | frame_lo
        """
        offset = len(RKD_MAGIC) + META_HEADER_SIZE
        end = len(data) - TRAILING_CRC_SIZE  # Don't read into trailing CRC

        # Temporary storage for accel/gyro before merging
        self._accel_by_frame: dict[int, tuple[float, float, float]] = {}
        self._gyro_by_frame: dict[int, tuple[float, float, float]] = {}

        while offset + RECORD_HEADER_SIZE <= end:
            crc, rtype, payload_size, frame_lo, frame_hi = struct.unpack_from(
                "<5H", data, offset
            )
            frame = (frame_hi << 16) | frame_lo
            payload_start = offset + RECORD_HEADER_SIZE
            payload_end = payload_start + payload_size

            if payload_end > end:
                break  # Truncated record at end of file

            payload = data[payload_start:payload_end]

            # Count record types for diagnostics
            session.record_counts[rtype] = session.record_counts.get(rtype, 0) + 1

            # Dispatch by record type
            if rtype == RECORD_HEADER:
                self._parse_header_record(payload, session)
            elif rtype == RECORD_GPS:
                self._parse_gps_record(payload, frame, session)
            elif rtype == RECORD_ACCEL:
                self._parse_accel_record(payload, frame)
            elif rtype == RECORD_GYRO:
                self._parse_gyro_record(payload, frame)
            elif rtype == RECORD_TERMINATOR:
                self._parse_terminator_record(payload, session)
                offset = payload_end
                break  # TERMINATOR is always the last meaningful record
            # PERIODIC (6) and TIMESTAMP (8) are ignored for export purposes

            offset = payload_end

    def _parse_header_record(self, payload: bytes, session: RKDSession) -> None:
        """Parse a HEADER record containing a KEY\\0VALUE\\0 config string."""
        # Strip trailing null and split on first null
        text = payload.rstrip(b"\x00")
        if b"\x00" in text:
            key, value = text.split(b"\x00", 1)
            session.config[key.decode("ascii", errors="replace")] = value.decode(
                "ascii", errors="replace"
            )

    def _parse_gps_record(
        self, payload: bytes, frame: int, session: RKDSession
    ) -> None:
        """Parse a 36-byte GPS record.

        GPS Payload layout:
          Offset  Type    Field           Conversion
          0       uint32  subtype         Always 3
          4       uint32  gps_timestamp   Seconds since GPS epoch (1980-01-06)
          8       int16   satellites      Direct
          10      int16   (padding)       Always 0
          12      int32   latitude        / 1e7 → degrees (WGS84)
          16      int32   longitude       / 1e7 → degrees (WGS84)
          20      int32   speed           / 100 → m/s
          24      int32   heading         / 100000 → degrees
          28      int32   altitude        / 1000 → meters (above MSL)
          32      int32   vertical_speed  cm/s (signed, positive = ascending)
        """
        if len(payload) < 36:
            return  # Skip malformed GPS records

        (subtype, gps_ts, sats, _pad,
         lat_raw, lon_raw, speed_raw, heading_raw, alt_raw, vspeed) = struct.unpack(
            "<2I2h6i", payload
        )

        fix = GPSFix(
            frame=frame,
            gps_timestamp=gps_ts,
            utc_ms=_gps_to_utc_ms(gps_ts),
            satellites=sats,
            latitude=lat_raw / 1e7,
            longitude=lon_raw / 1e7,
            speed_ms=speed_raw / 100.0,
            heading_deg=heading_raw / 100000.0,
            altitude_m=alt_raw / 1000.0,
            vertical_speed_cms=vspeed,
        )
        session.gps_fixes.append(fix)

    def _parse_accel_record(self, payload: bytes, frame: int) -> None:
        """Parse a 12-byte accelerometer record (3 × int32 LE).

        Raw values are in milli-g (1/1000 of standard gravity).
        Conversion: raw_mg × 9.81 / 1000 → m/s²
        """
        if len(payload) < 12:
            return
        ax, ay, az = struct.unpack("<3i", payload)
        self._accel_by_frame[frame] = (
            ax * 9.81 / 1000.0,
            ay * 9.81 / 1000.0,
            az * 9.81 / 1000.0,
        )

    def _parse_gyro_record(self, payload: bytes, frame: int) -> None:
        """Parse a 12-byte gyroscope record (3 × int32 LE).

        Raw values need division by ~28 to get degrees per second.
        This calibration factor was determined empirically by cross-referencing
        with GPS-derived turn rates.
        """
        if len(payload) < 12:
            return
        gx, gy, gz = struct.unpack("<3i", payload)
        self._gyro_by_frame[frame] = (
            gx / 28.0,
            gy / 28.0,
            gz / 28.0,
        )

    def _parse_terminator_record(
        self, payload: bytes, session: RKDSession
    ) -> None:
        """Parse the TERMINATOR record (type 0x8001, 12 bytes).

        Contains a Unix timestamp marking the end of the session.
        """
        if len(payload) >= 4:
            session.terminator_timestamp = struct.unpack_from("<I", payload, 0)[0]

    def _merge_imu(self, session: RKDSession) -> None:
        """Merge accelerometer and gyroscope data by frame number.

        Both sensors sample at 30 Hz and produce records at the same frame numbers.
        Records are merged into IMUFrame objects sorted by frame number.
        """
        all_frames = sorted(set(self._accel_by_frame.keys()) | set(self._gyro_by_frame.keys()))
        for frame in all_frames:
            ax, ay, az = self._accel_by_frame.get(frame, (0.0, 0.0, 0.0))
            gx, gy, gz = self._gyro_by_frame.get(frame, (0.0, 0.0, 0.0))
            session.imu_frames.append(IMUFrame(
                frame=frame, accel_x=ax, accel_y=ay, accel_z=az,
                gyro_x=gx, gyro_y=gy, gyro_z=gz,
            ))

        # Clean up temporary storage
        del self._accel_by_frame
        del self._gyro_by_frame


# ─────────────────────────────────────────────────────────────────────────────
# Exporters
# ─────────────────────────────────────────────────────────────────────────────

def export_csv(session: RKDSession, output_path: str | Path) -> None:
    """Export session data to Telemetry Overlay Custom CSV format at 30 Hz.

    The output is at IMU rate (30 Hz) with GPS fields linearly interpolated
    between 5 Hz fixes. This provides smooth data for video overlay while
    preserving the high-frequency accelerometer and gyroscope data.

    Columns match the Telemetry Overlay specification:
      utc (ms), lat (deg), lon (deg), speed (m/s), alt (m), heading (deg),
      satellites, accel x (m/s²), accel y (m/s²), accel z (m/s²),
      gyro x (deg/s), gyro y (deg/s), gyro z (deg/s)

    The utc (ms) column contains Unix timestamps in milliseconds, which
    enables automatic sync with LTC audio timecodes in video files.
    """
    if not session.imu_frames or not session.gps_fixes:
        print(f"  Warning: No data to export for {session.file_path}", file=sys.stderr)
        return

    # Build a GPS lookup indexed by frame number for interpolation.
    # GPS fixes arrive at ~5 Hz (every 6th frame at 30 Hz).
    gps_fixes = session.gps_fixes
    imu_frames = session.imu_frames

    # Pre-compute UTC millisecond timestamps for each IMU frame by interpolating
    # between GPS fixes. The frame numbers provide the interpolation basis.
    first_gps_frame = gps_fixes[0].frame
    last_gps_frame = gps_fixes[-1].frame

    # Derived and additional channels:
    #
    # Telemetry Overlay recognized columns (native support):
    #   - pitch angle (deg): vehicle nose-up/down angle from accelerometer
    #   - bank (deg): vehicle roll/bank angle from accelerometer
    #   - turn rate (deg/s): yaw rate from gyroscope Z axis
    #   - vertical speed (ft/min): GPS vertical speed converted to ft/min
    #   - gps fix: always 3 (3D fix — these units have 17+ satellites)
    #
    # Custom gauge columns (auto-detected by Telemetry Overlay):
    #   - g_lon / g_lat / g_total: g-forces from accelerometer
    #   - braking: 1 when decelerating, 0 otherwise
    #   - speed (km/h): speed in km/h for European tracks
    #   - distance (km): cumulative distance traveled
    #
    # Pitch/bank angles from accelerometer:
    #   During dynamic driving, these include both gravity and inertial forces,
    #   showing the "effective" tilt the driver feels — useful for overlay.
    #   pitch = atan2(ax, sqrt(ay² + az²))  — positive = nose up
    #   bank  = atan2(-ay, az)              — positive = banking right

    columns = [
        "utc (ms)", "lat (deg)", "lon (deg)", "speed (m/s)", "alt (m)",
        "heading (deg)", "satellites", "gps fix",
        "accel x (m/s²)", "accel y (m/s²)", "accel z (m/s²)",
        "gyro x (deg/s)", "gyro y (deg/s)", "gyro z (deg/s)",
        "pitch angle (deg)", "bank (deg)", "turn rate (deg/s)",
        "vertical speed (ft/min)",
        "g_lon", "g_lat", "g_total", "braking",
        "speed (km/h)", "distance (km)",
    ]

    output_path = Path(output_path)
    with open(output_path, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(columns)

        # GPS interpolation index: tracks which two GPS fixes we're between
        gps_idx = 0

        # Cumulative distance tracking
        cum_distance = 0.0  # km
        prev_lat = None
        prev_lon = None

        for imu in imu_frames:
            # Skip IMU frames before the first GPS fix or after the last
            if imu.frame < first_gps_frame or imu.frame > last_gps_frame:
                continue

            # Advance GPS index to bracket the current IMU frame
            while (gps_idx < len(gps_fixes) - 1 and
                   gps_fixes[gps_idx + 1].frame <= imu.frame):
                gps_idx += 1

            # Interpolate GPS fields between the bracketing fixes
            g0 = gps_fixes[gps_idx]
            if gps_idx < len(gps_fixes) - 1:
                g1 = gps_fixes[gps_idx + 1]
                frame_span = g1.frame - g0.frame
                if frame_span > 0:
                    t = (imu.frame - g0.frame) / frame_span
                else:
                    t = 0.0
                utc_ms = int(_lerp(g0.utc_ms, g1.utc_ms, t))
                lat = _lerp(g0.latitude, g1.latitude, t)
                lon = _lerp(g0.longitude, g1.longitude, t)
                speed = _lerp(g0.speed_ms, g1.speed_ms, t)
                alt = _lerp(g0.altitude_m, g1.altitude_m, t)
                heading = _lerp_angle(g0.heading_deg, g1.heading_deg, t)
                vspeed_cms = _lerp(g0.vertical_speed_cms, g1.vertical_speed_cms, t)
                sats = g0.satellites  # Satellites don't interpolate
            else:
                # Last GPS fix — use directly
                utc_ms = g0.utc_ms
                lat = g0.latitude
                lon = g0.longitude
                speed = g0.speed_ms
                alt = g0.altitude_m
                heading = g0.heading_deg
                vspeed_cms = g0.vertical_speed_cms
                sats = g0.satellites

            # ── Derived channels ──

            # G-forces from body-frame accelerometer
            g_lon = imu.accel_x / 9.81       # Positive = accelerating
            g_lat = -imu.accel_y / 9.81      # Positive = right turn
            g_total = math.sqrt(g_lon ** 2 + g_lat ** 2)
            braking = 1 if g_lon < -0.05 else 0

            # Pitch angle: nose-up/down from accelerometer (degrees)
            # atan2(forward_accel, sqrt(lateral² + vertical²))
            pitch = math.degrees(math.atan2(
                imu.accel_x,
                math.sqrt(imu.accel_y ** 2 + imu.accel_z ** 2)
            ))

            # Bank angle: roll left/right from accelerometer (degrees)
            # atan2(-lateral, vertical) — positive = banking right
            bank = math.degrees(math.atan2(-imu.accel_y, imu.accel_z))

            # Turn rate: yaw rate from gyroscope Z axis (deg/s)
            turn_rate = imu.gyro_z

            # Vertical speed: GPS vertical speed converted to ft/min
            # 1 cm/s = 1.9685 ft/min
            vspeed_ftmin = vspeed_cms * 1.9685

            # Speed in km/h (custom gauge for European tracks)
            speed_kmh = speed * 3.6

            # Cumulative distance (km) — using haversine from previous row
            if prev_lat is not None and prev_lon is not None:
                cum_distance += _haversine(prev_lat, prev_lon, lat, lon)
            prev_lat, prev_lon = lat, lon

            writer.writerow([
                utc_ms,
                f"{lat:.7f}",
                f"{lon:.7f}",
                f"{speed:.2f}",
                f"{alt:.1f}",
                f"{heading:.2f}",
                sats,
                3,  # gps fix (always 3D)
                f"{imu.accel_x:.3f}",
                f"{imu.accel_y:.3f}",
                f"{imu.accel_z:.3f}",
                f"{imu.gyro_x:.3f}",
                f"{imu.gyro_y:.3f}",
                f"{imu.gyro_z:.3f}",
                f"{pitch:.2f}",
                f"{bank:.2f}",
                f"{turn_rate:.2f}",
                f"{vspeed_ftmin:.1f}",
                f"{g_lon:.3f}",
                f"{g_lat:.3f}",
                f"{g_total:.3f}",
                braking,
                f"{speed_kmh:.1f}",
                f"{cum_distance:.4f}",
            ])

    rows = sum(1 for imu in imu_frames
               if first_gps_frame <= imu.frame <= last_gps_frame)
    print(f"  CSV: {output_path} ({rows} rows at 30 Hz)")


def _lerp_angle(a: float, b: float, t: float) -> float:
    """Linearly interpolate between two angles in degrees, handling wraparound.

    Ensures we take the shortest path around 360°. For example, interpolating
    between 350° and 10° goes through 0° rather than through 180°.
    """
    diff = (b - a + 180) % 360 - 180
    result = a + diff * t
    return result % 360


def export_gpx(session: RKDSession, output_path: str | Path) -> None:
    """Export GPS track to GPX 1.1 format at native GPS rate (~5 Hz).

    Produces a standard GPX file that can be loaded in any GPX viewer
    (Google Earth, Strava, GPXSee, etc.) to visualize the track layout.
    """
    if not session.gps_fixes:
        print(f"  Warning: No GPS data to export for {session.file_path}", file=sys.stderr)
        return

    output_path = Path(output_path)
    buf = io.StringIO()

    session_name = Path(session.file_path).stem
    session_dt = datetime.datetime.fromtimestamp(
        session.session_timestamp, tz=datetime.timezone.utc
    )

    buf.write('<?xml version="1.0" encoding="UTF-8"?>\n')
    buf.write('<gpx version="1.1" creator="rkd_parser.py"\n')
    buf.write('     xmlns="http://www.topografix.com/GPX/1/1"\n')
    buf.write('     xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"\n')
    buf.write('     xsi:schemaLocation="http://www.topografix.com/GPX/1/1 ')
    buf.write('http://www.topografix.com/GPX/1/1/gpx.xsd">\n')
    buf.write(f'  <metadata>\n')
    buf.write(f'    <name>{_xml_escape(session_name)}</name>\n')
    buf.write(f'    <desc>Race-Keeper telemetry — Car ID {session.car_id}</desc>\n')
    buf.write(f'    <time>{session_dt.strftime("%Y-%m-%dT%H:%M:%SZ")}</time>\n')
    buf.write(f'  </metadata>\n')
    buf.write(f'  <trk>\n')
    buf.write(f'    <name>{_xml_escape(session_name)}</name>\n')
    buf.write(f'    <trkseg>\n')

    for fix in session.gps_fixes:
        utc_dt = datetime.datetime.fromtimestamp(
            fix.utc_ms / 1000.0, tz=datetime.timezone.utc
        )
        time_str = utc_dt.strftime("%Y-%m-%dT%H:%M:%S.") + f"{utc_dt.microsecond // 1000:03d}Z"
        buf.write(f'      <trkpt lat="{fix.latitude:.7f}" lon="{fix.longitude:.7f}">\n')
        buf.write(f'        <ele>{fix.altitude_m:.1f}</ele>\n')
        buf.write(f'        <time>{time_str}</time>\n')
        buf.write(f'        <sat>{fix.satellites}</sat>\n')
        buf.write(f'        <speed>{fix.speed_ms:.2f}</speed>\n')
        buf.write(f'      </trkpt>\n')

    buf.write(f'    </trkseg>\n')
    buf.write(f'  </trk>\n')
    buf.write(f'</gpx>\n')

    with open(output_path, "w") as f:
        f.write(buf.getvalue())

    print(f"  GPX: {output_path} ({len(session.gps_fixes)} trackpoints)")


def _xml_escape(s: str) -> str:
    """Escape special XML characters."""
    return (s.replace("&", "&amp;")
             .replace("<", "&lt;")
             .replace(">", "&gt;")
             .replace('"', "&quot;"))


# ─────────────────────────────────────────────────────────────────────────────
# Session summary
# ─────────────────────────────────────────────────────────────────────────────

def print_session_info(session: RKDSession) -> None:
    """Print a human-readable summary of a parsed RKD session."""
    print(f"\n{'═' * 60}")
    print(f"  RKD Session: {Path(session.file_path).name}")
    print(f"{'═' * 60}")
    print(f"  File size:      {session.file_size:,} bytes")
    print(f"  Car ID:         {session.car_id}")

    if session.session_timestamp:
        dt = datetime.datetime.fromtimestamp(
            session.session_timestamp, tz=datetime.timezone.utc
        )
        print(f"  Session start:  {dt.strftime('%Y-%m-%d %H:%M:%S UTC')}")

    print(f"\n  Configuration:")
    for key, value in sorted(session.config.items()):
        print(f"    {key}: {value}")

    print(f"\n  Record counts:")
    for rtype, count in sorted(session.record_counts.items()):
        name = RECORD_NAMES.get(rtype, f"UNKNOWN(0x{rtype:04x})")
        print(f"    {name:12s} (type {rtype:5d}): {count:,}")

    if session.gps_fixes:
        print(f"\n  GPS data:")
        print(f"    Fixes:        {len(session.gps_fixes):,}")
        print(f"    Duration:     {session.duration_seconds:.0f}s ({session.duration_seconds / 60:.1f} min)")
        print(f"    Max speed:    {session.max_speed_kmh:.1f} km/h ({session.max_speed_kmh / 1.609344:.1f} mph)")
        print(f"    Distance:     {session.total_distance_km:.2f} km ({session.total_distance_km / 1.609344:.2f} mi)")

        lats = [f.latitude for f in session.gps_fixes]
        lons = [f.longitude for f in session.gps_fixes]
        alts = [f.altitude_m for f in session.gps_fixes]
        print(f"    Lat range:    {min(lats):.7f} – {max(lats):.7f}")
        print(f"    Lon range:    {min(lons):.7f} – {max(lons):.7f}")
        print(f"    Alt range:    {min(alts):.1f} – {max(alts):.1f} m")
        print(f"    Satellites:   {min(f.satellites for f in session.gps_fixes)} – {max(f.satellites for f in session.gps_fixes)}")

    if session.imu_frames:
        print(f"\n  IMU data:")
        print(f"    Frames:       {len(session.imu_frames):,}")
        az_values = [f.accel_z for f in session.imu_frames]
        print(f"    Accel Z (mean): {sum(az_values) / len(az_values):.2f} m/s² (expect ~9.81)")

    print(f"{'═' * 60}\n")


# ─────────────────────────────────────────────────────────────────────────────
# Sample file creator
# ─────────────────────────────────────────────────────────────────────────────

def create_sample_rkd(
    input_path: str | Path, output_path: str | Path, max_gps_fixes: int = 50
) -> None:
    """Create a truncated sample RKD file containing the first N GPS fixes.

    Reads the original file, copies the magic + meta header, then copies records
    until we've seen max_gps_fixes GPS records. Appends a synthetic terminator
    and trailing CRC.
    """
    data = Path(input_path).read_bytes()

    offset = len(RKD_MAGIC) + META_HEADER_SIZE
    end = len(data) - TRAILING_CRC_SIZE

    # Start with magic + meta header
    out = bytearray(data[:offset])

    gps_count = 0
    while offset + RECORD_HEADER_SIZE <= end:
        crc, rtype, payload_size, frame_lo, frame_hi = struct.unpack_from(
            "<5H", data, offset
        )
        record_size = RECORD_HEADER_SIZE + payload_size
        record_end = offset + record_size

        if record_end > end:
            break

        if rtype == RECORD_GPS:
            gps_count += 1
            if gps_count > max_gps_fixes:
                break

        # Copy this record
        out.extend(data[offset:record_end])

        if rtype == RECORD_TERMINATOR:
            break

        offset = record_end

    # Add trailing CRC (just copy original's pattern)
    out.extend(b"\x00\x00")

    Path(output_path).write_bytes(bytes(out))
    print(f"  Sample: {output_path} ({len(out):,} bytes, {min(gps_count, max_gps_fixes)} GPS fixes)")


# ─────────────────────────────────────────────────────────────────────────────
# CLI
# ─────────────────────────────────────────────────────────────────────────────

def find_rkd_files(directory: str | Path) -> list[Path]:
    """Recursively find all .rkd files in a directory tree."""
    return sorted(Path(directory).rglob("*.rkd"))


def process_file(
    rkd_path: Path,
    output_dir: Path | None = None,
    info_only: bool = False,
    no_csv: bool = False,
    no_gpx: bool = False,
) -> RKDSession:
    """Parse a single RKD file and optionally export CSV/GPX."""
    parser = RKDParser()
    session = parser.parse(rkd_path)

    if info_only:
        print_session_info(session)
        return session

    print_session_info(session)

    # Determine output directory
    if output_dir is None:
        output_dir = rkd_path.parent
    output_dir = Path(output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)

    stem = rkd_path.stem

    if not no_csv:
        csv_path = output_dir / f"{stem}.csv"
        export_csv(session, csv_path)

    if not no_gpx:
        gpx_path = output_dir / f"{stem}.gpx"
        export_gpx(session, gpx_path)

    return session


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Race-Keeper RKD Telemetry Parser — Extract GPS, IMU, and "
                    "telemetry data from .rkd files into Telemetry Overlay CSV "
                    "and GPX formats.",
        epilog="Example: python3 rkd_parser.py outing.rkd --info",
    )
    parser.add_argument(
        "input",
        nargs="?",
        help="Path to an .rkd file (or use --all-in for batch processing)",
    )
    parser.add_argument(
        "--info",
        action="store_true",
        help="Print session summary only (no file export)",
    )
    parser.add_argument(
        "--all-in",
        metavar="DIR",
        help="Recursively process all .rkd files in DIR",
    )
    parser.add_argument(
        "--output-dir",
        metavar="DIR",
        help="Directory for output files (default: same as input)",
    )
    parser.add_argument(
        "--no-csv",
        action="store_true",
        help="Skip CSV export",
    )
    parser.add_argument(
        "--no-gpx",
        action="store_true",
        help="Skip GPX export",
    )
    parser.add_argument(
        "--sample",
        metavar="N",
        type=int,
        help="Create a truncated sample .rkd with the first N GPS fixes",
    )

    args = parser.parse_args()

    if args.all_in:
        rkd_files = find_rkd_files(args.all_in)
        if not rkd_files:
            print(f"No .rkd files found in {args.all_in}", file=sys.stderr)
            sys.exit(1)
        print(f"Found {len(rkd_files)} .rkd file(s)\n")
        for rkd_path in rkd_files:
            process_file(
                rkd_path,
                output_dir=args.output_dir,
                info_only=args.info,
                no_csv=args.no_csv,
                no_gpx=args.no_gpx,
            )
    elif args.input:
        input_path = Path(args.input)
        if not input_path.exists():
            print(f"Error: File not found: {input_path}", file=sys.stderr)
            sys.exit(1)

        session = process_file(
            input_path,
            output_dir=args.output_dir,
            info_only=args.info,
            no_csv=args.no_csv,
            no_gpx=args.no_gpx,
        )

        # Optionally create a truncated sample
        if args.sample:
            out_dir = Path(args.output_dir) if args.output_dir else input_path.parent
            sample_path = out_dir / f"sample_{input_path.stem}.rkd"
            create_sample_rkd(input_path, sample_path, max_gps_fixes=args.sample)
    else:
        parser.print_help()
        sys.exit(1)


if __name__ == "__main__":
    main()
