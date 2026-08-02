package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildCapacityReportMeasuresArchiveAndBoundedPercentages(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "first.snapshot"), make([]byte, 17), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "second.snapshot"), make([]byte, 25), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := BuildCapacityReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if report.ArchiveBytes != 42 {
		t.Fatalf("archive bytes = %d, want 42", report.ArchiveBytes)
	}
	if report.StorageUsedPercent < 0 || report.StorageUsedPercent > 100 {
		t.Fatalf("storage used percent = %f, want value in [0,100]", report.StorageUsedPercent)
	}
	if report.MemoryUsedPercent < 0 || report.MemoryUsedPercent > 100 {
		t.Fatalf("memory used percent = %f, want value in [0,100]", report.MemoryUsedPercent)
	}
}
