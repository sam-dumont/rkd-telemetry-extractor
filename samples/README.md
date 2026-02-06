# Sample Data

This directory contains sample data from a Race-Keeper recording at **Circuit de Mettet** (Belgium), recorded on April 4, 2021.

## Files

### `sample_mettet.rkd`
A truncated RKD binary file containing the first ~50 GPS fixes from an Audi R8 V10 session. This is approximately 10 seconds of data, enough to:
- Test a parser implementation against the format spec
- Examine the binary structure in a hex editor
- Verify record type decoding

### `sample_output.csv`
The Telemetry Overlay Custom CSV export from the sample RKD file. Contains columns:
- `utc (ms)` — Unix timestamp in milliseconds
- `lat (deg)`, `lon (deg)` — WGS84 coordinates
- `speed (m/s)` — GPS speed
- `alt (m)` — Altitude above MSL
- `heading (deg)` — True heading
- `satellites` — GPS satellite count
- `accel x/y/z (m/s²)` — 3-axis accelerometer
- `gyro x/y/z (deg/s)` — 3-axis gyroscope
- `g_lon`, `g_lat`, `g_total` — Derived g-forces
- `braking` — Binary braking indicator (1 = braking)

### `sample_output.gpx`
A GPX 1.1 track file from the sample data. Can be loaded in Google Earth, GPXSee, or any GPX viewer.

## Generating Samples

To regenerate these samples from a full RKD file:

```bash
# Generate sample RKD (first 50 GPS fixes) + CSV + GPX
python3 rkd_parser.py outing.rkd --sample 50 --output-dir samples/
```

## Data Source

- **Circuit:** Circuit de Mettet, Belgium (50.30° N, 4.65° E)
- **Car:** Audi R8 V10 (Car ID: 11098)
- **Date:** April 4, 2021
- **Organizer:** Sprint Racing
