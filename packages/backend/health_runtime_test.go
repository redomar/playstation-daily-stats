package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestHealthRuntimeCollectsDurableStateBootsSnapshotsAndCapacity(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	lastSuccess := now.Add(-time.Hour)
	dir := t.TempDir()
	acceptedOlder := writeHealthSnapshot(t, dir, now.Add(-2*time.Hour), 169)
	acceptedNewest := writeHealthSnapshot(t, dir, lastSuccess, 170)
	excludedNewest := writeHealthSnapshot(t, dir, now.Add(-30*time.Minute), 100)
	auditPath := filepath.Join(dir, "corpus-audit.json")
	manifest := corpusAuditManifest{Version: 1, ExcludedCount: 1, Entries: []corpusAuditEntry{{
		Filename:       excludedNewest,
		Classification: auditExcluded,
		Reason:         reasonLibraryRegression,
	}}}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auditPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}

	events := newEventStore(filepath.Join(dir, "events.jsonl"))
	for _, timestamp := range []time.Time{now.Add(-2 * time.Hour), now.Add(-30 * time.Minute), now.Add(-time.Minute)} {
		if err := events.Append(eventRecord{Timestamp: timestamp, Kind: eventKindBoot, Message: "Service started."}); err != nil {
			t.Fatal(err)
		}
	}
	application := &appState{}
	application.setCredential(credentialResolution{Value: "not-public", State: credentialReady})
	application.applyDurableFetchState(durableFetchState{
		LatestOutcome: fetchSucceeded,
		LastSuccessAt: &lastSuccess,
		BaselineCount: 170,
	})

	runtime := newHealthRuntime(application, events, dir, auditPath)
	runtime.now = func() time.Time { return now }
	runtime.buildCapacity = func(string) (CapacityReport, error) {
		return CapacityReport{ArchiveBytes: 1234, StorageUsedPercent: 18.4, MemoryUsedPercent: 51.7}, nil
	}

	response := runtime.report()
	if response.Status != healthStatusHealthy {
		t.Fatalf("status = %q, want healthy; response=%#v", response.Status, response)
	}
	if response.SnapshotCount != 2 || response.BootsLastHour != 2 {
		t.Fatalf("durable counts = snapshots %d, boots %d; want 2, 2", response.SnapshotCount, response.BootsLastHour)
	}
	if acceptedOlder == acceptedNewest {
		t.Fatal("test snapshots unexpectedly collided")
	}
	if response.LastSuccess == nil || !response.LastSuccess.Equal(lastSuccess) {
		t.Fatalf("last success = %v, want %v", response.LastSuccess, lastSuccess)
	}
	if response.CredentialState != healthCredentialOK {
		t.Fatalf("credential state = %q, want ok", response.CredentialState)
	}
	if response.StorageUsedPercent != 18.4 || response.MemoryUsedPercent != 51.7 || response.ArchiveBytes != 1234 {
		t.Fatalf("capacity = %#v", response)
	}
}

func TestHealthRuntimeDoesNotTrustSuccessTimestampWithoutAValidSnapshot(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	durableSuccess := now.Add(-time.Hour)
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "corpus-audit.json")
	manifestData, err := json.Marshal(corpusAuditManifest{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auditPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	events := newEventStore(filepath.Join(dir, "events.jsonl"))
	if err := events.Append(eventRecord{Timestamp: now, Kind: eventKindBoot, Message: "Service started."}); err != nil {
		t.Fatal(err)
	}
	application := &appState{}
	application.setCredential(credentialResolution{Value: "not-public", State: credentialReady})
	application.applyDurableFetchState(durableFetchState{LatestOutcome: fetchSucceeded, LastSuccessAt: &durableSuccess})
	runtime := newHealthRuntime(application, events, dir, auditPath)
	runtime.now = func() time.Time { return now }
	runtime.buildCapacity = func(string) (CapacityReport, error) { return CapacityReport{}, nil }

	response := runtime.report()
	if response.Status != healthStatusDegraded || response.LastSuccess != nil || response.SnapshotCount != 0 {
		t.Fatalf("health trusted missing snapshot: %#v", response)
	}
}

func writeHealthSnapshot(t *testing.T, dir string, capturedAt time.Time, titleCount int) string {
	t.Helper()
	name := "output_" + strconv.FormatInt(capturedAt.Unix(), 10) + ".json"
	data, err := json.Marshal(canonicalSnapshot{Titles: rawTitles(t, validTitlePage(0, titleCount))})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestHandleHealthUsesContractStatusCodeAndPublicBody(t *testing.T) {
	previous := reportCurrentHealth
	t.Cleanup(func() { reportCurrentHealth = previous })

	for _, test := range []struct {
		name     string
		status   healthStatus
		wantCode int
	}{
		{name: "healthy", status: healthStatusHealthy, wantCode: http.StatusOK},
		{name: "degraded", status: healthStatusDegraded, wantCode: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			reportCurrentHealth = func() healthResponse {
				return healthResponse{Status: test.status, CredentialState: healthCredentialOK}
			}
			recorder := httptest.NewRecorder()
			handleHealth(recorder, httptest.NewRequest(http.MethodGet, "/api/health", nil))

			if recorder.Code != test.wantCode {
				t.Fatalf("status code = %d, want %d; body=%s", recorder.Code, test.wantCode, recorder.Body.String())
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("content type = %q, want application/json", got)
			}
			var body map[string]json.RawMessage
			if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if _, exists := body["npsso_length"]; exists {
				t.Fatalf("public body exposed npsso_length: %s", recorder.Body.String())
			}
		})
	}
}
