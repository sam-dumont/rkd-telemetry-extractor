# Claude Code Session Log

*Key interactions from the reverse-engineering of the Race-Keeper RKD format and building both Python and Go implementations.*

## Session Overview

The RKD format was reverse-engineered across **three Claude Code sessions** on February 6, 2026, followed by a **fourth session** on February 7 for documentation cleanup, test suite creation, and Go implementation.

| Session | ID | Duration | Focus |
|---------|----|----------|-------|
| 1 | `5c005614-3279-4557-91ae-e03dd61f0a3e` | ~3 hours | Initial binary analysis, format discovery, GPS decoding |
| 2 | `dbf1c487-c79d-4d8f-a6a4-0b20c0f1666b` | ~2 hours | IMU calibration, cross-validation, export implementation |
| 3 | `cd56f11a-e6bb-474b-b36f-afefd1e92687` | ~15 min | Plan review and refinement |
| 4 | `d3a26917-342c-4637-b7fb-6c38877eec8c` | ~3 hours | Docs, tests, CI/CD, Go implementation, repo reorganization |

### Transcript Locations

Full JSONL transcripts are stored at:
```
/Users/sam/.claude/projects/-Users-sam-Desktop-CAR-VIDEO/5c005614-3279-4557-91ae-e03dd61f0a3e.jsonl
/Users/sam/.claude/projects/-Users-sam-Desktop-CAR-VIDEO/dbf1c487-c79d-4d8f-a6a4-0b20c0f1666b.jsonl
/Users/sam/.claude/projects/-Users-sam-Desktop-CAR-VIDEO/cd56f11a-e6bb-474b-b36f-afefd1e92687.jsonl
```

### Transcript Statistics

| Session | Lines | User Messages | Assistant Messages | File Size |
|---------|-------|---------------|-------------------|-----------|
| 1 | 399 | 172 | 193 | 3.6 MB |
| 2 | 362 | 164 | 181 | 2.4 MB |
| 3 | 106 | 40 | 57 | 273 KB |

---

## Key Interactions

### Session 1: Discovery Phase

#### 1. First Look at the Binary

**User prompt (paraphrased):** "I have these .rkd files from a track day. Can you help me understand the binary format?"

**What Claude Code did:**
- Read the first 256 bytes with `xxd`
- Immediately recognized the PNG-style magic signature `\x89RKD\r\n\x1a\n`
- Identified the meta header structure with Car ID and Unix timestamp
- Discovered the 10-byte record header format

#### 2. Breaking the Record Types

**User prompt:** "There seem to be different record types. Can you scan all of them?"

**What Claude Code did:**
- Wrote a Python script to scan the entire file
- Identified 7 distinct record types with their frequencies
- Determined that Type 1 records contained ASCII config strings
- Correlated Type 2 (36 bytes, ~5 Hz) with GPS data
- Correlated Types 7 and 12 (12 bytes each, 30 Hz) with IMU data

#### 3. Decoding GPS Coordinates

**User prompt:** "Can you figure out what the GPS fields are?"

**What Claude Code did:**
- Tested multiple struct formats for the 36-byte GPS payload
- Identified the GPS epoch timestamp (seconds since 1980-01-06)
- Found that dividing lat/lon integers by 1e7 gives coordinates at Circuit de Mettet
- Validated altitude against topographic data for the circuit

#### 4. The Speed Discovery

**User prompt:** "What about speed? I know the max was around 156 km/h for the Audi."

**What Claude Code did:**
- Tested divisors of 1, 10, 100, 1000 for the speed field
- Determined that ÷100 gives m/s, ×3.6 gives km/h
- Found max = 156.5 km/h — matching the user's recollection
- Cross-validated against haversine distance ÷ time between GPS fixes
- Error was consistently < 1%

### Session 2: Calibration and Export

#### 5. Accelerometer Calibration

**User prompt:** "The IMU data — can we figure out the units?"

**What Claude Code did:**
- Observed Z-axis raw values clustering around ~1000
- Hypothesized milli-g encoding (1000 mg = 1g = 9.81 m/s²)
- Computed mean Z-accel across entire session: exactly 9.81 m/s²
- Confirmed milli-g interpretation

#### 6. Gyroscope Factor

**User prompt:** "What about the gyroscope? The values look different."

**What Claude Code did:**
- Compared gyro Z-axis values against GPS heading change rate
- Tested divisors from 1 to 100
- Found that ÷28 produces degree/second values that match GPS-derived yaw rates
- Noted this is an empirical calibration that may vary with hardware revision

#### 7. Building the Exporter

**User prompt:** "I want to export this as CSV for Telemetry Overlay and GPX."

**What Claude Code did:**
- Researched the Telemetry Overlay Custom CSV specification
- Implemented 30 Hz CSV output with GPS interpolation between 5 Hz fixes
- Added GPX 1.1 export at native GPS rate
- Computed derived g-force channels from the 30 Hz IMU data

### Session 3: Planning

#### 8. Documentation Plan

**User prompt:** "Let's plan how to package this as a public GitHub repo."

**What Claude Code did:**
- Created a comprehensive implementation plan
- Identified all deliverables: parser, format spec, research notes, session log, README
- Designed the repo structure
- Established the verification checklist

---

## Key Discoveries (Chronological)

| # | Discovery | Session | Technique |
|---|-----------|---------|-----------|
| 1 | PNG-style magic: `\x89RKD\r\n\x1a\n` | 1 | Hex dump pattern recognition |
| 2 | 28-byte meta header with Car ID at offset 0x18 | 1 | uint32 array unpacking |
| 3 | 10-byte universal record header | 1 | Structural analysis of repeating patterns |
| 4 | Type 1 = `KEY\0VALUE\0` config strings | 1 | ASCII visible in hex dump |
| 5 | Type 2 = GPS data at 5 Hz | 1 | Payload size (36 bytes) + frequency analysis |
| 6 | GPS timestamps use GPS epoch (1980-01-06) | 1 | Epoch enumeration |
| 7 | Lat/lon encoded as int32 ÷ 1e7 | 1 | Standard u-blox format knowledge |
| 8 | Speed in cm/s (÷100 = m/s) | 1 | Trial divisors + haversine cross-validation |
| 9 | Heading at ÷100,000 precision | 1 | Angular range fitting |
| 10 | Altitude at ÷1,000 (mm → m) | 1 | Topographic map validation |
| 11 | Type 7 = accelerometer in milli-g | 2 | Z-axis ≈ 1000 at rest |
| 12 | Type 12 = gyroscope, ÷28 ≈ deg/s | 2 | GPS heading rate comparison |
| 13 | Type 0x8001 = session terminator | 2 | End-of-file analysis |
| 14 | Frame numbers split across two uint16 fields | 1 | Structural analysis |
| 15 | 30 Hz IMU enables derived g-force channels | 3 | Domain knowledge |

### Session 4: Documentation, Tests, and Go Implementation

After the initial commit of the Python parser and format documentation, a fourth session prepared the project for public release.

#### 9. Documentation Cleanup

**User prompt (paraphrased):** "Remove incorrect credits, fix the manufacturer name, use my GitHub handle instead of first name, and note OBD-2 support status."

**What Claude Code did:**
- Removed credits for Telemetry Overlay, Sprint Racing, and hardware vendor
- Changed "Sam" to "@sam-dumont" across all files
- Fixed manufacturer: identified Trivinci Systems LLC (USA) through web research
- Added OBD-2 limitation note — the recorder supports it, but test recordings had no OBD-2 scanner connected

#### 10. Test Suite and CI/CD

**User prompt:** "I expect 100% code coverage."

**What Claude Code did:**
- Created comprehensive test suite with 77 tests in `tests/test_rkd_parser.py`
- Achieved 100% branch coverage (identified one provably unreachable branch, marked with `pragma: no cover`)
- Created `CLAUDE.md` with PR guidelines for contributors using Claude Code
- Set up GitHub Actions CI (Python 3.9/3.11/3.13, coverage enforcement)

#### 11. Go Implementation

**User prompt:** "How hard would this be to create a Go implementation? With the full test suite and spec it should be trivial to do TDD."

**What Claude Code did:**
- Built complete Go implementation from the Python reference and format spec
- Structured as `go/rkd/` package (parser, exporters) + `go/main.go` (CLI)
- Used `encoding/binary` for all binary parsing, `encoding/csv` with `UseCRLF=true`
- Fixed a subtle `math.Mod` difference — Go preserves sign of dividend unlike Python `%`, requiring a `pyMod()` helper for angle interpolation
- **Verified byte-for-byte identical CSV and GPX output** between Python and Go
- Wrote 76 Go tests including cross-implementation output comparison

#### 12. Repository Reorganization

**User prompt:** "Reorganize to have `/go`, `/python` etc. so the structure is clean."

**What Claude Code did:**
- Moved Python files under `python/` (parser, tests, config, Makefile)
- Organized Go files under `go/` (CLI, parser package, Makefile)
- Kept `samples/` at root (shared by both implementations)
- Created root `Makefile` delegating to both implementations
- Updated `CLAUDE.md` for the new dual-implementation structure

---

## Tools Used

- **Claude Code** (claude-opus-4-6) — Primary analysis and coding partner
- **Python 3** — Original parser implementation
- **Go 1.24** — Second implementation (byte-for-byte identical output)
- **xxd** — Initial hex dump analysis
- **struct module** — Python binary unpacking
- **encoding/binary** — Go binary unpacking
- **haversine formula** — GPS-derived speed cross-validation
- **pytest + pytest-cov** — Python test suite with 100% branch coverage

---

## Reproducing This Work

To reproduce the key discovery steps:

1. Start a Claude Code session in the directory containing the `.rkd` files
2. Ask Claude to examine the binary header with `xxd -l 128 outing.rkd`
3. Request identification of the record structure
4. Ask for GPS field decoding and validation against known circuit location
5. Request IMU calibration analysis
6. Build the exporter

The full process from "unknown binary" to "working parser with exports" took approximately **4-5 hours** of interactive work.

---

*Note: Session transcripts contain the full back-and-forth including failed hypotheses, debugging steps, and iterative refinement that aren't captured in this summary. The JSONL files are the authoritative record.*
