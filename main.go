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
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/cors"
)

const (
	clientID     = "09515159-7237-4370-9b40-3806e67c0891"
	clientSecret = "ucPjka5tntB2KqsP"
	redirectURI  = "com.scee.psxandroid.scecompcall://redirect"
	tokenFile    = "token.json"
	outputDir    = "/app/output"
)

type TokenInfo struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: Error loading .env file:", err)
	}

	npsso := os.Getenv("NPSSO")
	if npsso == "" {
		log.Fatal("NPSSO environment variable is not set")
	}

	go dailyFetch(npsso)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/latest-output", handleLatestOutput)

	allowedOrigins := strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",")
	c := cors.New(cors.Options{
		AllowedOrigins: allowedOrigins,
	})
	//log allowed origins
	log.Println("Allowed Origins: ", allowedOrigins)

	handler := c.Handler(mux)

	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}

func dailyFetch(npsso string) {
	for {
		if err := fetchAndSaveData(npsso); err != nil {
			log.Println("Error fetching and saving data:", err)
		}
		// Wait for 24 hours before the next fetch
		time.Sleep(24 * time.Hour)
	}
}

func fetchAndSaveData(npsso string) error {
	token, err := getValidToken(npsso)
	if err != nil {
		return fmt.Errorf("error getting valid token: %w", err)
	}

	log.Println("Valid Authentication Token obtained")

	testURI := "https://m.np.playstation.com/api/gamelist/v2/users/me/titles?limit=200"
	resp, err := makeAuthorizedRequest(testURI, token)
	if err != nil {
		return fmt.Errorf("error making authorized request: %w", err)
	}

	filename := fmt.Sprintf("output_%d.json", time.Now().Unix())
	if err := os.WriteFile(filepath.Join(outputDir, filename), resp, 0600); err != nil {
		return fmt.Errorf("error saving resource to file: %w", err)
	}

	log.Printf("Data fetched and saved to %s\n", filename)
	return nil
}

func handleLatestOutput(w http.ResponseWriter, r *http.Request) {
    files, err := os.ReadDir(outputDir)
    if err != nil {
        http.Error(w, "Unable to read output directory", http.StatusInternalServerError)
        return
    }

    if len(files) == 0 {
        http.Error(w, "No output files found", http.StatusNotFound)
        return
    }

    sort.Slice(files, func(i, j int) bool {
        return files[i].Name() > files[j].Name()
    })

    latestFile := files[0]
    content, err := os.ReadFile(filepath.Join(outputDir, latestFile.Name()))
    if err != nil {
        http.Error(w, "Unable to read latest file", http.StatusInternalServerError)
        return
    }

    var data map[string]interface{}
    if err := json.Unmarshal(content, &data); err != nil {
        http.Error(w, "Unable to parse JSON content", http.StatusInternalServerError)
        return
    }

    // Add filename to the top-level of the JSON
    data["filename"] = latestFile.Name()

	// file name can be this "output_1725731550.json" just print the unix timestamp, remove the "output_" part and the ".json" part

	data["timestamp"] = latestFile.Name()[7:len(latestFile.Name())-5]
	

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(data)
}

// ... (rest of your functions remain the same)
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
