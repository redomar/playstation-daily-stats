package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fetchOutcome string

const (
	fetchSucceeded fetchOutcome = "succeeded"
	fetchFailed    fetchOutcome = "failed"
	fetchSkipped   fetchOutcome = "skipped"
)

type failureReason string

const (
	reasonAuthenticationRejected      failureReason = "authentication_rejected"
	reasonUpstreamTimeout             failureReason = "upstream_timeout"
	reasonUpstreamStatus              failureReason = "upstream_status"
	reasonInvalidSchema               failureReason = "invalid_schema"
	reasonIncompletePagination        failureReason = "incomplete_pagination"
	reasonImplausibleSnapshot         failureReason = "implausible_snapshot"
	reasonLibraryRegression           failureReason = "library_regression"
	reasonLibraryRegressionPersistent failureReason = "library_regression_persistent"
	reasonInterruptedFetch            failureReason = "interrupted_fetch"
	reasonStorageFailure              failureReason = "storage_failure"
)

type fetchResult struct {
	Outcome        fetchOutcome
	Reason         failureReason
	Err            error
	Snapshot       string
	AttemptID      string
	TitleCount     int
	AlreadyRunning bool
}

type attemptLock struct {
	mu      sync.Mutex
	running bool
}

func (l *attemptLock) tryStart() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running {
		return false
	}
	l.running = true
	return true
}

func (l *attemptLock) done() {
	l.mu.Lock()
	l.running = false
	l.mu.Unlock()
}

type fetchService struct {
	outputDir        string
	fetchStateFile   string
	corpusAuditFile  string
	tokenFile        string
	gameListURL      string
	authorizationURL string
	tokenURL         string
	httpClient       *http.Client
	authHTTPClient   *http.Client
	attemptTimeout   time.Duration
	pageSize         int
	now              func() time.Time
	state            *appState
	attempts         *attemptLock
}

type attemptMarker struct {
	ID        string    `json:"id"`
	StartedAt time.Time `json:"startedAt"`
}

type durableFetchState struct {
	LatestOutcome      fetchOutcome            `json:"latestOutcome,omitempty"`
	LastAttemptAt      *time.Time              `json:"lastAttemptAt,omitempty"`
	LastSuccessAt      *time.Time              `json:"lastSuccessAt,omitempty"`
	ConsecutiveFailure int                     `json:"consecutiveFailures"`
	Reason             failureReason           `json:"reason,omitempty"`
	Summary            string                  `json:"summary,omitempty"`
	BaselineCount      int                     `json:"baselineCount"`
	ActiveAttempt      *attemptMarker          `json:"activeAttempt,omitempty"`
	LibraryRegression  *libraryRegressionState `json:"libraryRegression,omitempty"`
}

type libraryRegressionState struct {
	BaselineCount    int       `json:"baselineCount"`
	CandidateCount   int       `json:"candidateCount"`
	FirstSeenAt      time.Time `json:"firstSeenAt"`
	ConsecutiveCount int       `json:"consecutiveCount"`
	ScheduledCount   int       `json:"scheduledCount"`
	LastSeenAt       time.Time `json:"lastSeenAt"`
}

type canonicalSnapshot struct {
	AttemptID string            `json:"attemptId"`
	Titles    []json.RawMessage `json:"titles"`
}

var psnDurationPattern = regexp.MustCompile(`^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)

func (s *fetchService) run(parent context.Context, force bool) fetchResult {
	if !s.attempts.tryStart() {
		return fetchResult{AlreadyRunning: true}
	}
	defer s.attempts.done()
	return s.runClaimed(parent, force)
}

func (s *fetchService) runClaimed(parent context.Context, force bool) fetchResult {
	ctx, cancel := context.WithTimeout(parent, s.attemptTimeout)
	defer cancel()
	return s.runAttempt(ctx, force)
}

func (s *fetchService) start(force bool, completed func(fetchResult)) bool {
	if !s.attempts.tryStart() {
		return false
	}
	go func() {
		defer s.attempts.done()
		completed(s.runClaimed(context.Background(), force))
	}()
	return true
}

func (s *fetchService) runAttempt(ctx context.Context, force bool) fetchResult {
	now := s.now()
	if !force {
		recent, err := s.hasRecentValidSnapshot(12 * time.Hour)
		if err != nil {
			return s.storageFailure(err)
		}
		if recent {
			durable, err := s.loadDurableState()
			if err != nil {
				return s.storageFailure(err)
			}
			durable.LatestOutcome = fetchSkipped
			if err := s.persistDurableState(durable); err != nil {
				return s.storageFailure(err)
			}
			s.state.applyDurableFetchState(durable)
			return fetchResult{Outcome: fetchSkipped}
		}
	}

	attemptID, err := newAttemptID()
	if err != nil {
		return s.storageFailure(err)
	}

	durable, err := s.loadDurableState()
	if err != nil {
		return s.storageFailure(err)
	}
	durable.ActiveAttempt = &attemptMarker{ID: attemptID, StartedAt: now}
	if err := s.persistDurableState(durable); err != nil {
		return s.storageFailure(err)
	}

	npsso := s.state.getNPSSO()
	if npsso == "" {
		return s.failAttempt(durable, attemptID, reasonAuthenticationRejected, "no usable persisted credential is configured", nil)
	}
	token, err := s.getValidToken(ctx, npsso)
	if err != nil {
		reason, summary := classifyExternalFailure(err, reasonAuthenticationRejected, "authentication failed")
		return s.failAttempt(durable, attemptID, reason, summary, err)
	}

	titles, err := s.fetchAllPages(ctx, token)
	if err != nil {
		var failure *fetchFailure
		if errors.As(err, &failure) {
			if len(failure.Payload) > 0 {
				if quarantineErr := s.quarantine(attemptID, failure.Reason, failure.Payload); quarantineErr != nil {
					return s.failAttempt(durable, attemptID, reasonStorageFailure, "quarantine invalid candidate", quarantineErr)
				}
			}
			return s.failAttempt(durable, attemptID, failure.Reason, failure.Summary, err)
		}
		var networkError net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
			return s.failAttempt(durable, attemptID, reasonUpstreamTimeout, "upstream request exceeded its deadline", err)
		}
		return s.failAttempt(durable, attemptID, reasonInvalidSchema, "fetch attempt failed", err)
	}
	snapshotData, err := json.Marshal(canonicalSnapshot{AttemptID: attemptID, Titles: titles})
	if err != nil {
		return s.failAttempt(durable, attemptID, reasonStorageFailure, "encode canonical snapshot", err)
	}
	if err := validateTitles(titles); err != nil {
		if quarantineErr := s.quarantine(attemptID, reasonInvalidSchema, snapshotData); quarantineErr != nil {
			return s.failAttempt(durable, attemptID, reasonStorageFailure, "quarantine invalid candidate", quarantineErr)
		}
		return s.failAttempt(durable, attemptID, reasonInvalidSchema, "candidate failed structural validation", err)
	}
	if durable.BaselineCount > 0 && len(titles) < durable.BaselineCount {
		regression := durable.LibraryRegression
		if regression == nil || regression.BaselineCount != durable.BaselineCount {
			regression = &libraryRegressionState{
				BaselineCount:  durable.BaselineCount,
				CandidateCount: len(titles),
				FirstSeenAt:    now,
			}
		}
		regression.CandidateCount = len(titles)
		regression.ConsecutiveCount++
		if !force {
			regression.ScheduledCount++
		}
		regression.LastSeenAt = now
		durable.LibraryRegression = regression

		reason := reasonLibraryRegression
		if regression.ScheduledCount >= 2 || now.Sub(regression.FirstSeenAt) >= 30*time.Hour {
			reason = reasonLibraryRegressionPersistent
		}
		if quarantineErr := s.quarantine(attemptID, reason, snapshotData); quarantineErr != nil {
			return s.failAttempt(durable, attemptID, reasonStorageFailure, "quarantine implausible candidate", quarantineErr)
		}
		return s.failAttempt(durable, attemptID, reason,
			fmt.Sprintf("candidate title count %d is below baseline %d", len(titles), durable.BaselineCount), nil)
	}
	path, err := s.nextSnapshotPath(now)
	if err != nil {
		return s.failAttempt(durable, attemptID, reasonStorageFailure, "select canonical snapshot path", err)
	}
	if err := writeFileAtomically(path, snapshotData, 0o600); err != nil {
		return s.failAttempt(durable, attemptID, reasonStorageFailure, "commit canonical snapshot", err)
	}

	durable.ActiveAttempt = nil
	durable.LatestOutcome = fetchSucceeded
	durable.LastAttemptAt = timePointer(now)
	durable.LastSuccessAt = timePointer(now)
	durable.ConsecutiveFailure = 0
	durable.Reason = ""
	durable.Summary = ""
	durable.BaselineCount = len(titles)
	durable.LibraryRegression = nil
	if err := s.persistDurableState(durable); err != nil {
		result := s.storageFailure(fmt.Errorf("persist succeeded fetch state: %w", err))
		result.AttemptID = attemptID
		return result
	}
	s.state.applyDurableFetchState(durable)
	return fetchResult{Outcome: fetchSucceeded, Snapshot: path, AttemptID: attemptID, TitleCount: len(titles)}
}

func (s *fetchService) nextSnapshotPath(capturedAt time.Time) (string, error) {
	for timestamp := capturedAt.Unix(); ; timestamp++ {
		path := filepath.Join(s.outputDir, fmt.Sprintf("output_%d.json", timestamp))
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
	}
}

func (s *fetchService) hasRecentValidSnapshot(maxAge time.Duration) (bool, error) {
	_, capturedAt, found, err := s.newestValidSnapshot("")
	return found && s.now().Sub(capturedAt) < maxAge, err
}

func (s *fetchService) restore() error {
	durable, err := s.loadDurableState()
	if err != nil {
		return err
	}
	if durable.ActiveAttempt != nil {
		marker := durable.ActiveAttempt
		titleCount, capturedAt, committed, err := s.newestValidSnapshot(marker.ID)
		if err != nil {
			return err
		}
		durable.ActiveAttempt = nil
		durable.LastAttemptAt = timePointer(marker.StartedAt)
		if committed {
			durable.LatestOutcome = fetchSucceeded
			durable.LastSuccessAt = timePointer(capturedAt)
			durable.ConsecutiveFailure = 0
			durable.Reason = ""
			durable.Summary = ""
			durable.BaselineCount = titleCount
			durable.LibraryRegression = nil
		} else {
			durable.LatestOutcome = fetchFailed
			durable.ConsecutiveFailure++
			durable.Reason = reasonInterruptedFetch
			durable.Summary = "fetch attempt was interrupted before durable snapshot commit"
		}
		if err := s.persistDurableState(durable); err != nil {
			return err
		}
	} else if durable.BaselineCount == 0 {
		titleCount, capturedAt, found, err := s.newestValidSnapshot("")
		if err != nil {
			return err
		}
		if found {
			durable.LatestOutcome = fetchSucceeded
			durable.LastSuccessAt = timePointer(capturedAt)
			durable.BaselineCount = titleCount
			if err := s.persistDurableState(durable); err != nil {
				return err
			}
		}
	}
	s.state.applyDurableFetchState(durable)
	return nil
}

func (s *fetchService) newestValidSnapshot(attemptID string) (int, time.Time, bool, error) {
	excluded := map[string]failureReason{}
	if s.corpusAuditFile != "" {
		loaded, err := auditExclusions(s.corpusAuditFile)
		if err != nil && !os.IsNotExist(err) {
			return 0, time.Time{}, false, err
		}
		if loaded != nil {
			excluded = loaded
		}
	}
	entries, err := os.ReadDir(s.outputDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, time.Time{}, false, nil
		}
		return 0, time.Time{}, false, err
	}
	type snapshotFile struct {
		name      string
		timestamp int64
	}
	files := make([]snapshotFile, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "output_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		if _, isExcluded := excluded[name]; isExcluded {
			continue
		}
		timestampText := strings.TrimSuffix(strings.TrimPrefix(name, "output_"), ".json")
		timestamp, err := strconv.ParseInt(timestampText, 10, 64)
		if err != nil {
			continue
		}
		files = append(files, snapshotFile{name: name, timestamp: timestamp})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].timestamp > files[j].timestamp })
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(s.outputDir, file.name))
		if err != nil {
			continue
		}
		var snapshot canonicalSnapshot
		if err := json.Unmarshal(data, &snapshot); err != nil || validateTitles(snapshot.Titles) != nil {
			continue
		}
		if attemptID != "" && snapshot.AttemptID != attemptID {
			continue
		}
		return len(snapshot.Titles), time.Unix(file.timestamp, 0), true, nil
	}
	return 0, time.Time{}, false, nil
}

type fetchFailure struct {
	Reason  failureReason
	Summary string
	Err     error
	Payload []byte
}

func (e *fetchFailure) Error() string { return e.Summary }
func (e *fetchFailure) Unwrap() error { return e.Err }

func (s *fetchService) fetchAllPages(ctx context.Context, token string) ([]json.RawMessage, error) {
	var all []json.RawMessage
	seenPages := make(map[string]struct{})
	seenTitleIDs := make(map[string]struct{})
	var expectedTotal *int
	for offset := 0; ; offset += s.pageSize {
		requestURL, err := url.Parse(s.gameListURL)
		if err != nil {
			return nil, fmt.Errorf("parse game list URL: %w", err)
		}
		query := requestURL.Query()
		query.Set("limit", fmt.Sprint(s.pageSize))
		query.Set("offset", fmt.Sprint(offset))
		requestURL.RawQuery = query.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, &fetchFailure{Reason: reasonUpstreamStatus, Summary: fmt.Sprintf("upstream returned HTTP %d", resp.StatusCode)}
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, &fetchFailure{Reason: reasonInvalidSchema, Summary: "upstream page is not valid JSON", Err: err, Payload: body}
		}
		rawTitles, ok := envelope["titles"]
		if !ok {
			return nil, &fetchFailure{Reason: reasonInvalidSchema, Summary: "upstream page has no titles array", Payload: body}
		}
		var page []json.RawMessage
		if err := json.Unmarshal(rawTitles, &page); err != nil {
			return nil, &fetchFailure{Reason: reasonInvalidSchema, Summary: "upstream titles field is not an array", Err: err, Payload: body}
		}
		fingerprint := string(rawTitles)
		if _, repeated := seenPages[fingerprint]; repeated && len(page) > 0 {
			return nil, &fetchFailure{Reason: reasonIncompletePagination, Summary: "upstream pagination repeated a page"}
		}
		seenPages[fingerprint] = struct{}{}
		pageTitleIDs := make(map[string]struct{}, len(page))
		for _, rawTitle := range page {
			var identity struct {
				TitleID string `json:"titleId"`
			}
			if json.Unmarshal(rawTitle, &identity) != nil || identity.TitleID == "" {
				continue
			}
			if _, duplicateOnPage := pageTitleIDs[identity.TitleID]; duplicateOnPage {
				continue
			}
			if _, repeatedFromEarlierPage := seenTitleIDs[identity.TitleID]; repeatedFromEarlierPage {
				return nil, &fetchFailure{Reason: reasonIncompletePagination, Summary: "upstream pagination repeated titles from an earlier page"}
			}
			pageTitleIDs[identity.TitleID] = struct{}{}
		}
		for titleID := range pageTitleIDs {
			seenTitleIDs[titleID] = struct{}{}
		}

		if rawTotal, exists := envelope["totalItemCount"]; exists {
			var total int
			if err := json.Unmarshal(rawTotal, &total); err != nil || total < 0 {
				return nil, &fetchFailure{Reason: reasonIncompletePagination, Summary: "upstream pagination returned invalid completion metadata", Err: err}
			}
			if expectedTotal != nil && total != *expectedTotal {
				return nil, &fetchFailure{Reason: reasonIncompletePagination, Summary: "upstream pagination changed its total item count"}
			}
			expectedTotal = &total
			if offset+len(page) > total {
				return nil, &fetchFailure{Reason: reasonIncompletePagination, Summary: "upstream page exceeded its reported total item count"}
			}
		}
		all = append(all, page...)
		if expectedTotal != nil && len(all) == *expectedTotal {
			return all, nil
		}
		if len(page) < s.pageSize {
			if expectedTotal != nil && len(all) != *expectedTotal {
				return nil, &fetchFailure{Reason: reasonIncompletePagination, Summary: "upstream ended before its reported total item count"}
			}
			return all, nil
		}
	}
}

func validateTitles(titles []json.RawMessage) error {
	if len(titles) == 0 {
		return errors.New("candidate contains zero titles")
	}
	seen := make(map[string]struct{}, len(titles))
	for index, raw := range titles {
		var title struct {
			TitleID      *string `json:"titleId"`
			Name         *string `json:"name"`
			PlayCount    *int    `json:"playCount"`
			PlayDuration *string `json:"playDuration"`
			Category     *string `json:"category"`
		}
		if err := json.Unmarshal(raw, &title); err != nil {
			return fmt.Errorf("title %d is not an object: %w", index, err)
		}
		if title.TitleID == nil || strings.TrimSpace(*title.TitleID) == "" {
			return fmt.Errorf("title %d has no titleId", index)
		}
		if _, duplicate := seen[*title.TitleID]; duplicate {
			return fmt.Errorf("duplicate titleId %q", *title.TitleID)
		}
		seen[*title.TitleID] = struct{}{}
		if title.Name == nil || strings.TrimSpace(*title.Name) == "" {
			return fmt.Errorf("title %q has no name", *title.TitleID)
		}
		if title.Category == nil || strings.TrimSpace(*title.Category) == "" {
			return fmt.Errorf("title %q has no category", *title.TitleID)
		}
		if title.PlayCount == nil || *title.PlayCount < 0 {
			return fmt.Errorf("title %q has invalid playCount", *title.TitleID)
		}
		if title.PlayDuration == nil || !validPSNDuration(*title.PlayDuration) {
			return fmt.Errorf("title %q has invalid playDuration", *title.TitleID)
		}
	}
	return nil
}

func validPSNDuration(value string) bool {
	match := psnDurationPattern.FindStringSubmatch(value)
	return match != nil && (match[1] != "" || match[2] != "" || match[3] != "")
}

func (s *fetchService) getValidToken(ctx context.Context, npsso string) (string, error) {
	credentialID := credentialIdentifier(npsso)
	if token, ok := s.loadCachedToken(credentialID); ok {
		return token, nil
	}
	code, err := s.getAuthorizationCode(ctx, npsso)
	if err != nil {
		return "", err
	}
	return s.exchangeCodeForToken(ctx, code, credentialID)
}

func (s *fetchService) loadCachedToken(credentialID string) (string, bool) {
	data, err := os.ReadFile(s.tokenFile)
	if err != nil {
		return "", false
	}
	var token TokenInfo
	if err := json.Unmarshal(data, &token); err != nil {
		return "", false
	}
	if token.AccessToken == "" || token.CredentialID != credentialID || !s.now().Before(token.ExpiresAt) {
		return "", false
	}
	return token.AccessToken, true
}

func (s *fetchService) getAuthorizationCode(ctx context.Context, npsso string) (string, error) {
	requestURL, err := url.Parse(s.authorizationURL)
	if err != nil {
		return "", err
	}
	query := requestURL.Query()
	query.Set("access_type", "offline")
	query.Set("client_id", clientID)
	query.Set("response_type", "code")
	query.Set("scope", "psn:mobile.v2.core psn:clientapp")
	query.Set("redirect_uri", redirectURI)
	requestURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", "npsso="+npsso)
	client := s.authHTTPClient
	if client == nil {
		client = s.httpClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		reason := reasonUpstreamStatus
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			reason = reasonAuthenticationRejected
		}
		return "", &fetchFailure{Reason: reason, Summary: fmt.Sprintf("authorization endpoint returned HTTP %d", resp.StatusCode)}
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		return "", &fetchFailure{Reason: reasonAuthenticationRejected, Summary: "authorization redirect was invalid", Err: err}
	}
	code := location.Query().Get("code")
	if !strings.HasPrefix(code, "v3.") {
		return "", &fetchFailure{Reason: reasonAuthenticationRejected, Summary: "authorization endpoint rejected the persisted credential"}
	}
	return code, nil
}

func (s *fetchService) exchangeCodeForToken(ctx context.Context, code, credentialID string) (string, error) {
	form := url.Values{
		"code":         {code},
		"redirect_uri": {redirectURI},
		"grant_type":   {"authorization_code"},
		"token_format": {"jwt"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(clientID+":"+clientSecret)))
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		reason := reasonUpstreamStatus
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			reason = reasonAuthenticationRejected
		}
		return "", &fetchFailure{Reason: reason, Summary: fmt.Sprintf("token endpoint returned HTTP %d", resp.StatusCode)}
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.AccessToken == "" {
		return "", &fetchFailure{Reason: reasonAuthenticationRejected, Summary: "token endpoint returned an invalid token response", Err: err}
	}
	if result.ExpiresIn <= 0 {
		result.ExpiresIn = 3600
	}
	token := TokenInfo{AccessToken: result.AccessToken, ExpiresAt: s.now().Add(time.Duration(result.ExpiresIn) * time.Second), CredentialID: credentialID}
	data, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	if err := writeFileAtomically(s.tokenFile, data, 0o600); err != nil {
		return "", &fetchFailure{Reason: reasonStorageFailure, Summary: "persist cached access token", Err: err}
	}
	return result.AccessToken, nil
}

func (s *fetchService) loadDurableState() (durableFetchState, error) {
	data, err := os.ReadFile(s.fetchStateFile)
	if errors.Is(err, os.ErrNotExist) {
		return durableFetchState{}, nil
	}
	if err != nil {
		return durableFetchState{}, fmt.Errorf("read fetch state: %w", err)
	}
	var state durableFetchState
	if err := json.Unmarshal(data, &state); err != nil {
		return durableFetchState{}, fmt.Errorf("decode fetch state: %w", err)
	}
	return state, nil
}

func (s *fetchService) persistDurableState(state durableFetchState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeFileAtomically(s.fetchStateFile, data, 0o600)
}

type quarantineRecord struct {
	AttemptID string          `json:"attemptId"`
	Reason    failureReason   `json:"reason"`
	SeenAt    time.Time       `json:"seenAt"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Raw       []byte          `json:"raw,omitempty"`
}

func (s *fetchService) quarantine(attemptID string, reason failureReason, payload []byte) error {
	record := quarantineRecord{AttemptID: attemptID, Reason: reason, SeenAt: s.now()}
	if json.Valid(payload) {
		record.Payload = append(json.RawMessage(nil), payload...)
	} else {
		record.Raw = append([]byte(nil), payload...)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	filename := fmt.Sprintf("candidate_%d_%s.json", s.now().Unix(), attemptID)
	return writeFileAtomically(filepath.Join(s.outputDir, "quarantine", filename), data, 0o600)
}

func (s *fetchService) failAttempt(state durableFetchState, attemptID string, reason failureReason, summary string, err error) fetchResult {
	now := s.now()
	state.ActiveAttempt = nil
	state.LatestOutcome = fetchFailed
	state.LastAttemptAt = timePointer(now)
	state.ConsecutiveFailure++
	state.Reason = reason
	state.Summary = summary
	if persistErr := s.persistDurableState(state); persistErr != nil {
		result := s.storageFailure(fmt.Errorf("persist failed fetch state: %w", persistErr))
		result.AttemptID = attemptID
		return result
	}
	s.state.applyDurableFetchState(state)
	return fetchResult{Outcome: fetchFailed, Reason: reason, Err: err, AttemptID: attemptID}
}

func (s *appState) applyDurableFetchState(fetchState durableFetchState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecutiveFails = fetchState.ConsecutiveFailure
	s.lastFetchOK = fetchState.ConsecutiveFailure == 0 && (fetchState.LatestOutcome == fetchSucceeded || fetchState.LatestOutcome == fetchSkipped)
	if fetchState.LastAttemptAt != nil {
		s.lastFetchTime = *fetchState.LastAttemptAt
	}
	if fetchState.Reason != "" {
		s.activeFetchFailure = string(fetchState.Reason) + ": " + fetchState.Summary
	} else {
		s.activeFetchFailure = ""
	}
}

func newAttemptID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("create attempt identity: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func credentialIdentifier(npsso string) string {
	digest := sha256.Sum256([]byte(npsso))
	return hex.EncodeToString(digest[:])
}

func classifyExternalFailure(err error, fallback failureReason, summary string) (failureReason, string) {
	var failure *fetchFailure
	if errors.As(err, &failure) {
		return failure.Reason, failure.Summary
	}
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return reasonUpstreamTimeout, "upstream request exceeded its deadline"
	}
	return fallback, summary
}

func (s *fetchService) storageFailure(err error) fetchResult {
	s.state.recordFetchStateFailure(err)
	return fetchResult{Outcome: fetchFailed, Reason: reasonStorageFailure, Err: err}
}

func timePointer(value time.Time) *time.Time { return &value }
