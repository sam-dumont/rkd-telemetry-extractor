// rkd-parser — Race-Keeper RKD Telemetry Parser & Exporter (Go)
//
// Parses proprietary Race-Keeper (.rkd) binary telemetry files and exports to:
//   - Telemetry Overlay Custom CSV (30 Hz, with GPS interpolation)
//   - GPX 1.1 track format (at native GPS rate, ~5 Hz)
//
// Author: @sam-dumont (with Claude Code)
// License: MIT
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sam-dumont/rkd-telemetry-extractor/go/rkd"
)

func findRKDFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".rkd") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func processFile(rkdPath string, outputDir string, infoOnly, noCSV, noGPX bool) (*rkd.RKDSession, error) {
	parser := &rkd.Parser{}
	session, err := parser.Parse(rkdPath)
	if err != nil {
		return nil, err
	}

	if infoOnly {
		rkd.PrintSessionInfo(session)
		return session, nil
	}

	rkd.PrintSessionInfo(session)

	if outputDir == "" {
		outputDir = filepath.Dir(rkdPath)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, err
	}

	stem := strings.TrimSuffix(filepath.Base(rkdPath), filepath.Ext(rkdPath))

	if !noCSV {
		csvPath := filepath.Join(outputDir, stem+".csv")
		if err := rkd.ExportCSV(session, csvPath); err != nil {
			return nil, err
		}
	}

	if !noGPX {
		gpxPath := filepath.Join(outputDir, stem+".gpx")
		if err := rkd.ExportGPX(session, gpxPath); err != nil {
			return nil, err
		}
	}

	return session, nil
}

func run() int {
	fs := flag.NewFlagSet("rkd-parser", flag.ContinueOnError)
	info := fs.Bool("info", false, "Print session summary only (no file export)")
	allIn := fs.String("all-in", "", "Recursively process all .rkd files in DIR")
	outputDir := fs.String("output-dir", "", "Directory for output files (default: same as input)")
	noCSV := fs.Bool("no-csv", false, "Skip CSV export")
	noGPX := fs.Bool("no-gpx", false, "Skip GPX export")
	sample := fs.Int("sample", 0, "Create a truncated sample .rkd with the first N GPS fixes")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: rkd-parser [options] <file.rkd>\n\n")
		fmt.Fprintf(os.Stderr, "Race-Keeper RKD Telemetry Parser — Extract GPS, IMU, and\n")
		fmt.Fprintf(os.Stderr, "telemetry data from .rkd files into Telemetry Overlay CSV\n")
		fmt.Fprintf(os.Stderr, "and GPX formats.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample: rkd-parser outing.rkd --info\n")
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		return 1
	}

	if *allIn != "" {
		files, err := findRKDFiles(*allIn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning directory: %v\n", err)
			return 1
		}
		if len(files) == 0 {
			fmt.Fprintf(os.Stderr, "No .rkd files found in %s\n", *allIn)
			return 1
		}
		fmt.Printf("Found %d .rkd file(s)\n\n", len(files))
		for _, f := range files {
			if _, err := processFile(f, *outputDir, *info, *noCSV, *noGPX); err != nil {
				fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", f, err)
			}
		}
		return 0
	}

	if fs.NArg() < 1 {
		fs.Usage()
		return 1
	}

	inputPath := fs.Arg(0)
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: File not found: %s\n", inputPath)
		return 1
	}

	if _, err := processFile(inputPath, *outputDir, *info, *noCSV, *noGPX); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	if *sample > 0 {
		outDir := *outputDir
		if outDir == "" {
			outDir = filepath.Dir(inputPath)
		}
		stem := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
		samplePath := filepath.Join(outDir, "sample_"+stem+".rkd")
		if err := rkd.CreateSampleRKD(inputPath, samplePath, *sample); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating sample: %v\n", err)
			return 1
		}
	}

	return 0
}

func main() {
	os.Exit(run())
}
