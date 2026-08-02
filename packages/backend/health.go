package main

import "time"

type healthStatus string

const (
	healthStatusHealthy  healthStatus = "healthy"
	healthStatusDegraded healthStatus = "degraded"
)

type healthCredentialState string

const (
	healthCredentialOK       healthCredentialState = "ok"
	healthCredentialExpiring healthCredentialState = "expiring"
	healthCredentialExpired  healthCredentialState = "expired"
)

type healthInput struct {
	LastSuccessAt            *time.Time
	SnapshotCount            int
	ConsecutiveFailures      int
	ActiveFetchFailureReason failureReason
	CredentialState          healthCredentialState
	BootsLastHour            int
	StorageUsedPercent       float64
	MemoryUsedPercent        float64
	ArchiveBytes             int64
}

type healthResponse struct {
	Status                   healthStatus          `json:"status"`
	LastSuccess              *time.Time            `json:"last_success"`
	SnapshotAgeHours         *float64              `json:"snapshot_age_hours"`
	SnapshotCount            int                   `json:"snapshot_count"`
	ConsecutiveFailures      int                   `json:"consecutive_failures"`
	ActiveFetchFailureReason *failureReason        `json:"last_failure_reason"`
	CredentialState          healthCredentialState `json:"credential_state"`
	BootsLastHour            int                   `json:"boots_last_hour"`
	StorageUsedPercent       float64               `json:"storage_used_percent"`
	MemoryUsedPercent        float64               `json:"memory_used_percent"`
	ArchiveBytes             int64                 `json:"archive_bytes"`
}

func evaluateHealth(now time.Time, input healthInput) healthResponse {
	var lastSuccess *time.Time
	var snapshotAgeHours *float64
	var activeFetchFailureReason *failureReason
	if input.ActiveFetchFailureReason != "" {
		value := input.ActiveFetchFailureReason
		activeFetchFailureReason = &value
	}
	status := healthStatusDegraded
	if input.LastSuccessAt != nil {
		value := input.LastSuccessAt.UTC()
		lastSuccess = &value
		age := now.Sub(*input.LastSuccessAt).Hours()
		snapshotAgeHours = &age
		if age < 30 && input.ConsecutiveFailures == 0 && input.ActiveFetchFailureReason == "" && input.BootsLastHour < 3 {
			status = healthStatusHealthy
		}
	}

	return healthResponse{
		Status:                   status,
		LastSuccess:              lastSuccess,
		SnapshotAgeHours:         snapshotAgeHours,
		SnapshotCount:            input.SnapshotCount,
		ConsecutiveFailures:      input.ConsecutiveFailures,
		ActiveFetchFailureReason: activeFetchFailureReason,
		CredentialState:          input.CredentialState,
		BootsLastHour:            input.BootsLastHour,
		StorageUsedPercent:       input.StorageUsedPercent,
		MemoryUsedPercent:        input.MemoryUsedPercent,
		ArchiveBytes:             input.ArchiveBytes,
	}
}
