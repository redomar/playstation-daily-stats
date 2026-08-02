package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxEventLogBytes   = int64(10 * 1024 * 1024)
	retainedEventFiles = 3
	defaultRecentLimit = 100
	maxRecentLimit     = 1000
	maxEventLineBytes  = 64 * 1024
	reverseReadChunk   = 32 * 1024
	maxEventFields     = 32
	maxEventFieldKey   = 64
	maxEventShortText  = 128
	maxEventMessage    = 1024
	maxEventFieldValue = 1024
)

type eventKind string

const (
	eventKindFetch    eventKind = "fetch"
	eventKindAuth     eventKind = "auth"
	eventKindAlert    eventKind = "alert"
	eventKindBoot     eventKind = "boot"
	eventKindCapacity eventKind = "capacity"
)

type eventRecord struct {
	Timestamp time.Time      `json:"ts"`
	Kind      eventKind      `json:"kind"`
	Outcome   fetchOutcome   `json:"outcome,omitempty"`
	Reason    failureReason  `json:"reason,omitempty"`
	Message   string         `json:"msg,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

type eventQuery struct {
	Kind  eventKind
	Since time.Time
	Limit int
}

type eventStore struct {
	mu   sync.Mutex
	path string
}

func newEventStore(path string) *eventStore {
	return &eventStore{path: path}
}

func (s *eventStore) Append(event eventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	event = sanitizeEvent(event)
	if err := validateEvent(event); err != nil {
		return err
	}
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	line = append(line, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create event directory: %w", err)
	}
	if err := discardPartialEventTail(s.path); err != nil {
		return err
	}
	created := false
	info, err := os.Stat(s.path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("event log %s is not a regular file", s.path)
		}
		if info.Size()+int64(len(line)) > maxEventLogBytes {
			if err := s.rotate(); err != nil {
				return err
			}
		}
	} else if errors.Is(err, os.ErrNotExist) {
		created = true
	} else {
		return fmt.Errorf("inspect event log: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("set event log permissions: %w", err)
	}
	if written, err := file.Write(line); err != nil {
		_ = file.Close()
		return fmt.Errorf("append event: %w", err)
	} else if written != len(line) {
		_ = file.Close()
		return fmt.Errorf("append event: %w", io.ErrShortWrite)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("flush event log: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close event log: %w", err)
	}
	if created {
		if err := syncDirectory(filepath.Dir(s.path)); err != nil {
			return fmt.Errorf("flush event directory: %w", err)
		}
	}
	return nil
}

func discardPartialEventTail(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open event log for repair: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("inspect event log for repair: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return fmt.Errorf("event log %s is not a regular file", path)
	}
	if info.Size() == 0 {
		return file.Close()
	}
	last := []byte{0}
	if _, err := file.ReadAt(last, info.Size()-1); err != nil {
		_ = file.Close()
		return fmt.Errorf("inspect event log tail: %w", err)
	}
	if last[0] == '\n' {
		return file.Close()
	}

	truncateAt := int64(0)
	buffer := make([]byte, reverseReadChunk)
	for offset := info.Size(); offset > 0; {
		readSize := int64(len(buffer))
		if offset < readSize {
			readSize = offset
		}
		start := offset - readSize
		n, readErr := file.ReadAt(buffer[:readSize], start)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			_ = file.Close()
			return fmt.Errorf("read event log tail: %w", readErr)
		}
		if index := bytes.LastIndexByte(buffer[:n], '\n'); index >= 0 {
			truncateAt = start + int64(index) + 1
			break
		}
		offset = start
	}
	if err := file.Truncate(truncateAt); err != nil {
		_ = file.Close()
		return fmt.Errorf("discard partial event tail: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("flush repaired event log: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close repaired event log: %w", err)
	}
	return nil
}

func (s *eventStore) rotate() error {
	oldest := rotatedEventPath(s.path, retainedEventFiles-1)
	if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove oldest event log: %w", err)
	}
	for index := retainedEventFiles - 1; index >= 1; index-- {
		source := s.path
		if index > 1 {
			source = rotatedEventPath(s.path, index-1)
		}
		if err := os.Rename(source, rotatedEventPath(s.path, index)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rotate event log: %w", err)
		}
	}
	if err := syncDirectory(filepath.Dir(s.path)); err != nil {
		return fmt.Errorf("flush rotated event directory: %w", err)
	}
	return nil
}

func rotatedEventPath(path string, index int) string {
	extension := filepath.Ext(path)
	base := strings.TrimSuffix(path, extension)
	return fmt.Sprintf("%s.%d%s", base, index, extension)
}

func validateEvent(event eventRecord) error {
	if event.Timestamp.IsZero() {
		return errors.New("event timestamp is required")
	}
	if !validEventKind(event.Kind) {
		return fmt.Errorf("unknown event kind %q", event.Kind)
	}
	if event.Outcome != "" && !validEventOutcome(event.Outcome) {
		return fmt.Errorf("unknown event outcome %q", event.Outcome)
	}
	if event.Reason != "" && !validEventReason(event.Reason) {
		return fmt.Errorf("unknown event reason %q", event.Reason)
	}
	if len(event.Fields) > maxEventFields {
		return fmt.Errorf("event has %d fields; maximum is %d", len(event.Fields), maxEventFields)
	}
	for key, value := range event.Fields {
		if len(key) == 0 || len(key) > maxEventFieldKey {
			return fmt.Errorf("event field key length must be between 1 and %d bytes", maxEventFieldKey)
		}
		for _, r := range key {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
				return fmt.Errorf("event field %q has an invalid key", key)
			}
		}
		if sensitiveEventField(key) {
			return fmt.Errorf("sensitive event field %q is not permitted", key)
		}
		switch value := value.(type) {
		case string, bool,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			json.Number:
		case float32:
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return fmt.Errorf("event field %q has a non-finite number", key)
			}
		case float64:
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("event field %q has a non-finite number", key)
			}
		default:
			return fmt.Errorf("event field %q must be a scalar", key)
		}
	}
	return nil
}

func sanitizeEvent(event eventRecord) eventRecord {
	event.Timestamp = event.Timestamp.UTC()
	event.Outcome = fetchOutcome(sanitizeEventText(string(event.Outcome), maxEventShortText))
	event.Reason = failureReason(sanitizeEventText(string(event.Reason), maxEventShortText))
	event.Message = sanitizeEventText(event.Message, maxEventMessage)
	if len(event.Fields) == 0 {
		return event
	}
	fields := make(map[string]any, len(event.Fields))
	for key, value := range event.Fields {
		if text, ok := value.(string); ok {
			fields[key] = sanitizeEventText(text, maxEventFieldValue)
		} else {
			fields[key] = value
		}
	}
	event.Fields = fields
	return event
}

func sanitizeEventText(value string, maxBytes int) string {
	var sanitized strings.Builder
	sanitized.Grow(min(len(value), maxBytes))
	wantSpace := false
	wrote := false
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			if wrote {
				wantSpace = true
			}
			continue
		}
		runeBytes := utf8.RuneLen(r)
		spaceBytes := 0
		if wantSpace {
			spaceBytes = 1
		}
		if sanitized.Len()+spaceBytes+runeBytes > maxBytes {
			break
		}
		if wantSpace {
			sanitized.WriteByte(' ')
		}
		sanitized.WriteRune(r)
		wrote = true
		wantSpace = false
	}
	return sanitized.String()
}

func sensitiveEventField(key string) bool {
	parts := strings.FieldsFunc(strings.ToLower(key), func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	for _, part := range parts {
		switch part {
		case "npsso", "token", "secret", "password", "authorization", "cookie",
			"credential", "response", "body", "host", "hostname", "path", "address", "url":
			return true
		}
	}
	return false
}

func validEventKind(kind eventKind) bool {
	switch kind {
	case eventKindFetch, eventKindAuth, eventKindAlert, eventKindBoot, eventKindCapacity:
		return true
	default:
		return false
	}
}

func validEventOutcome(outcome fetchOutcome) bool {
	switch outcome {
	case fetchSucceeded, fetchFailed, fetchSkipped:
		return true
	default:
		return false
	}
}

func validEventReason(reason failureReason) bool {
	switch reason {
	case reasonAuthenticationRejected,
		reasonUpstreamTimeout,
		reasonUpstreamStatus,
		reasonInvalidSchema,
		reasonIncompletePagination,
		reasonImplausibleSnapshot,
		reasonLibraryRegression,
		reasonLibraryRegressionPersistent,
		reasonInterruptedFetch,
		reasonStorageFailure:
		return true
	default:
		return false
	}
}

func (s *eventStore) Recent(query eventQuery) ([]eventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	limit := query.Limit
	if limit <= 0 {
		limit = defaultRecentLimit
	} else if limit > maxRecentLimit {
		limit = maxRecentLimit
	}
	events := make([]eventRecord, 0, limit)
	paths := make([]string, 0, retainedEventFiles)
	paths = append(paths, s.path)
	for index := 1; index < retainedEventFiles; index++ {
		paths = append(paths, rotatedEventPath(s.path, index))
	}
	for _, path := range paths {
		file, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("open event log: %w", err)
		}
		err = scanEventLinesReverse(file, func(line []byte) bool {
			if len(bytes.TrimSpace(line)) == 0 {
				return false
			}
			var event eventRecord
			if err := json.Unmarshal(line, &event); err != nil {
				return false
			}
			event = sanitizeEvent(event)
			if validateEvent(event) != nil {
				return false
			}
			if query.Kind != "" && event.Kind != query.Kind {
				return false
			}
			if !query.Since.IsZero() && event.Timestamp.Before(query.Since) {
				return false
			}
			events = append(events, event)
			return len(events) >= limit
		})
		closeErr := file.Close()
		if err != nil {
			return nil, fmt.Errorf("read event log: %w", err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close event log: %w", closeErr)
		}
		if len(events) >= limit {
			break
		}
	}
	return events, nil
}

func scanEventLinesReverse(file *os.File, visit func([]byte) bool) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("event log is not a regular file")
	}
	offset := info.Size()
	buffer := make([]byte, reverseReadChunk)
	var pending []byte
	discarding := false
	if offset > 0 {
		last := []byte{0}
		if _, err := file.ReadAt(last, offset-1); err != nil {
			return err
		}
		discarding = last[0] != '\n'
	}
	for offset > 0 {
		readSize := int64(len(buffer))
		if offset < readSize {
			readSize = offset
		}
		start := offset - readSize
		n, readErr := file.ReadAt(buffer[:readSize], start)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		chunk := buffer[:n]
		end := len(chunk)
		for index := len(chunk) - 1; index >= 0; index-- {
			if chunk[index] != '\n' {
				continue
			}
			if discarding {
				discarding = false
				pending = nil
			} else {
				line := make([]byte, 0, end-index-1+len(pending))
				line = append(line, chunk[index+1:end]...)
				line = append(line, pending...)
				if len(line) <= maxEventLineBytes && visit(line) {
					return nil
				}
				pending = nil
			}
			end = index
		}
		prefix := chunk[:end]
		if !discarding {
			if len(prefix)+len(pending) > maxEventLineBytes {
				discarding = true
				pending = nil
			} else {
				line := make([]byte, 0, len(prefix)+len(pending))
				line = append(line, prefix...)
				line = append(line, pending...)
				pending = line
			}
		}
		offset = start
	}
	if !discarding && len(pending) > 0 {
		visit(pending)
	}
	return nil
}
