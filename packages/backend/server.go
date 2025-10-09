package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rs/cors"
)

func startServerMode() {
	startServer()
}

func startServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/latest-output", handleLatestOutput)

	allowedOrigins := strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",")
	handler := cors.New(cors.Options{
		AllowedOrigins: allowedOrigins,
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
