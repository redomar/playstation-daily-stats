package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/cors"
)

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
	mux.HandleFunc("/api/update-npsso", handleUpdateNPSSO)
	mux.HandleFunc("/api/trigger-fetch", handleTriggerFetch)

	allowedOrigins := strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",")
	handler := cors.New(cors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type"},
	}).Handler(mux)

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

func handleHealth(w http.ResponseWriter, r *http.Request) {
	status := state.getStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
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

	if body.NPSSO == "" {
		http.Error(w, "npsso field is required", http.StatusBadRequest)
		return
	}

	// Update the in-memory NPSSO
	state.setNPSSO(body.NPSSO)

	// Persist to file so it survives restarts
	if err := persistNPSSO(body.NPSSO); err != nil {
		log.Println("Warning: Failed to persist NPSSO:", err)
	}

	// Clear the cached token so the new NPSSO is used immediately
	os.Remove(tokenFile)

	log.Println("NPSSO token updated via API")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "NPSSO updated. Use POST /api/trigger-fetch to test it immediately.",
	})
}

func handleTriggerFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST method required", http.StatusMethodNotAllowed)
		return
	}

	log.Println("Manual fetch triggered via API")

	// Clear the cached token to force re-authentication with current NPSSO
	os.Remove(tokenFile)

	go func() {
		err := fetchAndSaveDataForced()
		state.recordFetch(err)
		if err != nil {
			log.Println("Manual fetch failed:", err)
		} else {
			cache.invalidate()
			log.Println("Manual fetch completed successfully")
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Fetch triggered in background. Check GET /api/health for results.",
	})
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

	data["filename"] = latestFile.Name()
	data["timestamp"] = latestFile.Name()[7 : len(latestFile.Name())-5]

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
