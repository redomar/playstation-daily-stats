# Alerting setup

The service accepts three optional secret URLs. A blank value prevents external network calls locally; an active push alert records the missing ntfy configuration as a failed delivery attempt. Configure real values in the deployment environment; never commit them or paste them into logs, issues, or screenshots.

| Variable | Purpose |
|---|---|
| `NTFY_TOPIC_URL` | Full ntfy topic URL for alerts sent by the application |
| `HEALTHCHECKS_LIVENESS_URL` | healthchecks.io ping URL for service liveness |
| `HEALTHCHECKS_SUCCESS_URL` | healthchecks.io ping URL for succeeded daily collection |

## ntfy

1. Generate a long, random, unguessable topic name. Treat the resulting full topic URL as a secret.
2. Set `NTFY_TOPIC_URL` to that full URL in the backend deployment environment.
3. Subscribe to the same topic in the ntfy phone app and confirm phone notifications are enabled.

The application sends P1 and P2 alerts to this topic. P3 conditions do not produce phone notifications.

## healthchecks.io

Create two independent checks:

| Check | Period | Grace | Application behaviour |
|---|---:|---:|---|
| Service liveness | 15 minutes | 20 minutes | Pinged unconditionally while the service monitor is running |
| Daily collection success | 24 hours | 2 hours | Pinged only after a succeeded fetch durably commits a valid snapshot |

The daily check therefore alerts after 26 hours without a succeeded fetch. A skipped or failed fetch must not ping it.

Copy each check's secret ping URL into `HEALTHCHECKS_LIVENESS_URL` and `HEALTHCHECKS_SUCCESS_URL` respectively. In healthchecks.io, configure both checks to notify the same private ntfy topic used by the phone. These are external dead-man checks: the application does not attempt to diagnose or retry a missed heartbeat itself.

## UptimeRobot

Create an HTTP monitor for the plain public URL:

`http://psn.rx1.uk`

Set the monitoring interval to 5 minutes. This reachability probe is configured entirely in UptimeRobot and requires no application environment variable.
Configure its notification contact to publish into the same private ntfy topic as the application and both healthchecks.io checks.

## External P1 notifications

Configure the two healthchecks.io contacts and the UptimeRobot contact as urgent P1 notifications. Each notification must retain the five-line shape and point to its public runbook:

```text
P1 ACT NOW psn: Liveness heartbeat stopped.
IMPACT: Service execution status became unknown.
RECOVERY: NONE
ACTION: EXAMINE the service runtime.
RUNBOOK: http://psn.rx1.uk/runbook/liveness-heartbeat/
```

```text
P1 ACT NOW psn: Daily success heartbeat stopped.
IMPACT: Scheduled collection stopped.
RECOVERY: NONE
ACTION: EXAMINE the active fetch failure.
RUNBOOK: http://psn.rx1.uk/runbook/daily-success-heartbeat/
```

```text
P1 ACT NOW psn: Public frontend became unreachable.
IMPACT: Public access stopped.
RECOVERY: NONE
ACTION: EXAMINE public routing.
RUNBOOK: http://psn.rx1.uk/runbook/public-probe/
```

Set ntfy priority to `urgent` for all three. Provider-generated metadata can supplement these lines but must not replace the severity, recovery, action, or runbook fields.

## Deployment checklist

- Store all three URL values as backend environment secrets in the deployment platform.
- Keep the long random ntfy topic private and subscribe the phone before relying on it.
- Configure both healthchecks.io checks to notify that same ntfy topic.
- Configure the UptimeRobot public URL reachability probe at a 5-minute interval and route it to the same ntfy topic.
- Leave integrations blank in development unless external notifications are intentionally being tested.
