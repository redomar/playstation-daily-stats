package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTriggerFetchReportsAlreadyRunningWhenFetchIsActive(t *testing.T) {
	withManualFetchStarter(t, func() bool { return false })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/trigger-fetch", nil)
	handleTriggerFetch(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	response := decodeTriggerFetchResponse(t, recorder)
	if response.Status != "already_running" {
		t.Fatalf("response status = %q, want %q", response.Status, "already_running")
	}
}

func TestTriggerFetchReportsAcceptedWhenFetchStarts(t *testing.T) {
	withManualFetchStarter(t, func() bool { return true })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/trigger-fetch", nil)
	handleTriggerFetch(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	response := decodeTriggerFetchResponse(t, recorder)
	if response.Status != "accepted" {
		t.Fatalf("response status = %q, want %q", response.Status, "accepted")
	}
}

func withManualFetchStarter(t *testing.T, start func() bool) {
	t.Helper()
	original := startManualFetch
	startManualFetch = start
	t.Cleanup(func() {
		startManualFetch = original
	})
}

func decodeTriggerFetchResponse(t *testing.T, recorder *httptest.ResponseRecorder) struct {
	Status  string `json:"status"`
	Message string `json:"message"`
} {
	t.Helper()
	var response struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, recorder.Body.String())
	}
	return response
}
