package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type TokenInfo struct {
	AccessToken  string    `json:"access_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	CredentialID string    `json:"credential_id,omitempty"`
}

// appState holds mutable application state that can be updated at runtime.
type appState struct {
	mu                 sync.RWMutex
	credentialUpdate   sync.Mutex
	npsso              string
	credentialState    credentialState
	credentialReason   string
	lastFetchTime      time.Time
	activeFetchFailure string
	lastFetchOK        bool
	nextFetchTime      time.Time
	consecutiveFails   int
}

var state = &appState{}

var activeFetchService = newDefaultFetchService()

func (s *appState) getNPSSO() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.npsso
}

func (s *appState) setCredential(credential credentialResolution) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.npsso = credential.Value
	s.credentialState = credential.State
	s.credentialReason = credential.Reason
}

func (s *appState) updateCredential(npssoPath, cachedTokenPath, npsso string) error {
	s.credentialUpdate.Lock()
	defer s.credentialUpdate.Unlock()

	if err := persistCredential(npssoPath, npsso); err != nil {
		return err
	}

	s.mu.Lock()
	s.npsso = npsso
	s.credentialState = credentialReady
	s.credentialReason = ""
	s.mu.Unlock()

	if err := os.Remove(cachedTokenPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("invalidate cached token: %w", err)
	}
	return nil
}

func (s *appState) setNextFetch(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextFetchTime = t
}

func (s *appState) getStatus() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := map[string]interface{}{
		"npsso_configured":  s.npsso != "",
		"npsso_length":      len(s.npsso),
		"credential_state":  s.credentialState,
		"last_fetch_ok":     s.lastFetchOK,
		"consecutive_fails": s.consecutiveFails,
	}
	if s.credentialReason != "" {
		status["credential_reason"] = s.credentialReason
	}
	if !s.lastFetchTime.IsZero() {
		status["last_fetch_time"] = s.lastFetchTime.Format(time.RFC3339)
	}
	if s.activeFetchFailure != "" {
		status["active_fetch_failure"] = s.activeFetchFailure
	}
	if !s.nextFetchTime.IsZero() {
		status["next_fetch_time"] = s.nextFetchTime.Format(time.RFC3339)
		status["next_fetch_in"] = time.Until(s.nextFetchTime).String()
	}
	// Check if cached token is still valid
	tokenInfo, err := loadTokenFromFile()
	if err == nil && time.Now().Before(tokenInfo.ExpiresAt) {
		status["token_valid"] = true
		status["token_expires_in"] = time.Until(tokenInfo.ExpiresAt).String()
	} else {
		status["token_valid"] = false
	}
	return status
}

func startAPIMode(credential credentialResolution) {
	state.setCredential(credential)
	activeFetchService = newDefaultFetchService()
	if err := activeFetchService.restore(); err != nil {
		state.recordFetchStateFailure(err)
		log.Printf("Unable to restore durable fetch state: %v", err)
	}

	// Keep the HTTP recovery surface available while the initial fetch runs.
	activeFetchService.start(false, logFetchResult)

	// Start the scheduled fetch in a goroutine
	go scheduledFetch()

	log.Println("--- Server in API Mode Enabled ---")

	// Start the server
	startServer()
}

func scheduledFetch() {
	for {
		// Calculate time until next 6am
		now := time.Now()
		next6am := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, now.Location())

		// If it's already past 6am today, schedule for tomorrow
		if now.After(next6am) {
			next6am = next6am.Add(24 * time.Hour)
		}

		duration := next6am.Sub(now)
		state.setNextFetch(next6am)
		log.Printf("Next fetch scheduled at %v (in %v)", next6am.Format("2006-01-02 15:04:05"), duration)

		// Wait until 6am
		time.Sleep(duration)

		logFetchResult(activeFetchService.run(context.Background(), false))
	}
}

func newDefaultFetchService() *fetchService {
	const requestTimeout = 30 * time.Second
	return &fetchService{
		outputDir:        outputDir,
		fetchStateFile:   fetchStateFile,
		corpusAuditFile:  corpusAuditFile,
		tokenFile:        tokenFile,
		gameListURL:      "https://m.np.playstation.com/api/gamelist/v2/users/me/titles",
		authorizationURL: "https://ca.account.sony.com/api/authz/v3/oauth/authorize",
		tokenURL:         "https://ca.account.sony.com/api/authz/v3/oauth/token",
		httpClient:       &http.Client{Timeout: requestTimeout},
		authHTTPClient: &http.Client{
			Timeout: requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		attemptTimeout: 2 * time.Minute,
		pageSize:       200,
		now:            time.Now,
		state:          state,
		attempts:       &attemptLock{},
	}
}

func logFetchResult(result fetchResult) {
	if result.AlreadyRunning {
		log.Println("Fetch trigger rejected because an attempt is already running")
		return
	}
	switch result.Outcome {
	case fetchSucceeded:
		cache.invalidate()
		log.Printf("Succeeded fetch committed %d titles", result.TitleCount)
	case fetchSkipped:
		log.Println("Skipped fetch because a recent valid snapshot exists")
	case fetchFailed:
		if result.Err != nil {
			log.Printf("Failed fetch reason=%s: %v", result.Reason, result.Err)
		} else {
			log.Printf("Failed fetch reason=%s", result.Reason)
		}
	}
}

func (s *appState) recordFetchStateFailure(_ error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFetchOK = false
	s.activeFetchFailure = string(reasonStorageFailure) + ": durable fetch state is unavailable"
	s.consecutiveFails++
}

func loadTokenFromFile() (TokenInfo, error) {
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return TokenInfo{}, err
	}
	var tokenInfo TokenInfo
	return tokenInfo, json.Unmarshal(data, &tokenInfo)
}
