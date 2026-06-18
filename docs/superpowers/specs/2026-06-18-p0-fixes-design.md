# P0 Fixes Design — Ping over SSH + Hub Update Badge

Status: Design (pending approval)
Date: 2026-06-18
Branch: `custom`

## Background

A full review of the `custom` branch surfaced two P0 defects that affect a
personal 10-server deployment. Both are correctness bugs independent of
geography; the mirror/proxy concerns from the review were descoped (see
"Non-goals" below).

## Problem 1 — Ping config never reaches SSH-only agents

`PushConfigToAll` (`internal/hub/systems/monitors.go:39-47`) iterates every
system but **skips any system without an active WebSocket connection**:

```go
if sys.WsConn == nil || !sys.WsConn.IsConnected() {
    continue
}
```

Yet `sys.request` (`internal/hub/systems/system.go:410-434`) is already a
unified transport that tries WebSocket first and **falls back to SSH** on its
own. The skip guard is the only thing preventing SSH-only systems from
receiving the `SetConfig` message.

Consequence: any agent reachable only via SSH never receives ping targets, so
its latency chart is permanently empty — with no log, badge, or UI indication
that the feature is inert for that machine.

## Problem 2 — Hub "update available" badge ignores the fork

`getUpdate` (`internal/hub/api.go:160-182`) calls:

```go
ghupdate.FetchLatestRelease(context.Background(), http.DefaultClient, "")
```

An empty URL makes `FetchLatestRelease` default to
`https://api.github.com/repos/henrygd/beszel/releases/latest`
(`internal/ghupdate/ghupdate.go:221-224`) — **always upstream**, regardless of
`AGENT_REPO`. A fork hub therefore compares its version against upstream
releases and links the badge to upstream release pages. At best confusing,
at worst the user clicks "update" thinking there's a fork release when there
isn't.

Related footgun: `ghupdate`'s `UseMirror` config swaps the API/asset host to
`gh.beszel.dev` (`ghupdate.go:266-268, 371-376`), a mirror that proxies
**only `henrygd` repos**. A fork that passes `--china-mirrors` (or sets the
flag) gets a 403 from `gh.beszel.dev/repos/<fork-owner>/beszel/...` on both
the API call and the asset download. The failure is silent at the call site
(returns an error that surfaces only via `log.Fatal` in `Update`).

## Goals

- **G1.** Ping targets reach every connected agent regardless of transport
  (WebSocket or SSH), with no behavior change for WS-connected systems.
- **G2.** The hub's in-app update badge reflects the fork (`AGENT_REPO`) —
  checks the fork's releases and links to the fork's release page. Upstream
  installs (no `AGENT_REPO`) are byte-for-byte unchanged.
- **G3.** `--china-mirrors` / `UseMirror` cannot silently 403 a fork
  self-update. Forks self-update via direct GitHub connection; any future
  proxy need is satisfied by the standard `HTTPS_PROXY` environment variable
  (no project-code proxy logic).

## Non-goals

- No prefix-proxy (`ghfast.top` style) logic in Go self-update. The servers are
  overseas; direct GitHub works. If a proxy is ever needed, the operator sets
  `HTTPS_PROXY` in the service environment — Go's `http.DefaultClient` already
  honors it, requiring zero code.
- No periodic re-push of monitor config (a ~5min ticker was considered as an
  optional hardening; deferred — see "Open questions / deferred").
- No checksum/signature verification of self-update assets, no rollback, no
  tar-slip guard. These are pre-existing `ghupdate` inherited limitations,
  out of scope for this fix set.

## Design

### Component 1 — `PushConfigToAll` (Fix G1)

`internal/hub/systems/monitors.go`

Remove the WebSocket-only guard so `pushConfigToSystem` runs for every system.
`sys.request` handles the WS→SSH fallback internally, so the per-system
goroutine works uniformly:

```go
func (sm *SystemManager) PushConfigToAll() {
    cfg := loadMonitorConfig(sm.hub)
    for _, sys := range sm.systems.Values() {
        go sm.pushConfigToSystem(sys, cfg)
    }
}
```

`pushConfigToSystem` already has a 5s timeout and logs failures at Debug
(`monitors.go:50-57`) — unchanged. SSH fallback means a system whose WS is
down will incur one SSH round-trip per push; this is bounded by the 5s
timeout and only happens on monitors create/update/delete (not on a hot
path).

**Behavior change summary:** SSH-only systems now receive config on the same
events WS systems do (monitors CRUD + on WS-connect via `AddWebSocketSystem`).
WS systems: identical. There is **no** new push trigger for an SSH-only
system that is unreachable at the moment of a monitors change — it catches
up on the next monitors edit or its next WS reconnect. Acceptable for 10
hosts.

### Component 2 — Hub update badge honors `AGENT_REPO` (Fix G2)

`internal/hub/api.go`, `getUpdate`

Reuse the existing `ghRepoField` parser (`internal/hub/update.go:103-109`,
identical logic to `agent/update.go`'s `repoOwner`/`repoName`). Build the
fork API URL when `AGENT_REPO` is set; otherwise pass `""` so
`FetchLatestRelease` keeps its upstream default (zero upstream regression):

```go
func (info *UpdateInfo) getUpdate(e *core.RequestEvent) error {
    if time.Since(info.lastCheck) < 6*time.Hour {
        return e.JSON(http.StatusOK, info)
    }
    info.lastCheck = time.Now()

    // Honor AGENT_REPO so a fork hub checks the fork's releases instead of
    // upstream henrygd/beszel. Empty => upstream default (no behavior change).
    apiUrl := ""
    if owner, repo := ghRepoField(0), ghRepoField(1); owner != "" && repo != "" {
        apiUrl = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
    }

    latestRelease, err := ghupdate.FetchLatestRelease(context.Background(), http.DefaultClient, apiUrl)
    if err != nil {
        return err
    }
    // ... rest unchanged (semver compare, set info.Version / info.Url)
}
```

`FetchLatestRelease` returns a `release` whose `Url` is the release's HTML page
on whatever repo the API URL pointed at — so for a fork it links to the fork's
release page automatically. No extra mapping needed.

`api.go` already imports `fmt`; `ghRepoField` lives in the same `hub` package,
so no new import.

### Component 3 — Defuse `UseMirror` for forks (Fix G3)

`internal/ghupdate/ghupdate.go`, inside `update()` after Owner/Repo defaults
are resolved (~line 102):

```go
// gh.beszel.dev mirrors only the henrygd repos. Using it for a fork
// self-update 403s on both the API call and the asset download. For any
// non-upstream repo, ignore the mirror flag and self-update over a direct
// GitHub connection. Operators behind a restricted network can set the
// standard HTTPS_PROXY env var instead.
if p.config.UseMirror && (p.config.Owner != "henrygd" || p.config.Repo != "beszel") {
    ColorPrint(ColorYellow, "Ignoring --china-mirrors for fork repo; using direct GitHub connection. Set HTTPS_PROXY if a proxy is needed.")
    p.config.UseMirror = false
}
```

This single guard covers **both** `hub.Update` and `agent.Update` (both funnel
through `ghupdate.Update`). Upstream (`Owner=henrygd, Repo=beszel`) is
untouched. Forks now never hit the 403 footgun; if they ever need a proxy,
`HTTPS_PROXY` works with no code.

## Data flow

**Ping (after fix):**
```
monitors CRUD → SystemManager.onMonitorsChanged → PushConfigToAll
  → for EACH system (WS or SSH): goroutine → pushConfigToSystem
    → sys.request(SetConfig, cfg)  [WS first, SSH fallback]
      → agent SetConfigHandler → pingManager.SetTargets
```

**Hub badge (after fix):**
```
frontend polls /api/beszel/update  → getUpdate (6h throttle)
  → if AGENT_REPO set: api.github.com/repos/<owner>/<repo>/releases/latest
     else: "" → FetchLatestRelease upstream default
  → semver compare vs beszel.Version
  → if newer: info.Version + info.Url (fork release page)
```

## Error handling

- **Ping push over SSH fails** (agent down, SSH unreachable): `sys.request`
  returns an error → logged at Debug in `pushConfigToSystem`. No retry, no
  panic. The system catches up on next monitors edit or WS reconnect.
  Consistent with existing WS-path failure handling.
- **Badge API fetch fails** (network, fork repo has no releases): `getUpdate`
  returns the error to the caller (unchanged). `info.Version`/`info.Url` stay
  at their last values; the badge simply doesn't update. No crash.
- **Malformed `AGENT_REPO`** (no slash): `ghRepoField` returns `""` for both
  fields → badge falls back to upstream default. Same silent-fallback
  behavior as the self-update path; no new validation added (out of scope —
  a warning-on-malformed is a separate nicety).

## Testing

- **G1 unit test** (`internal/hub/systems/monitors_test.go` or a new
  `_test.go`): construct a `SystemManager` with two systems — one with a
  connected `WsConn`, one with `WsConn == nil` but an SSH transport — call
  `PushConfigToAll`, assert **both** received the `SetConfig` request.
  (Requires the test infrastructure already used by `monitors_test.go`'s
  `TestMonitorsListSortCreated`; if a mock `sys.request` is needed, follow the
  existing test patterns in that file.)
- **G2 unit test** (`internal/hub`): set `AGENT_REPO=glh08/beszel`, call
  `getUpdate` logic with a stub HTTP client returning a canned release JSON,
  assert the API URL hit was `api.github.com/repos/glh08/beszel/...` and
  `info.Url` points at the fork. Unset `AGENT_REPO`, assert upstream URL.
- **G3 unit test** (`internal/ghupdate/ghupdate_test.go`): call `Update` with
  `UseMirror=true, Owner="glh08", Repo="beszel"` and assert the mirror is not
  used (e.g. the API URL is `api.github.com`, not `gh.beszel.dev`). Existing
  `TestReleaseFindAssetBySuffix` / `TestExtractFailure` remain green.
- No frontend tests (project has no frontend test runner).

## Files touched

| File | Change |
|---|---|
| `internal/hub/systems/monitors.go` | Remove WS-only guard in `PushConfigToAll` |
| `internal/hub/api.go` | `getUpdate` builds fork API URL from `AGENT_REPO` |
| `internal/ghupdate/ghupdate.go` | Defuse `UseMirror` for non-henrygd repos |
| `internal/hub/systems/monitors_test.go` (or new) | G1 test |
| `internal/hub/*_test.go` (new or existing) | G2 test |
| `internal/ghupdate/ghupdate_test.go` | G3 test |

No entity/CBOR/migration/protocol changes. No `MinVersion*` bump. Purely
hub-side routing + one ghupdate guard.

## Open questions / deferred

- **Periodic monitor-config re-push** (fix #1 option B): a ~5min ticker
  calling `PushConfigToAll` would close the "monitors changed while an
  SSH-only host was unreachable" gap. Deferred — the gap is narrow (10 hosts,
  rare unreachable-at-edit-moment), and the WS-connect push already covers
  reconnects. Revisit only if it bites.
- **Malformed `AGENT_REPO` warning**: `ghRepoField` silently falls back to
  upstream on a missing slash. A one-time warning log would help debugging
  but is a separate, optional improvement.
