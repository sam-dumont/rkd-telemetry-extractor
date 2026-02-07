package rkd

import (
	"encoding/binary"
	"fmt"
	"os"
)

// CreateSampleRKD creates a truncated sample RKD file containing the first N GPS fixes.
func CreateSampleRKD(inputPath, outputPath string, maxGPSFixes int) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}

	offset := len(RKDMagic) + metaHeaderSize
	end := len(data) - trailingCRCSize

	// Start with magic + meta header
	out := make([]byte, 0, len(data))
	out = append(out, data[:offset]...)

	gpsCount := 0
	for offset+recordHeaderSize <= end {
		rtype := binary.LittleEndian.Uint16(data[offset+2 : offset+4])
		payloadSize := int(binary.LittleEndian.Uint16(data[offset+4 : offset+6]))
		recordEnd := offset + recordHeaderSize + payloadSize

		if recordEnd > end {
			break
		}

		if rtype == RecordGPS {
			gpsCount++
			if gpsCount > maxGPSFixes {
				break
			}
		}

		out = append(out, data[offset:recordEnd]...)

		if rtype == RecordTerminator {
			break
		}

		offset = recordEnd
	}

	// Trailing CRC
	out = append(out, 0, 0)

	if err := os.WriteFile(outputPath, out, 0644); err != nil {
		return err
	}

	actual := gpsCount
	if actual > maxGPSFixes {
		actual = maxGPSFixes
	}
	fmt.Printf("  Sample: %s (%s bytes, %d GPS fixes)\n", outputPath, formatInt(len(out)), actual)
	return nil
}
