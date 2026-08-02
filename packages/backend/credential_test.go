package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCredentialPrefersPersistedValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "npsso.json")
	writeCredentialFixture(t, path, "persisted-value")

	got := resolveCredential(path, "environment-value")

	if got.State != credentialReady {
		t.Fatalf("state = %q, want %q (reason: %s)", got.State, credentialReady, got.Reason)
	}
	if got.Value != "persisted-value" {
		t.Fatalf("value = %q, want persisted value", got.Value)
	}
}

func TestResolveCredentialBootstrapsOnlyWhenPersistenceIsAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "npsso.json")

	got := resolveCredential(path, "bootstrap-value")

	if got.State != credentialReady || got.Value != "bootstrap-value" {
		t.Fatalf("resolution = %#v, want ready bootstrap credential", got)
	}
	reloaded := resolveCredential(path, "older-environment-value")
	if reloaded.State != credentialReady || reloaded.Value != "bootstrap-value" {
		t.Fatalf("reloaded resolution = %#v, want persisted bootstrap credential", reloaded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestResolveCredentialDoesNotOverwriteDamagedPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "npsso.json")
	damaged := []byte(`{"npsso":`)
	if err := os.WriteFile(path, damaged, 0o600); err != nil {
		t.Fatal(err)
	}

	got := resolveCredential(path, "environment-value")

	if got.State != credentialDamaged {
		t.Fatalf("state = %q, want %q", got.State, credentialDamaged)
	}
	if got.Value != "" {
		t.Fatalf("damaged persisted state activated credential %q", got.Value)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, damaged) {
		t.Fatalf("damaged evidence was overwritten: got %q", after)
	}
}

func TestUpdateNPSSOPersistsBeforeActivatingCredential(t *testing.T) {
	dir := t.TempDir()
	oldNPSSOFile, oldTokenFile, oldState := npssoFile, tokenFile, state
	t.Cleanup(func() {
		npssoFile, tokenFile, state = oldNPSSOFile, oldTokenFile, oldState
	})
	npssoFile = filepath.Join(dir, "npsso.json")
	tokenFile = filepath.Join(dir, "token.json")
	state = &appState{}
	state.setCredential(credentialResolution{Value: "old-value", State: credentialReady})
	if err := os.WriteFile(tokenFile, []byte(`{"access_token":"cached"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/update-npsso", bytes.NewBufferString(`{"npsso":"new-value"}`))
	recorder := httptest.NewRecorder()
	handleUpdateNPSSO(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", recorder.Code, recorder.Body.String())
	}
	if got := state.getNPSSO(); got != "new-value" {
		t.Fatalf("active credential = %q, want new value", got)
	}
	reloaded := resolveCredential(npssoFile, "older-environment-value")
	if reloaded.State != credentialReady || reloaded.Value != "new-value" {
		t.Fatalf("reloaded resolution = %#v, want committed update", reloaded)
	}
	if _, err := os.Stat(tokenFile); !os.IsNotExist(err) {
		t.Fatalf("cached token still exists after update: %v", err)
	}
}

func TestUpdateNPSSORetainsActiveCredentialWhenPersistenceFails(t *testing.T) {
	dir := t.TempDir()
	oldNPSSOFile, oldTokenFile, oldState := npssoFile, tokenFile, state
	t.Cleanup(func() {
		npssoFile, tokenFile, state = oldNPSSOFile, oldTokenFile, oldState
	})
	npssoFile = filepath.Join(dir, "directory-not-file")
	if err := os.Mkdir(npssoFile, 0o700); err != nil {
		t.Fatal(err)
	}
	tokenFile = filepath.Join(dir, "token.json")
	state = &appState{}
	state.setCredential(credentialResolution{Value: "old-value", State: credentialReady})
	if err := os.WriteFile(tokenFile, []byte(`{"access_token":"cached"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/update-npsso", bytes.NewBufferString(`{"npsso":"new-value"}`))
	recorder := httptest.NewRecorder()
	handleUpdateNPSSO(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if got := state.getNPSSO(); got != "old-value" {
		t.Fatalf("active credential = %q, want retained old value", got)
	}
	if _, err := os.Stat(tokenFile); err != nil {
		t.Fatalf("cached token was invalidated after failed persistence: %v", err)
	}
}

func TestUpdateNPSSORejectsWhitespaceOnlyCredential(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/update-npsso", bytes.NewBufferString(`{"npsso":"   "}`))
	recorder := httptest.NewRecorder()

	handleUpdateNPSSO(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func writeCredentialFixture(t *testing.T, path, value string) {
	t.Helper()
	data, err := json.Marshal(persistedCredential{NPSSO: value})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
