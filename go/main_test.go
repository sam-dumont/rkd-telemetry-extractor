package main

import (
	"os"
	"path/filepath"
	"testing"
)

func samplePath() string {
	return filepath.Join("..", "samples", "sample_mettet.rkd")
}

func TestRun_NoArgs(t *testing.T) {
	os.Args = []string{"rkd-parser"}
	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRun_FileNotFound(t *testing.T) {
	os.Args = []string{"rkd-parser", "/nonexistent/file.rkd"}
	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRun_InfoOnly(t *testing.T) {
	path := samplePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	os.Args = []string{"rkd-parser", "-info", path}
	code := run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestRun_FullExport(t *testing.T) {
	path := samplePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	tmpDir := t.TempDir()
	os.Args = []string{"rkd-parser", "-output-dir", tmpDir, path}
	code := run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	// Check that CSV and GPX files were created
	if _, err := os.Stat(filepath.Join(tmpDir, "sample_mettet.csv")); os.IsNotExist(err) {
		t.Error("expected CSV file")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "sample_mettet.gpx")); os.IsNotExist(err) {
		t.Error("expected GPX file")
	}
}

func TestRun_NoCSV(t *testing.T) {
	path := samplePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	tmpDir := t.TempDir()
	os.Args = []string{"rkd-parser", "-no-csv", "-output-dir", tmpDir, path}
	code := run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "sample_mettet.csv")); !os.IsNotExist(err) {
		t.Error("expected no CSV file")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "sample_mettet.gpx")); os.IsNotExist(err) {
		t.Error("expected GPX file")
	}
}

func TestRun_NoGPX(t *testing.T) {
	path := samplePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	tmpDir := t.TempDir()
	os.Args = []string{"rkd-parser", "-no-gpx", "-output-dir", tmpDir, path}
	code := run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "sample_mettet.csv")); os.IsNotExist(err) {
		t.Error("expected CSV file")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "sample_mettet.gpx")); !os.IsNotExist(err) {
		t.Error("expected no GPX file")
	}
}

func TestRun_Sample(t *testing.T) {
	path := samplePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	tmpDir := t.TempDir()
	os.Args = []string{"rkd-parser", "-sample", "5", "-output-dir", tmpDir, path}
	code := run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "sample_sample_mettet.rkd")); os.IsNotExist(err) {
		t.Error("expected sample RKD file")
	}
}

func TestRun_AllIn(t *testing.T) {
	path := samplePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	tmpDir := t.TempDir()
	os.Args = []string{"rkd-parser", "-all-in", filepath.Dir(path), "-output-dir", tmpDir}
	code := run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestRun_AllIn_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	os.Args = []string{"rkd-parser", "-all-in", tmpDir}
	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRun_AllIn_BadDir(t *testing.T) {
	os.Args = []string{"rkd-parser", "-all-in", "/nonexistent/dir"}
	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRun_DefaultOutputDir(t *testing.T) {
	// processFile with outputDir="" uses the input file's directory
	path := samplePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	// Copy sample to temp dir so output goes there
	tmpDir := t.TempDir()
	data, _ := os.ReadFile(path)
	tmpFile := filepath.Join(tmpDir, "test.rkd")
	os.WriteFile(tmpFile, data, 0644)
	os.Args = []string{"rkd-parser", tmpFile}
	code := run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "test.csv")); os.IsNotExist(err) {
		t.Error("expected CSV file in same dir as input")
	}
}

func TestRun_SampleDefaultOutputDir(t *testing.T) {
	// -sample with no -output-dir defaults to input file's directory
	path := samplePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sample file not found")
	}
	tmpDir := t.TempDir()
	data, _ := os.ReadFile(path)
	tmpFile := filepath.Join(tmpDir, "test.rkd")
	os.WriteFile(tmpFile, data, 0644)
	os.Args = []string{"rkd-parser", "-sample", "5", tmpFile}
	code := run()
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "sample_test.rkd")); os.IsNotExist(err) {
		t.Error("expected sample file in same dir as input")
	}
}

func TestRun_InvalidFile(t *testing.T) {
	// Existing file that isn't a valid RKD
	tmpDir := t.TempDir()
	badFile := filepath.Join(tmpDir, "bad.rkd")
	os.WriteFile(badFile, []byte("not an rkd file"), 0644)
	os.Args = []string{"rkd-parser", badFile}
	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRun_AllIn_WithBadFile(t *testing.T) {
	// all-in directory with an invalid .rkd file (covers error log in loop)
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "bad.rkd"), []byte("not valid"), 0644)
	os.Args = []string{"rkd-parser", "-all-in", tmpDir}
	code := run()
	// Should succeed (errors are logged, not fatal)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestRun_InvalidFlag(t *testing.T) {
	os.Args = []string{"rkd-parser", "-invalid-flag"}
	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestFindRKDFiles(t *testing.T) {
	tmpDir := t.TempDir()
	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "a.rkd"), []byte{}, 0644)
	os.WriteFile(filepath.Join(tmpDir, "b.RKD"), []byte{}, 0644)
	os.WriteFile(filepath.Join(tmpDir, "c.txt"), []byte{}, 0644)

	files, err := findRKDFiles(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 .rkd files, got %d", len(files))
	}
}
