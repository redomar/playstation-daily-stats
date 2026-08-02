package main

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestEvaluateHealthReportsFreshSucceededFetchAsHealthy(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	lastSuccess := now.Add(-29*time.Hour - 30*time.Minute)

	got := evaluateHealth(now, healthInput{
		LastSuccessAt:            &lastSuccess,
		SnapshotCount:            472,
		CredentialState:          healthCredentialOK,
		BootsLastHour:            2,
		StorageUsedPercent:       18.4,
		MemoryUsedPercent:        51.7,
		ArchiveBytes:             456130560,
		ConsecutiveFailures:      0,
		ActiveFetchFailureReason: "",
	})

	if got.Status != healthStatusHealthy {
		t.Fatalf("status = %q, want %q", got.Status, healthStatusHealthy)
	}
	if got.LastSuccess == nil || !got.LastSuccess.Equal(lastSuccess) {
		t.Fatalf("last success = %v, want %v", got.LastSuccess, lastSuccess)
	}
	if got.SnapshotAgeHours == nil || *got.SnapshotAgeHours != 29.5 {
		t.Fatalf("snapshot age = %v, want 29.5", got.SnapshotAgeHours)
	}
	if got.SnapshotCount != 472 || got.CredentialState != healthCredentialOK || got.BootsLastHour != 2 {
		t.Fatalf("durable health fields = %#v", got)
	}
	if got.StorageUsedPercent != 18.4 || got.MemoryUsedPercent != 51.7 || got.ArchiveBytes != 456130560 {
		t.Fatalf("capacity health fields = %#v", got)
	}
	if got.ConsecutiveFailures != 0 || got.ActiveFetchFailureReason != nil {
		t.Fatalf("failure health fields = %#v", got)
	}
}

func TestEvaluateHealthReportsSnapshotAtThirtyHoursAsDegraded(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	lastSuccess := now.Add(-30 * time.Hour)

	got := evaluateHealth(now, healthInput{
		LastSuccessAt:   &lastSuccess,
		CredentialState: healthCredentialOK,
	})

	if got.Status != healthStatusDegraded {
		t.Fatalf("status = %q, want %q", got.Status, healthStatusDegraded)
	}
}

func TestEvaluateHealthReportsActiveFetchFailureAsDegraded(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	lastSuccess := now.Add(-time.Hour)

	got := evaluateHealth(now, healthInput{
		LastSuccessAt:            &lastSuccess,
		ConsecutiveFailures:      1,
		ActiveFetchFailureReason: reasonAuthenticationRejected,
		CredentialState:          healthCredentialExpired,
	})

	if got.Status != healthStatusDegraded {
		t.Fatalf("status = %q, want %q", got.Status, healthStatusDegraded)
	}
	if got.ActiveFetchFailureReason == nil || *got.ActiveFetchFailureReason != reasonAuthenticationRejected {
		t.Fatalf("active fetch failure reason = %v, want %q", got.ActiveFetchFailureReason, reasonAuthenticationRejected)
	}
}

func TestEvaluateHealthReportsThirdBootWithinHourAsDegraded(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	lastSuccess := now.Add(-time.Hour)

	got := evaluateHealth(now, healthInput{
		LastSuccessAt:   &lastSuccess,
		CredentialState: healthCredentialOK,
		BootsLastHour:   3,
	})

	if got.Status != healthStatusDegraded {
		t.Fatalf("status = %q, want %q", got.Status, healthStatusDegraded)
	}
}

func TestEvaluateHealthReportsMissingSucceededFetchAsDegraded(t *testing.T) {
	got := evaluateHealth(time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC), healthInput{
		CredentialState: healthCredentialOK,
	})

	if got.Status != healthStatusDegraded {
		t.Fatalf("status = %q, want %q", got.Status, healthStatusDegraded)
	}
	if got.LastSuccess != nil || got.SnapshotAgeHours != nil {
		t.Fatalf("missing succeeded fetch produced timestamps: %#v", got)
	}
}

func TestEvaluateHealthTreatsCredentialStateAsInformational(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	lastSuccess := now.Add(-time.Hour)

	got := evaluateHealth(now, healthInput{
		LastSuccessAt:   &lastSuccess,
		CredentialState: healthCredentialExpired,
	})

	if got.Status != healthStatusHealthy {
		t.Fatalf("status = %q, want %q", got.Status, healthStatusHealthy)
	}
}

func TestHealthResponseJSONContainsOnlyPublicContractFields(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	lastSuccess := now.Add(-time.Hour)
	response := evaluateHealth(now, healthInput{
		LastSuccessAt:   &lastSuccess,
		CredentialState: healthCredentialExpiring,
	})

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	gotKeys := make([]string, 0, len(object))
	for key := range object {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(gotKeys)
	wantKeys := []string{
		"archive_bytes",
		"boots_last_hour",
		"consecutive_failures",
		"credential_state",
		"last_failure_reason",
		"last_success",
		"memory_used_percent",
		"snapshot_age_hours",
		"snapshot_count",
		"status",
		"storage_used_percent",
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("JSON keys = %v, want %v; JSON: %s", gotKeys, wantKeys, data)
	}
	if string(object["last_failure_reason"]) != "null" {
		t.Fatalf("last_failure_reason = %s, want null", object["last_failure_reason"])
	}
}
