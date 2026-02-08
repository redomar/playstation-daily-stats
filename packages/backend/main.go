package main

import (
	"flag"
	"log"
	"os"

	"github.com/joho/godotenv"
)

const (
	clientID     = "09515159-7237-4370-9b40-3806e67c0891"
	clientSecret = "ucPjka5tntB2KqsP"
	redirectURI  = "com.scee.psxandroid.scecompcall://redirect"
	tokenFile = "output/token.json"
	npssoFile = "output/npsso.json"
	outputDir = "output/"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: Error loading .env file:", err)
	}

	// Add command line flags
	apiMode := flag.Bool("api", true, "Run in API mode (fetch data and serve)")
	serverOnly := flag.Bool("server-only", false, "Run in server-only mode")
	flag.Parse()

	if *serverOnly {
		log.Println("--- Server Mode Enabled ---")
		startServerMode()
		return
	}

	if *apiMode {
		npsso := os.Getenv("NPSSO")
		if npsso == "" {
			// Try loading from persisted file
			persisted := loadPersistedNPSSO()
			if persisted == "" {
				log.Fatal("NPSSO environment variable is not set and no persisted NPSSO found")
			}
			npsso = persisted
			log.Println("Using persisted NPSSO token from", npssoFile)
		} else {
			// Persist the NPSSO from env var so it survives token refresh via API
			if err := persistNPSSO(npsso); err != nil {
				log.Println("Warning: Failed to persist NPSSO:", err)
			}
		}
		log.Println("--- API Mode Enabled ---")
		startAPIMode(npsso)
		return
	}
}
