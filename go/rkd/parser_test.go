package rkd

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleRKDPath returns the path to the shared sample file.
func sampleRKDPath() string {
	// go/rkd/ -> go/ -> repo root -> samples/
	return filepath.Join("..", "..", "samples", "sample_mettet.rkd")
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers — build minimal RKD binary data
// ─────────────────────────────────────────────────────────────────────────────

func metaHeader(carID, timestamp uint32) []byte {
	buf := make([]byte, metaHeaderSize)
	binary.LittleEndian.PutUint32(buf[0:4], 0x00148000)  // flags
	binary.LittleEndian.PutUint32(buf[4:8], 0)            // reserved
	binary.LittleEndian.PutUint32(buf[8:12], 1)           // seq
	binary.LittleEndian.PutUint32(buf[12:16], 0)          // reserved
	binary.LittleEndian.PutUint32(buf[16:20], carID)      // car_id
	binary.LittleEndian.PutUint32(buf[20:24], timestamp)  // timestamp
	binary.LittleEndian.PutUint32(buf[24:28], 0)          // reserved
	return buf
}

func makeRecord(rtype uint16, payload []byte, frame int) []byte {
	frameLo := uint16(frame & 0xFFFF)
	frameHi := uint16((frame >> 16) & 0xFFFF)
	header := make([]byte, recordHeaderSize)
	binary.LittleEndian.PutUint16(header[0:2], 0)                     // crc
	binary.LittleEndian.PutUint16(header[2:4], rtype)                 // type
	binary.LittleEndian.PutUint16(header[4:6], uint16(len(payload)))  // payload_size
	binary.LittleEndian.PutUint16(header[6:8], frameLo)               // frame_lo
	binary.LittleEndian.PutUint16(header[8:10], frameHi)              // frame_hi
	return append(header, payload...)
}

func minimalRKD(records ...[]byte) []byte {
	data := make([]byte, 0, 1024)
	data = append(data, RKDMagic...)
	data = append(data, metaHeader(11098, 1617532800)...)
	for _, rec := range records {
		data = append(data, rec...)
	}
	data = append(data, 0, 0) // trailing CRC
	return data
}

func gpsPayload(gpsTS uint32, sats int16, latRaw, lonRaw, speedRaw, headingRaw, altRaw, vspeed int32) []byte {
	buf := make([]byte, 36)
	binary.LittleEndian.PutUint32(buf[0:4], 3)       // fix type
	binary.LittleEndian.PutUint32(buf[4:8], gpsTS)
	binary.LittleEndian.PutUint16(buf[8:10], uint16(sats))
	binary.LittleEndian.PutUint16(buf[10:12], 0)      // padding
	binary.LittleEndian.PutUint32(buf[12:16], uint32(latRaw))
	binary.LittleEndian.PutUint32(buf[16:20], uint32(lonRaw))
	binary.LittleEndian.PutUint32(buf[20:24], uint32(speedRaw))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(headingRaw))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(altRaw))
	binary.LittleEndian.PutUint32(buf[32:36], uint32(vspeed))
	return buf
}

func defaultGPSPayload() []byte {
	return gpsPayload(1302000000, 18, 503000000, 46500000, 1000, 18000000, 250000, 10)
}

func accelPayload(ax, ay, az int32) []byte {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(ax))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(ay))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(az))
	return buf
}

func gyroPayload(gx, gy, gz int32) []byte {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(gx))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(gy))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(gz))
	return buf
}

func terminatorPayload(timestamp uint32) []byte {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[0:4], timestamp)
	return buf
}

func makeSessionWithData(numGPS, numIMU int, brakingFrame int) *RKDSession {
	session := &RKDSession{
		FilePath:     "test.rkd",
		FileSize:     1000,
		CarID:        11098,
		Timestamp:    1617532800,
		Config:       make(map[string]string),
		RecordCounts: make(map[uint16]int),
	}
	baseGPSTS := uint32(1302000000)
	for i := 0; i < numGPS; i++ {
		ts := baseGPSTS + uint32(i)
		session.GPSFixes = append(session.GPSFixes, GPSFix{
			Frame:            i * 6,
			GPSTimestamp:     ts,
			UTCMS:            GPSToUTCMs(ts),
			Satellites:       18,
			Latitude:         50.3 + float64(i)*0.0001,
			Longitude:        4.65 + float64(i)*0.0001,
			SpeedMS:          10.0 + float64(i),
			HeadingDeg:       90.0 + float64(i)*10,
			AltitudeM:        250.0 + float64(i),
			VerticalSpeedCms: 5,
		})
	}
	firstFrame := 0
	lastFrame := 0
	if numGPS > 0 {
		lastFrame = (numGPS - 1) * 6
	}
	for i := 0; i < numIMU; i++ {
		frame := firstFrame + i
		if frame > lastFrame {
			break
		}
		ax := 1.0
		if brakingFrame >= 0 && frame == brakingFrame {
			ax = -1.0
		}
		session.IMUFrames = append(session.IMUFrames, IMUFrame{
			Frame:  frame,
			AccelX: ax,
			AccelY: 0.5,
			AccelZ: 9.81,
			GyroX:  0.1,
			GyroY:  0.2,
			GyroZ:  5.0,
		})
	}
	return session
}

// ─────────────────────────────────────────────────────────────────────────────
// Pure utility functions
// ─────────────────────────────────────────────────────────────────────────────

func TestHaversine_SamePoint(t *testing.T) {
	d := Haversine(50.0, 4.0, 50.0, 4.0)
	if d != 0.0 {
		t.Errorf("expected 0.0, got %f", d)
	}
}

func TestHaversine_KnownDistance(t *testing.T) {
	d := Haversine(0.0, 0.0, 1.0, 0.0)
	if d < 110.0 || d > 112.0 {
		t.Errorf("expected ~111 km, got %f", d)
	}
}

func TestGPSToUTCMs_KnownConversion(t *testing.T) {
	result := GPSToUTCMs(gpsLeapSeconds)
	expected := gpsEpoch.UnixMilli()
	if result != expected {
		t.Errorf("expected %d, got %d", expected, result)
	}
}

func TestGPSToUTCMs_PositiveValue(t *testing.T) {
	result := GPSToUTCMs(1302000000)
	if result <= 0 {
		t.Errorf("expected positive value, got %d", result)
	}
}

func TestLerp_Midpoint(t *testing.T) {
	if Lerp(0.0, 10.0, 0.5) != 5.0 {
		t.Error("midpoint failed")
	}
}

func TestLerp_Endpoints(t *testing.T) {
	if Lerp(3.0, 7.0, 0.0) != 3.0 {
		t.Error("start endpoint failed")
	}
	if Lerp(3.0, 7.0, 1.0) != 7.0 {
		t.Error("end endpoint failed")
	}
}

func TestLerpAngle_Normal(t *testing.T) {
	result := LerpAngle(10.0, 20.0, 0.5)
	if math.Abs(result-15.0) > 0.01 {
		t.Errorf("expected ~15.0, got %f", result)
	}
}

func TestLerpAngle_Wraparound(t *testing.T) {
	result := LerpAngle(350.0, 10.0, 0.5)
	if math.Abs(result-0.0) > 0.01 && math.Abs(result-360.0) > 0.01 {
		t.Errorf("expected ~0.0 or ~360.0, got %f", result)
	}
}

func TestXMLEscape_AllSpecialChars(t *testing.T) {
	result := XMLEscape(`A & B <C> "D"`)
	expected := "A &amp; B &lt;C&gt; &quot;D&quot;"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestXMLEscape_NoSpecialChars(t *testing.T) {
	result := XMLEscape("hello")
	if result != "hello" {
		t.Errorf("expected %q, got %q", "hello", result)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RKDSession properties
// ─────────────────────────────────────────────────────────────────────────────

func TestDurationSeconds_Empty(t *testing.T) {
	s := &RKDSession{}
	if s.DurationSeconds() != 0.0 {
		t.Error("expected 0.0 for empty session")
	}
}

func TestDurationSeconds_OneFix(t *testing.T) {
	s := &RKDSession{}
	s.GPSFixes = append(s.GPSFixes, GPSFix{GPSTimestamp: 100})
	if s.DurationSeconds() != 0.0 {
		t.Error("expected 0.0 for single fix")
	}
}

func TestDurationSeconds_TwoFixes(t *testing.T) {
	s := &RKDSession{}
	s.GPSFixes = append(s.GPSFixes, GPSFix{GPSTimestamp: 100})
	s.GPSFixes = append(s.GPSFixes, GPSFix{GPSTimestamp: 110})
	if s.DurationSeconds() != 10.0 {
		t.Errorf("expected 10.0, got %f", s.DurationSeconds())
	}
}

func TestMaxSpeedKmh_Empty(t *testing.T) {
	s := &RKDSession{}
	if s.MaxSpeedKmh() != 0.0 {
		t.Error("expected 0.0 for empty session")
	}
}

func TestMaxSpeedKmh_WithData(t *testing.T) {
	s := &RKDSession{}
	s.GPSFixes = append(s.GPSFixes, GPSFix{SpeedMS: 10.0})
	expected := 36.0
	if math.Abs(s.MaxSpeedKmh()-expected) > 0.01 {
		t.Errorf("expected %f, got %f", expected, s.MaxSpeedKmh())
	}
}

func TestTotalDistanceKm_Empty(t *testing.T) {
	s := &RKDSession{}
	if s.TotalDistanceKm() != 0.0 {
		t.Error("expected 0.0 for empty session")
	}
}

func TestTotalDistanceKm_OneFix(t *testing.T) {
	s := &RKDSession{}
	s.GPSFixes = append(s.GPSFixes, GPSFix{Latitude: 50.0, Longitude: 4.0})
	if s.TotalDistanceKm() != 0.0 {
		t.Error("expected 0.0 for single fix")
	}
}

func TestTotalDistanceKm_TwoFixes(t *testing.T) {
	s := &RKDSession{}
	s.GPSFixes = append(s.GPSFixes, GPSFix{Latitude: 50.0, Longitude: 4.0})
	s.GPSFixes = append(s.GPSFixes, GPSFix{Latitude: 50.001, Longitude: 4.001})
	if s.TotalDistanceKm() <= 0 {
		t.Error("expected positive distance")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Parser validation
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateMagic_TooSmall(t *testing.T) {
	p := &Parser{}
	err := p.validateMagic([]byte("short"))
	if err == nil || !strings.Contains(err.Error(), "too small") {
		t.Error("expected error for short data")
	}
}

func TestValidateMagic_WrongMagic(t *testing.T) {
	p := &Parser{}
	err := p.validateMagic(make([]byte, 40))
	if err == nil || !strings.Contains(err.Error(), "invalid RKD magic") {
		t.Error("expected error for wrong magic")
	}
}

func TestValidateMagic_Valid(t *testing.T) {
	p := &Parser{}
	data := append([]byte{}, RKDMagic...)
	data = append(data, make([]byte, 30)...)
	if err := p.validateMagic(data); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseMetaHeader_TooSmall(t *testing.T) {
	p := &Parser{}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	data := append([]byte{}, RKDMagic...)
	data = append(data, make([]byte, 10)...)
	err := p.parseMetaHeader(data, session)
	if err == nil || !strings.Contains(err.Error(), "too small for meta header") {
		t.Error("expected error for short meta header")
	}
}

func TestParseMetaHeader_Valid(t *testing.T) {
	p := &Parser{}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	data := append([]byte{}, RKDMagic...)
	data = append(data, metaHeader(12345, 1617532800)...)
	if err := p.parseMetaHeader(data, session); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if session.CarID != 12345 {
		t.Errorf("expected CarID 12345, got %d", session.CarID)
	}
	if session.Timestamp != 1617532800 {
		t.Errorf("expected Timestamp 1617532800, got %d", session.Timestamp)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Record parsing
// ─────────────────────────────────────────────────────────────────────────────

func TestParseHeaderRecord_ValidKeyValue(t *testing.T) {
	p := &Parser{}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	payload := []byte("HW_ID\x00RK1234\x00")
	p.parseHeaderRecord(payload, session)
	if session.Config["HW_ID"] != "RK1234" {
		t.Errorf("expected RK1234, got %s", session.Config["HW_ID"])
	}
}

func TestParseHeaderRecord_NoNullSeparator(t *testing.T) {
	p := &Parser{}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	p.parseHeaderRecord([]byte("NOVALUE"), session)
	if len(session.Config) != 0 {
		t.Error("expected empty config for no-separator payload")
	}
}

func TestParseGPSRecord_Valid(t *testing.T) {
	p := &Parser{}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	p.parseGPSRecord(defaultGPSPayload(), 0, session)
	if len(session.GPSFixes) != 1 {
		t.Fatal("expected 1 GPS fix")
	}
	fix := session.GPSFixes[0]
	if math.Abs(fix.Latitude-50.3) > 0.001 {
		t.Errorf("expected lat ~50.3, got %f", fix.Latitude)
	}
	if math.Abs(fix.Longitude-4.65) > 0.001 {
		t.Errorf("expected lon ~4.65, got %f", fix.Longitude)
	}
}

func TestParseGPSRecord_Malformed(t *testing.T) {
	p := &Parser{}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	p.parseGPSRecord(make([]byte, 10), 0, session)
	if len(session.GPSFixes) != 0 {
		t.Error("expected 0 GPS fixes for malformed payload")
	}
}

func TestParseAccelRecord_Valid(t *testing.T) {
	p := &Parser{accelByFrame: make(map[int][3]float64)}
	p.parseAccelRecord(accelPayload(0, 0, 1000), 100)
	if _, ok := p.accelByFrame[100]; !ok {
		t.Fatal("expected frame 100 in accelByFrame")
	}
	if math.Abs(p.accelByFrame[100][2]-9.81) > 0.01 {
		t.Errorf("expected ~9.81, got %f", p.accelByFrame[100][2])
	}
}

func TestParseAccelRecord_Malformed(t *testing.T) {
	p := &Parser{accelByFrame: make(map[int][3]float64)}
	p.parseAccelRecord(make([]byte, 5), 100)
	if _, ok := p.accelByFrame[100]; ok {
		t.Error("expected no entry for malformed payload")
	}
}

func TestParseGyroRecord_Valid(t *testing.T) {
	p := &Parser{gyroByFrame: make(map[int][3]float64)}
	p.parseGyroRecord(gyroPayload(280, 0, 0), 100)
	if _, ok := p.gyroByFrame[100]; !ok {
		t.Fatal("expected frame 100 in gyroByFrame")
	}
	if math.Abs(p.gyroByFrame[100][0]-10.0) > 0.01 {
		t.Errorf("expected ~10.0, got %f", p.gyroByFrame[100][0])
	}
}

func TestParseGyroRecord_Malformed(t *testing.T) {
	p := &Parser{gyroByFrame: make(map[int][3]float64)}
	p.parseGyroRecord(make([]byte, 5), 100)
	if _, ok := p.gyroByFrame[100]; ok {
		t.Error("expected no entry for malformed payload")
	}
}

func TestParseTerminatorRecord_Valid(t *testing.T) {
	p := &Parser{}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	payload := make([]byte, 12)
	binary.LittleEndian.PutUint32(payload[0:4], 1617533000)
	p.parseTerminatorRecord(payload, session)
	if session.TerminatorTimestamp != 1617533000 {
		t.Errorf("expected 1617533000, got %d", session.TerminatorTimestamp)
	}
}

func TestParseTerminatorRecord_Short(t *testing.T) {
	p := &Parser{}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	p.parseTerminatorRecord(make([]byte, 2), session)
	if session.TerminatorTimestamp != 0 {
		t.Errorf("expected 0, got %d", session.TerminatorTimestamp)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// mergeIMU
// ─────────────────────────────────────────────────────────────────────────────

func TestMergeIMU_Empty(t *testing.T) {
	p := &Parser{accelByFrame: make(map[int][3]float64), gyroByFrame: make(map[int][3]float64)}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	p.mergeIMU(session)
	if len(session.IMUFrames) != 0 {
		t.Error("expected 0 IMU frames")
	}
}

func TestMergeIMU_BothSensors(t *testing.T) {
	p := &Parser{
		accelByFrame: map[int][3]float64{100: {1.0, 2.0, 9.81}},
		gyroByFrame:  map[int][3]float64{100: {0.1, 0.2, 0.3}},
	}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	p.mergeIMU(session)
	if len(session.IMUFrames) != 1 {
		t.Fatal("expected 1 IMU frame")
	}
	if session.IMUFrames[0].AccelX != 1.0 {
		t.Errorf("expected AccelX 1.0, got %f", session.IMUFrames[0].AccelX)
	}
	if session.IMUFrames[0].GyroZ != 0.3 {
		t.Errorf("expected GyroZ 0.3, got %f", session.IMUFrames[0].GyroZ)
	}
}

func TestMergeIMU_AccelOnly(t *testing.T) {
	p := &Parser{
		accelByFrame: map[int][3]float64{100: {1.0, 2.0, 9.81}},
		gyroByFrame:  make(map[int][3]float64),
	}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	p.mergeIMU(session)
	if len(session.IMUFrames) != 1 {
		t.Fatal("expected 1 IMU frame")
	}
	if session.IMUFrames[0].GyroX != 0.0 {
		t.Errorf("expected GyroX 0.0, got %f", session.IMUFrames[0].GyroX)
	}
}

func TestMergeIMU_GyroOnly(t *testing.T) {
	p := &Parser{
		accelByFrame: make(map[int][3]float64),
		gyroByFrame:  map[int][3]float64{100: {0.1, 0.2, 0.3}},
	}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	p.mergeIMU(session)
	if len(session.IMUFrames) != 1 {
		t.Fatal("expected 1 IMU frame")
	}
	if session.IMUFrames[0].AccelX != 0.0 {
		t.Errorf("expected AccelX 0.0, got %f", session.IMUFrames[0].AccelX)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Parse records dispatch
// ─────────────────────────────────────────────────────────────────────────────

func TestParseRecords_NoRecords(t *testing.T) {
	p := &Parser{}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	raw := minimalRKD()
	p.accelByFrame = make(map[int][3]float64)
	p.gyroByFrame = make(map[int][3]float64)
	p.parseRecords(raw, session)
	if len(session.GPSFixes) != 0 {
		t.Error("expected 0 GPS fixes")
	}
}

func TestParseRecords_TruncatedRecord(t *testing.T) {
	// Record header claims payload is 100 bytes but file ends
	badRecord := make([]byte, recordHeaderSize)
	binary.LittleEndian.PutUint16(badRecord[2:4], RecordGPS)
	binary.LittleEndian.PutUint16(badRecord[4:6], 100)
	raw := minimalRKD(badRecord)
	p := &Parser{accelByFrame: make(map[int][3]float64), gyroByFrame: make(map[int][3]float64)}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	p.parseRecords(raw, session)
	if len(session.GPSFixes) != 0 {
		t.Error("expected 0 GPS fixes for truncated record")
	}
}

func TestParseRecords_AllRecordTypes(t *testing.T) {
	headerRec := makeRecord(RecordHeader, []byte("KEY\x00VAL\x00"), 0)
	gpsRec := makeRecord(RecordGPS, defaultGPSPayload(), 0)
	accelRec := makeRecord(RecordAccel, accelPayload(0, 0, 1000), 0)
	gyroRec := makeRecord(RecordGyro, gyroPayload(0, 0, 0), 0)
	periodicRec := makeRecord(RecordPeriodic, make([]byte, 4), 0)
	timestampRec := makeRecord(RecordTimestamp, make([]byte, 4), 0)
	termRec := makeRecord(RecordTerminator, terminatorPayload(1617533000), 0)

	raw := minimalRKD(headerRec, gpsRec, accelRec, gyroRec, periodicRec, timestampRec, termRec)
	p := &Parser{accelByFrame: make(map[int][3]float64), gyroByFrame: make(map[int][3]float64)}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	p.parseRecords(raw, session)

	if session.Config["KEY"] != "VAL" {
		t.Errorf("expected Config[KEY]=VAL, got %s", session.Config["KEY"])
	}
	if len(session.GPSFixes) != 1 {
		t.Error("expected 1 GPS fix")
	}
	if _, ok := p.accelByFrame[0]; !ok {
		t.Error("expected accel at frame 0")
	}
	if _, ok := p.gyroByFrame[0]; !ok {
		t.Error("expected gyro at frame 0")
	}
	if session.RecordCounts[RecordPeriodic] != 1 {
		t.Error("expected 1 PERIODIC record")
	}
	if session.RecordCounts[RecordTimestamp] != 1 {
		t.Error("expected 1 TIMESTAMP record")
	}
	if session.RecordCounts[RecordTerminator] != 1 {
		t.Error("expected 1 TERMINATOR record")
	}
	if session.TerminatorTimestamp != 1617533000 {
		t.Errorf("expected terminator timestamp 1617533000, got %d", session.TerminatorTimestamp)
	}
}

func TestParseRecords_NoTerminator(t *testing.T) {
	gpsRec := makeRecord(RecordGPS, defaultGPSPayload(), 0)
	raw := minimalRKD(gpsRec)
	p := &Parser{accelByFrame: make(map[int][3]float64), gyroByFrame: make(map[int][3]float64)}
	session := &RKDSession{Config: make(map[string]string), RecordCounts: make(map[uint16]int)}
	p.parseRecords(raw, session)
	if len(session.GPSFixes) != 1 {
		t.Error("expected 1 GPS fix")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Full parse integration
// ─────────────────────────────────────────────────────────────────────────────

func TestParseSampleFile(t *testing.T) {
	path := sampleRKDPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	p := &Parser{}
	session, err := p.Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.CarID != 11098 {
		t.Errorf("expected CarID 11098, got %d", session.CarID)
	}
	if len(session.GPSFixes) != 50 {
		t.Errorf("expected 50 GPS fixes, got %d", len(session.GPSFixes))
	}
	if len(session.IMUFrames) == 0 {
		t.Error("expected IMU frames")
	}
	if session.MaxSpeedKmh() <= 0 {
		t.Error("expected positive max speed")
	}
	if session.TotalDistanceKm() <= 0 {
		t.Error("expected positive distance")
	}
	if session.DurationSeconds() <= 0 {
		t.Error("expected positive duration")
	}
}

func TestParseCraftedFile(t *testing.T) {
	gps1 := makeRecord(RecordGPS, gpsPayload(1302000000, 18, 503000000, 46500000, 1000, 18000000, 250000, 10), 0)
	accel1 := makeRecord(RecordAccel, accelPayload(0, 0, 1000), 0)
	gyro1 := makeRecord(RecordGyro, gyroPayload(0, 0, 0), 0)
	gps2 := makeRecord(RecordGPS, gpsPayload(1302000001, 18, 503001000, 46501000, 1100, 18100000, 251000, 10), 6)
	accel2 := makeRecord(RecordAccel, accelPayload(0, 0, 1000), 6)
	gyro2 := makeRecord(RecordGyro, gyroPayload(0, 0, 0), 6)

	raw := minimalRKD(gps1, accel1, gyro1, gps2, accel2, gyro2)
	tmpDir := t.TempDir()
	rkdFile := filepath.Join(tmpDir, "test.rkd")
	if err := os.WriteFile(rkdFile, raw, 0644); err != nil {
		t.Fatal(err)
	}

	p := &Parser{}
	session, err := p.Parse(rkdFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(session.GPSFixes) != 2 {
		t.Errorf("expected 2 GPS fixes, got %d", len(session.GPSFixes))
	}
	if len(session.IMUFrames) != 2 {
		t.Errorf("expected 2 IMU frames, got %d", len(session.IMUFrames))
	}
}

func TestParseBytes_Invalid(t *testing.T) {
	p := &Parser{}
	_, err := p.ParseBytes([]byte("not an rkd file"), "bad.rkd")
	if err == nil {
		t.Error("expected error for invalid data")
	}
}

func TestParse_FileNotFound(t *testing.T) {
	p := &Parser{}
	_, err := p.Parse("/nonexistent/file.rkd")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CSV export
// ─────────────────────────────────────────────────────────────────────────────

func TestExportCSV_NoIMUFrames(t *testing.T) {
	session := &RKDSession{
		FilePath:     "test.rkd",
		Config:       make(map[string]string),
		RecordCounts: make(map[uint16]int),
	}
	session.GPSFixes = append(session.GPSFixes, GPSFix{})
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "out.csv")
	err := ExportCSV(session, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// File should not exist (early return with warning)
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("expected no CSV file for empty IMU data")
	}
}

func TestExportCSV_NoGPSFixes(t *testing.T) {
	session := &RKDSession{
		FilePath:     "test.rkd",
		Config:       make(map[string]string),
		RecordCounts: make(map[uint16]int),
	}
	session.IMUFrames = append(session.IMUFrames, IMUFrame{})
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "out.csv")
	err := ExportCSV(session, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExportCSV_NormalExport(t *testing.T) {
	session := makeSessionWithData(3, 18, 3)
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "out.csv")
	err := ExportCSV(session, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("utc (ms)")) {
		t.Error("expected header row")
	}
	lines := strings.Split(string(content), "\r\n")
	// Should have header + data rows + trailing empty
	if len(lines) < 3 {
		t.Error("expected at least header + data rows")
	}
	// Check for braking values
	hasBraking := strings.Contains(string(content), ",1,")
	hasNotBraking := strings.Contains(string(content), ",0,")
	if !hasBraking || !hasNotBraking {
		t.Error("expected both braking and non-braking values")
	}
}

func TestExportCSV_SingleGPSFix(t *testing.T) {
	session := &RKDSession{
		FilePath:     "test.rkd",
		FileSize:     100,
		Config:       make(map[string]string),
		RecordCounts: make(map[uint16]int),
	}
	session.GPSFixes = append(session.GPSFixes, GPSFix{
		Frame: 0, GPSTimestamp: 1302000000, UTCMS: GPSToUTCMs(1302000000),
		Satellites: 18, Latitude: 50.3, Longitude: 4.65,
		SpeedMS: 10.0, HeadingDeg: 90.0, AltitudeM: 250.0, VerticalSpeedCms: 5,
	})
	session.IMUFrames = append(session.IMUFrames, IMUFrame{
		Frame: 0, AccelX: 1.0, AccelY: 0.5, AccelZ: 9.81,
		GyroX: 0.1, GyroY: 0.2, GyroZ: 5.0,
	})
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "out.csv")
	err := ExportCSV(session, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(out); os.IsNotExist(err) {
		t.Error("expected CSV file to exist")
	}
}

func TestExportCSV_IMUOutsideGPSRange(t *testing.T) {
	session := &RKDSession{
		FilePath:     "test.rkd",
		FileSize:     100,
		Config:       make(map[string]string),
		RecordCounts: make(map[uint16]int),
	}
	session.GPSFixes = append(session.GPSFixes, GPSFix{
		Frame: 10, GPSTimestamp: 1302000000, UTCMS: GPSToUTCMs(1302000000),
		Satellites: 18, Latitude: 50.3, Longitude: 4.65,
		SpeedMS: 10.0, HeadingDeg: 90.0, AltitudeM: 250.0, VerticalSpeedCms: 5,
	})
	session.GPSFixes = append(session.GPSFixes, GPSFix{
		Frame: 20, GPSTimestamp: 1302000001, UTCMS: GPSToUTCMs(1302000001),
		Satellites: 18, Latitude: 50.3001, Longitude: 4.6501,
		SpeedMS: 11.0, HeadingDeg: 100.0, AltitudeM: 251.0, VerticalSpeedCms: 5,
	})
	// IMU frames: before, inside, and after GPS range
	session.IMUFrames = append(session.IMUFrames, IMUFrame{Frame: 5, AccelX: 1.0, AccelY: 0.5, AccelZ: 9.81, GyroX: 0.1, GyroY: 0.2, GyroZ: 5.0})
	session.IMUFrames = append(session.IMUFrames, IMUFrame{Frame: 15, AccelX: 1.0, AccelY: 0.5, AccelZ: 9.81, GyroX: 0.1, GyroY: 0.2, GyroZ: 5.0})
	session.IMUFrames = append(session.IMUFrames, IMUFrame{Frame: 25, AccelX: 1.0, AccelY: 0.5, AccelZ: 9.81, GyroX: 0.1, GyroY: 0.2, GyroZ: 5.0})
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "out.csv")
	err := ExportCSV(session, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// Count data lines (header + 1 data row for frame 15)
	lines := strings.Split(strings.TrimRight(string(content), "\r\n"), "\r\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines (header + 1 data), got %d", len(lines))
	}
}

func TestExportCSV_IMUAtLastGPSFix(t *testing.T) {
	session := &RKDSession{
		FilePath:     "test.rkd",
		FileSize:     100,
		Config:       make(map[string]string),
		RecordCounts: make(map[uint16]int),
	}
	session.GPSFixes = append(session.GPSFixes, GPSFix{
		Frame: 0, GPSTimestamp: 1302000000, UTCMS: GPSToUTCMs(1302000000),
		Satellites: 18, Latitude: 50.3, Longitude: 4.65,
		SpeedMS: 10.0, HeadingDeg: 90.0, AltitudeM: 250.0, VerticalSpeedCms: 5,
	})
	session.GPSFixes = append(session.GPSFixes, GPSFix{
		Frame: 6, GPSTimestamp: 1302000001, UTCMS: GPSToUTCMs(1302000001),
		Satellites: 18, Latitude: 50.3001, Longitude: 4.6501,
		SpeedMS: 11.0, HeadingDeg: 100.0, AltitudeM: 251.0, VerticalSpeedCms: 5,
	})
	session.IMUFrames = append(session.IMUFrames, IMUFrame{Frame: 0, AccelX: 1.0, AccelY: 0.5, AccelZ: 9.81, GyroX: 0.1, GyroY: 0.2, GyroZ: 5.0})
	session.IMUFrames = append(session.IMUFrames, IMUFrame{Frame: 6, AccelX: 1.0, AccelY: 0.5, AccelZ: 9.81, GyroX: 0.1, GyroY: 0.2, GyroZ: 5.0})
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "out.csv")
	err := ExportCSV(session, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(content), "\r\n"), "\r\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (header + 2 data), got %d", len(lines))
	}
}

func TestExportCSV_SampleFile(t *testing.T) {
	path := sampleRKDPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	p := &Parser{}
	session, err := p.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "sample.csv")
	if err := ExportCSV(session, out); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(content), "\r\n"), "\r\n")
	if len(lines) < 2 {
		t.Error("expected at least header + data")
	}
	// Check header count
	headers := strings.Split(lines[0], ",")
	if len(headers) != 24 {
		t.Errorf("expected 24 columns, got %d", len(headers))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GPX export
// ─────────────────────────────────────────────────────────────────────────────

func TestExportGPX_NoGPSFixes(t *testing.T) {
	session := &RKDSession{
		FilePath:     "test.rkd",
		Config:       make(map[string]string),
		RecordCounts: make(map[uint16]int),
	}
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "out.gpx")
	err := ExportGPX(session, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExportGPX_NormalExport(t *testing.T) {
	session := makeSessionWithData(3, 18, -1)
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "out.gpx")
	err := ExportGPX(session, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)
	if !strings.Contains(s, `<?xml version="1.0"`) {
		t.Error("expected XML declaration")
	}
	if !strings.Contains(s, "<trkpt") {
		t.Error("expected trkpt elements")
	}
	if strings.Count(s, "<trkpt") != 3 {
		t.Errorf("expected 3 trkpt elements, got %d", strings.Count(s, "<trkpt"))
	}
}

func TestExportGPX_SampleFile(t *testing.T) {
	path := sampleRKDPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	p := &Parser{}
	session, err := p.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "sample.gpx")
	if err := ExportGPX(session, out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); os.IsNotExist(err) {
		t.Error("expected GPX file to exist")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Print session info
// ─────────────────────────────────────────────────────────────────────────────

func TestPrintSessionInfo_Full(t *testing.T) {
	session := makeSessionWithData(3, 18, -1)
	session.Config["HW_ID"] = "RK1234"
	session.RecordCounts = map[uint16]int{RecordGPS: 3, RecordAccel: 18, 999: 1}
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	PrintSessionInfo(session)
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()
	if !strings.Contains(out, "RKD Session") {
		t.Error("expected 'RKD Session' in output")
	}
	if !strings.Contains(out, "Car ID") {
		t.Error("expected 'Car ID' in output")
	}
	if !strings.Contains(out, "GPS data") {
		t.Error("expected 'GPS data' in output")
	}
	if !strings.Contains(out, "IMU data") {
		t.Error("expected 'IMU data' in output")
	}
	if !strings.Contains(out, "HW_ID") {
		t.Error("expected 'HW_ID' in output")
	}
	if !strings.Contains(out, "UNKNOWN(0x03e7)") {
		t.Error("expected 'UNKNOWN(0x03e7)' in output")
	}
}

func TestPrintSessionInfo_Empty(t *testing.T) {
	session := &RKDSession{
		Config:       make(map[string]string),
		RecordCounts: make(map[uint16]int),
	}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	PrintSessionInfo(session)
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()
	if !strings.Contains(out, "RKD Session") {
		t.Error("expected 'RKD Session' in output")
	}
	if strings.Contains(out, "GPS data") {
		t.Error("expected no 'GPS data' in output")
	}
	if strings.Contains(out, "IMU data") {
		t.Error("expected no 'IMU data' in output")
	}
}

func TestPrintSessionInfo_NoTimestamp(t *testing.T) {
	session := &RKDSession{
		Timestamp:    0,
		Config:       make(map[string]string),
		RecordCounts: make(map[uint16]int),
	}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	PrintSessionInfo(session)
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()
	if strings.Contains(out, "Session start") {
		t.Error("expected no 'Session start' for zero timestamp")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateSampleRKD
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateSampleRKD_FromSampleFile(t *testing.T) {
	path := sampleRKDPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "truncated.rkd")
	if err := CreateSampleRKD(path, out, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); os.IsNotExist(err) {
		t.Error("expected output file to exist")
	}
	p := &Parser{}
	session, err := p.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.GPSFixes) > 5 {
		t.Errorf("expected <= 5 GPS fixes, got %d", len(session.GPSFixes))
	}
}

func TestCreateSampleRKD_WithTerminator(t *testing.T) {
	gpsRec := makeRecord(RecordGPS, defaultGPSPayload(), 0)
	termRec := makeRecord(RecordTerminator, terminatorPayload(1617533000), 0)
	raw := minimalRKD(gpsRec, termRec)
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "with_term.rkd")
	os.WriteFile(src, raw, 0644)
	out := filepath.Join(tmpDir, "sample_with_term.rkd")
	if err := CreateSampleRKD(src, out, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); os.IsNotExist(err) {
		t.Error("expected output file to exist")
	}
}

func TestCreateSampleRKD_TruncatedRecord(t *testing.T) {
	gpsRec := makeRecord(RecordGPS, defaultGPSPayload(), 0)
	badHeader := make([]byte, recordHeaderSize)
	binary.LittleEndian.PutUint16(badHeader[2:4], RecordGPS)
	binary.LittleEndian.PutUint16(badHeader[4:6], 100)
	raw := minimalRKD(gpsRec, badHeader)
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "trunc.rkd")
	os.WriteFile(src, raw, 0644)
	out := filepath.Join(tmpDir, "sample_trunc.rkd")
	if err := CreateSampleRKD(src, out, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); os.IsNotExist(err) {
		t.Error("expected output file to exist")
	}
}

func TestCreateSampleRKD_NoRecords(t *testing.T) {
	raw := minimalRKD()
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "empty.rkd")
	os.WriteFile(src, raw, 0644)
	out := filepath.Join(tmpDir, "sample_empty.rkd")
	if err := CreateSampleRKD(src, out, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); os.IsNotExist(err) {
		t.Error("expected output file to exist")
	}
}

func TestCreateSampleRKD_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	err := CreateSampleRKD("/nonexistent/file.rkd", filepath.Join(tmpDir, "out.rkd"), 10)
	if err == nil {
		t.Error("expected error for missing input file")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// formatInt helper
// ─────────────────────────────────────────────────────────────────────────────

func TestFormatInt(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1000000, "1,000,000"},
		{123456789, "123,456,789"},
	}
	for _, tc := range tests {
		result := formatInt(tc.input)
		if result != tc.expected {
			t.Errorf("formatInt(%d) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// baseName helper
// ─────────────────────────────────────────────────────────────────────────────

func TestBaseName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"foo/bar/baz.rkd", "baz.rkd"},
		{"baz.rkd", "baz.rkd"},
		{"/absolute/path/file.txt", "file.txt"},
	}
	for _, tc := range tests {
		result := baseName(tc.input)
		if result != tc.expected {
			t.Errorf("baseName(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Error paths
// ─────────────────────────────────────────────────────────────────────────────

func TestExportCSV_CreateError(t *testing.T) {
	session := makeSessionWithData(3, 18, -1)
	err := ExportCSV(session, "/nonexistent/dir/out.csv")
	if err == nil {
		t.Error("expected error for invalid output path")
	}
}

func TestExportGPX_CreateError(t *testing.T) {
	session := makeSessionWithData(3, 18, -1)
	err := ExportGPX(session, "/nonexistent/dir/out.gpx")
	if err == nil {
		t.Error("expected error for invalid output path")
	}
}

func TestCreateSampleRKD_WriteError(t *testing.T) {
	gpsRec := makeRecord(RecordGPS, defaultGPSPayload(), 0)
	raw := minimalRKD(gpsRec)
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "input.rkd")
	os.WriteFile(src, raw, 0644)
	err := CreateSampleRKD(src, "/nonexistent/dir/out.rkd", 10)
	if err == nil {
		t.Error("expected error for invalid output path")
	}
}

func TestParseBytes_ValidMagicShortMeta(t *testing.T) {
	// Valid magic but not enough data for meta header
	data := append([]byte{}, RKDMagic...)
	data = append(data, make([]byte, 10)...) // not enough for 28-byte meta header
	p := &Parser{}
	_, err := p.ParseBytes(data, "short.rkd")
	if err == nil {
		t.Error("expected error for short meta header")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PrintSessionInfo min/max branch coverage
// ─────────────────────────────────────────────────────────────────────────────

func TestPrintSessionInfo_DecreasingGPSData(t *testing.T) {
	session := &RKDSession{
		FilePath:     "test.rkd",
		FileSize:     1000,
		CarID:        11098,
		Timestamp:    1617532800,
		Config:       make(map[string]string),
		RecordCounts: map[uint16]int{RecordGPS: 3},
	}
	// GPS fixes with DECREASING lat, lon, alt and VARYING sats
	// to hit the minLat, minLon, minAlt, minSat, maxSat branches
	session.GPSFixes = []GPSFix{
		{Frame: 0, GPSTimestamp: 1302000000, UTCMS: GPSToUTCMs(1302000000),
			Satellites: 15, Latitude: 50.302, Longitude: 4.652,
			SpeedMS: 10.0, HeadingDeg: 90.0, AltitudeM: 260.0},
		{Frame: 6, GPSTimestamp: 1302000001, UTCMS: GPSToUTCMs(1302000001),
			Satellites: 12, Latitude: 50.301, Longitude: 4.651,
			SpeedMS: 11.0, HeadingDeg: 100.0, AltitudeM: 255.0},
		{Frame: 12, GPSTimestamp: 1302000002, UTCMS: GPSToUTCMs(1302000002),
			Satellites: 19, Latitude: 50.300, Longitude: 4.650,
			SpeedMS: 12.0, HeadingDeg: 110.0, AltitudeM: 250.0},
	}
	session.IMUFrames = []IMUFrame{
		{Frame: 0, AccelZ: 9.81},
	}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	PrintSessionInfo(session)
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()
	// Verify min/max ranges appear
	if !strings.Contains(out, "50.3000000") {
		t.Error("expected min lat in output")
	}
	if !strings.Contains(out, "12 – 19") {
		t.Error("expected satellite range 12–19 in output")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Byte-for-byte identical output with Python
// ─────────────────────────────────────────────────────────────────────────────

func TestCSVIdenticalToPython(t *testing.T) {
	path := sampleRKDPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	refCSV := filepath.Join("..", "..", "samples", "sample_output.csv")
	if _, err := os.Stat(refCSV); os.IsNotExist(err) {
		t.Skip("reference CSV not found")
	}
	p := &Parser{}
	session, err := p.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "sample_mettet.csv")
	if err := ExportCSV(session, out); err != nil {
		t.Fatal(err)
	}
	goCSV, _ := os.ReadFile(out)
	pyCSV, _ := os.ReadFile(refCSV)
	if !bytes.Equal(goCSV, pyCSV) {
		t.Error("Go CSV output differs from Python reference")
	}
}

func TestGPXIdenticalToPython(t *testing.T) {
	path := sampleRKDPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	refGPX := filepath.Join("..", "..", "samples", "sample_output.gpx")
	if _, err := os.Stat(refGPX); os.IsNotExist(err) {
		t.Skip("reference GPX not found")
	}
	p := &Parser{}
	session, err := p.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "sample_mettet.gpx")
	if err := ExportGPX(session, out); err != nil {
		t.Fatal(err)
	}
	goGPX, _ := os.ReadFile(out)
	pyGPX, _ := os.ReadFile(refGPX)
	if !bytes.Equal(goGPX, pyGPX) {
		t.Error("Go GPX output differs from Python reference")
	}
}
