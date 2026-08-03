package main

import (
	"strings"
	"testing"
	"time"
)

func TestAlertRendersP1Notification(t *testing.T) {
	detectedAt := time.Date(2026, time.August, 3, 9, 30, 0, 0, time.UTC)
	alert := Alert{
		Condition:  alertConditionCredentialExpired,
		DetectedAt: detectedAt,
		Severity:   alertSeverityP1,
		Subject:    "Persisted credential",
		Verb:       "expired",
		Impact:     "Scheduled collection stopped",
		Recovery:   alertRecoveryNone,
		Action:     "REPLACE the persisted credential",
		RunbookURL: "http://psn.rx1.uk/runbook/credential/",
	}

	got, err := alert.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	want := "P1 ACT NOW psn: Persisted credential expired.\n" +
		"IMPACT: Scheduled collection stopped.\n" +
		"RECOVERY: NONE\n" +
		"ACTION: REPLACE the persisted credential.\n" +
		"RUNBOOK: http://psn.rx1.uk/runbook/credential/"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
	if got := alert.SuppressionKey(); got != "credential_expired:P1" {
		t.Fatalf("SuppressionKey() = %q, want %q", got, "credential_expired:P1")
	}
}

func TestAlertRendersP2AndP3Notifications(t *testing.T) {
	detectedAt := time.Date(2026, time.August, 3, 9, 30, 0, 0, time.UTC)
	actBy := detectedAt.Add(48 * time.Hour)
	until := detectedAt.Add(72 * time.Hour)
	tests := []struct {
		name  string
		alert Alert
		want  string
	}{
		{
			name: "P2 NONE carries a real action deadline",
			alert: Alert{
				Condition:  alertConditionCandidateQuarantined,
				DetectedAt: detectedAt,
				Severity:   alertSeverityP2,
				Subject:    "Fetched candidate",
				Verb:       "entered",
				Object:     "quarantine",
				Impact:     "Analytics retained the newest valid snapshot",
				Recovery:   alertRecoveryNone,
				Action:     "EXAMINE the quarantined candidate",
				ActBy:      &actBy,
				RunbookURL: "http://psn.rx1.uk/runbook/quarantined-candidate/",
			},
			want: "P2 ACT BY 2026-08-05 psn: Fetched candidate entered quarantine.\n" +
				"IMPACT: Analytics retained the newest valid snapshot.\n" +
				"RECOVERY: NONE\n" +
				"ACTION: EXAMINE the quarantined candidate.\n" +
				"RUNBOOK: http://psn.rx1.uk/runbook/quarantined-candidate/",
		},
		{
			name: "P2 SELF-LIMITED carries an action deadline and recovery time",
			alert: Alert{
				Condition:  alertConditionCapacityPressure,
				DetectedAt: detectedAt,
				Severity:   alertSeverityP2,
				Subject:    "Storage usage",
				Verb:       "reached",
				Object:     "91%",
				Impact:     "Snapshot storage retains 9% capacity remaining",
				Recovery:   alertRecoverySelfLimited,
				Until:      &until,
				Action:     "EXAMINE storage growth",
				ActBy:      &actBy,
				RunbookURL: "http://psn.rx1.uk/runbook/capacity-pressure/",
			},
			want: "P2 ACT BY 2026-08-05 psn: Storage usage reached 91%.\n" +
				"IMPACT: Snapshot storage retains 9% capacity remaining.\n" +
				"RECOVERY: SELF-LIMITED until 2026-08-06T09:30:00Z\n" +
				"ACTION: EXAMINE storage growth.\n" +
				"RUNBOOK: http://psn.rx1.uk/runbook/capacity-pressure/",
		},
		{
			name: "P3 SELF renders no action",
			alert: Alert{
				Condition:  alertConditionFetchFailedOnce,
				DetectedAt: detectedAt,
				Severity:   alertSeverityP3,
				Subject:    "Scheduled fetch",
				Verb:       "failed",
				Impact:     "Analytics retained the newest valid snapshot",
				Recovery:   alertRecoverySelf,
				RunbookURL: "http://psn.rx1.uk/runbook/fetch-failure/",
			},
			want: "P3 NOTE psn: Scheduled fetch failed.\n" +
				"IMPACT: Analytics retained the newest valid snapshot.\n" +
				"RECOVERY: SELF\n" +
				"ACTION: NONE\n" +
				"RUNBOOK: http://psn.rx1.uk/runbook/fetch-failure/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.alert.Render()
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAlertConditionsEnforceTheirSeverityAndRecovery(t *testing.T) {
	tests := []struct {
		condition AlertCondition
		severity  AlertSeverity
		recovery  AlertRecovery
		runbook   string
	}{
		{alertConditionCredentialExpired, alertSeverityP1, alertRecoveryNone, "credential"},
		{alertConditionCredentialExpiring, alertSeverityP2, alertRecoveryNone, "credential"},
		{alertConditionFetchFailedOnce, alertSeverityP3, alertRecoverySelf, "fetch-failure"},
		{alertConditionFetchFailedThreeTimes, alertSeverityP1, alertRecoveryNone, "fetch-failure"},
		{alertConditionCandidateQuarantined, alertSeverityP2, alertRecoveryNone, "quarantined-candidate"},
		{alertConditionCrashLoop, alertSeverityP1, alertRecoveryNone, "crash-loop"},
		{alertConditionLivenessMissed, alertSeverityP1, alertRecoveryNone, "liveness-heartbeat"},
		{alertConditionDailySuccessMissed, alertSeverityP1, alertRecoveryNone, "daily-success-heartbeat"},
		{alertConditionPublicProbeFailed, alertSeverityP1, alertRecoveryNone, "public-probe"},
		{alertConditionCapacityPressure, alertSeverityP2, alertRecoverySelfLimited, "capacity-pressure"},
	}

	for _, tt := range tests {
		t.Run(string(tt.condition), func(t *testing.T) {
			alert := validAlertFixture(tt.condition, tt.severity, tt.recovery, tt.runbook)
			if err := alert.Validate(); err != nil {
				t.Fatalf("valid alert rejected: %v", err)
			}

			alert.Severity = alertSeverityP3
			if tt.severity == alertSeverityP3 {
				alert.Severity = alertSeverityP1
			}
			if err := alert.Validate(); err == nil {
				t.Fatal("alert with wrong severity accepted")
			}

			alert = validAlertFixture(tt.condition, tt.severity, tt.recovery, tt.runbook)
			if tt.recovery == alertRecoverySelf {
				alert.Recovery = alertRecoveryNone
				alert.Action = "EXAMINE the recorded evidence"
			} else {
				alert.Recovery = alertRecoverySelf
				alert.Action = ""
				alert.Until = nil
			}
			if err := alert.Validate(); err == nil {
				t.Fatal("alert with wrong recovery accepted")
			}
		})
	}
}

func TestAlertSeverityMapsToStandardScales(t *testing.T) {
	tests := []struct {
		severity AlertSeverity
		syslog   int
		otel     string
	}{
		{alertSeverityP1, 2, "ERROR"},
		{alertSeverityP2, 4, "WARN"},
		{alertSeverityP3, 6, "INFO"},
	}
	for _, test := range tests {
		t.Run(string(test.severity), func(t *testing.T) {
			if got := test.severity.SyslogLevel(); got != test.syslog {
				t.Fatalf("SyslogLevel() = %d, want %d", got, test.syslog)
			}
			if got := test.severity.OTelLevel(); got != test.otel {
				t.Fatalf("OTelLevel() = %q, want %q", got, test.otel)
			}
		})
	}
}

func validAlertFixture(condition AlertCondition, severity AlertSeverity, recovery AlertRecovery, runbook string) Alert {
	detectedAt := time.Date(2026, time.August, 3, 9, 30, 0, 0, time.UTC)
	actBy := detectedAt.Add(48 * time.Hour)
	until := detectedAt.Add(72 * time.Hour)
	alert := Alert{
		Condition:  condition,
		DetectedAt: detectedAt,
		Severity:   severity,
		Subject:    "Collection service",
		Verb:       "reported",
		Object:     "failure",
		Impact:     "Analytics retained the newest valid snapshot",
		Recovery:   recovery,
		RunbookURL: "http://psn.rx1.uk/runbook/" + runbook + "/",
	}
	if recovery != alertRecoverySelf {
		alert.Action = "EXAMINE the recorded evidence"
	}
	if severity == alertSeverityP2 {
		alert.ActBy = &actBy
	}
	if recovery == alertRecoverySelfLimited {
		alert.Until = &until
	}
	return alert
}

func TestAlertRejectsInvalidStructureAndLanguage(t *testing.T) {
	base := validAlertFixture(alertConditionCandidateQuarantined, alertSeverityP2, alertRecoveryNone, "quarantined-candidate")
	nonUTC := time.Date(2026, time.August, 3, 10, 30, 0, 0, time.FixedZone("BST", 3600))
	tooSoon := base.DetectedAt.Add(24 * time.Hour)
	tests := []struct {
		name   string
		mutate func(*Alert)
	}{
		{"unknown condition", func(alert *Alert) { alert.Condition = "unknown" }},
		{"missing detected time", func(alert *Alert) { alert.DetectedAt = time.Time{} }},
		{"detected time outside UTC", func(alert *Alert) { alert.DetectedAt = nonUTC }},
		{"missing subject", func(alert *Alert) { alert.Subject = "" }},
		{"missing verb", func(alert *Alert) { alert.Verb = "" }},
		{"missing impact", func(alert *Alert) { alert.Impact = "" }},
		{"headline adds another sentence", func(alert *Alert) { alert.Object = "quarantine. It persisted" }},
		{"impact adds another sentence", func(alert *Alert) { alert.Impact = "History stayed valid. Collection stopped" }},
		{"NONE omits action", func(alert *Alert) { alert.Action = "" }},
		{"P2 omits action deadline", func(alert *Alert) { alert.ActBy = nil }},
		{"P2 deadline is not more than 24 hours away", func(alert *Alert) { alert.ActBy = &tooSoon }},
		{"P2 deadline outside UTC", func(alert *Alert) { alert.ActBy = &nonUTC }},
		{"runbook belongs to another condition", func(alert *Alert) { alert.RunbookURL = "http://psn.rx1.uk/runbook/credential/" }},
		{"action contains two clauses", func(alert *Alert) { alert.Action = "COPY the evidence, then POST the credential" }},
		{"action joins two instructions", func(alert *Alert) { alert.Action = "EXAMINE the evidence and RESTART the service" }},
		{"action is advisory", func(alert *Alert) { alert.Action = "You should EXAMINE the evidence" }},
		{"action is not imperative", func(alert *Alert) { alert.Action = "The operator examines the evidence" }},
		{"action starts with a noun", func(alert *Alert) { alert.Action = "Evidence requires review" }},
		{"action offers alternatives", func(alert *Alert) { alert.Action = "EXAMINE logs or RESTART service" }},
		{"quantity omits units", func(alert *Alert) { alert.Impact = "Snapshot count fell to 12" }},
		{"headline exceeds 20 words", func(alert *Alert) {
			alert.Subject = strings.Repeat("word ", 19)
			alert.Verb = "stopped"
			alert.Object = "collection"
		}},
		{"impact exceeds 20 words", func(alert *Alert) { alert.Impact = strings.TrimSpace(strings.Repeat("word ", 21)) }},
		{"action exceeds 20 words", func(alert *Alert) { alert.Action = "EXAMINE " + strings.TrimSpace(strings.Repeat("evidence ", 20)) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alert := base
			tt.mutate(&alert)
			if err := alert.Validate(); err == nil {
				t.Fatal("Validate() accepted invalid alert")
			}
		})
	}
}

func TestAlertRejectsTicketBannedVocabulary(t *testing.T) {
	banned := []string{
		"CHECK the recorded evidence",
		"VERIFY the recorded evidence",
		"CONFIRM the recorded evidence",
		"ENSURE the recorded evidence remains",
		"VALIDATE the recorded evidence",
		"EXAMINE the broken fetch",
		"EXAMINE the service down state",
		"The fetch may recover",
		"The fetch might recover",
		"The fetch appears to recover",
		"EXAMINE the issue",
		"EXAMINE the problem",
		"Something went wrong",
		"RETRY the fetch",
		"CONSIDER replacing the credential",
		"EXAMINE the evidence soon",
		"EXAMINE recently recorded evidence",
		"Collection will stop",
	}

	for _, phrase := range banned {
		t.Run(phrase, func(t *testing.T) {
			alert := validAlertFixture(alertConditionCredentialExpired, alertSeverityP1, alertRecoveryNone, "credential")
			alert.Action = phrase
			if err := alert.Validate(); err == nil {
				t.Fatal("Validate() accepted banned vocabulary")
			}
		})
	}
}

func TestAlertAcceptsTwentyWordsAndAbsoluteQuantities(t *testing.T) {
	alert := validAlertFixture(alertConditionCandidateQuarantined, alertSeverityP2, alertRecoveryNone, "quarantined-candidate")
	alert.Impact = "Snapshot history retained 472 files"
	alert.Action = "EXAMINE " + strings.TrimSpace(strings.Repeat("evidence ", 19))

	if err := alert.Validate(); err != nil {
		t.Fatalf("Validate() rejected 20-word action with an absolute quantity: %v", err)
	}
}

func TestAlertEnforcesRecoveryTimeEquivalences(t *testing.T) {
	detectedAt := time.Date(2026, time.August, 3, 9, 30, 0, 0, time.UTC)
	beforeDetection := detectedAt.Add(-time.Minute)
	nonUTC := time.Date(2026, time.August, 6, 10, 30, 0, 0, time.FixedZone("BST", 3600))
	tests := []struct {
		name   string
		alert  Alert
		mutate func(*Alert)
	}{
		{
			name:   "SELF carries an action",
			alert:  validAlertFixture(alertConditionFetchFailedOnce, alertSeverityP3, alertRecoverySelf, "fetch-failure"),
			mutate: func(alert *Alert) { alert.Action = "EXAMINE the failed fetch" },
		},
		{
			name:   "SELF carries an until time",
			alert:  validAlertFixture(alertConditionFetchFailedOnce, alertSeverityP3, alertRecoverySelf, "fetch-failure"),
			mutate: func(alert *Alert) { alert.Until = &beforeDetection },
		},
		{
			name:   "SELF-LIMITED omits until time",
			alert:  validAlertFixture(alertConditionCapacityPressure, alertSeverityP2, alertRecoverySelfLimited, "capacity-pressure"),
			mutate: func(alert *Alert) { alert.Until = nil },
		},
		{
			name:   "SELF-LIMITED ends before detection",
			alert:  validAlertFixture(alertConditionCapacityPressure, alertSeverityP2, alertRecoverySelfLimited, "capacity-pressure"),
			mutate: func(alert *Alert) { alert.Until = &beforeDetection },
		},
		{
			name:   "SELF-LIMITED until time outside UTC",
			alert:  validAlertFixture(alertConditionCapacityPressure, alertSeverityP2, alertRecoverySelfLimited, "capacity-pressure"),
			mutate: func(alert *Alert) { alert.Until = &nonUTC },
		},
		{
			name:   "P1 carries an action deadline",
			alert:  validAlertFixture(alertConditionCredentialExpired, alertSeverityP1, alertRecoveryNone, "credential"),
			mutate: func(alert *Alert) { alert.ActBy = &detectedAt },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alert := tt.alert
			tt.mutate(&alert)
			if err := alert.Validate(); err == nil {
				t.Fatal("Validate() accepted an invalid recovery time combination")
			}
		})
	}
}
