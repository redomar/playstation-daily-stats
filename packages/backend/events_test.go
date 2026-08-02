package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEventStoreDiscardsPartialTailBeforeAppending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte(`{"ts":"partial`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newEventStore(path)
	want := eventRecord{
		Timestamp: time.Date(2026, time.August, 2, 8, 0, 0, 0, time.UTC),
		Kind:      eventKindBoot,
		Message:   "Service started.",
	}
	if err := store.Append(want); err != nil {
		t.Fatal(err)
	}

	got, err := store.Recent(eventQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != eventKindBoot || got[0].Message != want.Message {
		t.Fatalf("events = %#v, want only appended boot", got)
	}
}

func TestEventStoreRecentDiscardsValidJSONWithoutTerminalNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	data, err := json.Marshal(eventRecord{
		Timestamp: time.Date(2026, time.August, 2, 8, 0, 0, 0, time.UTC),
		Kind:      eventKindBoot,
		Message:   "Incomplete write.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := newEventStore(path).Recent(eventQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("events = %#v, want unterminated tail discarded", got)
	}
}

func TestEventStoreAppendsAndReturnsNewestEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store := newEventStore(path)
	firstAt := time.Date(2026, time.August, 2, 8, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(time.Minute)

	if err := store.Append(eventRecord{
		Timestamp: firstAt,
		Kind:      eventKindFetch,
		Outcome:   "failed",
		Reason:    "upstream_timeout",
		Message:   "upstream timed out",
		Fields:    map[string]any{"attempt": 1, "forced": false},
	}); err != nil {
		t.Fatalf("append first event: %v", err)
	}
	if err := store.Append(eventRecord{
		Timestamp: secondAt,
		Kind:      eventKindBoot,
		Message:   "process started",
	}); err != nil {
		t.Fatalf("append second event: %v", err)
	}

	got, err := store.Recent(eventQuery{Limit: 10})
	if err != nil {
		t.Fatalf("read recent events: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("recent events = %d, want 2", len(got))
	}
	if got[0].Kind != eventKindBoot || !got[0].Timestamp.Equal(secondAt) {
		t.Fatalf("newest event = %#v, want boot at %s", got[0], secondAt)
	}
	if got[1].Kind != eventKindFetch || got[1].Outcome != "failed" || got[1].Reason != "upstream_timeout" {
		t.Fatalf("oldest event = %#v, want failed fetch", got[1])
	}
}

func TestEventStoreRecentFiltersByKindAndTimeBeforeApplyingLimit(t *testing.T) {
	store := newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
	start := time.Date(2026, time.August, 2, 8, 0, 0, 0, time.UTC)
	events := []eventRecord{
		{Timestamp: start, Kind: eventKindBoot, Message: "old boot"},
		{Timestamp: start.Add(time.Minute), Kind: eventKindFetch, Message: "fetch"},
		{Timestamp: start.Add(2 * time.Minute), Kind: eventKindBoot, Message: "first matching boot"},
		{Timestamp: start.Add(3 * time.Minute), Kind: eventKindBoot, Message: "newest matching boot"},
		{Timestamp: start.Add(4 * time.Minute), Kind: eventKindFetch, Message: "newest event"},
	}
	for _, event := range events {
		if err := store.Append(event); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}

	got, err := store.Recent(eventQuery{
		Kind:  eventKindBoot,
		Since: start.Add(90 * time.Second),
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("read filtered events: %v", err)
	}
	if len(got) != 1 || got[0].Message != "newest matching boot" {
		t.Fatalf("filtered events = %#v, want newest matching boot only", got)
	}
}

func TestEventStoreRejectsUnsafeEvents(t *testing.T) {
	now := time.Date(2026, time.August, 2, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		event eventRecord
	}{
		{name: "missing timestamp", event: eventRecord{Kind: eventKindBoot}},
		{name: "unknown kind", event: eventRecord{Timestamp: now, Kind: "debug"}},
		{name: "unknown fetch outcome", event: eventRecord{Timestamp: now, Kind: eventKindFetch, Outcome: "completed"}},
		{name: "unknown failure reason", event: eventRecord{Timestamp: now, Kind: eventKindFetch, Outcome: "failed", Reason: "raw_error"}},
		{name: "nested field", event: eventRecord{Timestamp: now, Kind: eventKindFetch, Fields: map[string]any{"response": map[string]string{"status": "bad"}}}},
		{name: "sensitive field", event: eventRecord{Timestamp: now, Kind: eventKindAuth, Fields: map[string]any{"access_token": "secret"}}},
		{name: "response body field", event: eventRecord{Timestamp: now, Kind: eventKindFetch, Fields: map[string]any{"response_body": "bad"}}},
		{name: "host path field", event: eventRecord{Timestamp: now, Kind: eventKindBoot, Fields: map[string]any{"host_path": "/internal"}}},
		{name: "credential field", event: eventRecord{Timestamp: now, Kind: eventKindAuth, Fields: map[string]any{"credential": "opaque"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
			if err := store.Append(tt.event); err == nil {
				t.Fatal("Append() error = nil, want unsafe event rejected")
			}
			got, err := store.Recent(eventQuery{Limit: 10})
			if err != nil {
				t.Fatalf("read events after rejection: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("events after rejection = %#v, want none", got)
			}
		})
	}
}

func TestEventStoreSanitizesAndBoundsPersistedText(t *testing.T) {
	store := newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
	if err := store.Append(eventRecord{
		Timestamp: time.Date(2026, time.August, 2, 8, 0, 0, 0, time.UTC),
		Kind:      eventKindFetch,
		Outcome:   "  succeeded\n ",
		Message:   "  process\nstarted\t\x00 ",
		Fields: map[string]any{
			"detail":    strings.Repeat("x", 1100),
			"multiline": "line1\r\nline2",
		},
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	got, err := store.Recent(eventQuery{Limit: 1})
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("recent events = %d, want 1", len(got))
	}
	if got[0].Outcome != "succeeded" || got[0].Message != "process started" {
		t.Fatalf("sanitized event = %#v", got[0])
	}
	if got[0].Fields["multiline"] != "line1 line2" {
		t.Fatalf("sanitized multiline field = %#v, want line1 line2", got[0].Fields["multiline"])
	}
	detail, ok := got[0].Fields["detail"].(string)
	if !ok || len(detail) != 1024 || detail != strings.Repeat("x", 1024) {
		t.Fatalf("bounded detail has type %T and length %d, want string length 1024", got[0].Fields["detail"], len(detail))
	}
}

func TestEventStoreRotatesAtTenMegabytesAndKeepsThreeFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	store := newEventStore(path)
	start := time.Date(2026, time.August, 2, 8, 0, 0, 0, time.UTC)
	appendBoot := func(at time.Time, message string) {
		t.Helper()
		if err := store.Append(eventRecord{Timestamp: at, Kind: eventKindBoot, Message: message}); err != nil {
			t.Fatalf("append %s: %v", message, err)
		}
	}
	fillActive := func() {
		t.Helper()
		if err := os.Truncate(path, maxEventLogBytes); err != nil {
			t.Fatalf("fill active event file: %v", err)
		}
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("open filled event file: %v", err)
		}
		if _, err := file.WriteAt([]byte{'\n'}, maxEventLogBytes-1); err != nil {
			_ = file.Close()
			t.Fatalf("terminate filled event file: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("close filled event file: %v", err)
		}
	}

	appendBoot(start, "oldest")
	fillActive()
	appendBoot(start.Add(time.Minute), "second")
	fillActive()
	appendBoot(start.Add(2*time.Minute), "third")
	fillActive()
	appendBoot(start.Add(3*time.Minute), "newest")

	for _, name := range []string{"events.jsonl", "events.1.jsonl", "events.2.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "events.3.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("events.3.jsonl exists or stat failed with %v; want only three files", err)
	}

	got, err := store.Recent(eventQuery{Kind: eventKindBoot, Limit: 10})
	if err != nil {
		t.Fatalf("read rotated events: %v", err)
	}
	if len(got) != 3 || got[0].Message != "newest" || got[1].Message != "third" || got[2].Message != "second" {
		t.Fatalf("rotated events = %#v, want newest, third, second", got)
	}
}

func TestEventStoreRecentCapsRequestedLimit(t *testing.T) {
	store := newEventStore(filepath.Join(t.TempDir(), "events.jsonl"))
	start := time.Date(2026, time.August, 2, 8, 0, 0, 0, time.UTC)
	for index := 0; index < 1001; index++ {
		if err := store.Append(eventRecord{
			Timestamp: start.Add(time.Duration(index) * time.Second),
			Kind:      eventKindBoot,
			Message:   fmt.Sprintf("boot-%d", index),
		}); err != nil {
			t.Fatalf("append event %d: %v", index, err)
		}
	}

	got, err := store.Recent(eventQuery{Limit: 2000})
	if err != nil {
		t.Fatalf("read recent events: %v", err)
	}
	if len(got) != 1000 {
		t.Fatalf("bounded events length=%d, want 1000", len(got))
	}
	if got[0].Message != "boot-1000" || got[999].Message != "boot-1" {
		t.Fatalf("bounded events first=%q last=%q; want boot-1000 through boot-1", got[0].Message, got[999].Message)
	}
}
