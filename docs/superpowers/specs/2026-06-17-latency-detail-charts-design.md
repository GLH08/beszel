# Latency redesign — per-system detail-page line charts

Date: 2026-06-17
Status: Design (pending approval)

## Goal

Move ping/latency from a single-value home-page table to **per-system detail-page
line charts** that behave like the existing CPU / load / temperature charts:
one line per ping target (e.g. 移动 / 联通 / 电信), persisted history with
rollup across time ranges (1m / 1h / 12h / 24h / 1w / 30d), and realtime updates
in the 1m view.

## Non-goals

- No change to how ping targets are configured (the `monitors` collection, the
  settings page, the `SetConfig` push to agents, the 10s probe interval).
- No higher-than-1-minute resolution. Latency follows the same minute-resolution
  rollup tiers as every other metric.
- No loss% chart. Loss is surfaced in the chart tooltip only.

## Decisions (from brainstorming)

- Chart value: **latest latency** per probe (`PingResult.Latency`). Loss% and avg
  available in the hover tooltip.
- Home page: **remove** the latency table entirely.
- Layout: **one chart, one line per target**, Recharts legend toggling for
  isolating a target.
- Resolution / history: **1-minute**, reusing existing rollup tiers and the
  existing time-range dropdown.
- Storage: **Approach A** — add a `Pings map[string]*PingResult` field to
  `system.Stats`, riding in the existing `system_stats.stats` JSON column with
  per-key rollup, exactly like `Temperatures` / `GPUData` / `NetworkInterfaces`.

## Data model

### Entity (`internal/entities/system/system.go`)

Add to `Stats`:

```go
// Pings holds the latest latency-test result per ping target id.
// Persisted in system_stats and averaged per-key across rollup tiers,
// like Temperatures / GPUData.
Pings map[string]*PingResult `json:"p,omitempty" cbor:"36,keyasint,omitempty"`
```

- CBOR tag **36** — next free (current max is `DiskIoStats`=35). Append-only;
  never renumber existing tags.
- JSON key `"p"`, short to match existing keys.
- `PingResult` (`internal/entities/system/ping.go`) is unchanged; it already
  carries cbor tags `0..3` (`Id` / `Latency` / `Loss` / `Avg`) suitable for use
  as a map value.

### CombinedData (`internal/entities/system/system.go`)

`PingResults []*PingResult` (cbor 6, json `ping`) is **kept** in the struct and
the agent still populates it in `gatherStats`. Rationale: it is part of the
realtime-channel wire shape and the CBOR append-only contract; leaving it
populated is zero-risk and avoids touching the wire. The frontend simply no
longer reads `data.ping` (it reads `data.stats.p` instead).

### Dead-code removal (orphaned by removing the home table)

Removing the home latency table orphans the only consumer of `/api/beszel/ping`:
- Remove the `GET /api/beszel/ping` route + `getPing` handler
  (`internal/hub/api.go`).
- Remove `PingSummary` struct + `PingSummaries()` method
  (`internal/hub/systems/monitors.go`).
- `loadMonitorConfig`, `PushConfigToAll`, `pushConfigToSystem`, and the monitors
  event hooks all **stay** — they drive the `SetConfig` push to agents, which is
  still needed.

## Agent (`agent/agent.go` gatherStats, `agent/ping.go`)

`agent/ping.go` already produces a `map[string]*system.PingResult` snapshot via
`Results()`. In `gatherStats`, on **both** the cached and uncached paths (where
`data.PingResults = a.pingManager.sortedResults()` is currently set), also
populate the persisted field:

```go
if m := a.pingManager.Results(); len(m) > 0 {
    data.Stats.Pings = m
}
```

- `Results()` already returns a fresh copy, safe to assign directly.
- `data.PingResults` continues to be set for the realtime payload (unchanged).
- The map is keyed by target id, so it aligns 1:1 with the `monitors` records
  the frontend fetches for line labels/colors.

No other agent change.

## Hub write path — no change

`internal/hub/systems/system.go createRecords` already does
`systemStatsRecord.Set("stats", data.Stats)`. The new `Pings` map rides along in
the JSON for free. One `1m` row per system per minute now includes per-target
latency.

## Hub rollup (`internal/records/records.go`)

`AverageSystemStatsSlice` accumulates per-key maps then divides. Add a `Pings`
block modelled on the `GPUData` block:

**Accumulate** (in the per-record loop, near the GPU block):
- If `stats.Pings != nil`, lazily init `sum.Pings`.
- For each `id, value`: if the key is new, create a zero `*PingResult`. Sum
  `Latency`, `Avg`; track a per-key success count to recompute `Loss` as a mean.
  Specifically: accumulate `latencySum`, `avgSum`, and `count` (number of
  records in which this target appeared), and `lossSum` (the loss% values). A
  target that appears in some records but not others is averaged only over the
  records it appears in (matching the GPU/Temperature "skip missing keys"
  behavior).

**Average** (in the average section, near the GPU block):
- For each key: `Latency = twoDecimals(latencySum / count)`,
  `Avg = twoDecimals(avgSum / count)`, `Loss = twoDecimals(lossSum / count)`.
  Keep `Id` from the first occurrence.

This mirrors the existing per-key averaging discipline exactly.

## Frontend

### Remove home latency table

- Delete `internal/site/src/components/latency-table/latency-table.tsx`.
- Remove its import + `<LatencyTable />` from
  `internal/site/src/components/routes/home.tsx`.

### New chart component

`internal/site/src/components/routes/system/charts/latency-chart.tsx`, modelled
on `load-average-chart.tsx`. Difference: line set is **dynamic** (one per target
id present in the data), built with a hook modelled on `useNetworkInterfaces`
(`internal/site/src/components/charts/hooks.ts`):

- A `usePingTargets(records)` helper (in `hooks.ts` or inline) scans the
  `SystemStatsRecord[]` for all `stats.p` keys, returns a stable sorted list of
  target ids with assigned colors (same hue-rotation scheme as
  `useNetworkInterfaces`).
- `dataPoints` = one entry per target id:
  `{ label: targetNameOrId, color, dataKey: ({ stats }) => stats?.p?.[id]?.l }`
  where `.l` is `Latency`.
- Target **names**: the chart needs human-readable labels (移动/联通/电信), but
  `stats.p` is keyed by monitor record **id**. The component fetches the
  `monitors` collection once (`pb.collection("monitors").getFullList()`) and
  builds an `id -> name` map for labels. If a monitor was deleted, fall back to
  the id (the persisted history still renders; the line just shows the id).
- Tooltip (`contentFormatter`): show latency ms; if `loss > 0`, append
  `(loss%)`; if latency is 0 and loss is 100, show "timeout". Pulls
  `stats?.p?.[id]?.lo` for loss.
- `tickFormatter`: latency in ms, 1 decimal.
- Empty state: `ChartCard` already handles `dataEmpty`; additionally hide the
  chart entirely (return `null`) when no `stats.p` keys exist across the loaded
  records AND no monitors are configured — so systems with no targets show no
  latency card.

### Mount in detail page

`internal/site/src/components/routes/system.tsx`: add `<LatencyChart {...coreProps} />`
to **both** the default layout grid and the tabbed layout grid, placed after
`<LoadAverageChart>` (sensible grouping with other per-series charts). It accepts
the same `{ chartData, grid, dataEmpty }` props.

### Realtime (1m view) — no subscriber change

The realtime subscriber in `use-system-data.ts` builds
`{ created: now, stats: data.stats }`. Because the agent now puts ping data into
`data.Stats.Pings`, it arrives inside `data.stats.p` automatically. The existing
`setSystemStats((prev) => appendData(prev, [statsPoint], 1000, 60))` path feeds
the chart with no code change. The `data.ping` (CombinedData) payload is ignored
by the chart.

### Historical fetch — no change

`chart-data.ts getStats("system_stats", systemId, chartTime)` already fetches
the full `stats` JSON sorted by `created`. The new `p` field is included. No new
endpoint, no new query.

### Types (`internal/site/src/types.d.ts`)

- Add `p?: Record<string, PingResult>` to the `SystemStats` interface (the stats
  JSON shape). `PingResult` already exists as `{ id, l, lo, a }`.

## Backward compatibility

- Old agents (pre-this-change) never set `Stats.Pings`; the field is `omitempty`
  so the JSON omits it. The chart sees no `p` keys and hides itself — no errors.
- Old hub rows (written before deploy) simply lack `p`; rollup averaging skips
  missing keys. New rows populate it going forward. There is **no backfill** of
  historical latency — charts start filling from deploy time forward, which is
  acceptable (matches how any new metric field behaves).
- CBOR: tag 36 is newly appended; existing agents that don't send it are fine
  (the hub decode just leaves the map nil). `MinVersion*` constants are NOT
  bumped — this is an additive, optional field.
- The `monitors` collection, settings page, `SetConfig` push, and `/api/beszel/ping`
  endpoint are all unchanged.

## Testing

- **Entity/rollup unit test** (`internal/records`): extend an existing
  averaging test (or add one) to include `Stats.Pings` with 2-3 targets across
  several records, assert per-key averages and that a target missing from some
  records is averaged only over records it appears in.
- **Agent gatherStats**: existing pattern; the change is a 3-line assignment.
  Verified by build + the fact that `Results()` is already tested by virtue of
  the running ping manager.
- **Frontend**: no test runner configured; verify by build (`bun run build`) and
  Biome (`bun run check`). Manual verification on the deployed server per the
  established workflow.
- **Regression**: the existing `TestMonitorsListSortCreated` test stays green
  (unchanged monitors collection behavior).

## Files touched

Go:
- `internal/entities/system/system.go` — add `Stats.Pings` field.
- `agent/agent.go` — populate `data.Stats.Pings` in `gatherStats` (both paths).
- `internal/records/records.go` — accumulate + average `Pings` per key.
- `internal/hub/api.go` — remove `/ping` route + `getPing` handler.
- `internal/hub/systems/monitors.go` — remove `PingSummary` + `PingSummaries`.

Frontend:
- `internal/site/src/types.d.ts` — add `p` to `SystemStats`.
- `internal/site/src/components/routes/system/charts/latency-chart.tsx` — new.
- `internal/site/src/components/charts/hooks.ts` — add `usePingTargets` (or
  inline in the chart component).
- `internal/site/src/components/routes/system.tsx` — mount `LatencyChart` in
  both layouts.
- `internal/site/src/components/routes/home.tsx` — remove `LatencyTable` import
  + usage.
- `internal/site/src/components/latency-table/latency-table.tsx` — **delete**.

No migrations, no new collections, no new API endpoints, no CBOR renumbering.

## Deployment

Standard GHCR rebuild of hub + agent (agent carries the `Stats.Pings` change;
hub carries the rollup + frontend). Pull images, `docker compose up -d`,
hard-refresh. Latency charts begin populating from deploy time; older history is
not backfilled.
