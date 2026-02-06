# Reverse-Engineering the Race-Keeper RKD Format

*A chronological narrative of how the proprietary RKD binary telemetry format was decoded — the first public documentation of its kind.*

## Background

In April 2021, I participated in a track day at **Circuit de Mettet** (Belgium) organized by Sprint Racing. I drove two supercars: an **Audi R8 V10** and a **Lamborghini Huracan**. The track day included professional in-car video recording via a **Race-Keeper "Instant Video"** system, which captures synchronized video and telemetry data.

At the end of the day, I received a USB stick containing the recordings. The stick included a Windows-only video player (`PPKVIDEO.exe`) that renders the video with telemetry overlays — speed gauges, track maps, lap timers, and g-force indicators. The `autorun.inf` on the USB even triggers auto-play on Windows.

There was just one problem: **I use macOS**. And the file helpfully includes `X This software is not Apple Mac compatible X.txt`.

The video files (`.mp4`) play fine anywhere, but the telemetry data lives in proprietary `.rkd` binary files that only the Windows player can read. I wanted to extract this telemetry to use with **Telemetry Overlay** (a cross-platform tool for adding data overlays to video), but no documentation or third-party tools existed for the RKD format.

So I decided to reverse-engineer it — with the help of **Claude Code** as a collaborative analysis partner.

---

## The USB Stick

The USB stick layout reveals the hardware and software ecosystem:

```
USB Root/
├── autorun.inf                    # Windows auto-play trigger
├── PPKVIDEO/                      # Windows player + dependencies
│   ├── PPKVIDEO.exe               # Qt4-based video player
│   ├── RK-ExportTool.exe          # Data export utility
│   ├── Gauges/                    # Overlay graphics (speedometer, tach, etc.)
│   └── *.dll                      # Qt4, ffmpeg, libvideo dependencies
├── 06107796_R8V10_11098_20210404_130024/
│   ├── video.mp4                  # Synchronized multi-camera video
│   ├── outing.rkd                 # ← THE TELEMETRY FILE (474 KB)
│   ├── video_hrd.bin              # Video header/index data
│   └── trivinci_*.log             # Device diagnostic log
├── 06107796_HUR_11133_20210404_131601/
│   ├── video.mp4                  # Second session video
│   ├── outing.rkd                 # ← SECOND TELEMETRY FILE (826 KB)
│   └── ...
├── Play Video.bat                 # Launcher script
└── ppk.log                        # Historical session log
```

Key observations from the directory structure:
- **Folder naming convention:** `{SERIAL}_{CARNAME}_{CARID}_{DATE}_{TIME}`
- **Two sessions:** R8V10 (Audi) and HUR (Lamborghini), same device serial
- **MPEG4 video** with a separate binary header file
- The company names visible: **Trivinci** (manufacturer) and **IENSO Inc.** (parent company)
- The `.rkd` extension: **R**ace-**K**eeper **D**ata

---

## Phase 1: First Contact with the Binary

### The Magic Bytes

The very first thing you do with an unknown binary file is look at its header:

```
00000000: 89 52 4B 44 0D 0A 1A 0A  .RKD....
```

**Aha moment #1:** This is a **PNG-style magic signature**! The pattern is unmistakable:
- `\x89` — High bit set to catch 8-bit stripping
- `RKD` — Format name in ASCII
- `\r\n` — Detects newline conversion
- `\x1a` — Stops DOS `TYPE` command
- `\n` — Detects reverse newline conversion

This was the first sign that the developers knew what they were doing. The PNG magic signature pattern is a well-known best practice for binary formats, and seeing it here told me this was a deliberately designed format, not just a raw data dump.

### The Meta Header

After the 8-byte magic, the next 28 bytes contained some structure:

```
00000008: 00 80 14 00  00 00 00 00  01 00 00 00  00 00 00 00
00000018: 5A 2B 00 00  28 72 69 60  00 00 00 00
```

Reading as 7 × uint32 little-endian: `1343488, 0, 1, 0, 11098, 1617523240, 0`

- The value **11098** immediately matched the `CARID` in the folder name
- **1617523240** converts to `2021-04-04 08:00:40 UTC` — the session date! This is a Unix timestamp.

### The Record Stream

At offset `0x24`, I found what appeared to be a repeating structure. Testing various header sizes, a **10-byte record header** with the pattern `[uint16 × 5]` produced consistent results:

```
crc(2) | type(2) | payload_size(2) | frame_lo(2) | frame_hi(2)
```

The first records after the meta header had `type=1` with varying payload sizes, and the payloads contained readable ASCII text:

```
CAPTURE_VERSION\03.13.0\0
BUILD_DATE\0Jan 30 2021 18:11:52\0
```

**Aha moment #2:** Type 1 records are null-terminated `KEY\0VALUE\0` configuration strings! This immediately gave us dozens of known values to validate against.

---

## Phase 2: Decoding GPS Records

### Finding the GPS Data

With the record header structure established, scanning the file revealed several distinct record types. Type 2 records had a consistent 36-byte payload and appeared at a regular interval — about every 6th record. This screamed "GPS data at 5 Hz" (since the video is 30 fps, and 30/5 = 6).

### The Subtype Field

The first 4 bytes of every type 2 payload were always `03 00 00 00` (uint32 = 3). This is likely a GPS fix type indicator (3D fix).

### GPS Timestamp

Bytes 4-7 of the GPS payload contained values like `1301565641`. This was too large for a Unix timestamp (which would be ~1.6 billion for 2021) but too small for microseconds. The key insight came from recognizing this as a **GPS epoch timestamp** — seconds since January 6, 1980:

```python
GPS_EPOCH + timedelta(seconds=1301565641) = 2021-04-04 10:00:41 UTC
```

After subtracting 18 leap seconds (GPS time doesn't track leap seconds), this gives **10:00:23 UTC**, which in CEST (UTC+2) is **12:00:23** — matching the session time.

**Aha moment #3:** The raw GPS timestamps use the GPS epoch, not Unix epoch. This confirmed the data comes directly from the GPS chipset with minimal processing.

### Cracking the Coordinate Format

For latitude, the value `503010636` divided by 10,000,000 gives `50.3010636°` — a location that maps precisely to Circuit de Mettet in Belgium. The same 1e7 divisor worked for longitude.

This is a standard GPS coordinate encoding (matching the u-blox UBX protocol format), confirming the Race-Keeper uses a u-blox or compatible GPS receiver.

### Speed: The cm/s Discovery

The speed field required more detective work. Raw values like `2409` didn't immediately map to anything obvious:
- `2409` km/h? No — way too fast
- `2409` mph? No — absurd
- `2409 / 10` = 240.9 km/h? Possibly, but too high for the first GPS fix
- `2409 / 100` = **24.09 m/s** = **86.7 km/h**? ✓ — realistic for mid-corner!

**Aha moment #4:** Speed is stored as **centimeters per second** (divide by 100 for m/s). Cross-validated by computing GPS-derived speed from consecutive lat/lon positions using the haversine formula — the values matched within 1%.

### Heading and Altitude

- **Heading:** Values like `3188000` ÷ 100,000 = `31.88°` (northeast, matching the track direction at that point)
- **Altitude:** Values like `256600` ÷ 1,000 = `256.6 meters` — Circuit de Mettet sits at approximately 248-265m elevation on topographic maps. **Perfect match.**

---

## Phase 3: IMU Data (Accelerometer & Gyroscope)

### Type 7: Accelerometer

Type 7 records contained exactly 12 bytes and appeared at every frame (30 Hz). Three int32 values:

```
Raw: x=203, y=-187, z=1031
```

The Z-axis value of ~1000 was the giveaway — at rest, the accelerometer reads **1g** in the vertical axis. So the raw values are in **milli-g** (1/1000 of standard gravity):

```
203 mg × 9.81/1000 = 1.99 m/s² (forward acceleration)
-187 mg × 9.81/1000 = -1.83 m/s² (lateral force)
1031 mg × 9.81/1000 = 10.11 m/s² (gravity + vertical)
```

**Validation:** The mean Z-acceleration across the entire R8V10 session was **9.81 m/s²** — exactly 1g, confirming the milli-g interpretation.

### Type 12: Gyroscope

Type 12 records had the same 12-byte, 3×int32 structure but with different value ranges. After testing several calibration factors:

```
Raw: x=14, y=28, z=43
Dividing by 28: x=0.50°/s, y=1.00°/s, z=1.54°/s
```

The factor of **~28** was determined by comparing yaw rate (Z-axis gyro) against the GPS heading change rate. When the car turns at known GPS-derived rates, the gyro Z-axis should match — and at divisor 28, it does.

---

## Phase 4: Cross-Validation

### Speed Validation

The most rigorous test: compare the recorded GPS speed against haversine-derived speed from consecutive positions:

```
Position 1: (50.3010636°, 4.6550936°) at t=0
Position 2: (50.3011006°, 4.6551297°) at t=0.2s
Haversine distance: ~4.8m
Derived speed: 4.8m / 0.2s = 24.0 m/s
Recorded speed: 24.09 m/s
Error: < 0.4%
```

This sub-1% agreement across thousands of data points conclusively validates the speed encoding.

### Track Shape

Plotting all GPS coordinates as a scatter plot produces a recognizable race circuit layout that matches satellite imagery of Circuit de Mettet.

### Max Speed Location

The maximum speed points (156.5 km/h for R8V10, 198.4 km/h for Huracan) all occur at the same GPS coordinates — the main straight of the circuit. This is exactly where you'd expect maximum velocity.

### Altitude Profile

The altitude variation (249-264m) shows a consistent pattern of elevation changes that repeats with each lap — the natural undulation of the circuit terrain.

---

## Phase 5: Remaining Records

### Type 6 (PERIODIC)

4-byte records at ~1 Hz with slowly-changing or constant values (e.g., `437` throughout a session). Likely a system health metric like battery voltage or temperature. Not critical for telemetry export.

### Type 8 (TIMESTAMP)

4-byte records paired with GPS fixes (~5 Hz). Values are hardware timer ticks that fluctuate around a base value. These appear to be used internally for GPS-to-video synchronization. The consistent 6-frame spacing confirms the GPS-to-video frame alignment.

### Type 0x8001 (TERMINATOR)

The final record in every file. Contains 12 bytes including a Unix timestamp marking the end of recording. The difference between this and the meta header timestamp gives the session duration.

---

## The Role of Claude Code

This reverse-engineering project was done as a **collaboration between human intuition and AI pattern analysis**. Here's how the workflow operated:

1. **Human:** "Look at the hex dump of this binary file. What structure do you see?"
2. **Claude Code:** Identifies repeating patterns, suggests struct formats, tests hypotheses
3. **Human:** "That value 11098 matches the folder name — it's a car ID!"
4. **Claude Code:** Uses that anchor point to decode surrounding fields, tests candidate encodings
5. **Human:** "Does this GPS coordinate match Circuit de Mettet?"
6. **Claude Code:** Validates against known geographic bounds, cross-references altitude data

The key advantage of using Claude Code was the ability to **rapidly test hypotheses**. When I suspected a field might be speed in cm/s, Claude Code could immediately:
- Convert the raw value to m/s and km/h
- Compute GPS-derived speed from the previous position
- Compare the two values
- Report the error percentage

What would have taken hours of manual calculation and scripting happened in seconds, allowing us to explore dozens of encoding hypotheses in a single session.

---

## Timeline

| Phase | Discovery | Key Technique |
|-------|-----------|---------------|
| 1 | PNG-style magic bytes | Pattern recognition |
| 1 | 10-byte record headers | Structural analysis |
| 1 | Type 1 = config strings | ASCII in hex dump |
| 2 | GPS epoch timestamps | Epoch enumeration |
| 2 | Lat/lon at 1e-7 degrees | Standard GPS encoding knowledge |
| 2 | Speed in cm/s | Cross-validation with haversine |
| 2 | Heading at 1e-5 degrees | Angular range analysis |
| 2 | Altitude at mm precision | Topographic validation |
| 3 | Accelerometer in milli-g | Z-axis = 1g at rest |
| 3 | Gyroscope divisor ≈ 28 | GPS heading rate comparison |
| 4 | Full cross-validation | Derived vs. recorded speed < 1% |
| 5 | Terminator structure | End-of-file analysis |

---

## What We Don't Know

A few aspects remain uncertain:
1. **CRC algorithm:** Both per-record and trailing CRC-16 checksums are present but the polynomial hasn't been identified
2. **PERIODIC record meaning:** The exact metric (battery voltage? temperature?) is unknown
3. **TIMESTAMP hardware timer:** The exact clock source and tick rate are unknown
4. **TERMINATOR fields 2-3:** Two 32-bit values with unclear meaning (possibly floats)
5. **Gyro calibration factor:** The divisor of ~28 is empirical; the exact conversion may depend on the specific MEMS sensor used

These unknowns don't affect practical data extraction — all the telemetry channels needed for video overlay are fully decoded.

---

## Conclusion

The Race-Keeper RKD format turned out to be a well-designed, straightforward binary format. The PNG-style magic signature, clean record structure, and standard GPS encoding conventions suggest experienced embedded systems engineers. The data quality is excellent — 5 Hz GPS with 19-satellite fixes and 30 Hz IMU measurements at milli-g precision.

This reverse-engineering effort produced:
- The **first public documentation** of the RKD format
- A **complete Python parser** with no dependencies
- **Export to Telemetry Overlay CSV and GPX** formats
- A **formal specification** for other implementors

The total effort was approximately **4 hours** of collaborative work with Claude Code — from first hex dump to validated parser with exports. Without AI assistance, this would likely have taken several days of manual analysis.

---

*This document supports a blog post about using Claude Code for binary format reverse engineering. Session transcripts are available in the companion `SESSION_LOG.md`.*
