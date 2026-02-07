// Package rkd provides a parser and exporters for Race-Keeper (.rkd) binary
// telemetry files used by Race-Keeper "Instant Video" systems (Trivinci Systems LLC).
package rkd

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
	"time"
)

// RKD magic signature: PNG-style binary format marker.
var RKDMagic = []byte{0x89, 'R', 'K', 'D', '\r', '\n', 0x1a, '\n'}

const (
	// GPS epoch: January 6, 1980 00:00:00 UTC.
	gpsLeapSeconds = 18 // Leap seconds as of 2021 (valid 2017-01-01 through at least 2025)

	// Record types in the RKD binary stream.
	RecordHeader     = 1
	RecordGPS        = 2
	RecordPeriodic   = 6
	RecordAccel      = 7
	RecordTimestamp  = 8
	RecordGyro       = 12
	RecordTerminator = 0x8001

	// Sizes.
	metaHeaderSize   = 28 // 7 × uint32
	recordHeaderSize = 10 // crc(2) + type(2) + payload_size(2) + frame_lo(2) + frame_hi(2)
	trailingCRCSize  = 2
)

// RecordNames maps record type numbers to human-readable names.
var RecordNames = map[uint16]string{
	RecordHeader:     "HEADER",
	RecordGPS:        "GPS",
	RecordPeriodic:   "PERIODIC",
	RecordAccel:      "ACCEL",
	RecordTimestamp:  "TIMESTAMP",
	RecordGyro:       "GYRO",
	RecordTerminator: "TERMINATOR",
}

var gpsEpoch = time.Date(1980, 1, 6, 0, 0, 0, 0, time.UTC)

// GPSFix represents a single GPS measurement.
type GPSFix struct {
	Frame            int
	GPSTimestamp     uint32  // Seconds since GPS epoch (1980-01-06)
	UTCMS            int64   // Unix timestamp in milliseconds (UTC)
	Satellites       int16
	Latitude         float64 // Degrees (WGS84)
	Longitude        float64 // Degrees (WGS84)
	SpeedMS          float64 // Speed in m/s
	HeadingDeg       float64 // Heading in degrees (0-360)
	AltitudeM        float64 // Altitude in meters (above MSL)
	VerticalSpeedCms int32   // Vertical speed in cm/s (signed)
}

// IMUFrame represents a merged accelerometer + gyroscope measurement.
type IMUFrame struct {
	Frame  int
	AccelX float64 // m/s²
	AccelY float64 // m/s²
	AccelZ float64 // m/s²
	GyroX  float64 // deg/s
	GyroY  float64 // deg/s
	GyroZ  float64 // deg/s
}

// RKDSession holds all data parsed from a single RKD file.
type RKDSession struct {
	FilePath  string
	FileSize  int
	CarID     uint32
	Timestamp uint32 // Unix timestamp from meta header

	Config       map[string]string
	GPSFixes     []GPSFix
	IMUFrames    []IMUFrame
	RecordCounts map[uint16]int

	TerminatorTimestamp uint32
}

// DurationSeconds returns the session duration derived from GPS timestamp range.
func (s *RKDSession) DurationSeconds() float64 {
	if len(s.GPSFixes) < 2 {
		return 0.0
	}
	return float64(s.GPSFixes[len(s.GPSFixes)-1].GPSTimestamp - s.GPSFixes[0].GPSTimestamp)
}

// MaxSpeedKmh returns the maximum speed in km/h.
func (s *RKDSession) MaxSpeedKmh() float64 {
	if len(s.GPSFixes) == 0 {
		return 0.0
	}
	max := 0.0
	for _, f := range s.GPSFixes {
		if f.SpeedMS > max {
			max = f.SpeedMS
		}
	}
	return max * 3.6
}

// TotalDistanceKm returns total distance in km computed from GPS positions.
func (s *RKDSession) TotalDistanceKm() float64 {
	if len(s.GPSFixes) < 2 {
		return 0.0
	}
	total := 0.0
	for i := 1; i < len(s.GPSFixes); i++ {
		total += Haversine(
			s.GPSFixes[i-1].Latitude, s.GPSFixes[i-1].Longitude,
			s.GPSFixes[i].Latitude, s.GPSFixes[i].Longitude,
		)
	}
	return total
}

// GPSToUTCMs converts a GPS timestamp (seconds since 1980-01-06) to Unix milliseconds.
func GPSToUTCMs(gpsSeconds uint32) int64 {
	utc := gpsEpoch.Add(time.Duration(int64(gpsSeconds)-gpsLeapSeconds) * time.Second)
	return utc.UnixMilli()
}

// Haversine returns the distance in km between two lat/lon points.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// Lerp performs linear interpolation between a and b at parameter t ∈ [0, 1].
func Lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}

// LerpAngle interpolates between two angles in degrees, handling wraparound.
func LerpAngle(a, b, t float64) float64 {
	diff := pyMod(b-a+180, 360) - 180
	result := a + diff*t
	return pyMod(result, 360)
}

// pyMod returns a % b with Python semantics (always non-negative for positive b).
func pyMod(a, b float64) float64 {
	r := math.Mod(a, b)
	if r < 0 {
		r += b
	}
	return r
}

// XMLEscape escapes special XML characters.
func XMLEscape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			out = append(out, []byte("&amp;")...)
		case '<':
			out = append(out, []byte("&lt;")...)
		case '>':
			out = append(out, []byte("&gt;")...)
		case '"':
			out = append(out, []byte("&quot;")...)
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

// Parser parses Race-Keeper RKD binary telemetry files.
type Parser struct {
	accelByFrame map[int][3]float64
	gyroByFrame  map[int][3]float64
}

// Parse reads an RKD file and returns a session with all telemetry data.
func (p *Parser) Parse(path string) (*RKDSession, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return p.ParseBytes(data, path)
}

// ParseBytes parses RKD data from a byte slice.
func (p *Parser) ParseBytes(data []byte, path string) (*RKDSession, error) {
	session := &RKDSession{
		FilePath:     path,
		FileSize:     len(data),
		Config:       make(map[string]string),
		RecordCounts: make(map[uint16]int),
	}

	if err := p.validateMagic(data); err != nil {
		return nil, err
	}
	if err := p.parseMetaHeader(data, session); err != nil {
		return nil, err
	}
	p.parseRecords(data, session)
	p.mergeIMU(session)
	return session, nil
}

func (p *Parser) validateMagic(data []byte) error {
	if len(data) < len(RKDMagic) {
		return fmt.Errorf("file too small to be an RKD file")
	}
	for i, b := range RKDMagic {
		if data[i] != b {
			return fmt.Errorf("invalid RKD magic: expected %x, got %x", RKDMagic, data[:8])
		}
	}
	return nil
}

func (p *Parser) parseMetaHeader(data []byte, session *RKDSession) error {
	offset := len(RKDMagic)
	if len(data) < offset+metaHeaderSize {
		return fmt.Errorf("file too small for meta header")
	}
	// 7 × uint32 LE: flags, reserved, seq, reserved, car_id, timestamp, reserved
	session.CarID = binary.LittleEndian.Uint32(data[offset+16 : offset+20])
	session.Timestamp = binary.LittleEndian.Uint32(data[offset+20 : offset+24])
	return nil
}

func (p *Parser) parseRecords(data []byte, session *RKDSession) {
	offset := len(RKDMagic) + metaHeaderSize
	end := len(data) - trailingCRCSize

	p.accelByFrame = make(map[int][3]float64)
	p.gyroByFrame = make(map[int][3]float64)

	for offset+recordHeaderSize <= end {
		// crc(2) + type(2) + payload_size(2) + frame_lo(2) + frame_hi(2)
		rtype := binary.LittleEndian.Uint16(data[offset+2 : offset+4])
		payloadSize := int(binary.LittleEndian.Uint16(data[offset+4 : offset+6]))
		frameLo := int(binary.LittleEndian.Uint16(data[offset+6 : offset+8]))
		frameHi := int(binary.LittleEndian.Uint16(data[offset+8 : offset+10]))
		frame := (frameHi << 16) | frameLo

		payloadStart := offset + recordHeaderSize
		payloadEnd := payloadStart + payloadSize

		if payloadEnd > end {
			break
		}

		payload := data[payloadStart:payloadEnd]
		session.RecordCounts[rtype]++

		switch rtype {
		case RecordHeader:
			p.parseHeaderRecord(payload, session)
		case RecordGPS:
			p.parseGPSRecord(payload, frame, session)
		case RecordAccel:
			p.parseAccelRecord(payload, frame)
		case RecordGyro:
			p.parseGyroRecord(payload, frame)
		case RecordTerminator:
			p.parseTerminatorRecord(payload, session)
			offset = payloadEnd
			goto done
		}
		// PERIODIC and TIMESTAMP are silently ignored

		offset = payloadEnd
	}
done:
}

func (p *Parser) parseHeaderRecord(payload []byte, session *RKDSession) {
	// Strip trailing nulls
	end := len(payload)
	for end > 0 && payload[end-1] == 0 {
		end--
	}
	text := payload[:end]

	// Find null separator between key and value
	for i, b := range text {
		if b == 0 {
			key := string(text[:i])
			value := string(text[i+1:])
			session.Config[key] = value
			return
		}
	}
}

func (p *Parser) parseGPSRecord(payload []byte, frame int, session *RKDSession) {
	if len(payload) < 36 {
		return
	}
	gpsTS := binary.LittleEndian.Uint32(payload[4:8])
	sats := int16(binary.LittleEndian.Uint16(payload[8:10]))
	latRaw := int32(binary.LittleEndian.Uint32(payload[12:16]))
	lonRaw := int32(binary.LittleEndian.Uint32(payload[16:20]))
	speedRaw := int32(binary.LittleEndian.Uint32(payload[20:24]))
	headingRaw := int32(binary.LittleEndian.Uint32(payload[24:28]))
	altRaw := int32(binary.LittleEndian.Uint32(payload[28:32]))
	vspeed := int32(binary.LittleEndian.Uint32(payload[32:36]))

	fix := GPSFix{
		Frame:            frame,
		GPSTimestamp:     gpsTS,
		UTCMS:            GPSToUTCMs(gpsTS),
		Satellites:       sats,
		Latitude:         float64(latRaw) / 1e7,
		Longitude:        float64(lonRaw) / 1e7,
		SpeedMS:          float64(speedRaw) / 100.0,
		HeadingDeg:       float64(headingRaw) / 100000.0,
		AltitudeM:        float64(altRaw) / 1000.0,
		VerticalSpeedCms: vspeed,
	}
	session.GPSFixes = append(session.GPSFixes, fix)
}

func (p *Parser) parseAccelRecord(payload []byte, frame int) {
	if len(payload) < 12 {
		return
	}
	ax := int32(binary.LittleEndian.Uint32(payload[0:4]))
	ay := int32(binary.LittleEndian.Uint32(payload[4:8]))
	az := int32(binary.LittleEndian.Uint32(payload[8:12]))
	p.accelByFrame[frame] = [3]float64{
		float64(ax) * 9.81 / 1000.0,
		float64(ay) * 9.81 / 1000.0,
		float64(az) * 9.81 / 1000.0,
	}
}

func (p *Parser) parseGyroRecord(payload []byte, frame int) {
	if len(payload) < 12 {
		return
	}
	gx := int32(binary.LittleEndian.Uint32(payload[0:4]))
	gy := int32(binary.LittleEndian.Uint32(payload[4:8]))
	gz := int32(binary.LittleEndian.Uint32(payload[8:12]))
	p.gyroByFrame[frame] = [3]float64{
		float64(gx) / 28.0,
		float64(gy) / 28.0,
		float64(gz) / 28.0,
	}
}

func (p *Parser) parseTerminatorRecord(payload []byte, session *RKDSession) {
	if len(payload) >= 4 {
		session.TerminatorTimestamp = binary.LittleEndian.Uint32(payload[0:4])
	}
}

func (p *Parser) mergeIMU(session *RKDSession) {
	frameSet := make(map[int]bool)
	for f := range p.accelByFrame {
		frameSet[f] = true
	}
	for f := range p.gyroByFrame {
		frameSet[f] = true
	}

	frames := make([]int, 0, len(frameSet))
	for f := range frameSet {
		frames = append(frames, f)
	}
	sort.Ints(frames)

	for _, frame := range frames {
		accel := p.accelByFrame[frame] // zero value [3]float64{0,0,0} if missing
		gyro := p.gyroByFrame[frame]
		session.IMUFrames = append(session.IMUFrames, IMUFrame{
			Frame:  frame,
			AccelX: accel[0], AccelY: accel[1], AccelZ: accel[2],
			GyroX: gyro[0], GyroY: gyro[1], GyroZ: gyro[2],
		})
	}

	p.accelByFrame = nil
	p.gyroByFrame = nil
}
