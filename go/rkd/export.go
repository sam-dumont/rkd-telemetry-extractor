package rkd

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExportCSV writes session data to Telemetry Overlay Custom CSV format at 30 Hz.
func ExportCSV(session *RKDSession, outputPath string) error {
	if len(session.IMUFrames) == 0 || len(session.GPSFixes) == 0 {
		fmt.Fprintf(os.Stderr, "  Warning: No data to export for %s\n", session.FilePath)
		return nil
	}

	gpsFixes := session.GPSFixes
	imuFrames := session.IMUFrames

	firstGPSFrame := gpsFixes[0].Frame
	lastGPSFrame := gpsFixes[len(gpsFixes)-1].Frame

	firstGPSUtcMs := gpsFixes[0].UTCMS
	lastGPSUtcMs := gpsFixes[len(gpsFixes)-1].UTCMS
	totalFrames := lastGPSFrame - firstGPSFrame
	totalTimeMs := lastGPSUtcMs - firstGPSUtcMs

	var msPerFrame float64
	if totalFrames > 0 {
		msPerFrame = float64(totalTimeMs) / float64(totalFrames)
	} else {
		msPerFrame = 1000.0 / 30.0
	}

	columns := []string{
		"utc (ms)", "lat (deg)", "lon (deg)", "speed (m/s)", "alt (m)",
		"heading (deg)", "satellites", "gps fix",
		"accel x (m/s²)", "accel y (m/s²)", "accel z (m/s²)",
		"gyro x (deg/s)", "gyro y (deg/s)", "gyro z (deg/s)",
		"pitch angle (deg)", "bank (deg)", "turn rate (deg/s)",
		"vertical speed (ft/min)",
		"g_lon", "g_lat", "g_total", "braking",
		"speed (km/h)", "distance (km)",
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.UseCRLF = true // Match Python csv.writer output
	w.Write(columns)

	gpsIdx := 0
	cumDistance := 0.0
	var prevLat, prevLon *float64
	rowCount := 0

	for _, imu := range imuFrames {
		if imu.Frame < firstGPSFrame || imu.Frame > lastGPSFrame {
			continue
		}

		// Advance GPS index
		for gpsIdx < len(gpsFixes)-1 && gpsFixes[gpsIdx+1].Frame <= imu.Frame {
			gpsIdx++
		}

		utcMs := int64(float64(firstGPSUtcMs) + float64(imu.Frame-firstGPSFrame)*msPerFrame)

		g0 := gpsFixes[gpsIdx]
		var lat, lon, speed, alt, heading, vspeedCms float64
		var sats int16

		if gpsIdx < len(gpsFixes)-1 {
			g1 := gpsFixes[gpsIdx+1]
			frameSpan := g1.Frame - g0.Frame
			var t float64
			if frameSpan > 0 {
				t = float64(imu.Frame-g0.Frame) / float64(frameSpan)
			}
			lat = Lerp(g0.Latitude, g1.Latitude, t)
			lon = Lerp(g0.Longitude, g1.Longitude, t)
			speed = Lerp(g0.SpeedMS, g1.SpeedMS, t)
			alt = Lerp(g0.AltitudeM, g1.AltitudeM, t)
			heading = LerpAngle(g0.HeadingDeg, g1.HeadingDeg, t)
			vspeedCms = Lerp(float64(g0.VerticalSpeedCms), float64(g1.VerticalSpeedCms), t)
			sats = g0.Satellites
		} else {
			lat = g0.Latitude
			lon = g0.Longitude
			speed = g0.SpeedMS
			alt = g0.AltitudeM
			heading = g0.HeadingDeg
			vspeedCms = float64(g0.VerticalSpeedCms)
			sats = g0.Satellites
		}

		// Derived channels
		gLon := imu.AccelX / 9.81
		gLat := -imu.AccelY / 9.81
		gTotal := math.Sqrt(gLon*gLon + gLat*gLat)
		braking := 0
		if gLon < -0.05 {
			braking = 1
		}

		pitch := math.Atan2(imu.AccelX, math.Sqrt(imu.AccelY*imu.AccelY+imu.AccelZ*imu.AccelZ)) * 180 / math.Pi
		bank := math.Atan2(-imu.AccelY, imu.AccelZ) * 180 / math.Pi
		turnRate := imu.GyroZ
		vspeedFtmin := vspeedCms * 1.9685
		speedKmh := speed * 3.6

		if prevLat != nil && prevLon != nil {
			cumDistance += Haversine(*prevLat, *prevLon, lat, lon)
		}
		latCopy, lonCopy := lat, lon
		prevLat, prevLon = &latCopy, &lonCopy

		row := []string{
			fmt.Sprintf("%d", utcMs),
			fmt.Sprintf("%.7f", lat),
			fmt.Sprintf("%.7f", lon),
			fmt.Sprintf("%.2f", speed),
			fmt.Sprintf("%.1f", alt),
			fmt.Sprintf("%.2f", heading),
			fmt.Sprintf("%d", sats),
			"3",
			fmt.Sprintf("%.3f", imu.AccelX),
			fmt.Sprintf("%.3f", imu.AccelY),
			fmt.Sprintf("%.3f", imu.AccelZ),
			fmt.Sprintf("%.3f", imu.GyroX),
			fmt.Sprintf("%.3f", imu.GyroY),
			fmt.Sprintf("%.3f", imu.GyroZ),
			fmt.Sprintf("%.2f", pitch),
			fmt.Sprintf("%.2f", bank),
			fmt.Sprintf("%.2f", turnRate),
			fmt.Sprintf("%.1f", vspeedFtmin),
			fmt.Sprintf("%.3f", gLon),
			fmt.Sprintf("%.3f", gLat),
			fmt.Sprintf("%.3f", gTotal),
			fmt.Sprintf("%d", braking),
			fmt.Sprintf("%.1f", speedKmh),
			fmt.Sprintf("%.4f", cumDistance),
		}
		w.Write(row)
		rowCount++
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	fmt.Printf("  CSV: %s (%d rows at 30 Hz)\n", outputPath, rowCount)
	return nil
}

// ExportGPX writes GPS track to GPX 1.1 format.
func ExportGPX(session *RKDSession, outputPath string) error {
	if len(session.GPSFixes) == 0 {
		fmt.Fprintf(os.Stderr, "  Warning: No GPS data to export for %s\n", session.FilePath)
		return nil
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	sessionName := strings.TrimSuffix(filepath.Base(session.FilePath), filepath.Ext(session.FilePath))
	sessionDt := time.Unix(int64(session.Timestamp), 0).UTC()

	writeGPXContent(f, session, sessionName, sessionDt)

	fmt.Printf("  GPX: %s (%d trackpoints)\n", outputPath, len(session.GPSFixes))
	return nil
}

func writeGPXContent(w io.Writer, session *RKDSession, name string, dt time.Time) {
	fmt.Fprint(w, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	fmt.Fprint(w, "<gpx version=\"1.1\" creator=\"rkd_parser.py\"\n")
	fmt.Fprint(w, "     xmlns=\"http://www.topografix.com/GPX/1/1\"\n")
	fmt.Fprint(w, "     xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\"\n")
	fmt.Fprint(w, "     xsi:schemaLocation=\"http://www.topografix.com/GPX/1/1 ")
	fmt.Fprint(w, "http://www.topografix.com/GPX/1/1/gpx.xsd\">\n")
	fmt.Fprintf(w, "  <metadata>\n")
	fmt.Fprintf(w, "    <name>%s</name>\n", XMLEscape(name))
	fmt.Fprintf(w, "    <desc>Race-Keeper telemetry — Car ID %d</desc>\n", session.CarID)
	fmt.Fprintf(w, "    <time>%s</time>\n", dt.Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(w, "  </metadata>\n")
	fmt.Fprintf(w, "  <trk>\n")
	fmt.Fprintf(w, "    <name>%s</name>\n", XMLEscape(name))
	fmt.Fprintf(w, "    <trkseg>\n")

	for _, fix := range session.GPSFixes {
		utcDt := time.UnixMilli(fix.UTCMS).UTC()
		timeStr := utcDt.Format("2006-01-02T15:04:05.") + fmt.Sprintf("%03dZ", utcDt.Nanosecond()/1e6)
		fmt.Fprintf(w, "      <trkpt lat=\"%.7f\" lon=\"%.7f\">\n", fix.Latitude, fix.Longitude)
		fmt.Fprintf(w, "        <ele>%.1f</ele>\n", fix.AltitudeM)
		fmt.Fprintf(w, "        <time>%s</time>\n", timeStr)
		fmt.Fprintf(w, "        <sat>%d</sat>\n", fix.Satellites)
		fmt.Fprintf(w, "        <speed>%.2f</speed>\n", fix.SpeedMS)
		fmt.Fprintf(w, "      </trkpt>\n")
	}

	fmt.Fprintf(w, "    </trkseg>\n")
	fmt.Fprintf(w, "  </trk>\n")
	fmt.Fprintf(w, "</gpx>\n")
}
