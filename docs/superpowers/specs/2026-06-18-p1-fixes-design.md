# P1 Fixes Design — Traffic accuracy + latency chart + doc drift

Status: Design (self-approved per user instruction to proceed autonomously)
Date: 2026-06-18
Branch: `custom`

## Background

After the P0 fixes (ping over SSH, update badge), the next-highest-value items
from the full review are accuracy/correctness fixes that affect daily use of a
personal 10-server deployment. These are gathered into one batch.

## Problems

### 1. Monthly traffic overcounts when network interfaces change (#2)

`updateTrafficMonthly` (`internal/hub/systems/traffic.go`) sums every
interface's cumulative counters into one scalar (`sumNetBytes`), then compares
that scalar to the previous scalar. This loses per-interface granularity:

- One interface resets (reboot) while another grows: the summed current drops
  below the summed last, so the code treats the *entire* new smaller sum as a
  delta → large overcount.
- A NIC is added: its full since-boot cumulative total jumps the sum and is
  counted as fresh cycle traffic.
- A NIC is removed: the sum drops below last → the remaining total counted as a
  delta.

The agent already reports per-interface cumulative counters
(`Stats.NetworkInterfaces[name] = [upDelta, downDelta, totalSent, totalRecv]`);
the hub just discards the per-interface structure. This can trigger false
quota-exceeded alerts.

### 2. Live traffic rate freezes at the last non-zero value (#3)

`Bandwidth` (`internal/entities/system/system.go:41`) is tagged
`json:"b,omitzero"`. When the rate is exactly `[0,0]`, the field is omitted
from the realtime JSON. The traffic card
(`internal/site/src/components/traffic-card/traffic-card.tsx:71`) only calls
`setRate` when `data.stats?.b` is present, so when traffic drops to zero the
displayed rate freezes at the last non-zero value instead of showing 0.

The `omitzero` tag cannot simply be removed: it deliberately distinguishes "old
agent that does not report bandwidth" (frontend falls back to legacy `ns`/`nr`
fields) from "modern agent reporting zero bandwidth". Removing it would break
the legacy fallback for old agents.

### 3. Deleted ping target keeps rendering a stale line (#4)

`LatencyChart` (`internal/site/src/components/routes/system/charts/latency-chart.tsx`)
collects target ids by scanning *all* loaded history records (newest-first but
unioning every id seen). A target that was deleted still appears in old
`system_stats.stats.p` rows, so its line keeps rendering with stale data,
labeled by id (the name lookup fails). The chart should only render currently-
configured targets.

### 4. Stale documentation (#5, #6)

- `internal/site/src/types.d.ts:248` and `internal/entities/system/ping.go:3-5`
  still describe `PingResult` as "realtime only, not persisted". Since the
  latency-detail-charts change, `PingResult` is ALSO persisted via
  `Stats.Pings` (CBOR 36). The realtime-only payload is `CombinedData.PingResults`
  (CBOR 6), a different field.
- `agent/ping.go:153` comment says "use the last successful sample as the
  'current' latency", but the code sets `current = 0` whenever the *last* probe
  failed, even if earlier probes succeeded. The entity doc
  (`ping.go:9-10`) says "0 if the last probe failed" which is accurate; the
  inline comment is the misleading one.

## Goals

- **G1.** Monthly traffic accumulation is correct under per-interface counter
  resets, NIC addition, and NIC removal — no spurious overcounts.
- **G2.** The traffic card's live rate shows 0 when bandwidth is 0, without
  breaking the legacy-field fallback for old agents.
- **G3.** The latency chart only renders currently-configured ping targets.
- **G4.** Docs match the code.

## Non-goals

- No schema migration. The per-NIC baseline is kept in memory (rationale in
  Design G1); the existing `last_up`/`last_down` columns become unused but are
  left in place (harmless).
- No timezone handling changes for traffic periods.
- No ICMP ping; TCP-dial semantics unchanged.
- No frontend test runner (project has none); frontend changes verified by
  Biome + build.

## Design

### G1 — Per-interface traffic delta

Add an in-memory per-interface baseline to `System`
(`internal/hub/systems/system.go`):

```go
lastNetIfaces map[string][2]uint64 // per-NIC [lastSent, lastRecv] cumulative baseline for traffic deltas
```

`updateTrafficMonthly` runs in the system's single update goroutine, so this
field has no concurrent writers and needs no mutex.

Extract a pure, testable helper in `traffic.go`:

```go
// computeNetDelta computes per-interface byte deltas from cumulative counters,
// handling per-interface counter resets and newly-appeared/removed interfaces.
// Returns the summed deltas and the updated baseline. A newly-appearing
// interface is seeded (no delta this cycle) so its full since-boot total is
// not attributed as fresh traffic. A disappeared interface is dropped from the
// baseline. On a per-interface counter reset (reboot), the new smaller value
// is taken as the delta.
func computeNetDelta(current map[string][4]uint64, last map[string][2]uint64) (deltaSent, deltaRecv uint64, updated map[string][2]uint64)
```

`updateTrafficMonthly` becomes:

1. `deltaSent, deltaRecv, sys.lastNetIfaces = computeNetDelta(data.Stats.NetworkInterfaces, sys.lastNetIfaces)`.
2. If `deltaSent == 0 && deltaRecv == 0`, return (nothing to accumulate; also
   covers the first call where every interface is seeded).
3. Find/create the `traffic_monthly` row for `(system, period)`, add the deltas
   to `bytes_up`/`bytes_down`, quota-check, save.

The persisted `last_up`/`last_down` are no longer read or written. The
`sumNetBytes` helper is removed (orphaned by this change). The seed-row creation
path is removed: a row is created only when there is a non-zero delta, which is
the desired behavior (the card hides on 0/0/no-history anyway, and `has_history`
is driven by rows with `bytes_up>0 || bytes_down>0`).

Restart behavior: on hub restart `lastNetIfaces` is nil → first cycle seeds all
interfaces (no delta) → one ~60s cycle skipped. `bytes_up`/`bytes_down` are the
accumulated truth and are unaffected, so no double-count. Acceptable for the
deployment.

### G2 — Rate shows zero at zero

Frontend-only change in `traffic-card.tsx`. Replace:

```ts
if (active && data.stats?.b) setRate(data.stats.b)
```

with:

```ts
if (active) setRate(data.stats?.b ?? [0, 0])
```

- Modern agent, `b` present → use it.
- Modern agent, `b` absent (zero bandwidth) → `[0, 0]`.
- Old agent, `b` never present → `[0, 0]` (acceptable; old agents have no
  meaningful live rate, and the card relies on the cumulative poll anyway).

No entity or wire change; legacy `ns`/`nr` fallback elsewhere is untouched
because the field is still omitted for old agents.

### G3 — Latency chart honors current targets

In `latency-chart.tsx`, intersect the history-derived target ids with the
current monitors (`nameById`, already fetched). While monitors are still loading
(`nameById` empty), fall back to the history ids (transient, avoids a hide
flash). Once loaded, only current monitors render — deleted targets disappear.

```ts
const sortedIds = useMemo(() => {
    const histIds = targetIdsKey ? targetIdsKey.split("\0") : []
    if (Object.keys(nameById).length === 0) return histIds
    return histIds.filter((id) => id in nameById)
}, [targetIdsKey, nameById])
```

Disabled targets still exist as monitor records, so they remain in `nameById`
and keep rendering historical data (out of scope; the reported bug is deleted
targets).

### G4 — Doc fixes

- `types.d.ts:248` and `entities/system/ping.go:3-5`: describe `PingResult` as
  carried both in the realtime payload (`CombinedData.PingResults`) and
  persisted in `Stats.Pings`.
- `agent/ping.go:153`: correct the inline comment to "0 if the last probe
  failed, else the last probe's latency".

## Testing

- **G1 unit test** (`internal/hub/systems/traffic_test.go`, new): table-driven
  test of `computeNetDelta` covering: single NIC growth; single NIC reset;
  NIC added (seeded, no delta); NIC removed (dropped); two NICs where one
  resets (the core bug — summed approach would mis-handle, per-NIC is correct);
  first call (nil baseline → all seeded, zero delta).
- **G2, G3**: no frontend test runner; verified by Biome check + `bun run build`.
- **G4**: doc-only; verified by reading.

## Files touched

| File | Change |
|---|---|
| `internal/hub/systems/traffic.go` | `computeNetDelta` helper; rewrite `updateTrafficMonthly` to use it + in-memory baseline; remove `sumNetBytes` |
| `internal/hub/systems/system.go` | Add `lastNetIfaces` field to `System` |
| `internal/hub/systems/traffic_test.go` (new) | `computeNetDelta` tests |
| `internal/site/src/components/traffic-card/traffic-card.tsx` | `setRate(data.stats?.b ?? [0,0])` |
| `internal/site/src/components/routes/system/charts/latency-chart.tsx` | filter `sortedIds` by `nameById` |
| `internal/site/src/types.d.ts` | `PingResult` doc |
| `internal/entities/system/ping.go` | `PingResult` doc |
| `agent/ping.go` | inline comment fix |

No entity CBOR field-number changes. No migration. No protocol change.
