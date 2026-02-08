package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type TokenInfo struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// appState holds mutable application state that can be updated at runtime.
type appState struct {
	mu              sync.RWMutex
	npsso           string
	lastFetchTime   time.Time
	lastFetchError  string
	lastFetchOK     bool
	nextFetchTime   time.Time
	consecutiveFails int
}

var state = &appState{}

func (s *appState) getNPSSO() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.npsso
}

func (s *appState) setNPSSO(npsso string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.npsso = npsso
}

func (s *appState) recordFetch(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFetchTime = time.Now()
	if err != nil {
		s.lastFetchError = err.Error()
		s.lastFetchOK = false
		s.consecutiveFails++
	} else {
		s.lastFetchError = ""
		s.lastFetchOK = true
		s.consecutiveFails = 0
	}
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
		"last_fetch_ok":     s.lastFetchOK,
		"consecutive_fails": s.consecutiveFails,
	}
	if !s.lastFetchTime.IsZero() {
		status["last_fetch_time"] = s.lastFetchTime.Format(time.RFC3339)
	}
	if s.lastFetchError != "" {
		status["last_fetch_error"] = s.lastFetchError
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

// persistNPSSO saves the NPSSO token to a file for persistence across restarts.
func persistNPSSO(npsso string) error {
	data, err := json.Marshal(map[string]string{"npsso": npsso})
	if err != nil {
		return err
	}
	return os.WriteFile(npssoFile, data, 0600)
}

// loadPersistedNPSSO reads a previously persisted NPSSO token.
func loadPersistedNPSSO() string {
	data, err := os.ReadFile(npssoFile)
	if err != nil {
		return ""
	}
	var stored map[string]string
	if err := json.Unmarshal(data, &stored); err != nil {
		return ""
	}
	return stored["npsso"]
}

func startAPIMode(npsso string) {
	state.setNPSSO(npsso)

	// Run the initial fetch
	err := fetchAndSaveData()
	state.recordFetch(err)
	if err != nil {
		log.Println("Error fetching and saving data:", err)
	}

	// Start the scheduled fetch in a goroutine
	go scheduledFetch()

	log.Println("--- Server in API Mode Enabled ---")

	// Start the server
	startServer()
}

func hasRecentSnapshot(maxAge time.Duration) bool {
	files, err := os.ReadDir(outputDir)
	if err != nil {
		log.Println("Warning: Could not read output directory:", err)
		return false
	}

	now := time.Now()
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), "output_") || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		// Extract timestamp from filename: output_1234567890.json
		timestampStr := file.Name()[7 : len(file.Name())-5]
		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			continue
		}

		fileTime := time.Unix(timestamp, 0)
		age := now.Sub(fileTime)

		if age < maxAge {
			log.Printf("Found recent snapshot: %s (age: %v)", file.Name(), age)
			return true
		}
	}

	return false
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

		// Fetch data using current NPSSO (may have been updated via API)
		err := fetchAndSaveData()
		state.recordFetch(err)
		if err != nil {
			log.Println("Error fetching and saving data:", err)
		}
	}
}

func fetchAndSaveData() error {
	return doFetch(false)
}

func fetchAndSaveDataForced() error {
	return doFetch(true)
}

func doFetch(force bool) error {
	npsso := state.getNPSSO()
	if npsso == "" {
		return fmt.Errorf("NPSSO token is not configured")
	}

	// Check if we have a recent snapshot (within 12 hours), unless forced
	if !force && hasRecentSnapshot(12*time.Hour) {
		log.Println("Recent snapshot found (< 12 hours old), skipping fetch to avoid rate limiting")
		return nil
	}

	token, err := getValidToken(npsso)
	if err != nil {
		return fmt.Errorf("error getting valid token: %w", err)
	}

	log.Println("Valid Authentication Token obtained")

	baseURI := "https://m.np.playstation.com/api/gamelist/v2/users/me/titles"

	var allData []json.RawMessage
	offsets := []int{0, 200, 400}

	for _, offset := range offsets {
		testURI := fmt.Sprintf("%s?limit=200&offset=%d", baseURI, offset)
		resp, err := makeAuthorizedRequest(testURI, token)
		if err != nil {
			return fmt.Errorf("error making authorized request with offset %d: %w", offset, err)
		}

		var data map[string]json.RawMessage
		if err := json.Unmarshal(resp, &data); err != nil {
			return fmt.Errorf("error parsing JSON response: %w", err)
		}

		if titles, ok := data["titles"]; ok {
			var titlesArray []json.RawMessage
			if err := json.Unmarshal(titles, &titlesArray); err != nil {
				return fmt.Errorf("error parsing titles array: %w", err)
			}
			allData = append(allData, titlesArray...)
		}
	}

	finalResponse := map[string]interface{}{
		"titles": allData,
	}

	combinedJSON, err := json.Marshal(finalResponse)
	if err != nil {
		return fmt.Errorf("error marshaling combined data: %w", err)
	}

	filename := fmt.Sprintf("output_%d.json", time.Now().Unix())
	if err := os.WriteFile(filepath.Join(outputDir, filename), combinedJSON, 0600); err != nil {
		return fmt.Errorf("error saving resource to file: %w", err)
	}

	log.Printf("Data fetched and saved to %s\n", filename)
	return nil
}

func getValidToken(npsso string) (string, error) {
	tokenInfo, err := loadTokenFromFile()
	if err == nil && time.Now().Before(tokenInfo.ExpiresAt) {
		return tokenInfo.AccessToken, nil
	}

	token, err := getAuthenticationToken(npsso)
	if err != nil {
		return "", err
	}

	tokenInfo = TokenInfo{
		AccessToken: token,
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}

	if err := saveTokenToFile(tokenInfo); err != nil {
		log.Println("Warning: Failed to save token to file:", err)
	}

	return token, nil
}

func loadTokenFromFile() (TokenInfo, error) {
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return TokenInfo{}, err
	}
	var tokenInfo TokenInfo
	return tokenInfo, json.Unmarshal(data, &tokenInfo)
}

func saveTokenToFile(tokenInfo TokenInfo) error {
	data, err := json.Marshal(tokenInfo)
	if err != nil {
		return err
	}
	return os.WriteFile(tokenFile, data, 0600)
}

func getAuthenticationToken(npsso string) (string, error) {
	authCode, err := getAuthorizationCode(npsso)
	if err != nil {
		return "", fmt.Errorf("error getting authorization code: %w", err)
	}

	return exchangeCodeForToken(authCode)
}

func getAuthorizationCode(npsso string) (string, error) {
	params := url.Values{
		"access_type":   {"offline"},
		"client_id":     {clientID},
		"response_type": {"code"},
		"scope":         {"psn:mobile.v2.core psn:clientapp"},
		"redirect_uri":  {redirectURI},
	}
	authURL := "https://ca.account.sony.com/api/authz/v3/oauth/authorize?" + params.Encode()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequest("GET", authURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Cookie", "npsso="+npsso)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("NPSSO token is likely expired or invalid (status %d, body: %s). "+
			"Get a new NPSSO from playstation.com cookies and update via POST /api/update-npsso", resp.StatusCode, truncate(string(body), 200))
	}

	location := resp.Header.Get("Location")
	u, err := url.Parse(location)
	if err != nil {
		return "", err
	}

	code := u.Query().Get("code")
	if !strings.HasPrefix(code, "v3.") {
		errorParam := u.Query().Get("error")
		errorDesc := u.Query().Get("error_description")
		if errorParam != "" {
			return "", fmt.Errorf("NPSSO token rejected by Sony: %s - %s. "+
				"Get a new NPSSO from playstation.com cookies and update via POST /api/update-npsso", errorParam, errorDesc)
		}
		return "", fmt.Errorf("invalid authorization code (no v3. prefix). "+
			"NPSSO token is likely expired. Get a new one from playstation.com cookies and update via POST /api/update-npsso")
	}

	return code, nil
}

func exchangeCodeForToken(code string) (string, error) {
	data := url.Values{
		"code":         {code},
		"redirect_uri": {redirectURI},
		"grant_type":   {"authorization_code"},
		"token_format": {"jwt"},
	}

	req, err := http.NewRequest("POST", "https://ca.account.sony.com/api/authz/v3/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(clientID+":"+clientSecret)))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token exchange failed (status %d): %s", resp.StatusCode, truncate(string(body), 200))
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", err
	}

	return result.AccessToken, nil
}

func makeAuthorizedRequest(url, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
