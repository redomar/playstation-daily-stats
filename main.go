package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sort"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/cors"

)

const (
	clientID     = "09515159-7237-4370-9b40-3806e67c0891"
	clientSecret = "ucPjka5tntB2KqsP"
	redirectURI  = "com.scee.psxandroid.scecompcall://redirect"
	tokenFile    = "token.json"
)

type TokenInfo struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func main() {
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file")
	}

	npsso := os.Getenv("NPSSO")
	if npsso == "" {
		fmt.Println("NPSSO environment variable is not set")
		return
	}

	token, err := getValidToken(npsso)
	if err != nil {
		fmt.Println("Error getting valid token:", err)
		return
	}

	fmt.Println("Valid Authentication Token obtained")

	// Use the token for the API request
	var testUri = "https://m.np.playstation.com/api/gamelist/v2/users/me/titles"
	resp, err := makeAuthorizedRequest(testUri, token)
	if err != nil {
		fmt.Println("Error making authorized request:", err)
		return
	}

	// Print the response
	fmt.Println("Response body:", string(resp))

	// save the resource to a file with todays data as unix timestamp as filename
	err = os.WriteFile(fmt.Sprintf("/app/output/output_%d.json", time.Now().Unix()), resp, 0600)
	if err != nil {
		fmt.Println("Error saving resource to file:", err)
		return
	}

	http.HandleFunc("/api/latest-output", handleLatestOutput)
	fmt.Println("Starting server on :8080")
	c := cors.New(cors.Options{
		AllowedOrigins: []string{"http://localhost:5173"},
	})
	
	handler := c.Handler(http.DefaultServeMux)
	http.ListenAndServe(":8080", handler)
}

func handleLatestOutput(w http.ResponseWriter, r *http.Request) {
    files, err := os.ReadDir("/app/output")
    if err != nil {
        http.Error(w, "Unable to read output directory", http.StatusInternalServerError)
        return
    }

    if len(files) == 0 {
        http.Error(w, "No output files found", http.StatusNotFound)
        return
    }

    sort.Slice(files, func(i, j int) bool {
        infoI, _ := files[i].Info()
        infoJ, _ := files[j].Info()
        return infoI.ModTime().After(infoJ.ModTime())
    })

    latestFile := files[0]
    content, err := os.ReadFile(filepath.Join("/app/output", latestFile.Name()))
    if err != nil {
        http.Error(w, "Unable to read latest file", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.Write(content)
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

	err = saveTokenToFile(tokenInfo)
	if err != nil {
		fmt.Println("Warning: Failed to save token to file:", err)
	}

	return token, nil
}

func loadTokenFromFile() (TokenInfo, error) {
	var tokenInfo TokenInfo
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return tokenInfo, err
	}
	err = json.Unmarshal(data, &tokenInfo)
	return tokenInfo, err
}

func saveTokenToFile(tokenInfo TokenInfo) error {
	data, err := json.Marshal(tokenInfo)
	if err != nil {
		return err
	}
	return os.WriteFile(tokenFile, data, 0600)
}

func getAuthenticationToken(npsso string) (string, error) {
	// Step 1: Get the authorization code
	authCode, err := getAuthorizationCode(npsso)
	if err != nil {
		return "", fmt.Errorf("error getting authorization code: %w", err)
	}

	// Step 2: Exchange the authorization code for an access token
	token, err := exchangeCodeForToken(authCode)
	if err != nil {
		return "", fmt.Errorf("error exchanging code for token: %w", err)
	}

	return token, nil
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}
