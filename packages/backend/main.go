package main

import (
	"context"
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
	tokenFile           = "output/token.json"
	npssoFile           = "output/npsso.json"
	fetchStateFile      = "output/fetch-state.json"
	corpusAuditFile     = "output/corpus-audit.json"
	outputDir           = "output/"
	activeEventStore    *eventStore
	activeHealthRuntime *healthRuntime
	activeAlertObserver alertFetchObserver
	activeAlertRuntime  *alertCoordinator
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: Error loading .env file:", err)
	}

	// Add command line flags
	apiMode := flag.Bool("api", true, "Run in API mode (fetch data and serve)")
	serverOnly := flag.Bool("server-only", false, "Run in server-only mode")
	auditOnly := flag.Bool("audit-corpus", false, "Rebuild the historical snapshot audit manifest and exit")
	acknowledgeQuarantine := flag.Bool("acknowledge-quarantine", false, "Acknowledge retained quarantined candidates and exit; run with the service stopped")
	flag.Parse()
	if *acknowledgeQuarantine {
		acknowledged, err := acknowledgeQuarantinedCandidate(fetchStateFile)
		if err != nil {
			log.Fatal("Unable to acknowledge quarantined candidates:", err)
		}
		if acknowledged {
			log.Println("Acknowledged retained quarantined candidates")
		} else {
			log.Println("No quarantined candidate acknowledgement was pending")
		}
		return
	}

	if *auditOnly {
		manifest, err := auditCorpus(outputDir, corpusAuditFile, time.Now())
		if err != nil {
			log.Fatal("Unable to audit snapshot corpus:", err)
		}
		log.Printf("Corpus audit complete: accepted=%d excluded=%d", manifest.AcceptedCount, manifest.ExcludedCount)
		return
	}

	var corpusInitializationErr error
	if manifest, created, err := ensureCorpusAudit(outputDir, corpusAuditFile, time.Now()); err != nil {
		corpusInitializationErr = err
		log.Printf("Unable to load or create corpus audit: %v", err)
	} else if created {
		log.Printf("Corpus audit created: accepted=%d excluded=%d", manifest.AcceptedCount, manifest.ExcludedCount)
		cache.invalidate()
	}

	if !*serverOnly && !*apiMode {
		return
	}

	credential := resolveCredential(npssoFile, os.Getenv("NPSSO"))
	if credential.State == credentialReady {
		log.Println("Using durable NPSSO credential")
	} else {
		log.Printf("Starting degraded: credential state=%s reason=%s", credential.State, credential.Reason)
	}
	initializeRuntime(credential)
	if corpusInitializationErr != nil {
		state.recordFetchStateFailure(corpusInitializationErr)
	}
	go activeAlertRuntime.Run(context.Background())

	if *serverOnly {
		log.Println("--- Server Mode Enabled ---")
		startServerMode()
		return
	}

	if *apiMode {
		log.Println("--- API Mode Enabled ---")
		startAPIMode()
		return
	}
}

func initializeRuntime(credential credentialResolution) {
	state.setCredential(credential)
	activeFetchService = newDefaultFetchService()
	if err := activeFetchService.restore(); err != nil {
		state.recordFetchStateFailure(err)
		log.Printf("Unable to restore durable fetch state: %v", err)
	}

	activeEventStore = newEventStore(eventStorePath(outputDir))
	if err := recordBoot(activeEventStore, time.Now()); err != nil {
		state.recordFetchStateFailure(err)
		log.Printf("Unable to persist boot event: %v", err)
	}
	activeHealthRuntime = newHealthRuntime(state, activeEventStore, outputDir, corpusAuditFile)
	delivery := newDeliveryRuntime(deliveryConfig{
		NtfyTopicURL: os.Getenv("NTFY_TOPIC_URL"),
		LivenessURL:  os.Getenv("HEALTHCHECKS_LIVENESS_URL"),
		SuccessURL:   os.Getenv("HEALTHCHECKS_SUCCESS_URL"),
	}, activeEventStore)
	activeAlertRuntime = newAlertCoordinator(
		delivery,
		activeHealthRuntime.report,
		func() (quarantineAlertStatus, error) {
			durable, err := activeFetchService.loadDurableState()
			return quarantineAlertStatus{
				Active:               durable.QuarantinedCandidate,
				CausedCurrentFailure: durable.LatestFailureQuarantined,
			}, err
		},
		time.Now,
	)
	activeAlertObserver = activeAlertRuntime
}
