package rkd

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// PrintSessionInfo prints a human-readable summary of a parsed RKD session.
func PrintSessionInfo(session *RKDSession) {
	sep := strings.Repeat("═", 60)
	fmt.Printf("\n%s\n", sep)
	fmt.Printf("  RKD Session: %s\n", baseName(session.FilePath))
	fmt.Printf("%s\n", sep)
	fmt.Printf("  File size:      %s bytes\n", formatInt(session.FileSize))
	fmt.Printf("  Car ID:         %d\n", session.CarID)

	if session.Timestamp != 0 {
		dt := time.Unix(int64(session.Timestamp), 0).UTC()
		fmt.Printf("  Session start:  %s\n", dt.Format("2006-01-02 15:04:05 UTC"))
	}

	fmt.Printf("\n  Configuration:\n")
	keys := make([]string, 0, len(session.Config))
	for k := range session.Config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("    %s: %s\n", k, session.Config[k])
	}

	fmt.Printf("\n  Record counts:\n")
	types := make([]int, 0, len(session.RecordCounts))
	for t := range session.RecordCounts {
		types = append(types, int(t))
	}
	sort.Ints(types)
	for _, t := range types {
		rtype := uint16(t)
		count := session.RecordCounts[rtype]
		name, ok := RecordNames[rtype]
		if !ok {
			name = fmt.Sprintf("UNKNOWN(0x%04x)", rtype)
		}
		fmt.Printf("    %-12s (type %5d): %s\n", name, rtype, formatInt(count))
	}

	if len(session.GPSFixes) > 0 {
		dur := session.DurationSeconds()
		maxSpeed := session.MaxSpeedKmh()
		dist := session.TotalDistanceKm()

		fmt.Printf("\n  GPS data:\n")
		fmt.Printf("    Fixes:        %s\n", formatInt(len(session.GPSFixes)))
		fmt.Printf("    Duration:     %.0fs (%.1f min)\n", dur, dur/60)
		fmt.Printf("    Max speed:    %.1f km/h (%.1f mph)\n", maxSpeed, maxSpeed/1.609344)
		fmt.Printf("    Distance:     %.2f km (%.2f mi)\n", dist, dist/1.609344)

		minLat, maxLat := session.GPSFixes[0].Latitude, session.GPSFixes[0].Latitude
		minLon, maxLon := session.GPSFixes[0].Longitude, session.GPSFixes[0].Longitude
		minAlt, maxAlt := session.GPSFixes[0].AltitudeM, session.GPSFixes[0].AltitudeM
		minSat, maxSat := session.GPSFixes[0].Satellites, session.GPSFixes[0].Satellites
		for _, f := range session.GPSFixes[1:] {
			if f.Latitude < minLat {
				minLat = f.Latitude
			}
			if f.Latitude > maxLat {
				maxLat = f.Latitude
			}
			if f.Longitude < minLon {
				minLon = f.Longitude
			}
			if f.Longitude > maxLon {
				maxLon = f.Longitude
			}
			if f.AltitudeM < minAlt {
				minAlt = f.AltitudeM
			}
			if f.AltitudeM > maxAlt {
				maxAlt = f.AltitudeM
			}
			if f.Satellites < minSat {
				minSat = f.Satellites
			}
			if f.Satellites > maxSat {
				maxSat = f.Satellites
			}
		}
		fmt.Printf("    Lat range:    %.7f – %.7f\n", minLat, maxLat)
		fmt.Printf("    Lon range:    %.7f – %.7f\n", minLon, maxLon)
		fmt.Printf("    Alt range:    %.1f – %.1f m\n", minAlt, maxAlt)
		fmt.Printf("    Satellites:   %d – %d\n", minSat, maxSat)
	}

	if len(session.IMUFrames) > 0 {
		var azSum float64
		for _, f := range session.IMUFrames {
			azSum += f.AccelZ
		}
		azMean := azSum / float64(len(session.IMUFrames))
		fmt.Printf("\n  IMU data:\n")
		fmt.Printf("    Frames:       %s\n", formatInt(len(session.IMUFrames)))
		fmt.Printf("    Accel Z (mean): %.2f m/s² (expect ~9.81)\n", azMean)
	}

	fmt.Printf("%s\n\n", sep)
}

func baseName(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
