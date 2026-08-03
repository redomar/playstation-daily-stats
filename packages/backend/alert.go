package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const maxAlertSentenceWords = 20

type AlertCondition string

const (
	alertConditionCredentialExpired     AlertCondition = "credential_expired"
	alertConditionCredentialExpiring    AlertCondition = "credential_expiring"
	alertConditionFetchFailedOnce       AlertCondition = "fetch_failed_once"
	alertConditionFetchFailedThreeTimes AlertCondition = "fetch_failed_three_times"
	alertConditionCandidateQuarantined  AlertCondition = "candidate_quarantined"
	alertConditionCrashLoop             AlertCondition = "crash_loop"
	alertConditionLivenessMissed        AlertCondition = "liveness_missed"
	alertConditionDailySuccessMissed    AlertCondition = "daily_success_missed"
	alertConditionPublicProbeFailed     AlertCondition = "public_probe_failed"
	alertConditionCapacityPressure      AlertCondition = "capacity_pressure"
)

type AlertSeverity string

const (
	alertSeverityP1 AlertSeverity = "P1"
	alertSeverityP2 AlertSeverity = "P2"
	alertSeverityP3 AlertSeverity = "P3"
)

func (severity AlertSeverity) SyslogLevel() int {
	switch severity {
	case alertSeverityP1:
		return 2
	case alertSeverityP2:
		return 4
	case alertSeverityP3:
		return 6
	default:
		return 0
	}
}

func (severity AlertSeverity) OTelLevel() string {
	switch severity {
	case alertSeverityP1:
		return "ERROR"
	case alertSeverityP2:
		return "WARN"
	case alertSeverityP3:
		return "INFO"
	default:
		return ""
	}
}

type AlertRecovery string

const (
	alertRecoverySelf        AlertRecovery = "SELF"
	alertRecoverySelfLimited AlertRecovery = "SELF-LIMITED"
	alertRecoveryNone        AlertRecovery = "NONE"
)

type Alert struct {
	Condition  AlertCondition
	DetectedAt time.Time
	Severity   AlertSeverity
	Subject    string
	Verb       string
	Object     string
	Impact     string
	Recovery   AlertRecovery
	Until      *time.Time
	Action     string
	ActBy      *time.Time
	RunbookURL string
}

func (alert Alert) Validate() error {
	spec, ok := alertConditionContracts[alert.Condition]
	if !ok {
		return fmt.Errorf("unsupported alert condition %q", alert.Condition)
	}
	if alert.DetectedAt.IsZero() {
		return errors.New("detected time is required")
	}
	if !isAlertUTCTime(alert.DetectedAt) {
		return errors.New("detected time must use UTC")
	}
	if alert.Severity != spec.severity || alert.Recovery != spec.recovery {
		return fmt.Errorf("%s requires %s severity and %s recovery", alert.Condition, spec.severity, spec.recovery)
	}
	if alert.Subject == "" || alert.Verb == "" {
		return errors.New("alert headline requires a subject and verb")
	}
	if alert.Impact == "" {
		return errors.New("alert impact is required")
	}
	for label, value := range map[string]string{
		"subject": alert.Subject,
		"verb":    alert.Verb,
		"object":  alert.Object,
		"impact":  alert.Impact,
	} {
		if value == "" && label == "object" {
			continue
		}
		if err := validateAlertProse(label, value); err != nil {
			return err
		}
	}
	if len(strings.Fields(strings.Join(nonemptyAlertParts(alert.Subject, alert.Verb, alert.Object), " "))) > maxAlertSentenceWords {
		return fmt.Errorf("alert headline exceeds %d words", maxAlertSentenceWords)
	}
	hasAction := strings.TrimSpace(alert.Action) != ""
	if alert.Recovery == alertRecoverySelf && hasAction {
		return errors.New("SELF recovery requires ACTION NONE")
	}
	if alert.Recovery != alertRecoverySelf && !hasAction {
		return fmt.Errorf("%s recovery requires an action", alert.Recovery)
	}
	if hasAction {
		if err := validateAlertAction(alert.Action); err != nil {
			return err
		}
	}
	if alert.Recovery == alertRecoverySelfLimited {
		if alert.Until == nil || alert.Until.IsZero() {
			return errors.New("SELF-LIMITED recovery requires an until time")
		}
		if !isAlertUTCTime(*alert.Until) {
			return errors.New("SELF-LIMITED until time must use UTC")
		}
		if !alert.Until.After(alert.DetectedAt) {
			return errors.New("SELF-LIMITED until time must follow detection")
		}
	} else if alert.Until != nil {
		return fmt.Errorf("%s recovery cannot carry an until time", alert.Recovery)
	}
	if alert.Severity == alertSeverityP2 {
		if alert.ActBy == nil || alert.ActBy.IsZero() {
			return errors.New("P2 requires an action deadline")
		}
		if !isAlertUTCTime(*alert.ActBy) {
			return errors.New("P2 action deadline must use UTC")
		}
		if !alert.ActBy.After(alert.DetectedAt.Add(24 * time.Hour)) {
			return errors.New("P2 action deadline must be more than 24 hours after detection")
		}
	} else if alert.ActBy != nil {
		return fmt.Errorf("%s cannot carry an action deadline", alert.Severity)
	}
	if alert.RunbookURL != "http://psn.rx1.uk/runbook/"+spec.runbook+"/" {
		return errors.New("alert runbook does not match its condition")
	}
	return nil
}

func (alert Alert) Render() (string, error) {
	if err := alert.Validate(); err != nil {
		return "", err
	}
	headline := strings.Join(nonemptyAlertParts(alert.Subject, alert.Verb, alert.Object), " ")
	recovery := string(alert.Recovery)
	if alert.Until != nil {
		recovery += " until " + alert.Until.UTC().Format(time.RFC3339)
	}
	severity := string(alert.Severity)
	switch alert.Severity {
	case alertSeverityP1:
		severity += " ACT NOW"
	case alertSeverityP2:
		severity += " ACT BY " + alert.ActBy.UTC().Format(time.DateOnly)
	case alertSeverityP3:
		severity += " NOTE"
	}
	action := alert.Action
	if action == "" {
		action = "NONE"
	} else {
		action += "."
	}
	return fmt.Sprintf("%s psn: %s.\nIMPACT: %s.\nRECOVERY: %s\nACTION: %s\nRUNBOOK: %s",
		severity,
		headline,
		alert.Impact,
		recovery,
		action,
		alert.RunbookURL,
	), nil
}

func (alert Alert) SuppressionKey() string {
	return string(alert.Condition) + ":" + string(alert.Severity)
}

func nonemptyAlertParts(parts ...string) []string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func isAlertUTCTime(value time.Time) bool {
	_, offset := value.Zone()
	return offset == 0
}

func validateAlertProse(label, value string) error {
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("alert %s cannot have surrounding whitespace", label)
	}
	if strings.ContainsAny(value, ".?!;\r\n") {
		return fmt.Errorf("alert %s must be one sentence fragment", label)
	}
	if len(strings.Fields(value)) > maxAlertSentenceWords {
		return fmt.Errorf("alert %s exceeds %d words", label, maxAlertSentenceWords)
	}
	if banned := bannedAlertVocabulary(value, false); banned != "" {
		return fmt.Errorf("alert %s contains banned vocabulary %q", label, banned)
	}
	if err := validateAlertQuantities(value); err != nil {
		return fmt.Errorf("alert %s: %w", label, err)
	}
	return nil
}

func validateAlertAction(value string) error {
	if err := validateAlertProse("action", value); err != nil {
		return err
	}
	if banned := bannedAlertVocabulary(value, true); banned != "" {
		return fmt.Errorf("alert action contains banned vocabulary %q", banned)
	}
	words := alertWords(value)
	if len(words) == 0 {
		return errors.New("alert action is required")
	}
	imperatives := map[string]struct{}{
		"do": {}, "examine": {}, "make": {}, "replace": {},
	}
	if _, ok := imperatives[words[0]]; !ok {
		return errors.New("alert action must start with an imperative verb")
	}
	for _, connector := range []string{"also", "and", "or", "plus", "then"} {
		for _, word := range words[1:] {
			if word == connector {
				return errors.New("alert action must contain one instruction")
			}
		}
	}
	for _, word := range words[1:] {
		if _, secondImperative := imperatives[word]; secondImperative {
			return errors.New("alert action must contain one instruction")
		}
	}
	if strings.ContainsAny(value, ",:") {
		return errors.New("alert action must contain one instruction")
	}
	return nil
}

func bannedAlertVocabulary(value string, action bool) string {
	words := alertWords(value)
	joined := " " + strings.Join(words, " ") + " "
	for _, phrase := range []string{" appears to ", " may want ", " something went wrong "} {
		if strings.Contains(joined, phrase) {
			return strings.TrimSpace(phrase)
		}
	}
	banned := map[string]struct{}{
		"am": {}, "are": {}, "be": {}, "been": {}, "being": {}, "broken": {},
		"can": {}, "check": {}, "confirm": {}, "could": {}, "down": {},
		"ensure": {}, "had": {}, "has": {}, "have": {}, "is": {}, "issue": {},
		"may": {}, "might": {}, "problem": {}, "recently": {}, "shall": {},
		"should": {}, "soon": {}, "validate": {}, "verify": {}, "was": {},
		"were": {}, "will": {}, "would": {},
	}
	if action {
		banned["consider"] = struct{}{}
		banned["retry"] = struct{}{}
	}
	for _, word := range words {
		if _, found := banned[word]; found {
			return word
		}
	}
	return ""
}

func alertWords(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '%'
	})
}

func validateAlertQuantities(value string) error {
	runes := []rune(value)
	for index := 0; index < len(runes); index++ {
		if !unicode.IsDigit(runes[index]) {
			continue
		}
		if index > 0 && (unicode.IsLetter(runes[index-1]) || unicode.IsDigit(runes[index-1])) {
			continue
		}
		end := index + 1
		for end < len(runes) && (unicode.IsDigit(runes[end]) || runes[end] == '.') {
			end++
		}
		unit := end
		for unit < len(runes) && unicode.IsSpace(runes[unit]) {
			unit++
		}
		if unit >= len(runes) || (!unicode.IsLetter(runes[unit]) && runes[unit] != '%') {
			return errors.New("absolute quantity requires units")
		}
		index = end - 1
	}
	return nil
}

type alertConditionContract struct {
	severity AlertSeverity
	recovery AlertRecovery
	runbook  string
}

var alertConditionContracts = map[AlertCondition]alertConditionContract{
	alertConditionCredentialExpired:     {alertSeverityP1, alertRecoveryNone, "credential"},
	alertConditionCredentialExpiring:    {alertSeverityP2, alertRecoveryNone, "credential"},
	alertConditionFetchFailedOnce:       {alertSeverityP3, alertRecoverySelf, "fetch-failure"},
	alertConditionFetchFailedThreeTimes: {alertSeverityP1, alertRecoveryNone, "fetch-failure"},
	alertConditionCandidateQuarantined:  {alertSeverityP2, alertRecoveryNone, "quarantined-candidate"},
	alertConditionCrashLoop:             {alertSeverityP1, alertRecoveryNone, "crash-loop"},
	alertConditionLivenessMissed:        {alertSeverityP1, alertRecoveryNone, "liveness-heartbeat"},
	alertConditionDailySuccessMissed:    {alertSeverityP1, alertRecoveryNone, "daily-success-heartbeat"},
	alertConditionPublicProbeFailed:     {alertSeverityP1, alertRecoveryNone, "public-probe"},
	alertConditionCapacityPressure:      {alertSeverityP2, alertRecoverySelfLimited, "capacity-pressure"},
}
