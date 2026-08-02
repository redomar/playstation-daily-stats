# PlayStation Daily Stats

Language for describing collection outcomes and the snapshot history that powers the service.

## Fetch outcomes

**Succeeded fetch**:
A fetch attempt that produced a valid snapshot and committed it durably.
_Avoid_: Response received, fetch completed

**Failed fetch**:
A fetch attempt that did not produce and durably commit a valid snapshot.
_Avoid_: Empty day, zero-activity day

**Skipped fetch**:
A scheduled opportunity where policy determined that no fetch attempt was needed. It is neither a success nor a failure and does not change existing failure state.
_Avoid_: Successful fetch, no-op success

**Valid snapshot**:
A complete, structurally valid, and plausible observation of the PlayStation library that may enter the analytics history.
_Avoid_: Parsed response, downloaded data

**Quarantined candidate**:
A fetched payload retained for diagnosis because it failed snapshot validation. It is not part of the analytics history.
_Avoid_: Snapshot, bad snapshot

**Active fetch failure**:
The unresolved sequence of failed fetch attempts since the most recent succeeded fetch. A skipped fetch does not resolve it.
_Avoid_: Last error, transient error
