package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCorpusAuditClassifiesEverySnapshotWithoutDeletingRawFiles(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 9, 5, 10, 0, 0, 0, time.UTC)
	writeSnapshotFixture(t, dir, base, validTitlePage(0, 1))
	writeSnapshotFixture(t, dir, base.Add(time.Hour), validTitlePage(0, 3))
	writeSnapshotFixture(t, dir, base.Add(2*time.Hour), validTitlePage(0, 2))
	writeSnapshotFixture(t, dir, base.Add(3*time.Hour), validTitlePage(0, 2))
	errorName := fmt.Sprintf("output_%d.json", base.Add(4*time.Hour).Unix())
	if err := os.WriteFile(filepath.Join(dir, errorName), []byte(`{"error":{"reason":"BadRequest"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "corpus-audit.json")

	manifest, err := auditCorpus(dir, manifestPath, base.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Entries) != 5 {
		t.Fatalf("manifest entries = %d, want 5", len(manifest.Entries))
	}
	if manifest.AcceptedCount != 2 || manifest.ExcludedCount != 3 {
		t.Fatalf("manifest counts = accepted %d excluded %d, want 2 and 3", manifest.AcceptedCount, manifest.ExcludedCount)
	}
	wantReasons := map[string]failureReason{
		fmt.Sprintf("output_%d.json", base.Unix()):                  reasonImplausibleSnapshot,
		fmt.Sprintf("output_%d.json", base.Add(2*time.Hour).Unix()): reasonLibraryRegression,
		errorName: reasonInvalidSchema,
	}
	for _, entry := range manifest.Entries {
		if want, excluded := wantReasons[entry.Filename]; excluded {
			if entry.Classification != auditExcluded || entry.Reason != want {
				t.Errorf("entry %s = %s/%s, want excluded/%s", entry.Filename, entry.Classification, entry.Reason, want)
			}
			if _, err := os.Stat(filepath.Join(dir, entry.Filename)); err != nil {
				t.Errorf("excluded raw file %s was removed: %v", entry.Filename, err)
			}
		}
		if len(entry.Rules) != 3 {
			t.Errorf("entry %s has %d rule results, want 3", entry.Filename, len(entry.Rules))
		}
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted corpusAuditManifest
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.ExcludedCount != 3 {
		t.Fatalf("persisted excluded count = %d, want 3", persisted.ExcludedCount)
	}
}

func TestAnalyticsLoadsOnlySnapshotsAcceptedByCorpusAudit(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 12, 13, 6, 0, 0, 0, time.UTC)
	writeSnapshotFixture(t, dir, base, validTitlePage(0, 2))
	errorName := fmt.Sprintf("output_%d.json", base.Add(24*time.Hour).Unix())
	if err := os.WriteFile(filepath.Join(dir, errorName), []byte(`{"error":{"reason":"BadRequest"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSnapshotFixture(t, dir, base.Add(48*time.Hour), validTitlePage(0, 2))
	manifestPath := filepath.Join(dir, "corpus-audit.json")
	if _, err := auditCorpus(dir, manifestPath, base.Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}

	snapshots, err := loadSnapshots(dir, manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("analytics snapshots = %d, want 2 accepted snapshots", len(snapshots))
	}
	for _, snapshot := range snapshots {
		if snapshot.Filename == errorName {
			t.Fatalf("excluded candidate %s entered analytics", errorName)
		}
	}
}

func writeSnapshotFixture(t *testing.T, dir string, capturedAt time.Time, titles []map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"titles": titles})
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("output_%d.json", capturedAt.Unix())
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
