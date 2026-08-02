package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleEventsFiltersTheDurableTail(t *testing.T) {
	previous := activeEventStore
	t.Cleanup(func() { activeEventStore = previous })
	activeEventStore = newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
	base := time.Date(2026, time.August, 2, 10, 0, 0, 0, time.UTC)
	for _, event := range []eventRecord{
		{Timestamp: base, Kind: eventKindFetch, Outcome: fetchSucceeded, Message: "First."},
		{Timestamp: base.Add(time.Minute), Kind: eventKindBoot, Message: "Restarted."},
		{Timestamp: base.Add(2 * time.Minute), Kind: eventKindFetch, Outcome: fetchFailed, Reason: reasonUpstreamStatus, Message: "Second."},
	} {
		if err := activeEventStore.Append(event); err != nil {
			t.Fatal(err)
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/events?kind=fetch&since=2026-08-02T09:00:00Z&limit=1", nil)
	handleEvents(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var events []eventRecord
	if err := json.NewDecoder(recorder.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Outcome != fetchFailed {
		t.Fatalf("events = %#v, want newest failed fetch", events)
	}
}

func TestHandleEventsRejectsInvalidQuery(t *testing.T) {
	for _, target := range []string{
		"/api/events?kind=unknown",
		"/api/events?since=yesterday",
		"/api/events?limit=nope",
		"/api/events?limit=0",
	} {
		recorder := httptest.NewRecorder()
		handleEvents(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, recorder.Code)
		}
	}
}

func TestLogFetchResultPersistsOnlyStableFailureDetails(t *testing.T) {
	previous := activeEventStore
	t.Cleanup(func() { activeEventStore = previous })
	activeEventStore = newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))

	logFetchResult(fetchResult{
		Outcome: fetchFailed,
		Reason:  reasonAuthenticationRejected,
		Err:     errors.New("secret upstream response must not be persisted"),
	})

	events, err := activeEventStore.Recent(eventQuery{Kind: eventKindFetch, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Reason != reasonAuthenticationRejected {
		t.Fatalf("events = %#v, want stable authentication reason", events)
	}
	encoded, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret upstream response") {
		t.Fatalf("event leaked raw error: %s", encoded)
	}
}
