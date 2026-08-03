package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReconcileDeliversP1UrgentlyAfterRecordingAttempt(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	store := newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
	alert := validDeliveryAlert(now, alertSeverityP1)
	wantBody, err := alert.Render()
	if err != nil {
		t.Fatalf("render valid alert fixture: %v", err)
	}

	requestSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", request.Method)
		}
		if priority := request.Header.Get("Priority"); priority != "urgent" {
			t.Errorf("Priority = %q, want urgent", priority)
		}
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Errorf("read request body: %v", readErr)
		} else if string(body) != wantBody {
			t.Errorf("body = %q, want %q", body, wantBody)
		}
		events, readErr := store.Recent(eventQuery{Kind: eventKindAlert, Limit: 10})
		if readErr != nil {
			t.Errorf("read alert events during request: %v", readErr)
		} else if !hasDeliveryEvent(events, alert.SuppressionKey(), "attempt", "") {
			t.Error("ntfy request arrived before its durable attempt event")
		}
		requestSeen <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	runtime := newDeliveryRuntimeWithClient(deliveryConfig{NtfyTopicURL: server.URL}, store, server.Client(), func() time.Time { return now })
	if err := runtime.Reconcile(context.Background(), []Alert{alert}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	select {
	case <-requestSeen:
	default:
		t.Fatal("Reconcile() did not send the ntfy request")
	}

	events, err := store.Recent(eventQuery{Kind: eventKindAlert, Limit: 10})
	if err != nil {
		t.Fatalf("read alert events: %v", err)
	}
	if !hasDeliveryEvent(events, alert.SuppressionKey(), "result", "succeeded") {
		t.Fatal("successful ntfy result was not recorded")
	}
	if !hasAlertField(events, alert.SuppressionKey(), "recovery", string(alert.Recovery)) {
		t.Fatal("alert recovery contract was not recorded")
	}
	if !hasAlertField(events, alert.SuppressionKey(), "otel_severity", "ERROR") {
		t.Fatal("OTel severity mapping was not recorded")
	}
	if !hasNumericAlertField(events, alert.SuppressionKey(), "syslog_severity", 2) {
		t.Fatal("syslog severity mapping was not recorded")
	}
	for _, event := range events {
		if strings.Contains(event.Message, server.URL) {
			t.Fatal("secret ntfy URL was written to an event message")
		}
		for _, value := range event.Fields {
			if text, ok := value.(string); ok && strings.Contains(text, server.URL) {
				t.Fatal("secret ntfy URL was written to an event field")
			}
		}
	}
}

func TestReconcileSuppressesUnresolvedAlertForTwentyFourHoursAcrossRestart(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	store := newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
	alert := validDeliveryAlert(now, alertSeverityP1)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	config := deliveryConfig{NtfyTopicURL: server.URL}

	firstRuntime := newDeliveryRuntimeWithClient(config, store, server.Client(), func() time.Time { return now })
	if err := firstRuntime.Reconcile(context.Background(), []Alert{alert}); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}

	restartedRuntime := newDeliveryRuntimeWithClient(config, newEventStore(store.path), server.Client(), func() time.Time {
		return now.Add(time.Hour)
	})
	if err := restartedRuntime.Reconcile(context.Background(), []Alert{alert}); err != nil {
		t.Fatalf("restarted Reconcile() error = %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("ntfy requests = %d, want 1 during the suppression window", got)
	}
}

func TestReconcileDeliversAConditionAgainAfterItResolves(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	clock := now
	store := newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
	alert := validDeliveryAlert(now, alertSeverityP1)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runtime := newDeliveryRuntimeWithClient(
		deliveryConfig{NtfyTopicURL: server.URL},
		store,
		server.Client(),
		func() time.Time { return clock },
	)

	if err := runtime.Reconcile(context.Background(), []Alert{alert}); err != nil {
		t.Fatalf("active Reconcile() error = %v", err)
	}
	clock = now.Add(time.Hour)
	if err := runtime.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("resolved Reconcile() error = %v", err)
	}
	clock = now.Add(2 * time.Hour)
	if err := runtime.Reconcile(context.Background(), []Alert{alert}); err != nil {
		t.Fatalf("new episode Reconcile() error = %v", err)
	}

	if got := requests.Load(); got != 2 {
		t.Fatalf("ntfy requests = %d, want a new delivery after resolution", got)
	}
	events, err := store.Recent(eventQuery{Kind: eventKindAlert, Limit: 20})
	if err != nil {
		t.Fatalf("read alert events: %v", err)
	}
	if !hasAlertState(events, alert.SuppressionKey(), "resolved") {
		t.Fatal("resolved condition was not recorded durably")
	}
}

func TestPingLivenessPostsToConfiguredHeartbeat(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", request.Method)
		}
		if request.URL.Path != "/secret-liveness-token" {
			t.Errorf("path = %q, want configured heartbeat path", request.URL.Path)
		}
		if priority := request.Header.Get("Priority"); priority != "" {
			t.Errorf("Priority = %q, want no ntfy header", priority)
		}
		requestSeen <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	runtime := newDeliveryRuntimeWithClient(
		deliveryConfig{LivenessURL: server.URL + "/secret-liveness-token"},
		newEventStore(filepath.Join(t.TempDir(), "events.jsonl")),
		server.Client(),
		time.Now,
	)
	if err := runtime.PingLiveness(context.Background()); err != nil {
		t.Fatalf("PingLiveness() error = %v", err)
	}
	select {
	case <-requestSeen:
	default:
		t.Fatal("PingLiveness() did not send the heartbeat")
	}
}

func TestPingSuccessPostsToItsConfiguredHeartbeat(t *testing.T) {
	requestedPath := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestedPath <- request.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runtime := newDeliveryRuntimeWithClient(
		deliveryConfig{
			LivenessURL: server.URL + "/liveness-secret",
			SuccessURL:  server.URL + "/success-secret",
		},
		newEventStore(filepath.Join(t.TempDir(), "events.jsonl")),
		server.Client(),
		time.Now,
	)

	if err := runtime.PingSuccess(context.Background()); err != nil {
		t.Fatalf("PingSuccess() error = %v", err)
	}
	select {
	case path := <-requestedPath:
		if path != "/success-secret" {
			t.Fatalf("heartbeat path = %q, want success heartbeat", path)
		}
	default:
		t.Fatal("PingSuccess() did not send the heartbeat")
	}
}

func TestDeliveryConfigurationErrorsAreStableAndDoNotExposeSecrets(t *testing.T) {
	runtime := newDeliveryRuntimeWithClient(
		deliveryConfig{},
		newEventStore(filepath.Join(t.TempDir(), "events.jsonl")),
		http.DefaultClient,
		time.Now,
	)
	if err := runtime.PingLiveness(context.Background()); !errors.Is(err, errLivenessNotConfigured) {
		t.Fatalf("PingLiveness() error = %v, want missing config error", err)
	}
	if err := runtime.PingSuccess(context.Background()); !errors.Is(err, errSuccessNotConfigured) {
		t.Fatalf("PingSuccess() error = %v, want missing config error", err)
	}

	secretEndpoint := "http://127.0.0.1:1/secret-heartbeat-token"
	runtime.config.SuccessURL = secretEndpoint
	err := runtime.PingSuccess(context.Background())
	if !errors.Is(err, errDeliveryRequestFailed) {
		t.Fatalf("PingSuccess() error = %v, want request failure", err)
	}
	if strings.Contains(err.Error(), secretEndpoint) || strings.Contains(err.Error(), "secret-heartbeat-token") {
		t.Fatalf("heartbeat error exposed its secret endpoint: %v", err)
	}
}

func TestReconcileRenotifiesAnUnresolvedAlertAfterTwentyFourHours(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	clock := now
	alert := validDeliveryAlert(now, alertSeverityP1)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runtime := newDeliveryRuntimeWithClient(
		deliveryConfig{NtfyTopicURL: server.URL},
		newEventStore(filepath.Join(t.TempDir(), "events.jsonl")),
		server.Client(),
		func() time.Time { return clock },
	)
	if err := runtime.Reconcile(context.Background(), []Alert{alert}); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	clock = now.Add(deliveryRepeatAfter)
	if err := runtime.Reconcile(context.Background(), []Alert{alert}); err != nil {
		t.Fatalf("24-hour Reconcile() error = %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("ntfy requests = %d, want re-notification after 24 hours", got)
	}
}

func TestReconcileKeepsOriginalP2DeadlineAcrossRestart(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	clock := now
	store := newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
	bodies := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		bodies <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	config := deliveryConfig{NtfyTopicURL: server.URL}

	firstAlert := validP2DeliveryAlert(now)
	firstRuntime := newDeliveryRuntimeWithClient(config, store, server.Client(), func() time.Time { return clock })
	if err := firstRuntime.Reconcile(context.Background(), []Alert{firstAlert}); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}

	clock = now.Add(deliveryRepeatAfter)
	regeneratedAlert := validP2DeliveryAlert(clock)
	restartedRuntime := newDeliveryRuntimeWithClient(config, newEventStore(store.path), server.Client(), func() time.Time { return clock })
	if err := restartedRuntime.Reconcile(context.Background(), []Alert{regeneratedAlert}); err != nil {
		t.Fatalf("restarted Reconcile() error = %v", err)
	}

	firstBody := <-bodies
	secondBody := <-bodies
	if firstBody != secondBody {
		t.Fatalf("P2 reminder changed its original deadline:\nfirst:  %s\nsecond: %s", firstBody, secondBody)
	}
}

func TestReconcileRecordsP3WithoutSendingNtfy(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	store := newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
	alert := validP3DeliveryAlert(now)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runtime := newDeliveryRuntimeWithClient(
		deliveryConfig{NtfyTopicURL: server.URL},
		store,
		server.Client(),
		func() time.Time { return now },
	)

	if err := runtime.Reconcile(context.Background(), []Alert{alert}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("ntfy requests = %d, want P3 to remain event-only", got)
	}
	events, err := store.Recent(eventQuery{Kind: eventKindAlert, Limit: 10})
	if err != nil {
		t.Fatalf("read alert events: %v", err)
	}
	if !hasAlertState(events, alert.SuppressionKey(), "active") {
		t.Fatal("P3 alert was not written to the event store")
	}
	if hasDeliveryEvent(events, alert.SuppressionKey(), "attempt", "") {
		t.Fatal("P3 alert created a delivery attempt")
	}
}

func TestReconcileDeliversP2AtDefaultPriority(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	alert := validP2DeliveryAlert(now)
	prioritySeen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		prioritySeen <- request.Header.Get("Priority")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runtime := newDeliveryRuntimeWithClient(
		deliveryConfig{NtfyTopicURL: server.URL},
		newEventStore(filepath.Join(t.TempDir(), "events.jsonl")),
		server.Client(),
		func() time.Time { return now },
	)

	if err := runtime.Reconcile(context.Background(), []Alert{alert}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	select {
	case priority := <-prioritySeen:
		if priority != "default" {
			t.Fatalf("Priority = %q, want default", priority)
		}
	default:
		t.Fatal("Reconcile() did not send P2 to ntfy")
	}
}

func TestReconcileDoesNotRetryFailedDeliveryInsideSuppressionWindow(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	clock := now
	store := newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
	alert := validDeliveryAlert(now, alertSeverityP1)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	runtime := newDeliveryRuntimeWithClient(
		deliveryConfig{NtfyTopicURL: server.URL},
		store,
		server.Client(),
		func() time.Time { return clock },
	)

	if err := runtime.Reconcile(context.Background(), []Alert{alert}); !errors.Is(err, errDeliveryResponseFailed) {
		t.Fatalf("failed Reconcile() error = %v, want sanitized delivery failure", err)
	}
	clock = now.Add(time.Hour)
	if err := runtime.Reconcile(context.Background(), []Alert{alert}); err != nil {
		t.Fatalf("suppressed Reconcile() error = %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("ntfy requests = %d, want no in-band retry", got)
	}
	events, err := store.Recent(eventQuery{Kind: eventKindAlert, Limit: 10})
	if err != nil {
		t.Fatalf("read alert events: %v", err)
	}
	if !hasDeliveryEvent(events, alert.SuppressionKey(), "result", "failed") {
		t.Fatal("failed ntfy result was not recorded")
	}
}

func TestReconcileAttemptsEveryDueAlertWhenOneDeliveryFails(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runtime := newDeliveryRuntimeWithClient(
		deliveryConfig{NtfyTopicURL: server.URL},
		newEventStore(filepath.Join(t.TempDir(), "events.jsonl")),
		server.Client(),
		func() time.Time { return now },
	)

	err := runtime.Reconcile(context.Background(), []Alert{
		validDeliveryAlert(now, alertSeverityP1),
		validP2DeliveryAlert(now),
	})
	if !errors.Is(err, errDeliveryResponseFailed) {
		t.Fatalf("Reconcile() error = %v, want first delivery failure", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("ntfy requests = %d, want every due alert attempted", got)
	}
}

func TestReconcileRecordsLaterP3WhenPushConfigurationIsMissing(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	store := newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
	p3 := validP3DeliveryAlert(now)
	runtime := newDeliveryRuntimeWithClient(
		deliveryConfig{},
		store,
		http.DefaultClient,
		func() time.Time { return now },
	)

	err := runtime.Reconcile(context.Background(), []Alert{
		validDeliveryAlert(now, alertSeverityP1),
		p3,
	})
	if !errors.Is(err, errNtfyNotConfigured) {
		t.Fatalf("Reconcile() error = %v, want missing ntfy configuration", err)
	}
	events, readErr := store.Recent(eventQuery{Kind: eventKindAlert, Limit: 10})
	if readErr != nil {
		t.Fatalf("read alert events: %v", readErr)
	}
	if !hasAlertState(events, p3.SuppressionKey(), "active") {
		t.Fatal("P3 after failed push configuration was not recorded")
	}
	if hasDeliveryEvent(events, p3.SuppressionKey(), "attempt", "") {
		t.Fatal("P3 after failed push configuration created a delivery attempt")
	}
	p1Key := validDeliveryAlert(now, alertSeverityP1).SuppressionKey()
	if !hasDeliveryEvent(events, p1Key, "attempt", "") || !hasDeliveryEvent(events, p1Key, "result", "failed") {
		t.Fatal("missing ntfy configuration was not recorded as a failed delivery attempt")
	}
	if err := runtime.Reconcile(context.Background(), []Alert{validDeliveryAlert(now, alertSeverityP1), p3}); err != nil {
		t.Fatalf("suppressed missing-config Reconcile() error = %v", err)
	}
}

func validDeliveryAlert(now time.Time, severity AlertSeverity) Alert {
	alert := Alert{
		Condition:  alertConditionCredentialExpired,
		DetectedAt: now,
		Severity:   severity,
		Subject:    "Persisted credential",
		Verb:       "expired",
		Impact:     "Scheduled collection stopped",
		Recovery:   alertRecoveryNone,
		Action:     "REPLACE the persisted credential",
		RunbookURL: "http://psn.rx1.uk/runbook/credential/",
	}
	return alert
}

func validP3DeliveryAlert(now time.Time) Alert {
	return Alert{
		Condition:  alertConditionFetchFailedOnce,
		DetectedAt: now,
		Severity:   alertSeverityP3,
		Subject:    "Scheduled fetch",
		Verb:       "failed",
		Impact:     "Analytics retained the newest valid snapshot",
		Recovery:   alertRecoverySelf,
		RunbookURL: "http://psn.rx1.uk/runbook/fetch-failure/",
	}
}

func validP2DeliveryAlert(now time.Time) Alert {
	actBy := now.Add(48 * time.Hour)
	return Alert{
		Condition:  alertConditionCandidateQuarantined,
		DetectedAt: now,
		Severity:   alertSeverityP2,
		Subject:    "Fetched candidate",
		Verb:       "entered",
		Object:     "quarantine",
		Impact:     "Analytics retained the newest valid snapshot",
		Recovery:   alertRecoveryNone,
		Action:     "EXAMINE the quarantined candidate",
		ActBy:      &actBy,
		RunbookURL: "http://psn.rx1.uk/runbook/quarantined-candidate/",
	}
}

func hasDeliveryEvent(events []eventRecord, key, stage, result string) bool {
	for _, event := range events {
		if event.Fields["alert_key"] != key || event.Fields["delivery_stage"] != stage {
			continue
		}
		if result == "" || event.Fields["delivery_result"] == result {
			return true
		}
	}
	return false
}

func hasAlertState(events []eventRecord, key, state string) bool {
	for _, event := range events {
		if event.Fields["alert_key"] == key && event.Fields["alert_state"] == state {
			return true
		}
	}
	return false
}

func hasAlertField(events []eventRecord, key, field, value string) bool {
	for _, event := range events {
		if event.Fields["alert_key"] == key && event.Fields[field] == value {
			return true
		}
	}
	return false
}

func hasNumericAlertField(events []eventRecord, key, field string, value int) bool {
	for _, event := range events {
		if event.Fields["alert_key"] == key && event.Fields[field] == float64(value) {
			return true
		}
	}
	return false
}
