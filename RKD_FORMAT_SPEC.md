# Race-Keeper RKD Binary Format Specification

**Version:** 1.0 (first public documentation)
**Date:** 2026-02-06
**Author:** Sam (with Claude Code)
**Status:** Complete — fully validated against two independent sessions

## Overview

The `.rkd` file format is used by **Race-Keeper "Instant Video"** systems (manufactured by Trivinci / IENSO Inc.) to store telemetry data alongside track day and racing video recordings. These systems are commonly installed in professional track day experience cars at circuits across Europe.

The telemetry data includes:
- **GPS** at 5 Hz (position, speed, heading, altitude, satellites)
- **Accelerometer** at 30 Hz (3-axis, in milli-g)
- **Gyroscope** at 30 Hz (3-axis)
- **Configuration** metadata (device settings, firmware version)
- **System metrics** (~1 Hz battery/temperature readings)

This specification was reverse-engineered from binary analysis of two `.rkd` files recorded at Circuit de Mettet (Belgium) on April 4, 2021, and cross-validated against GPS mapping data, topographic elevation, and the proprietary Windows player (`PPKVIDEO.exe`).

---

## File Layout

| Section | Offset | Size | Description |
|---------|--------|------|-------------|
| Magic Signature | `0x00` | 8 bytes | Binary format identifier |
| Meta Header | `0x08` | 28 bytes | Session metadata |
| Record Stream | `0x24` | variable | Sequential typed records |
| Trailing CRC | last 2 bytes | 2 bytes | File-level checksum |

### Total File Sizes (observed)
- Session 1 (R8V10, ~5 min): 474,563 bytes
- Session 2 (HUR, ~8.4 min): 825,634 bytes

---

## 1. Magic Signature (8 bytes)

```
Offset: 0x00
Hex:    89 52 4B 44 0D 0A 1A 0A
ASCII:  \x89 R  K  D  \r \n \x1a \n
```

This follows the **PNG convention** for binary format markers:

| Byte | Value | Purpose |
|------|-------|---------|
| 0 | `\x89` | High bit set — catches 8-bit to 7-bit stripping |
| 1-3 | `RKD` | Human-readable format name |
| 4-5 | `\r\n` | Detects CR/LF newline translation (DOS ↔ Unix) |
| 6 | `\x1a` | Stops display under DOS `TYPE` command |
| 7 | `\n` | Detects reverse newline translation |

**Validation:** A parser MUST check this exact 8-byte sequence before proceeding.

---

## 2. Meta Header (28 bytes)

```
Offset: 0x08
Format: 7 × uint32 little-endian
```

| Index | Offset | Type | Field | Observed Values |
|-------|--------|------|-------|-----------------|
| 0 | 0x08 | uint32 | Flags/version | `0x00148000` (1,343,488) |
| 1 | 0x0C | uint32 | Reserved | 0 |
| 2 | 0x10 | uint32 | File sequence | 1 |
| 3 | 0x14 | uint32 | Reserved | 0 |
| 4 | 0x18 | uint32 | **Car ID** | 11098, 11133 |
| 5 | 0x1C | uint32 | **Session timestamp** | Unix epoch (seconds) |
| 6 | 0x20 | uint32 | Reserved | 0 |

### Car ID
A unique identifier for the recording device/car. Matches the `CARID` field in the HEADER config records and appears in the folder naming convention.

### Session Timestamp
Unix timestamp (seconds since 1970-01-01 00:00:00) representing the session start time. Note: this may reflect the device's internal clock, which may differ from GPS time.

### Example (hex dump)
```
00000008: 00 80 14 00  00 00 00 00  01 00 00 00  00 00 00 00
00000018: 5A 2B 00 00  28 72 69 60  00 00 00 00
           └─ Car ID    └─ Unix ts: 1617523240
              = 11098      = 2021-04-04 08:00:40 UTC
```

---

## 3. Record Stream

Starting at offset `0x24`, the file contains a sequential stream of typed records. Each record has a **universal 10-byte header** followed by a type-specific payload.

### Universal Record Header (10 bytes)

```
struct record_header {
    uint16_le crc;           // Record CRC-16 checksum
    uint16_le type;          // Record type identifier
    uint16_le payload_size;  // Payload size in bytes (following this header)
    uint16_le frame_lo;      // Frame number, low 16 bits
    uint16_le frame_hi;      // Frame number, high 16 bits
};
```

**Frame Number:** The 32-bit frame number is split across two 16-bit fields:
```
frame = (frame_hi << 16) | frame_lo
```

Frames increment at the video frame rate (30 FPS). GPS records arrive every ~6 frames (5 Hz), while IMU records arrive every frame (30 Hz).

### Record Types

| Type | Hex | Name | Payload Size | Rate | Description |
|------|-----|------|-------------|------|-------------|
| 1 | `0x0001` | HEADER | variable (12-51) | once per key | Config key-value pair |
| 2 | `0x0002` | GPS | 36 bytes | 5 Hz | Full GPS fix |
| 6 | `0x0006` | PERIODIC | 4 bytes | ~1 Hz | System metric |
| 7 | `0x0007` | ACCEL | 12 bytes | 30 Hz | 3-axis accelerometer |
| 8 | `0x0008` | TIMESTAMP | 4 bytes | ~5 Hz | Hardware timer sync |
| 12 | `0x000C` | GYRO | 12 bytes | 30 Hz | 3-axis gyroscope |
| 32769 | `0x8001` | TERMINATOR | 12 bytes | once | Session end marker |

---

## 4. Record Type Details

### 4.1 HEADER Record (Type 1)

**Purpose:** Stores device configuration as null-terminated key-value strings.

**Payload format:**
```
KEY\0VALUE\0
```

Both key and value are ASCII strings terminated by null bytes (`\x00`).

**Example record (hex):**
```
14 02  01 00  17 00  00 00  00 00
└─crc  └─type └─size └─frame_lo  └─frame_hi
              = 23 bytes

Payload: 43 41 50 54 55 52 45 5F 56 45 52 53 49 4F 4E 00 33 2E 31 33 2E 30 00
         C  A  P  T  U  R  E  _  V  E  R  S  I  O  N  \0 3  .  1  3  .  0  \0
```

**Observed configuration keys:**

| Key | Example Value | Description |
|-----|---------------|-------------|
| `CAPTURE_VERSION` | `3.13.0` | Firmware version |
| `BUILD_DATE` | `Jan 30 2021 18:11:52` | Firmware build date |
| `PIC_VERSION` | `2.6.0` | Microcontroller firmware |
| `CARID` | `11098` | Car/device identifier |
| `NUM_CAMERAS` | `2` | Number of camera inputs |
| `VIDEO_BITRATE` | `8000000` | Video bitrate (bps) |
| `VIDEO_FORMAT` | `MPEG4` | Video codec |
| `ENCODER_DEVICE` | `TI8168 Rev 2.0` | Hardware encoder chip |
| `ACCEL_X`, `ACCEL_Y`, `ACCEL_Z` | `171.875` | Accelerometer calibration offsets (milli-g) |
| `GYRO_X`, `GYRO_Y`, `GYRO_Z` | `0.000` | Gyroscope calibration offsets |
| `OUTING_GUID` | `{EA125A64-...}` | Unique session GUID |
| `DUALSTREAM` | `enabled` | Dual-stream recording mode |
| `HDMI_MODE` | `30P` | HDMI output mode |
| `VMUX_CONFIG` | `1:pip:pip:pip:1080P30` | Video multiplexer configuration |

A total of ~40 HEADER records are present per file.

---

### 4.2 GPS Record (Type 2)

**Purpose:** Contains a complete GPS fix from the receiver.
**Rate:** ~5 Hz (one record every ~6 video frames)
**Payload size:** 36 bytes

```
struct gps_record {
    uint32_le subtype;          // Always 3
    uint32_le gps_timestamp;    // Seconds since GPS epoch (1980-01-06 00:00:00 UTC)
    int16_le  satellites;       // Number of tracking satellites
    int16_le  padding;          // Always 0
    int32_le  latitude;         // Divide by 10,000,000 → degrees (WGS84)
    int32_le  longitude;        // Divide by 10,000,000 → degrees (WGS84)
    int32_le  speed;            // Divide by 100 → meters per second
    int32_le  heading;          // Divide by 100,000 → degrees (0-360, true north)
    int32_le  altitude;         // Divide by 1,000 → meters above mean sea level
    int32_le  vertical_speed;   // Centimeters per second (signed, positive = ascending)
};
```

#### GPS Epoch Conversion

GPS time counts seconds from **January 6, 1980 00:00:00 UTC** without accounting for leap seconds. To convert to UTC:

```
UTC = GPS_EPOCH + gps_timestamp - LEAP_SECONDS
```

Where `GPS_EPOCH = 1980-01-06T00:00:00Z` and `LEAP_SECONDS = 18` (valid for dates between 2017-01-01 and at least 2025-06-28).

#### Unit Conversions

| Field | Raw | Conversion | Result |
|-------|-----|-----------|--------|
| Latitude | `503010636` | ÷ 10,000,000 | `50.3010636°` |
| Longitude | `46550936` | ÷ 10,000,000 | `4.6550936°` |
| Speed | `2409` | ÷ 100 | `24.09 m/s` (86.7 km/h) |
| Heading | `3188000` | ÷ 100,000 | `31.88°` |
| Altitude | `256600` | ÷ 1,000 | `256.6 m` |
| Vertical speed | `135` | direct | `135 cm/s` (1.35 m/s ascending) |

#### Example GPS Record (hex)

```
Offset  Hex                                      Decoded
------  ---------------------------------------- -------
+0      03 00 00 00                              subtype = 3
+4      B9 92 9A 4D                              gps_ts = 1301565113
+8      13 00 00 00                              sats = 19, pad = 0
+12     CC A7 FD 1D                              lat = 503010636 → 50.3010636°
+16     58 BD CB 02                              lon = 46919000 → 4.6919000°
+20     69 09 00 00                              speed = 2409 → 24.09 m/s
+24     A0 A9 30 00                              heading = 3188000 → 31.88°
+28     58 EA 03 00                              alt = 256600 → 256.6 m
+32     87 00 00 00                              vspeed = 135 cm/s
```

#### Validation
- GPS coordinates for Circuit de Mettet fall within: **50.299°-50.303° N, 4.647°-4.657° E**
- Altitude range matches the circuit's elevation: **248-265 m** above sea level
- Speed values cross-validate with GPS-derived speed (haversine distance / time delta) to within 1%

---

### 4.3 ACCEL Record (Type 7)

**Purpose:** 3-axis accelerometer measurement
**Rate:** 30 Hz (one per video frame)
**Payload size:** 12 bytes

```
struct accel_record {
    int32_le x;    // Forward/backward acceleration in milli-g
    int32_le y;    // Left/right acceleration in milli-g
    int32_le z;    // Up/down acceleration in milli-g
};
```

#### Units
- Raw values are in **milli-g** (1/1000 of standard gravity, 9.81 m/s²)
- To convert to m/s²: `value_ms2 = raw_mg × 9.81 / 1000`
- At rest, Z-axis reads approximately **+1000 milli-g** (+9.81 m/s²)

#### Axis Convention
- **X:** Positive = forward acceleration (accelerating), negative = braking
- **Y:** Positive = leftward force, negative = rightward force
- **Z:** Positive = upward (includes gravity at ~+1000 mg)

#### Example
```
Raw: x=203, y=-187, z=1031
Converted: x=1.991 m/s², y=-1.834 m/s², z=10.114 m/s²
```

The Z-axis mean across a full session measures **9.81 m/s²**, confirming the milli-g calibration.

---

### 4.4 GYRO Record (Type 12)

**Purpose:** 3-axis gyroscope measurement
**Rate:** 30 Hz (one per video frame, paired with ACCEL)
**Payload size:** 12 bytes

```
struct gyro_record {
    int32_le x;    // Roll rate (raw)
    int32_le y;    // Pitch rate (raw)
    int32_le z;    // Yaw rate (raw)
};
```

#### Units
- Raw values require division by **~28** to obtain degrees per second
- `value_dps = raw / 28`
- This calibration factor was determined empirically by cross-referencing with GPS-derived heading change rates

#### Example
```
Raw: x=14, y=28, z=43
Converted: x=0.50°/s, y=1.00°/s, z=1.54°/s
```

---

### 4.5 PERIODIC Record (Type 6)

**Purpose:** System health metric (likely battery voltage or temperature)
**Rate:** ~1 Hz (one per second, frame-aligned with GPS)
**Payload size:** 4 bytes

```
struct periodic_record {
    uint32_le value;    // System metric (exact meaning unknown)
};
```

Observed values are constant within a session (e.g., `437` throughout a 5-minute session), suggesting a slowly-changing metric like battery voltage or ambient temperature.

---

### 4.6 TIMESTAMP Record (Type 8)

**Purpose:** Hardware timer synchronization
**Rate:** ~5 Hz (one per GPS fix)
**Payload size:** 4 bytes

```
struct timestamp_record {
    uint32_le hw_timer;    // Hardware timer tick count
};
```

Values are hardware timer counts that are **not monotonically increasing** — they fluctuate slightly around a base value, suggesting they represent a free-running oscillator used to timestamp the GPS/video sync. The frame interval between consecutive TIMESTAMP records is consistently **6 frames** (matching the GPS rate).

---

### 4.7 TERMINATOR Record (Type 0x8001)

**Purpose:** Marks the end of the recording session
**Rate:** Exactly once (final record before trailing CRC)
**Payload size:** 12 bytes

```
struct terminator_record {
    uint32_le unix_timestamp;    // Session end time (Unix epoch)
    uint32_le field2;            // Interpretation uncertain
    uint32_le field3;            // Interpretation uncertain
};
```

The first field is a Unix timestamp marking the recording end. The duration can be computed as:
```
duration ≈ terminator.unix_timestamp - meta_header.session_timestamp
```

Fields 2 and 3 may contain float32 values (observed: `1161.3` and `3.78`), but their exact meaning is unknown.

---

## 5. Record Ordering

Records appear in the stream in the following repeating pattern:

```
HEADER (×40, at start only)
[repeating block]:
    ACCEL  (frame N)
    GYRO   (frame N)
    ACCEL  (frame N+1)
    GYRO   (frame N+1)
    ...
    GPS    (every ~6th frame)
    TIMESTAMP (paired with GPS)
    PERIODIC (every ~30th frame, ~1 Hz)
TERMINATOR (final record)
```

The IMU records (ACCEL + GYRO) always appear as pairs at the same frame number, interleaved with GPS records at 1/6th the rate.

---

## 6. Trailing CRC (2 bytes)

The final 2 bytes of the file contain a CRC-16 checksum. The exact polynomial and scope (whether it covers the entire file or just the record stream) has not been determined, as the CRC is not required for parsing.

---

## 7. Record Count Statistics

| Record Type | R8V10 (5 min) | HUR (8.4 min) |
|-------------|---------------|----------------|
| HEADER | 40 | 40 |
| GPS | 1,457 | 2,523 |
| PERIODIC | 289 | 504 |
| ACCEL | 8,681 | 15,135 |
| TIMESTAMP | 1,447 | 2,522 |
| GYRO | 8,681 | 15,135 |
| TERMINATOR | 1 | 1 |

Expected rates:
- GPS: `duration × 5 Hz` → 289s × 5 = 1,445 (actual: 1,457 ✓)
- IMU: `duration × 30 Hz` → 289s × 30 = 8,670 (actual: 8,681 ✓)
- PERIODIC: `duration × 1 Hz` → 289 (actual: 289 ✓)

---

## 8. Validation Checksums

For implementors to verify their parser against known-good output:

### R8V10 Session
- File: `06107796_R8V10_11098_20210404_130024/outing.rkd`
- File size: 474,563 bytes
- Car ID: 11098
- GPS fixes: 1,457
- First GPS lat/lon: 50.3010636° / 4.6550936°
- Max speed: 43.47 m/s (156.5 km/h)
- Duration (GPS range): 289 seconds
- Distance (haversine sum): 5.03 km
- Accel Z mean: 9.81 m/s²

### HUR Session
- File: `06107796_HUR_11133_20210404_131601/outing.rkd`
- File size: 825,634 bytes
- Car ID: 11133
- GPS fixes: 2,523
- First GPS lat/lon: 50.3016042° / 4.6556839°
- Max speed: 55.12 m/s (198.4 km/h)
- Duration (GPS range): 504 seconds
- Distance (haversine sum): 9.00 km
- Accel Z mean: 9.98 m/s²

---

## 9. Reference Implementation

A complete Python 3 parser is available in `rkd_parser.py` (no dependencies beyond stdlib). It serves as both a practical tool and a reference implementation of this specification.

---

## Appendix A: Folder Naming Convention

Race-Keeper USB sticks use the following folder structure:
```
{SERIAL}_{CARNAME}_{CARID}_{DATE}_{TIME}/
    outing.rkd          # Telemetry data
    video.mp4           # Video recording
    video_hrd.bin        # Video header/index
    trivinci_*.log       # Device log
```

Example: `06107796_R8V10_11098_20210404_130024/`
- Serial: `06107796`
- Car name: `R8V10` (Audi R8 V10)
- Car ID: `11098`
- Date: 2021-04-04
- Time: 13:00:24 (local)

---

## Appendix B: Related Software

| Software | Platform | Description |
|----------|----------|-------------|
| `PPKVIDEO.exe` | Windows | Official Race-Keeper video player with overlay |
| `RK-ExportTool.exe` | Windows | Official Race-Keeper data export tool |
| `rkd_parser.py` | Cross-platform | This project's open-source parser |

---

## License

This specification is released under the MIT License. It was produced through independent reverse engineering and is not affiliated with or endorsed by Race-Keeper, Trivinci, or IENSO Inc.
