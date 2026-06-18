# P2 Fixes Design — Top-processes command, ping note, 80% traffic warning

Status: Design (self-approved per user instruction to proceed autonomously)
Date: 2026-06-18
Branch: `custom`

## Background

The P2 batch from the full review covers user-experience improvements for a
personal 10-server deployment. One item from the original P2 list is descoped:
Go-side i18n of the traffic-quota alert. The codebase has **no** Go-side i18n
infrastructure — every existing Go alert (status, system, smart) uses hardcoded
English titles/links (`"View " + systemName`, etc.). Translating only the
traffic alert would be inconsistent; doing all of them is a new subsystem, out
of scope. So #11 is dropped, leaving three items.

## Problems

### 1. Top-processes command line is truncated to 60 chars (#7)

`agent/processes.go:18` sets `cmdDisplayLimit = 60`. The agent truncates the
command before sending, so the frontend tooltip (`processes-table.tsx:55`
`title={p.c}`) can only show the truncated string. When diagnosing which
process is burning CPU, a 60-char cap frequently hides the distinguishing
arguments (paths, flags, ports). The value was chosen for payload size; 120
keeps payloads tiny while doubling the visible signal.

### 2. Ping uses TCP dial, not ICMP — no UI explanation (#9)

`probeTCP` (`agent/ping.go`) measures latency via a TCP handshake to
`host:port`, not ICMP. This is correct and cross-platform, but a user reading
"100% loss" for a host that is reachable may misread it as a network-layer
outage when really the probed port is closed/filtered. The settings page has no
note about this.

### 3. Traffic quota has only an exceeded alert, no early warning (#10)

`updateTrafficMonthly` (`internal/hub/systems/traffic.go`) fires a one-shot
alert when usage crosses 100% of quota. For a metered/billed deployment, an
early warning at 80% is more actionable. There is currently no warning tier.

## Goals

- **G1.** The top-processes command shows up to 120 chars (from 60).
- **G2.** The Ping Targets settings page explains that probing is a TCP dial,
  so a closed/filtered port reports 100% loss — not a network outage.
- **G3.** A one-shot 80%-of-quota warning alert fires (in addition to the
  existing exceeded alert), using the same delivery path.

## Non-goals

- No Go-side i18n (existing pattern is hardcoded English alerts; #11 dropped).
- No ICMP ping.
- No per-direction (up/down) warning thresholds — combined up+down vs quota,
  matching the existing exceeded semantics.
- No persistent "warning sent" column migration — the 80% warning is tracked
  in-memory on the `traffic_monthly` row's existing `notified` flag is NOT
  reused (it means "exceeded"); a separate in-memory set on System tracks the
  80% warning so it fires once per cycle.

## Design

### G1 — Widen command display limit

`agent/processes.go:18`: `const cmdDisplayLimit = 60` → `120`. Update the
comment. No frontend change (the tooltip already shows `p.c`). CBOR/protocol
unchanged (the field already carries a truncated string).

### G2 — Ping TCP-semantics note

In `internal/site/src/components/routes/settings/monitors.tsx`, add a
muted-foreground note line under the existing description paragraph:

```tsx
<p className="text-sm text-muted-foreground leading-relaxed">
	<Trans>Latency is measured via a TCP connection to the configured port, not
	ICMP. A host that is reachable but has the port closed or filtered will show
	100% loss.</Trans>
</p>
```

(New translatable string; `bun run sync` regenerates catalogs.)

### G3 — 80% traffic warning

Add a pure helper in `traffic.go`:

```go
const trafficWarnFraction = 0.8

// shouldWarnQuota reports whether usage has crossed the early-warning
// threshold (80% of quota) and a warning is still owed this cycle.
func shouldWarnQuota(quotaBytes, usedBytes uint64, warned bool) bool {
	return quotaBytes > 0 && !warned && float64(usedBytes) >= float64(quotaBytes)*trafficWarnFraction
}
```

Track the one-shot warning in memory on `System` (add field `trafficWarned bool`),
reset whenever the period changes. The exceeded `notified` flag stays as-is on
the row. In `updateTrafficMonthly`, after computing `bytesUp+bytesDown`:

```go
used := bytesUp + bytesDown
quotaBytes := uint64(quotaGiB) * bytesPerGiB

// 80% early warning (one-shot per cycle, in-memory)
periodChanged := rec.GetString("period") != "" // existing row vs new
// reset the warning flag when entering a new cycle
if sys.trafficPeriod != period {
	sys.trafficPeriod = period
	sys.trafficWarned = false
}
if shouldWarnQuota(quotaBytes, used, sys.trafficWarned) {
	sys.trafficWarned = true
	sys.notifyQuotaWarning(systemRecord, quotaGiB, used, period)
}
```

Add `trafficPeriod string` and `trafficWarned bool` to `System`. The existing
exceeded check (`quotaBytes > 0 && !notified && used >= quotaBytes`) stays. A
new `notifyQuotaWarning` mirrors `notifyQuotaExceeded` with an "approaching"
title/message.

This runs in the system's single update goroutine — no mutex needed on the
`trafficPeriod`/`trafficWarned` fields.

Restart behavior: `trafficWarned` resets to false on hub restart; if usage is
already ≥80% at restart, the warning re-fires once. Acceptable (one extra
notification). The exceeded `notified` flag is persisted, so it does not
re-fire.

## Testing

- **G3 unit test** (`internal/hub/systems/traffic_test.go`): table-driven
  `TestShouldWarnQuota` — below 80% (no warn), exactly 80% (warn), above (warn),
  already warned (no), quota 0/unlimited (no).
- **G1**: constant change; verified by reading + build. No new test (the
  truncation is exercised nowhere in tests today).
- **G2**: no frontend test runner; Biome + build + `bun run sync`.

## Files touched

| File | Change |
|---|---|
| `agent/processes.go` | `cmdDisplayLimit` 60 → 120 + comment |
| `internal/site/src/components/routes/settings/monitors.tsx` | TCP-semantics note |
| `internal/hub/systems/traffic.go` | `shouldWarnQuota` helper + 80% warning wiring + `notifyQuotaWarning` |
| `internal/hub/systems/system.go` | `trafficPeriod`, `trafficWarned` fields on `System` |
| `internal/hub/systems/traffic_test.go` | `TestShouldWarnQuota` |

No entity/CBOR/migration/protocol changes. The `notified` column semantics are
unchanged (still means "exceeded, alerted").
