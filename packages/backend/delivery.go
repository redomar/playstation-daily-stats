package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	deliveryHTTPTimeout = 5 * time.Second
	deliveryRepeatAfter = 24 * time.Hour
)

var (
	errDeliveryEventsUnavailable = errors.New("alert event history is unavailable")
	errNtfyNotConfigured         = errors.New("ntfy delivery is not configured")
	errLivenessNotConfigured     = errors.New("liveness heartbeat is not configured")
	errSuccessNotConfigured      = errors.New("success heartbeat is not configured")
	errDeliveryRequestInvalid    = errors.New("delivery request configuration is invalid")
	errDeliveryRequestFailed     = errors.New("delivery request failed")
	errDeliveryResponseFailed    = errors.New("delivery endpoint rejected the request")
	errDuplicateActiveAlert      = errors.New("active alert set contains a duplicate condition and severity")
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type deliveryConfig struct {
	NtfyTopicURL string
	LivenessURL  string
	SuccessURL   string
}

type deliveryRuntime struct {
	mu     sync.Mutex
	config deliveryConfig
	events *eventStore
	client httpDoer
	now    func() time.Time
}

func newDeliveryRuntime(config deliveryConfig, events *eventStore) *deliveryRuntime {
	return newDeliveryRuntimeWithClient(config, events, &http.Client{Timeout: deliveryHTTPTimeout}, time.Now)
}

func newDeliveryRuntimeWithClient(config deliveryConfig, events *eventStore, client httpDoer, now func() time.Time) *deliveryRuntime {
	return &deliveryRuntime{config: config, events: events, client: client, now: now}
}

// Reconcile records the complete current alert set and synchronously performs
// any delivery that is due. Callers decide which goroutine owns this work.
func (runtime *deliveryRuntime) Reconcile(ctx context.Context, alerts []Alert) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	if runtime.events == nil {
		return errDeliveryEventsUnavailable
	}
	history, err := runtime.readAlertHistory()
	if err != nil {
		return errDeliveryEventsUnavailable
	}
	type preparedAlert struct {
		alert   Alert
		message string
	}
	prepared := make([]preparedAlert, 0, len(alerts))
	current := make(map[string]bool, len(alerts))
	for _, alert := range alerts {
		message, err := alert.Render()
		if err != nil {
			return err
		}
		key := alert.SuppressionKey()
		if current[key] {
			return errDuplicateActiveAlert
		}
		current[key] = true
		prepared = append(prepared, preparedAlert{alert: alert, message: message})
	}
	for key, active := range history.active {
		if !active || current[key] {
			continue
		}
		fields := map[string]any{
			"alert_key":   key,
			"condition":   history.condition[key],
			"severity":    history.severity[key],
			"alert_state": "resolved",
		}
		if err := runtime.events.Append(eventRecord{
			Timestamp: runtime.now().UTC(),
			Kind:      eventKindAlert,
			Message:   "Alert resolved.",
			Fields:    fields,
		}); err != nil {
			return errDeliveryEventsUnavailable
		}
		history.active[key] = false
	}
	var deliveryErrors []error
	for _, item := range prepared {
		alert := item.alert
		message := item.message
		key := alert.SuppressionKey()
		if !history.active[key] {
			if err := runtime.appendAlertEvent(alert, message, map[string]any{
				"alert_state": "active",
			}); err != nil {
				return errDeliveryEventsUnavailable
			}
			history.active[key] = true
			history.episodeStarted[key] = runtime.now().UTC()
			history.detectedAt[key] = alert.DetectedAt
			if alert.ActBy != nil {
				history.actBy[key] = *alert.ActBy
			}
			if alert.Until != nil {
				history.until[key] = *alert.Until
			}
		}
		if alert.Severity == alertSeverityP3 {
			continue
		}
		if attemptedAt, found := history.lastAttempt[key]; found &&
			!attemptedAt.Before(history.episodeStarted[key]) &&
			attemptedAt.After(runtime.now().UTC().Add(-deliveryRepeatAfter)) {
			continue
		}
		if err := runtime.appendAlertEvent(alert, "Alert delivery attempted.", map[string]any{
			"delivery_stage": "attempt",
			"channel":        "ntfy",
		}); err != nil {
			return errDeliveryEventsUnavailable
		}
		history.lastAttempt[key] = runtime.now().UTC()

		priority := "default"
		if alert.Severity == alertSeverityP1 {
			priority = "urgent"
		}
		deliveryAlert := alert
		if detectedAt, found := history.detectedAt[key]; found {
			deliveryAlert.DetectedAt = detectedAt
		}
		if actBy, found := history.actBy[key]; found {
			deliveryAlert.ActBy = &actBy
		}
		if until, found := history.until[key]; found {
			deliveryAlert.Until = &until
		}
		deliveryMessage, err := deliveryAlert.Render()
		if err != nil {
			return err
		}
		deliveryErr := error(nil)
		if strings.TrimSpace(runtime.config.NtfyTopicURL) == "" {
			deliveryErr = errNtfyNotConfigured
		} else {
			deliveryErr = runtime.post(ctx, runtime.config.NtfyTopicURL, deliveryMessage, priority)
		}
		result := "succeeded"
		if deliveryErr != nil {
			result = "failed"
		}
		if err := runtime.appendAlertEvent(alert, "Alert delivery "+result+".", map[string]any{
			"delivery_stage":  "result",
			"delivery_result": result,
			"channel":         "ntfy",
		}); err != nil {
			return errDeliveryEventsUnavailable
		}
		if deliveryErr != nil {
			deliveryErrors = append(deliveryErrors, deliveryErr)
		}
	}
	return errors.Join(deliveryErrors...)
}

func (runtime *deliveryRuntime) PingLiveness(ctx context.Context) error {
	if strings.TrimSpace(runtime.config.LivenessURL) == "" {
		return errLivenessNotConfigured
	}
	return runtime.post(ctx, runtime.config.LivenessURL, "", "")
}

func (runtime *deliveryRuntime) PingSuccess(ctx context.Context) error {
	if strings.TrimSpace(runtime.config.SuccessURL) == "" {
		return errSuccessNotConfigured
	}
	return runtime.post(ctx, runtime.config.SuccessURL, "", "")
}

type alertDeliveryHistory struct {
	active         map[string]bool
	episodeStarted map[string]time.Time
	lastAttempt    map[string]time.Time
	detectedAt     map[string]time.Time
	actBy          map[string]time.Time
	until          map[string]time.Time
	condition      map[string]string
	severity       map[string]string
}

func (runtime *deliveryRuntime) readAlertHistory() (alertDeliveryHistory, error) {
	history := alertDeliveryHistory{
		active:         make(map[string]bool),
		episodeStarted: make(map[string]time.Time),
		lastAttempt:    make(map[string]time.Time),
		detectedAt:     make(map[string]time.Time),
		actBy:          make(map[string]time.Time),
		until:          make(map[string]time.Time),
		condition:      make(map[string]string),
		severity:       make(map[string]string),
	}
	events, err := runtime.events.Recent(eventQuery{Kind: eventKindAlert, Limit: maxRecentLimit})
	if err != nil {
		return history, err
	}
	stateSeen := make(map[string]bool)
	for _, event := range events {
		key, ok := event.Fields["alert_key"].(string)
		if !ok || key == "" {
			continue
		}
		if condition, ok := event.Fields["condition"].(string); ok && condition != "" {
			history.condition[key] = condition
		}
		if severity, ok := event.Fields["severity"].(string); ok && severity != "" {
			history.severity[key] = severity
		}
		if stage, _ := event.Fields["delivery_stage"].(string); stage == "attempt" {
			if _, found := history.lastAttempt[key]; !found {
				history.lastAttempt[key] = event.Timestamp
			}
		}
		state, _ := event.Fields["alert_state"].(string)
		if state == "" || stateSeen[key] {
			continue
		}
		stateSeen[key] = true
		history.active[key] = state == "active"
		history.episodeStarted[key] = event.Timestamp
		if state == "active" {
			if detectedAt, ok := parseAlertEventTime(event.Fields["detected_at"]); ok {
				history.detectedAt[key] = detectedAt
			}
			if actBy, ok := parseAlertEventTime(event.Fields["act_by"]); ok {
				history.actBy[key] = actBy
			}
			if until, ok := parseAlertEventTime(event.Fields["until"]); ok {
				history.until[key] = until
			}
		}
	}
	return history, nil
}

func (runtime *deliveryRuntime) appendAlertEvent(alert Alert, message string, fields map[string]any) error {
	fields["alert_key"] = alert.SuppressionKey()
	fields["condition"] = string(alert.Condition)
	fields["severity"] = string(alert.Severity)
	fields["recovery"] = string(alert.Recovery)
	fields["syslog_severity"] = alert.Severity.SyslogLevel()
	fields["otel_severity"] = alert.Severity.OTelLevel()
	fields["detected_at"] = alert.DetectedAt.UTC().Format(time.RFC3339)
	if alert.ActBy != nil {
		fields["act_by"] = alert.ActBy.UTC().Format(time.RFC3339)
	}
	if alert.Until != nil {
		fields["until"] = alert.Until.UTC().Format(time.RFC3339)
	}
	return runtime.events.Append(eventRecord{
		Timestamp: runtime.now().UTC(),
		Kind:      eventKindAlert,
		Message:   message,
		Fields:    fields,
	})
}

func parseAlertEventTime(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, text)
	return parsed, err == nil
}

func (runtime *deliveryRuntime) post(ctx context.Context, endpoint, body, priority string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return errDeliveryRequestInvalid
	}
	request.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if priority != "" {
		request.Header.Set("Priority", priority)
	}
	response, err := runtime.client.Do(request)
	if err != nil {
		return errDeliveryRequestFailed
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return errDeliveryResponseFailed
	}
	return nil
}
