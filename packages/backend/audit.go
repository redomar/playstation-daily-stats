package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type auditClassification string

const (
	auditAccepted auditClassification = "accepted"
	auditExcluded auditClassification = "excluded"
)

type auditRuleResult struct {
	Rule       string `json:"rule"`
	Applicable bool   `json:"applicable"`
	Passed     bool   `json:"passed"`
	Detail     string `json:"detail,omitempty"`
}

type corpusAuditEntry struct {
	Filename          string              `json:"filename"`
	Timestamp         int64               `json:"timestamp"`
	TitleCount        int                 `json:"titleCount"`
	Classification    auditClassification `json:"classification"`
	Reason            failureReason       `json:"reason,omitempty"`
	Rules             []auditRuleResult   `json:"rules"`
	structurallyValid bool
}

type corpusAuditManifest struct {
	Version       int                `json:"version"`
	GeneratedAt   time.Time          `json:"generatedAt"`
	AcceptedCount int                `json:"acceptedCount"`
	ExcludedCount int                `json:"excludedCount"`
	Entries       []corpusAuditEntry `json:"entries"`
}

func auditCorpus(dir, manifestPath string, generatedAt time.Time) (corpusAuditManifest, error) {
	entries, err := readCorpusEntries(dir)
	if err != nil {
		return corpusAuditManifest{}, err
	}

	for index := range entries {
		entry := &entries[index]
		structuralRule := auditRuleResult{Rule: "structural_validity", Applicable: true, Passed: entry.structurallyValid}
		if !entry.structurallyValid {
			structuralRule.Detail = "snapshot is not an object with a non-empty, structurally valid titles array"
			entry.Classification = auditExcluded
			entry.Reason = reasonInvalidSchema
		}
		entry.Rules = append(entry.Rules, structuralRule)

		rapidGrowthRule := auditRuleResult{Rule: "following_24h_more_than_double", Applicable: entry.structurallyValid, Passed: true}
		if entry.structurallyValid {
			for next := index + 1; next < len(entries); next++ {
				candidate := entries[next]
				if candidate.Timestamp-entry.Timestamp > int64(24*time.Hour/time.Second) {
					break
				}
				if candidate.structurallyValid && candidate.TitleCount > entry.TitleCount*2 {
					rapidGrowthRule.Passed = false
					rapidGrowthRule.Detail = fmt.Sprintf("following snapshot %s contains %d titles", candidate.Filename, candidate.TitleCount)
					if entry.Classification != auditExcluded {
						entry.Classification = auditExcluded
						entry.Reason = reasonImplausibleSnapshot
					}
					break
				}
			}
		}
		entry.Rules = append(entry.Rules, rapidGrowthRule)
	}

	previousObservedCount := 0
	havePreviousObservation := false
	for index := range entries {
		entry := &entries[index]
		regressionRule := auditRuleResult{Rule: "title_count_regression", Applicable: entry.structurallyValid && havePreviousObservation, Passed: true}
		if regressionRule.Applicable && entry.TitleCount < previousObservedCount {
			regressionRule.Passed = false
			regressionRule.Detail = fmt.Sprintf("title count decreased from %d to %d", previousObservedCount, entry.TitleCount)
			if entry.Classification != auditExcluded {
				entry.Classification = auditExcluded
				entry.Reason = reasonLibraryRegression
			}
		}
		entry.Rules = append(entry.Rules, regressionRule)
		if entry.structurallyValid {
			previousObservedCount = entry.TitleCount
			havePreviousObservation = true
		}
		if entry.Classification == "" {
			entry.Classification = auditAccepted
		}
	}

	manifest := corpusAuditManifest{Version: 1, GeneratedAt: generatedAt.UTC(), Entries: entries}
	for _, entry := range entries {
		if entry.Classification == auditAccepted {
			manifest.AcceptedCount++
		} else {
			manifest.ExcludedCount++
		}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return corpusAuditManifest{}, fmt.Errorf("encode corpus audit manifest: %w", err)
	}
	if err := writeFileAtomically(manifestPath, data, 0o600); err != nil {
		return corpusAuditManifest{}, fmt.Errorf("persist corpus audit manifest: %w", err)
	}
	return manifest, nil
}

func readCorpusEntries(dir string) ([]corpusAuditEntry, error) {
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read snapshot corpus: %w", err)
	}
	var entries []corpusAuditEntry
	for _, file := range files {
		if file.IsDir() || !strings.HasPrefix(file.Name(), "output_") || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		timestampText := strings.TrimSuffix(strings.TrimPrefix(file.Name(), "output_"), ".json")
		timestamp, parseErr := strconv.ParseInt(timestampText, 10, 64)
		entry := corpusAuditEntry{Filename: file.Name(), Timestamp: timestamp}
		data, readErr := os.ReadFile(filepath.Join(dir, file.Name()))
		if parseErr == nil && readErr == nil {
			var object map[string]json.RawMessage
			if json.Unmarshal(data, &object) == nil {
				if rawTitles, exists := object["titles"]; exists {
					var titles []json.RawMessage
					if json.Unmarshal(rawTitles, &titles) == nil {
						entry.TitleCount = len(titles)
						entry.structurallyValid = validateTitles(titles) == nil
					}
				}
			}
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Timestamp == entries[j].Timestamp {
			return entries[i].Filename < entries[j].Filename
		}
		return entries[i].Timestamp < entries[j].Timestamp
	})
	return entries, nil
}

func loadCorpusAuditManifest(path string) (corpusAuditManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return corpusAuditManifest{}, err
	}
	var manifest corpusAuditManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return corpusAuditManifest{}, fmt.Errorf("decode corpus audit manifest: %w", err)
	}
	if manifest.Version != 1 {
		return corpusAuditManifest{}, fmt.Errorf("unsupported corpus audit manifest version %d", manifest.Version)
	}
	return manifest, nil
}

func auditExclusions(path string) (map[string]failureReason, error) {
	manifest, err := loadCorpusAuditManifest(path)
	if err != nil {
		return nil, err
	}
	excluded := make(map[string]failureReason, manifest.ExcludedCount)
	for _, entry := range manifest.Entries {
		if entry.Classification == auditExcluded {
			excluded[entry.Filename] = entry.Reason
		}
	}
	return excluded, nil
}

func ensureCorpusAudit(dir, manifestPath string, generatedAt time.Time) (corpusAuditManifest, bool, error) {
	manifest, err := loadCorpusAuditManifest(manifestPath)
	if err == nil {
		return manifest, false, nil
	}
	if !os.IsNotExist(err) {
		return corpusAuditManifest{}, false, err
	}
	manifest, err = auditCorpus(dir, manifestPath, generatedAt)
	return manifest, true, err
}
