package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type credentialState string

const (
	credentialReady   credentialState = "ready"
	credentialMissing credentialState = "missing"
	credentialDamaged credentialState = "damaged"
)

type persistedCredential struct {
	NPSSO string `json:"npsso"`
}

type credentialResolution struct {
	Value  string
	State  credentialState
	Reason string
}

func resolveCredential(path, bootstrap string) credentialResolution {
	data, err := os.ReadFile(path)
	if err == nil {
		var stored persistedCredential
		if err := json.Unmarshal(data, &stored); err != nil {
			return credentialResolution{State: credentialDamaged, Reason: "malformed_persisted_credential"}
		}
		if strings.TrimSpace(stored.NPSSO) == "" {
			return credentialResolution{State: credentialDamaged, Reason: "empty_persisted_credential"}
		}
		return credentialResolution{Value: stored.NPSSO, State: credentialReady}
	}

	if !errors.Is(err, os.ErrNotExist) {
		return credentialResolution{State: credentialDamaged, Reason: "unreadable_persisted_credential"}
	}
	if strings.TrimSpace(bootstrap) == "" {
		return credentialResolution{State: credentialMissing, Reason: "credential_never_initialized"}
	}
	if err := persistCredential(path, bootstrap); err != nil {
		return credentialResolution{State: credentialMissing, Reason: "bootstrap_persistence_failed"}
	}
	return credentialResolution{Value: bootstrap, State: credentialReady}
}

func persistCredential(path, npsso string) error {
	if strings.TrimSpace(npsso) == "" {
		return errors.New("credential is empty")
	}
	data, err := json.Marshal(persistedCredential{NPSSO: npsso})
	if err != nil {
		return fmt.Errorf("encode credential: %w", err)
	}
	if err := writeFileAtomically(path, data, 0o600); err != nil {
		return fmt.Errorf("persist credential: %w", err)
	}
	return nil
}
