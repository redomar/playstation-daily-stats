package main

import (
	"flag"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const (
	clientID     = "09515159-7237-4370-9b40-3806e67c0891"
	clientSecret = "ucPjka5tntB2KqsP"
	redirectURI  = "com.scee.psxandroid.scecompcall://redirect"
)

var (
	tokenFile       = "output/token.json"
	npssoFile       = "output/npsso.json"
	fetchStateFile  = "output/fetch-state.json"
	corpusAuditFile = "output/corpus-audit.json"
	outputDir       = "output/"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: Error loading .env file:", err)
	}

	// Add command line flags
	apiMode := flag.Bool("api", true, "Run in API mode (fetch data and serve)")
	serverOnly := flag.Bool("server-only", false, "Run in server-only mode")
	auditOnly := flag.Bool("audit-corpus", false, "Rebuild the historical snapshot audit manifest and exit")
	flag.Parse()

	if *auditOnly {
		manifest, err := auditCorpus(outputDir, corpusAuditFile, time.Now())
		if err != nil {
			log.Fatal("Unable to audit snapshot corpus:", err)
		}
		log.Printf("Corpus audit complete: accepted=%d excluded=%d", manifest.AcceptedCount, manifest.ExcludedCount)
		return
	}

	if manifest, created, err := ensureCorpusAudit(outputDir, corpusAuditFile, time.Now()); err != nil {
		state.recordFetchStateFailure(err)
		log.Printf("Unable to load or create corpus audit: %v", err)
	} else if created {
		log.Printf("Corpus audit created: accepted=%d excluded=%d", manifest.AcceptedCount, manifest.ExcludedCount)
		cache.invalidate()
	}

	if *serverOnly {
		log.Println("--- Server Mode Enabled ---")
		startServerMode()
		return
	}

	if *apiMode {
		credential := resolveCredential(npssoFile, os.Getenv("NPSSO"))
		if credential.State == credentialReady {
			log.Println("Using durable NPSSO credential")
		} else {
			log.Printf("Starting degraded: credential state=%s reason=%s", credential.State, credential.Reason)
		}
		log.Println("--- API Mode Enabled ---")
		startAPIMode(credential)
		return
	}
}
