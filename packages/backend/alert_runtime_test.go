package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAlertCoordinatorTickPingsLivenessAndReconcilesIndependently(t *testing.T) {
	delivery := &recordingAlertDelivery{livenessErr: errors.New("heartbeat unavailable")}
	coordinator := newAlertCoordinator(
		delivery,
		func() healthResponse { return healthResponse{} },
		func() (quarantineAlertStatus, error) { return quarantineAlertStatus{}, nil },
		func() time.Time { return time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC) },
	)

	coordinator.Tick(context.Background())

	if delivery.livenessCalls != 1 {
		t.Fatalf("liveness calls = %d, want 1", delivery.livenessCalls)
	}
	if delivery.reconcileCalls != 1 {
		t.Fatalf("reconcile calls = %d, want 1 despite heartbeat failure", delivery.reconcileCalls)
	}
}

func TestAlertCoordinatorPingsDailyHeartbeatOnlyAfterSucceededFetch(t *testing.T) {
	tests := []struct {
		name        string
		outcome     fetchOutcome
		wantSuccess int
	}{
		{name: "succeeded", outcome: fetchSucceeded, wantSuccess: 1},
		{name: "failed", outcome: fetchFailed, wantSuccess: 0},
		{name: "skipped", outcome: fetchSkipped, wantSuccess: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			delivery := &recordingAlertDelivery{}
			coordinator := newAlertCoordinator(
				delivery,
				func() healthResponse { return healthResponse{} },
				func() (quarantineAlertStatus, error) { return quarantineAlertStatus{}, nil },
				time.Now,
			)

			coordinator.ObserveFetch(context.Background(), fetchResult{Outcome: test.outcome})

			if delivery.successCalls != test.wantSuccess {
				t.Fatalf("success heartbeat calls = %d, want %d", delivery.successCalls, test.wantSuccess)
			}
			if delivery.reconcileCalls != 1 {
				t.Fatalf("reconcile calls = %d, want 1", delivery.reconcileCalls)
			}
		})
	}
}

func TestLogFetchResultDoesNotWaitForAlertDelivery(t *testing.T) {
	previous := activeAlertObserver
	observer := &blockingAlertObserver{started: make(chan struct{}), release: make(chan struct{})}
	activeAlertObserver = observer
	t.Cleanup(func() {
		activeAlertObserver = previous
		close(observer.release)
	})

	returned := make(chan struct{})
	go func() {
		logFetchResult(fetchResult{Outcome: fetchSkipped})
		close(returned)
	}()

	select {
	case <-observer.started:
	case <-time.After(time.Second):
		t.Fatal("alert observer did not start")
	}
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("fetch callback waited for alert delivery")
	}
}

func TestBuildApplicationAlertsUsesDurableFacts(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	reason := reasonAuthenticationRejected
	alerts, err := buildApplicationAlerts(now, healthResponse{
		CredentialState:          healthCredentialExpired,
		ConsecutiveFailures:      3,
		ActiveFetchFailureReason: &reason,
		BootsLastHour:            3,
		StorageUsedPercent:       95,
		MemoryUsedPercent:        92,
	}, quarantineAlertStatus{Active: true, CausedCurrentFailure: true})
	if err != nil {
		t.Fatalf("buildApplicationAlerts() error = %v", err)
	}

	want := map[AlertCondition]bool{
		alertConditionCredentialExpired:    true,
		alertConditionCandidateQuarantined: true,
		alertConditionCrashLoop:            true,
	}
	for _, alert := range alerts {
		if err := alert.Validate(); err != nil {
			t.Fatalf("invalid generated alert %#v: %v", alert, err)
		}
		delete(want, alert.Condition)
		if alert.Condition == alertConditionCapacityPressure {
			t.Fatal("capacity alert was fabricated without an authoritative recovery deadline")
		}
		if alert.Condition == alertConditionCredentialExpiring {
			t.Fatal("credential expiry warning was fabricated without an authoritative expiry time")
		}
		if alert.Condition == alertConditionLivenessMissed ||
			alert.Condition == alertConditionDailySuccessMissed ||
			alert.Condition == alertConditionPublicProbeFailed {
			t.Fatalf("external-only condition %q was generated inside the application", alert.Condition)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing active conditions: %#v", want)
	}
}

func TestBuildApplicationAlertsSuppressesGenericFetchAlertForSpecificCause(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	quarantineReason := reasonInvalidSchema
	tests := []struct {
		name       string
		health     healthResponse
		quarantine quarantineAlertStatus
		want       AlertCondition
	}{
		{
			name:   "credential rejection",
			health: healthResponse{CredentialState: healthCredentialExpired, ConsecutiveFailures: 1},
			want:   alertConditionCredentialExpired,
		},
		{
			name:       "quarantined candidate",
			health:     healthResponse{ConsecutiveFailures: 1, ActiveFetchFailureReason: &quarantineReason},
			quarantine: quarantineAlertStatus{Active: true, CausedCurrentFailure: true},
			want:       alertConditionCandidateQuarantined,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			alerts, err := buildApplicationAlerts(now, test.health, test.quarantine)
			if err != nil {
				t.Fatalf("buildApplicationAlerts() error = %v", err)
			}
			if len(alerts) != 1 || alerts[0].Condition != test.want {
				t.Fatalf("alerts = %#v, want only %q", alerts, test.want)
			}
		})
	}
}

func TestBuildApplicationAlertsKeepsGenericP1ForLaterUnrelatedFailure(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	reason := reasonInvalidSchema
	alerts, err := buildApplicationAlerts(now, healthResponse{
		ConsecutiveFailures:      3,
		ActiveFetchFailureReason: &reason,
	}, quarantineAlertStatus{Active: true, CausedCurrentFailure: false})
	if err != nil {
		t.Fatalf("buildApplicationAlerts() error = %v", err)
	}
	want := map[AlertCondition]bool{
		alertConditionCandidateQuarantined:  true,
		alertConditionFetchFailedThreeTimes: true,
	}
	for _, alert := range alerts {
		delete(want, alert.Condition)
	}
	if len(alerts) != 2 || len(want) != 0 {
		t.Fatalf("alerts = %#v, want retained quarantine P2 plus current repeated-failure P1", alerts)
	}
}

func TestBuildApplicationAlertsMapsThreeGenericFailuresToP1None(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	alerts, err := buildApplicationAlerts(now, healthResponse{ConsecutiveFailures: 3}, quarantineAlertStatus{})
	if err != nil {
		t.Fatalf("buildApplicationAlerts() error = %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("alerts = %#v, want one repeated fetch failure alert", alerts)
	}
	alert := alerts[0]
	if alert.Condition != alertConditionFetchFailedThreeTimes || alert.Severity != alertSeverityP1 || alert.Recovery != alertRecoveryNone {
		t.Fatalf("alert = %#v, want P1 NONE repeated fetch failure", alert)
	}
}

func TestBuildApplicationAlertsMapsOneFailureToP3SelfRecovery(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	alerts, err := buildApplicationAlerts(now, healthResponse{ConsecutiveFailures: 1}, quarantineAlertStatus{})
	if err != nil {
		t.Fatalf("buildApplicationAlerts() error = %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("alerts = %#v, want one fetch failure alert", alerts)
	}
	alert := alerts[0]
	if alert.Condition != alertConditionFetchFailedOnce || alert.Severity != alertSeverityP3 || alert.Recovery != alertRecoverySelf {
		t.Fatalf("alert = %#v, want P3 SELF fetch failure", alert)
	}
}

type recordingAlertDelivery struct {
	mu             sync.Mutex
	livenessCalls  int
	successCalls   int
	reconcileCalls int
	livenessErr    error
}

func (delivery *recordingAlertDelivery) PingLiveness(context.Context) error {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	delivery.livenessCalls++
	return delivery.livenessErr
}

func (delivery *recordingAlertDelivery) PingSuccess(context.Context) error {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	delivery.successCalls++
	return nil
}

func (delivery *recordingAlertDelivery) Reconcile(context.Context, []Alert) error {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	delivery.reconcileCalls++
	return nil
}

type blockingAlertObserver struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (observer *blockingAlertObserver) ObserveFetch(context.Context, fetchResult) {
	observer.once.Do(func() { close(observer.started) })
	<-observer.release
}
