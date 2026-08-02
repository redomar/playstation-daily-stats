package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const healthCapacityCacheDuration = 30 * time.Second

type healthRuntime struct {
	state           *appState
	events          *eventStore
	snapshots       *fetchService
	outputDir       string
	corpusAuditFile string
	now             func() time.Time
	buildCapacity   func(string) (CapacityReport, error)

	capacityMu       sync.Mutex
	capacityCachedAt time.Time
	capacityCached   CapacityReport
	capacityValid    bool
}

func newHealthRuntime(state *appState, events *eventStore, outputDir, corpusAuditFile string) *healthRuntime {
	return &healthRuntime{
		state:           state,
		events:          events,
		snapshots:       &fetchService{outputDir: outputDir, corpusAuditFile: corpusAuditFile},
		outputDir:       outputDir,
		corpusAuditFile: corpusAuditFile,
		now:             time.Now,
		buildCapacity:   BuildCapacityReport,
	}
}

func (runtime *healthRuntime) report() healthResponse {
	now := runtime.now().UTC()
	fetch := runtime.state.healthState()

	snapshotCount, snapshotErr := acceptedSnapshotCount(runtime.outputDir, runtime.corpusAuditFile)
	var lastSuccessAt *time.Time
	if runtime.snapshots == nil {
		snapshotErr = errors.New("snapshot history is unavailable")
	} else {
		_, capturedAt, found, err := runtime.snapshots.newestValidSnapshot("")
		if err != nil {
			snapshotErr = err
		} else if found {
			lastSuccessAt = &capturedAt
		}
	}
	bootsLastHour := 0
	var bootsErr error
	if runtime.events == nil {
		bootsErr = errors.New("event store is unavailable")
	} else {
		boots, err := runtime.events.Recent(eventQuery{
			Kind:  eventKindBoot,
			Since: now.Add(-time.Hour),
			Limit: maxRecentLimit,
		})
		bootsLastHour = len(boots)
		bootsErr = err
	}

	activeFetchFailureReason := fetch.ActiveFetchFailureReason
	if snapshotErr != nil || bootsErr != nil {
		activeFetchFailureReason = reasonStorageFailure
	}
	capacity, err := runtime.capacity(now)
	if err != nil {
		log.Printf("Unable to collect capacity for health report: %v", err)
	}

	credential := healthCredentialOK
	if fetch.CredentialState != credentialReady || activeFetchFailureReason == reasonAuthenticationRejected {
		credential = healthCredentialExpired
	}
	return evaluateHealth(now, healthInput{
		LastSuccessAt:            lastSuccessAt,
		SnapshotCount:            snapshotCount,
		ConsecutiveFailures:      fetch.ConsecutiveFailures,
		ActiveFetchFailureReason: activeFetchFailureReason,
		CredentialState:          credential,
		BootsLastHour:            bootsLastHour,
		StorageUsedPercent:       capacity.StorageUsedPercent,
		MemoryUsedPercent:        capacity.MemoryUsedPercent,
		ArchiveBytes:             int64(capacity.ArchiveBytes),
	})
}

func (runtime *healthRuntime) capacity(now time.Time) (CapacityReport, error) {
	runtime.capacityMu.Lock()
	defer runtime.capacityMu.Unlock()
	if runtime.capacityValid && now.Sub(runtime.capacityCachedAt) < healthCapacityCacheDuration {
		return runtime.capacityCached, nil
	}
	report, err := runtime.buildCapacity(runtime.outputDir)
	if err != nil {
		return CapacityReport{}, err
	}
	runtime.capacityCachedAt = now
	runtime.capacityCached = report
	runtime.capacityValid = true
	return report, nil
}

func acceptedSnapshotCount(outputDir, corpusAuditPath string) (int, error) {
	excluded, err := auditExclusions(corpusAuditPath)
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "output_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		if _, isExcluded := excluded[name]; isExcluded {
			continue
		}
		timestamp := strings.TrimSuffix(strings.TrimPrefix(name, "output_"), ".json")
		if _, err := strconv.ParseInt(timestamp, 10, 64); err != nil {
			continue
		}
		count++
	}
	return count, nil
}

func recordEvent(event eventRecord) {
	if activeEventStore == nil {
		return
	}
	if err := activeEventStore.Append(event); err != nil {
		log.Printf("Unable to persist %s event: %v", event.Kind, err)
	}
}

func recordBoot(store *eventStore, now time.Time) error {
	return store.Append(eventRecord{
		Timestamp: now.UTC(),
		Kind:      eventKindBoot,
		Message:   "Service started.",
	})
}

func eventStorePath(outputDir string) string {
	return filepath.Join(outputDir, "events.jsonl")
}
