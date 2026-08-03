package main

import (
	"context"
	"log"
	"sync"
	"time"
)

const alertMonitorInterval = 15 * time.Minute

type alertDelivery interface {
	PingLiveness(context.Context) error
	PingSuccess(context.Context) error
	Reconcile(context.Context, []Alert) error
}

type alertFetchObserver interface {
	ObserveFetch(context.Context, fetchResult)
}

type quarantineAlertStatus struct {
	Active               bool
	CausedCurrentFailure bool
}

type alertCoordinator struct {
	mu               sync.Mutex
	delivery         alertDelivery
	reportHealth     func() healthResponse
	reportQuarantine func() (quarantineAlertStatus, error)
	now              func() time.Time
}

func newAlertCoordinator(
	delivery alertDelivery,
	reportHealth func() healthResponse,
	reportQuarantine func() (quarantineAlertStatus, error),
	now func() time.Time,
) *alertCoordinator {
	return &alertCoordinator{
		delivery:         delivery,
		reportHealth:     reportHealth,
		reportQuarantine: reportQuarantine,
		now:              now,
	}
}

func (coordinator *alertCoordinator) Run(ctx context.Context) {
	coordinator.Tick(ctx)
	ticker := time.NewTicker(alertMonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			coordinator.Tick(ctx)
		}
	}
}

func (coordinator *alertCoordinator) Tick(ctx context.Context) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if err := coordinator.delivery.PingLiveness(ctx); err != nil {
		log.Printf("Unable to deliver liveness heartbeat: %v", err)
	}
	coordinator.reconcile(ctx)
}

func (coordinator *alertCoordinator) ObserveFetch(ctx context.Context, result fetchResult) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if result.Outcome == fetchSucceeded {
		if err := coordinator.delivery.PingSuccess(ctx); err != nil {
			log.Printf("Unable to deliver daily success heartbeat: %v", err)
		}
	}
	coordinator.reconcile(ctx)
}

func (coordinator *alertCoordinator) reconcile(ctx context.Context) {
	quarantine, err := coordinator.reportQuarantine()
	if err != nil {
		log.Printf("Unable to read quarantined candidate state: %v", err)
		return
	}
	alerts, err := buildApplicationAlerts(coordinator.now().UTC(), coordinator.reportHealth(), quarantine)
	if err != nil {
		log.Printf("Unable to construct active alerts: %v", err)
		return
	}
	if err := coordinator.delivery.Reconcile(ctx, alerts); err != nil {
		log.Printf("Unable to reconcile alerts: %v", err)
	}
}

func buildApplicationAlerts(now time.Time, health healthResponse, quarantine quarantineAlertStatus) ([]Alert, error) {
	alerts := make([]Alert, 0, 4)
	appendAlert := func(alert Alert) error {
		if err := alert.Validate(); err != nil {
			return err
		}
		alerts = append(alerts, alert)
		return nil
	}

	if health.CredentialState == healthCredentialExpired {
		if err := appendAlert(Alert{
			Condition:  alertConditionCredentialExpired,
			DetectedAt: now,
			Severity:   alertSeverityP1,
			Subject:    "Persisted credential",
			Verb:       "expired",
			Impact:     "Scheduled collection stopped",
			Recovery:   alertRecoveryNone,
			Action:     "REPLACE the persisted credential",
			RunbookURL: "http://psn.rx1.uk/runbook/credential/",
		}); err != nil {
			return nil, err
		}
	}

	quarantineCausedCurrentFailure := quarantine.Active && quarantine.CausedCurrentFailure && isQuarantineFailureReason(health.ActiveFetchFailureReason)
	if health.CredentialState != healthCredentialExpired && !quarantineCausedCurrentFailure && health.ConsecutiveFailures >= 3 {
		if err := appendAlert(Alert{
			Condition:  alertConditionFetchFailedThreeTimes,
			DetectedAt: now,
			Severity:   alertSeverityP1,
			Subject:    "Scheduled fetch",
			Verb:       "failed",
			Object:     "three times",
			Impact:     "Library data remains stale",
			Recovery:   alertRecoveryNone,
			Action:     "EXAMINE the fetch logs",
			RunbookURL: "http://psn.rx1.uk/runbook/fetch-failure/",
		}); err != nil {
			return nil, err
		}
	} else if health.CredentialState != healthCredentialExpired && !quarantineCausedCurrentFailure && health.ConsecutiveFailures > 0 {
		if err := appendAlert(Alert{
			Condition:  alertConditionFetchFailedOnce,
			DetectedAt: now,
			Severity:   alertSeverityP3,
			Subject:    "Scheduled fetch",
			Verb:       "failed",
			Object:     "once",
			Impact:     "Latest library data remains stale",
			Recovery:   alertRecoverySelf,
			RunbookURL: "http://psn.rx1.uk/runbook/fetch-failure/",
		}); err != nil {
			return nil, err
		}
	}

	if quarantine.Active {
		actBy := now.Add(48 * time.Hour)
		if err := appendAlert(Alert{
			Condition:  alertConditionCandidateQuarantined,
			DetectedAt: now,
			Severity:   alertSeverityP2,
			Subject:    "Candidate snapshot",
			Verb:       "entered",
			Object:     "quarantine",
			Impact:     "Candidate data remains excluded",
			Recovery:   alertRecoveryNone,
			Action:     "EXAMINE the quarantined candidate",
			ActBy:      &actBy,
			RunbookURL: "http://psn.rx1.uk/runbook/quarantined-candidate/",
		}); err != nil {
			return nil, err
		}
	}

	if health.BootsLastHour >= 3 {
		if err := appendAlert(Alert{
			Condition:  alertConditionCrashLoop,
			DetectedAt: now,
			Severity:   alertSeverityP1,
			Subject:    "Service",
			Verb:       "entered",
			Object:     "a crash loop",
			Impact:     "Repeated starts interrupt scheduled collection",
			Recovery:   alertRecoveryNone,
			Action:     "EXAMINE the container logs",
			RunbookURL: "http://psn.rx1.uk/runbook/crash-loop/",
		}); err != nil {
			return nil, err
		}
	}

	// Credential-expiry and capacity alerts require authoritative timestamps.
	// External monitoring owns missed heartbeat and public-probe alerts.
	return alerts, nil
}

func isQuarantineFailureReason(reason *failureReason) bool {
	if reason == nil {
		return false
	}
	switch *reason {
	case reasonInvalidSchema, reasonImplausibleSnapshot, reasonLibraryRegression, reasonLibraryRegressionPersistent:
		return true
	default:
		return false
	}
}
