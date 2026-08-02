package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/cors"
)

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("PANIC recovered in %s %s: %v\n%s", r.Method, r.URL.Path, err, debug.Stack())
				http.Error(w, fmt.Sprintf("Internal server error: %v", err), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func startServerMode() {
	startServer()
}

func startServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/latest-output", handleLatestOutput)
	mux.HandleFunc("/api/analytics", handleAnalytics)
	mux.HandleFunc("/api/analytics/monthly/", handleMonthlyGames)
	mux.HandleFunc("/api/analytics/yearly/", handleYearlyTopGames)
	mux.HandleFunc("/api/analytics/milestones", handleMilestones)
	mux.HandleFunc("/api/analytics/years", handleAvailableYears)
	mux.HandleFunc("/api/analytics/yoy", handleYoYComparison)
	mux.HandleFunc("/api/analytics/game/", handleGameDeepDive)
	mux.HandleFunc("/api/analytics/genre-trends", handleGenreTrends)
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/events", handleEvents)
	mux.HandleFunc("/api/update-npsso", handleUpdateNPSSO)
	mux.HandleFunc("/api/trigger-fetch", handleTriggerFetch)

	allowedOrigins := strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",")
	handler := recoverMiddleware(cors.New(cors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type"},
	}).Handler(mux))

	server := &http.Server{
		Addr:    ":8080",
		Handler: handler,
	}

	log.Println("Starting server on :8080")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Println("Error starting server:", err)
	}
}

func handleAnalytics(w http.ResponseWriter, r *http.Request) {
	analytics, err := getAnalytics()
	if err != nil {
		http.Error(w, "Unable to calculate analytics", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(analytics)
}

func handleMonthlyGames(w http.ResponseWriter, r *http.Request) {
	// Extract month from URL path: /api/analytics/monthly/2024-08
	month := strings.TrimPrefix(r.URL.Path, "/api/analytics/monthly/")
	if month == "" {
		http.Error(w, "Month parameter required (format: YYYY-MM)", http.StatusBadRequest)
		return
	}

	monthlyGames, err := getMonthlyGames(month)
	if err != nil {
		http.Error(w, "Unable to get monthly games", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(monthlyGames)
}

func handleYearlyTopGames(w http.ResponseWriter, r *http.Request) {
	// Extract year from URL path: /api/analytics/yearly/2024
	yearStr := strings.TrimPrefix(r.URL.Path, "/api/analytics/yearly/")
	if yearStr == "" {
		http.Error(w, "Year parameter required", http.StatusBadRequest)
		return
	}

	year, err := strconv.Atoi(yearStr)
	if err != nil {
		http.Error(w, "Invalid year format", http.StatusBadRequest)
		return
	}

	yearlyGames, err := getYearlyTopGames(year)
	if err != nil {
		http.Error(w, "Unable to get yearly top games", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(yearlyGames)
}

func handleMilestones(w http.ResponseWriter, r *http.Request) {
	milestones, err := getMilestones()
	if err != nil {
		http.Error(w, "Unable to get milestones", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(milestones)
}

func handleAvailableYears(w http.ResponseWriter, r *http.Request) {
	years, err := getAvailableYears()
	if err != nil {
		http.Error(w, "Unable to get available years", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(years)
}

var reportCurrentHealth = func() healthResponse {
	if activeHealthRuntime == nil {
		return evaluateHealth(time.Now(), healthInput{CredentialState: healthCredentialExpired})
	}
	return activeHealthRuntime.report()
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET method required", http.StatusMethodNotAllowed)
		return
	}
	response := reportCurrentHealth()
	w.Header().Set("Content-Type", "application/json")
	if response.Status == healthStatusDegraded {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET method required", http.StatusMethodNotAllowed)
		return
	}
	query := eventQuery{}
	if kind := r.URL.Query().Get("kind"); kind != "" {
		query.Kind = eventKind(kind)
		if !validEventKind(query.Kind) {
			http.Error(w, "Invalid event kind", http.StatusBadRequest)
			return
		}
	}
	if since := r.URL.Query().Get("since"); since != "" {
		parsed, err := time.Parse(time.RFC3339, since)
		if err != nil {
			http.Error(w, "Invalid since timestamp", http.StatusBadRequest)
			return
		}
		query.Since = parsed
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		parsed, err := strconv.Atoi(limit)
		if err != nil || parsed <= 0 {
			http.Error(w, "Invalid event limit", http.StatusBadRequest)
			return
		}
		query.Limit = parsed
	}
	if activeEventStore == nil {
		http.Error(w, "Event history unavailable", http.StatusServiceUnavailable)
		return
	}
	events, err := activeEventStore.Recent(query)
	if err != nil {
		log.Printf("Unable to read event history: %v", err)
		http.Error(w, "Event history unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}

func handleUpdateNPSSO(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST method required", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		NPSSO string `json:"npsso"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(body.NPSSO) == "" {
		http.Error(w, "npsso field is required", http.StatusBadRequest)
		return
	}

	if err := state.updateCredential(npssoFile, tokenFile, body.NPSSO); err != nil {
		log.Println("Failed to persist NPSSO update:", err)
		http.Error(w, "Unable to persist NPSSO update", http.StatusInternalServerError)
		return
	}

	log.Println("NPSSO token updated via API")
	recordEvent(eventRecord{
		Timestamp: time.Now().UTC(),
		Kind:      eventKindAuth,
		Message:   "Credential updated.",
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "NPSSO updated. Use POST /api/trigger-fetch to test it immediately.",
	})
}

var startManualFetch = func() bool {
	return activeFetchService.start(true, logFetchResult)
}

func handleTriggerFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST method required", http.StatusMethodNotAllowed)
		return
	}

	log.Println("Manual fetch triggered via API")
	if !startManualFetch() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "already_running",
			"message": "A fetch attempt is already running.",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Fetch triggered in background. Check GET /api/health for results.",
	})
}

func handleLatestOutput(w http.ResponseWriter, r *http.Request) {
	files, err := os.ReadDir(outputDir)
	if err != nil {
		http.Error(w, "Unable to read output directory", http.StatusInternalServerError)
		return
	}

	// Filter to only output_*.json snapshot files
	var outputFiles []os.DirEntry
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "output_") && strings.HasSuffix(f.Name(), ".json") {
			outputFiles = append(outputFiles, f)
		}
	}

	if len(outputFiles) == 0 {
		http.Error(w, "No output files found", http.StatusNotFound)
		return
	}

	sort.Slice(outputFiles, func(i, j int) bool {
		return outputFiles[i].Name() > outputFiles[j].Name()
	})

	latestFile := outputFiles[0]
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

	data["filename"] = latestFile.Name()
	name := latestFile.Name()
	if len(name) > 12 { // "output_" (7) + at least 1 char + ".json" (5)
		data["timestamp"] = name[7 : len(name)-5]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func handleYoYComparison(w http.ResponseWriter, r *http.Request) {
	yoy, err := getYoYComparison()
	if err != nil {
		http.Error(w, "Unable to calculate year-over-year data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(yoy)
}

func handleGameDeepDive(w http.ResponseWriter, r *http.Request) {
	titleID := strings.TrimPrefix(r.URL.Path, "/api/analytics/game/")
	if titleID == "" {
		http.Error(w, "titleId parameter required", http.StatusBadRequest)
		return
	}

	deepDive, err := getGameDeepDive(titleID)
	if err != nil {
		http.Error(w, "Unable to get game deep dive", http.StatusInternalServerError)
		return
	}

	if deepDive == nil {
		http.Error(w, "Game not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(deepDive)
}

func handleGenreTrends(w http.ResponseWriter, r *http.Request) {
	trends, err := getGenreTrends()
	if err != nil {
		http.Error(w, "Unable to calculate genre trends", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trends)
}
