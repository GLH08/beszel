# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

Beszel is a lightweight server monitoring platform. It has two binaries:

- **Hub** (`internal/hub`, `internal/cmd/hub`): a [PocketBase](https://pocketbase.io/) web app that stores data, serves the React dashboard, and pulls/receives metrics from agents.
- **Agent** (`agent`, `internal/cmd/agent`): runs on each monitored system, collects metrics, and serves them to the hub.

The shared module path is `github.com/henrygd/beszel`. `beszel.go` holds the version and protocol-compatibility constants.

## Fork workflow (IMPORTANT — read before committing)

This repository is a **fork**. Remotes are already configured:
- `origin` → the fork (`GLH08/beszel`)
- `upstream` → the official repo (`henrygd/beszel`)

Branching rules (do not deviate):
- **`main` is reserved for tracking upstream.** Never commit custom changes to `main`; keep it clean so official releases always fast-forward without conflicts.
- **All custom modifications go on a dedicated work branch**, never on `main`. Create or switch to the work branch before editing any code.
- **Commit early and often for easy rollback.** After each logical change, run `git add .` then `git commit` with a short message, so any step can be reverted.

Sync the latest official version (run on `main`, which should stay a clean fast-forward):
```sh
git checkout main
git fetch upstream
git merge upstream/main
git push origin main
```
Then update the work branch from `main` (`git merge main`, or rebase) to pull in upstream changes.

## Commands

All `make` targets run from the repo root.

### Build
- `make build` — build both agent and hub into `./build/`
- `make build-agent` — build agent (on Windows also builds the .NET LibreHardwareMonitor helper and fetches `smartctl.exe`)
- `make build-hub` — build hub (builds the web UI first unless `SKIP_WEB=true`)
- `make build-hub-dev` — build hub with the `development` build tag (no embedded UI; reverse-proxies to the Vite dev server)
- Cross-compile with `OS` / `ARCH`, e.g. `OS=linux ARCH=arm64 make build-agent`
- `NVML=true|false|auto` toggles the `-tags glibc` agent build for Nvidia GPU support (auto-enables on linux/amd64 glibc hosts)

### Test / lint
- `make test` — runs `go test -tags=testing ./...`
- **Go tests require `-tags=testing`.** Test files carry `//go:build testing`, so a plain `go test` won't compile them. Run a single test with: `go test -tags=testing ./internal/hub/... -run TestName`
- `make lint` — `golangci-lint run` (no repo config file; uses golangci-lint defaults)
- `make tidy` — `go mod tidy`

### Dev workflow
- `KEY="..." make -j dev` — runs frontend, hub, and agent together
  - `make dev-server` — Vite dev server on `:5173`
  - `make dev-hub` — hub with `ENV=dev` + `development` tag on `:8090`, proxying the UI from `:5173` (uses `entr` for live reload if installed)
  - `make dev-agent` — runs the agent (uses `entr` for live reload if installed)
- In dev, browse the hub at `:8090`; it proxies HTML/assets from Vite and injects app config into `index.html`.
- `ENV=dev` enables PocketBase **automigrate**: schema changes made in the Admin UI auto-generate migration files in `internal/migrations`.

### Frontend (`internal/site`, prefer `bun`, falls back to `npm`)
- `bun run dev` — Vite dev server
- `bun run build` — `lingui extract && lingui compile && vite build`
- `bun run sync` — regenerate Lingui locale catalogs after changing translatable strings
- `bun run check` / `bun run check:fix` / `bun run format` — Biome (this project uses **Biome**, not ESLint/Prettier)
- No frontend test runner is configured.

## Architecture

### Hub
- Built on PocketBase: the `Hub` struct (`internal/hub/hub.go`) embeds `core.App` and composes managers — `AlertManager` (`internal/alerts`), `UserManager` (`internal/users`), `RecordManager` (`internal/records`), `SystemManager` (`internal/hub/systems`), and `Heartbeat` (`internal/hub/heartbeat`).
- Data collections/schema live in `internal/migrations` (a collections snapshot + initial settings). Treat these as the source of truth for the DB.
- Cron jobs (`registerCronJobs`): delete old records hourly; roll up shorter records into longer averaged records every 10 min (`RecordManager`). Retention/averaging tiers are defined in `internal/records/records.go` (1m → 10m → 20m → 120m → 480m).
- Generates an ed25519 keypair (`id_ed25519` in the data dir) used to authenticate to agents over SSH.
- Custom API routes/middleware in `internal/hub/api.go`; agent WebSocket entrypoint in `internal/hub/agent_connect.go`.

### Agent
- `agent.NewAgent()` (`agent/agent.go`) wires up collectors; `ConnectionManager` (`agent/connection_manager.go`) owns the connection lifecycle.
- Collectors: CPU, memory (incl. swap/ZFS ARC), disk + disk I/O, network, sensors/temperature, GPU (Nvidia/AMD/Intel), battery, S.M.A.R.T., systemd services, and Docker/Podman containers.
- Heavy use of **platform-specific files via build tags** — suffixes like `_linux`, `_windows`, `_darwin`, `_nonlinux`, `_unsupported`, `_stub` paired with `//go:build` constraints (e.g. `gpu_nvml_linux.go`, `sensors_windows.go`, `emmc_linux.go`, `smart_windows.go`). Add new platform variants by following this pattern, not with runtime `runtime.GOOS` branching.
- Windows sensor data comes from the bundled .NET `agent/lhm` (LibreHardwareMonitor) project.

### Hub ↔ agent communication
- Two transports behind one `Transport` interface (`internal/hub/transport/transport.go`): **WebSocket** (preferred) and **SSH** (fallback).
- The agent's `ConnectionManager` dials the hub over WebSocket first; if that fails it starts its own **SSH server** and waits for the hub to connect. WS connection auth uses a token + agent fingerprint.
- Wire format is **CBOR**. Message shapes and the `WebSocketAction` enum (`GetData`, `CheckFingerprint`, `GetContainerLogs`, `GetContainerInfo`, `GetSmartData`, `GetSystemdInfo`) are in `internal/common/common-ws.go`. SSH cipher/MAC/KEX allowlists are in `internal/common/common-ssh.go`.
- Shared data types are in `internal/entities` (`system`, `container`, `smart`, `systemd`). These structs carry **both `json` (DB storage) and `cbor` (wire) tags**.
  - `cbor:"N,keyasint"` field numbers are part of the wire protocol — **do not renumber existing fields**; only append new ones.
  - Backward compatibility is version-gated by `MinVersionCbor` / `MinVersionAgentResponse` in `beszel.go`. `transport.UnmarshalResponse` bridges legacy typed response fields (≤0.18) and the generic `Data` field (0.19+).

### Frontend (`internal/site`)
- React 19 + Vite + TypeScript, TailwindCSS v4, Radix UI primitives (`src/components/ui`), Recharts for charts.
- State via **nanostores** (`src/lib/stores.ts`); routing via `@nanostores/router` (`src/components/router.tsx`).
- Talks to the hub with the PocketBase JS SDK (`src/lib/api.ts`, `src/lib/systemsManager.ts`).
- i18n via **Lingui** (`src/locales`, `src/lib/i18n.ts`); translations managed on Crowdin.
- The built `dist/` is embedded into the hub binary via `//go:embed all:dist` (`internal/site/embed.go`); the production hub serves it directly.

## Conventions & gotchas
- Always pass `-tags=testing` when running Go tests.
- Keep the version in sync between `beszel.go` and `internal/site/package.json`.
- When changing entity structs, preserve CBOR field numbers and consider older agents/hubs (bump the relevant `MinVersion*` only when intentionally dropping support).
- Hub dev vs prod is a build-tag split: `development` proxies to Vite (`internal/hub/server_development.go`); the default build embeds and serves `dist/` (`internal/hub/server_production.go`).
