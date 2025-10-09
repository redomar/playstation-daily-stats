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
	"strings"
	"time"
)

type TokenInfo struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func startAPIMode(npsso string) {
	// Run the initial fetch
	if err := fetchAndSaveData(npsso); err != nil {
		log.Println("Error fetching and saving data:", err)
	}

	// Start the scheduled fetch in a goroutine
	go scheduledFetch(npsso)

	log.Println("--- Server in API Mode Enabled ---")

	// Start the server
	startServer()
}

func scheduledFetch(npsso string) {
	for {
		// Calculate time until next 6am
		now := time.Now()
		next6am := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, now.Location())

		// If it's already past 6am today, schedule for tomorrow
		if now.After(next6am) {
			next6am = next6am.Add(24 * time.Hour)
		}

		duration := next6am.Sub(now)
		log.Printf("Next fetch scheduled at %v (in %v)", next6am.Format("2006-01-02 15:04:05"), duration)

		// Wait until 6am
		time.Sleep(duration)

		// Fetch data
		if err := fetchAndSaveData(npsso); err != nil {
			log.Println("Error fetching and saving data:", err)
		}
	}
}

func fetchAndSaveData(npsso string) error {
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
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	u, err := url.Parse(location)
	if err != nil {
		return "", err
	}

	code := u.Query().Get("code")
	if !strings.HasPrefix(code, "v3.") {
		return "", fmt.Errorf("invalid authorization code")
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
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
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
