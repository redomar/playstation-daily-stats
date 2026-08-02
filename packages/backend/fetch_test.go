package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestFetchAttemptPaginatesUntilShortPageAndCommitsValidSnapshot(t *testing.T) {
	var offsets []int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil {
			t.Errorf("invalid offset: %v", err)
		}
		offsets = append(offsets, offset)
		w.Header().Set("Content-Type", "application/json")
		switch offset {
		case 0:
			_ = json.NewEncoder(w).Encode(map[string]any{"titles": validTitlePage(0, 200)})
		case 200:
			_ = json.NewEncoder(w).Encode(map[string]any{"titles": validTitlePage(200, 1)})
		default:
			t.Errorf("unexpected page offset %d", offset)
			http.Error(w, "unexpected offset", http.StatusBadRequest)
		}
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL)
	result := service.run(context.Background(), true)

	if result.Outcome != fetchSucceeded {
		t.Fatalf("outcome = %q, want %q; reason=%q err=%v", result.Outcome, fetchSucceeded, result.Reason, result.Err)
	}
	if fmt.Sprint(offsets) != "[0 200]" {
		t.Fatalf("requested offsets = %v, want [0 200]", offsets)
	}
	files, err := filepath.Glob(filepath.Join(service.outputDir, "output_*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("canonical snapshots = %d, want 1", len(files))
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		AttemptID string            `json:"attemptId"`
		Titles    []json.RawMessage `json:"titles"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.AttemptID == "" {
		t.Fatal("committed snapshot has no attempt identity")
	}
	if len(snapshot.Titles) != 201 {
		t.Fatalf("committed title count = %d, want 201", len(snapshot.Titles))
	}
}

func TestFetchAttemptQuarantinesPageWithoutTitles(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"reason": "BadRequest"}})
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL)
	result := service.run(context.Background(), true)

	if result.Outcome != fetchFailed || result.Reason != reasonInvalidSchema {
		t.Fatalf("result = %#v, want failed invalid_schema", result)
	}
	canonical, err := filepath.Glob(filepath.Join(service.outputDir, "output_*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 0 {
		t.Fatalf("invalid candidate admitted as canonical snapshot: %v", canonical)
	}
	quarantined, err := filepath.Glob(filepath.Join(service.outputDir, "quarantine", "candidate_*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined candidates = %d, want 1", len(quarantined))
	}
	data, err := os.ReadFile(quarantined[0])
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Reason  failureReason   `json:"reason"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Reason != reasonInvalidSchema || len(record.Payload) == 0 {
		t.Fatalf("quarantine record = %#v, want reason and retained payload", record)
	}
}

func TestFetchAttemptRejectsAndQuarantinesZeroTitles(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"titles": []any{}})
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL)
	result := service.run(context.Background(), true)

	if result.Outcome != fetchFailed || result.Reason != reasonInvalidSchema {
		t.Fatalf("result = %#v, want failed invalid_schema", result)
	}
	canonical, err := filepath.Glob(filepath.Join(service.outputDir, "output_*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 0 {
		t.Fatalf("zero-title candidate admitted as canonical snapshot: %v", canonical)
	}
	quarantined, err := filepath.Glob(filepath.Join(service.outputDir, "quarantine", "candidate_*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined candidates = %d, want 1", len(quarantined))
	}
}

func TestFetchAttemptKeepsFrozenBaselineAcrossLibraryRegression(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"titles": validTitlePage(0, 1)})
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL)
	if err := service.persistDurableState(durableFetchState{BaselineCount: 2}); err != nil {
		t.Fatal(err)
	}

	first := service.run(context.Background(), false)
	if first.Outcome != fetchFailed || first.Reason != reasonLibraryRegression {
		t.Fatalf("first result = %#v, want library_regression", first)
	}
	firstState, err := service.loadDurableState()
	if err != nil {
		t.Fatal(err)
	}
	if firstState.BaselineCount != 2 {
		t.Fatalf("baseline after first regression = %d, want 2", firstState.BaselineCount)
	}
	if firstState.LibraryRegression == nil || firstState.LibraryRegression.CandidateCount != 1 || firstState.LibraryRegression.ConsecutiveCount != 1 {
		t.Fatalf("first regression state = %#v", firstState.LibraryRegression)
	}

	second := service.run(context.Background(), false)
	if second.Outcome != fetchFailed || second.Reason != reasonLibraryRegressionPersistent {
		t.Fatalf("second result = %#v, want library_regression_persistent", second)
	}
	secondState, err := service.loadDurableState()
	if err != nil {
		t.Fatal(err)
	}
	if secondState.BaselineCount != 2 || secondState.LibraryRegression == nil || secondState.LibraryRegression.ConsecutiveCount != 2 {
		t.Fatalf("second regression state = %#v", secondState)
	}
	canonical, err := filepath.Glob(filepath.Join(service.outputDir, "output_*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 0 {
		t.Fatalf("regressed candidates admitted as canonical snapshots: %v", canonical)
	}
}

func TestSkippedFetchPreservesActiveFailureAcrossRestart(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("skip contacted upstream")
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL)
	failedAt := service.now().Add(-2 * time.Hour)
	if err := service.persistDurableState(durableFetchState{
		LatestOutcome:      fetchFailed,
		LastAttemptAt:      &failedAt,
		ConsecutiveFailure: 3,
		Reason:             reasonUpstreamStatus,
		Summary:            "upstream returned HTTP 503",
		BaselineCount:      1,
	}); err != nil {
		t.Fatal(err)
	}
	recentSnapshot := canonicalSnapshot{AttemptID: "earlier-success", Titles: rawTitles(t, validTitlePage(0, 1))}
	data, err := json.Marshal(recentSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	recentName := fmt.Sprintf("output_%d.json", service.now().Add(-time.Hour).Unix())
	if err := os.WriteFile(filepath.Join(service.outputDir, recentName), data, 0o600); err != nil {
		t.Fatal(err)
	}

	restarted := *service
	restarted.state = &appState{}
	restarted.attempts = &attemptLock{}
	result := restarted.run(context.Background(), false)

	if result.Outcome != fetchSkipped {
		t.Fatalf("outcome = %q, want skipped; result=%#v", result.Outcome, result)
	}
	reloaded, err := restarted.loadDurableState()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LatestOutcome != fetchSkipped || reloaded.ConsecutiveFailure != 3 || reloaded.Reason != reasonUpstreamStatus {
		t.Fatalf("durable state after skip = %#v", reloaded)
	}
	status := restarted.state.getStatus()
	if status["consecutive_fails"] != 3 || status["active_fetch_failure"] == nil {
		t.Fatalf("active failure missing after restart and skip: %#v", status)
	}
}

func TestRestoreTurnsUnfinishedAttemptIntoInterruptedFetchFailure(t *testing.T) {
	service := newTestFetchService(t, "http://unused.invalid")
	startedAt := service.now().Add(-10 * time.Minute)
	if err := service.persistDurableState(durableFetchState{
		LatestOutcome: fetchSucceeded,
		BaselineCount: 4,
		ActiveAttempt: &attemptMarker{ID: "unfinished-attempt", StartedAt: startedAt},
	}); err != nil {
		t.Fatal(err)
	}

	restarted := *service
	restarted.state = &appState{}
	if err := restarted.restore(); err != nil {
		t.Fatal(err)
	}

	restored, err := restarted.loadDurableState()
	if err != nil {
		t.Fatal(err)
	}
	if restored.ActiveAttempt != nil || restored.LatestOutcome != fetchFailed || restored.Reason != reasonInterruptedFetch || restored.ConsecutiveFailure != 1 {
		t.Fatalf("restored state = %#v, want interrupted fetch failure", restored)
	}
}

func TestRestoreReconcilesSnapshotCommittedBeforeTerminalState(t *testing.T) {
	service := newTestFetchService(t, "http://unused.invalid")
	startedAt := service.now().Add(-10 * time.Minute)
	if err := service.persistDurableState(durableFetchState{
		LatestOutcome:      fetchFailed,
		ConsecutiveFailure: 2,
		Reason:             reasonUpstreamStatus,
		BaselineCount:      3,
		ActiveAttempt:      &attemptMarker{ID: "committed-attempt", StartedAt: startedAt},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := canonicalSnapshot{AttemptID: "committed-attempt", Titles: rawTitles(t, validTitlePage(0, 4))}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("output_%d.json", service.now().Add(-5*time.Minute).Unix())
	if err := os.WriteFile(filepath.Join(service.outputDir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}

	restarted := *service
	restarted.state = &appState{}
	if err := restarted.restore(); err != nil {
		t.Fatal(err)
	}

	restored, err := restarted.loadDurableState()
	if err != nil {
		t.Fatal(err)
	}
	if restored.ActiveAttempt != nil || restored.LatestOutcome != fetchSucceeded || restored.ConsecutiveFailure != 0 || restored.BaselineCount != 4 {
		t.Fatalf("restored state = %#v, want reconciled success", restored)
	}
}

func TestRestoreIgnoresAuditExcludedSnapshot(t *testing.T) {
	service := newTestFetchService(t, "http://unused.invalid")
	service.corpusAuditFile = filepath.Join(service.outputDir, "corpus-audit.json")
	acceptedAt := service.now().Add(-24 * time.Hour)
	excludedAt := service.now().Add(-time.Hour)
	acceptedName := fmt.Sprintf("output_%d.json", acceptedAt.Unix())
	excludedName := fmt.Sprintf("output_%d.json", excludedAt.Unix())
	acceptedData, err := json.Marshal(canonicalSnapshot{Titles: rawTitles(t, validTitlePage(0, 2))})
	if err != nil {
		t.Fatal(err)
	}
	excludedData, err := json.Marshal(canonicalSnapshot{Titles: rawTitles(t, validTitlePage(0, 1))})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service.outputDir, acceptedName), acceptedData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service.outputDir, excludedName), excludedData, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := corpusAuditManifest{
		Version:       1,
		ExcludedCount: 1,
		Entries: []corpusAuditEntry{{
			Filename:       excludedName,
			Classification: auditExcluded,
			Reason:         reasonLibraryRegression,
		}},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.corpusAuditFile, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := service.restore(); err != nil {
		t.Fatal(err)
	}

	restored, err := service.loadDurableState()
	if err != nil {
		t.Fatal(err)
	}
	if restored.BaselineCount != 2 || restored.LastSuccessAt == nil || !restored.LastSuccessAt.Equal(acceptedAt) {
		t.Fatalf("restored state = %#v, want baseline from accepted snapshot", restored)
	}
	recent, err := service.hasRecentValidSnapshot(12 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if recent {
		t.Fatal("audit-excluded recent candidate justified a fetch skip")
	}
}

func TestFetchAttemptDeadlineIsAnUpstreamTimeoutFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"titles": validTitlePage(0, 1)})
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL)
	service.httpClient.Timeout = 25 * time.Millisecond
	service.attemptTimeout = 50 * time.Millisecond
	result := service.run(context.Background(), true)

	if result.Outcome != fetchFailed || result.Reason != reasonUpstreamTimeout {
		t.Fatalf("result = %#v, want failed upstream_timeout", result)
	}
}

func TestFetchAttemptRecordsAuthenticationRejection(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/authorize" {
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}
		http.Error(w, "rejected", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL+"/titles")
	service.authorizationURL = upstream.URL + "/authorize"
	service.tokenURL = upstream.URL + "/token"
	service.authHTTPClient = &http.Client{
		Timeout: time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if err := os.Remove(service.tokenFile); err != nil {
		t.Fatal(err)
	}

	result := service.run(context.Background(), true)

	if result.Outcome != fetchFailed || result.Reason != reasonAuthenticationRejected {
		t.Fatalf("result = %#v, want failed authentication_rejected", result)
	}
}

func TestFetchAttemptTreatsAuthorizationOutageAsUpstreamStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL+"/titles")
	service.authorizationURL = upstream.URL + "/authorize"
	service.tokenURL = upstream.URL + "/token"
	service.authHTTPClient = &http.Client{
		Timeout: time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if err := os.Remove(service.tokenFile); err != nil {
		t.Fatal(err)
	}

	result := service.run(context.Background(), true)

	if result.Outcome != fetchFailed || result.Reason != reasonUpstreamStatus {
		t.Fatalf("result = %#v, want failed upstream_status", result)
	}
}

func TestFetchAttemptAuthenticatesAndPersistsTokenAtomically(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			w.Header().Set("Location", "https://example.invalid/callback?code=v3.test-code")
			w.WriteHeader(http.StatusFound)
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh-access-token", "expires_in": 3600})
		case "/titles":
			if got := r.Header.Get("Authorization"); got != "Bearer fresh-access-token" {
				t.Errorf("authorization header = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"titles": validTitlePage(0, 1)})
		default:
			t.Errorf("unexpected upstream path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL+"/titles")
	service.authorizationURL = upstream.URL + "/authorize"
	service.tokenURL = upstream.URL + "/token"
	service.authHTTPClient = &http.Client{
		Timeout: time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if err := os.Remove(service.tokenFile); err != nil {
		t.Fatal(err)
	}

	result := service.run(context.Background(), true)

	if result.Outcome != fetchSucceeded {
		t.Fatalf("result = %#v, want succeeded", result)
	}
	data, err := os.ReadFile(service.tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	var token TokenInfo
	if err := json.Unmarshal(data, &token); err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "fresh-access-token" || token.CredentialID != credentialIdentifier("test-npsso") {
		t.Fatalf("persisted token = %#v", token)
	}
	info, err := os.Stat(service.tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestFetchAttemptRejectsNon2xxGameListResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL)
	result := service.run(context.Background(), true)

	if result.Outcome != fetchFailed || result.Reason != reasonUpstreamStatus {
		t.Fatalf("result = %#v, want failed upstream_status", result)
	}
	canonical, err := filepath.Glob(filepath.Join(service.outputDir, "output_*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 0 {
		t.Fatalf("non-2xx response admitted as canonical: %v", canonical)
	}
}

func TestFetchAttemptRejectsRepeatedPagination(t *testing.T) {
	page := validTitlePage(0, 200)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"titles": page})
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL)
	result := service.run(context.Background(), true)

	if result.Outcome != fetchFailed || result.Reason != reasonIncompletePagination {
		t.Fatalf("result = %#v, want failed incomplete_pagination", result)
	}
}

func TestSucceededFetchDoesNotOverwriteSnapshotFromSameSecond(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"titles": validTitlePage(0, 1)})
	}))
	defer upstream.Close()

	service := newTestFetchService(t, upstream.URL)
	first := service.run(context.Background(), true)
	second := service.run(context.Background(), true)
	if first.Outcome != fetchSucceeded || second.Outcome != fetchSucceeded {
		t.Fatalf("results = %#v and %#v, want two successes", first, second)
	}
	canonical, err := filepath.Glob(filepath.Join(service.outputDir, "output_*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 2 {
		t.Fatalf("canonical snapshots = %d, want 2", len(canonical))
	}
}

func newTestFetchService(t *testing.T, upstreamURL string) *fetchService {
	t.Helper()
	dir := t.TempDir()
	service := &fetchService{
		outputDir:      dir,
		fetchStateFile: filepath.Join(dir, "fetch-state.json"),
		tokenFile:      filepath.Join(dir, "token.json"),
		gameListURL:    upstreamURL,
		httpClient:     &http.Client{Timeout: time.Second},
		attemptTimeout: 2 * time.Second,
		pageSize:       200,
		now:            func() time.Time { return time.Unix(1_800_000_000, 0).UTC() },
		state:          &appState{},
		attempts:       &attemptLock{},
	}
	service.state.setCredential(credentialResolution{Value: "test-npsso", State: credentialReady})
	token := TokenInfo{AccessToken: "test-access-token", ExpiresAt: service.now().Add(time.Hour), CredentialID: credentialIdentifier("test-npsso")}
	data, err := json.Marshal(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.tokenFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return service
}

func validTitlePage(start, count int) []map[string]any {
	titles := make([]map[string]any, 0, count)
	for i := start; i < start+count; i++ {
		titles = append(titles, map[string]any{
			"titleId":      fmt.Sprintf("TITLE-%03d", i),
			"name":         fmt.Sprintf("Game %03d", i),
			"playCount":    i,
			"playDuration": "PT1H2M3S",
			"category":     "ps5_native_game",
		})
	}
	return titles
}

func rawTitles(t *testing.T, titles []map[string]any) []json.RawMessage {
	t.Helper()
	raw := make([]json.RawMessage, 0, len(titles))
	for _, title := range titles {
		data, err := json.Marshal(title)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, data)
	}
	return raw
}
