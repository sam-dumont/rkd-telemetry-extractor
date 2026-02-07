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
