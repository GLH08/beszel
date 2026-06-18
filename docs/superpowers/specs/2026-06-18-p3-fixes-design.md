# P3 Fixes Design — Self-update hardening + monitors uniqueness

Status: Design (self-approved per user instruction to proceed autonomously)
Date: 2026-06-18
Branch: `custom`

## Background

The P3 batch hardens the self-update path (inherited from PocketBase's
`ghupdate`, which trusts the downloaded release asset without verification and
extracts tarballs without a path-traversal guard) and tightens the monitors
config model. All within the "trust GitHub TLS" model of a personal deployment —
the goal is defense-in-depth and graceful failure, not a new trust anchor.

## Problems

### 1. Self-update does not verify the downloaded asset (#12)

`ghupdate.Update` downloads the release asset, extracts it, and swaps it in —
no checksum or signature check. The install scripts (`install-agent.sh`) DO
verify SHA256 against the release's `beszel_<version>_checksums.txt`. The
self-update path is weaker than first install. A corrupted/MITM'd asset would
be installed silently.

### 2. tar.gz extraction has no path-traversal guard (#13)

`extractTarGz` (`internal/ghupdate/extract.go:25-73`) does
`filepath.Join(destDir, header.Name)` with no prefix check. The zip path
(`extractFile:106-108`) DOES check for Zip Slip. A malicious tarball release
asset (or a corrupted one with `../` entries) could write outside the extract
dir.

### 3. A broken replacement binary bricks the service (#14)

`update()` renames old → `.old`, swaps new in, and `defer os.Remove(.old)`
deletes `.old` on return — before the service restart is attempted. If the new
binary is corrupt/wrong-arch, the service restart fails and `.old` is already
gone: manual recovery. There is no pre-swap sanity check.

### 4. Duplicate `host:port` ping targets are allowed (#15)

`idx_monitors_host_port` is non-unique (`1781654401_add_monitors.go:42`), and
neither the UI nor `loadMonitorConfig` dedupes. Two records with the same
host:port get pushed as two targets and pinged twice.

### 5. Malformed `AGENT_REPO` silently falls back to upstream (#16)

`ghRepoField` / `repoOwner` / `repoName` use `SplitN("/", 2)`. A value with no
slash (e.g. `AGENT_REPO=glh08`) yields one part → both fields return "" →
self-update silently targets upstream `henrygd/beszel` instead of the intended
fork. No warning. The operator thinks they're updating from their fork but
aren't.

## Goals

- **G1.** Self-update verifies the downloaded asset's SHA256 against the
  release's checksums file before swapping. Mismatch → abort, keep old binary.
- **G2.** `extractTarGz` rejects entries that escape the dest dir (Tar Slip),
  matching the zip path.
- **G3.** Before swapping, the extracted binary is smoke-tested
  (`<bin> --version`); if it fails, the old binary is kept and the update aborts.
- **G4.** The `monitors` collection enforces unique `(host, port)`; the UI
  prevents creating a duplicate.
- **G5.** A malformed `AGENT_REPO` (set but no slash) logs a warning at update
  time so the silent-upstream-fallback is visible.

## Non-goals

- No signature verification (no signing key infrastructure; checksums file is
  trusted via GitHub+TLS, same as install scripts).
- No `HTTPS_PROXY` / mirror changes (handled in P0).
- No change to the `.old` deletion timing beyond what the smoke test requires —
  the smoke test runs BEFORE the swap, so a bad binary never replaces the good
  one; `.old` lifecycle stays as-is.
- No frontend test runner.

## Design

### G2 — Tar Slip guard (do first; smallest, isolated)

In `extract.go`, add a `sanitizeTarPath` helper and a guard, mirroring the zip
path. Both the dir and file joins use the sanitized path:

```go
// sanitizeExtractPath joins name onto destDir and rejects paths that escape
// destDir (Zip Slip / Tar Slip). Returns the cleaned absolute path.
func sanitizeExtractPath(destDir, name string) (string, error) {
	destDir = filepath.Clean(destDir) + string(os.PathSeparator)
	path := filepath.Join(destDir, name)
	if !strings.HasPrefix(path, destDir) {
		return "", fmt.Errorf("invalid file path: %s", name)
	}
	return path, nil
}
```

Use it in `extractTarGz` for both the dir and file paths. (The zip path can be
refactored to use it too, but to keep the diff surgical the zip path stays
as-is; only tar gains the guard. Optional: unify later.)

Test: a tarball with a `../escape` entry → `extractTarGz` returns an error.

### G1 — Self-update SHA256 verification

The release's checksums file is `beszel_<version>_checksums.txt` (goreleaser
default; confirmed in `.goreleaser.yml` `project_name: beszel` and
`install-agent.sh:705`). It is an asset on the same release, with lines
`<sha256>  <assetname>`.

Add a step in `update()` after downloading the asset and BEFORE extracting:
download the checksums file (from the same base release URL), find the line for
`asset.Name`, compute the downloaded asset's SHA256, compare. Mismatch → abort
(keep old binary, since swap hasn't happened).

New pure helper + helpers:

```go
// parseChecksumLine extracts the hex digest for fileName from a checksums file
// body (format: "<sha256>  <fileName>" per line). Returns "" if not found.
func parseChecksumLine(body, fileName string) string
```

Download the checksums file via the existing `downloadFile` (it handles the
mirror rewrite). Build the checksums URL from the asset's download URL: same
directory (`releases/download/<tag>/`), filename `beszel_<version>_checksums.txt`
where `<version>` is `latest.Tag` (with the `v` prefix already present).

`update()` flow becomes: download asset → download+verify checksum → extract
(with G2 guard) → smoke (G3) → swap. On any verification failure, return an
error; the old binary is untouched (swap not reached).

The checksums file itself is trusted only via GitHub+TLS — same model as the
install scripts. This closes the "asset tampered/corrupted" gap, not the
"release source compromised" gap (out of scope: no signing).

### G3 — Pre-swap smoke test

After extraction, before the rename/swap, run the extracted binary with
`--version`. Both hub (cobra `RootCmd.Version`) and agent (`-v` flag) support
`--version` and exit 0 with no side effects. On non-zero exit or error, abort
(keep old binary).

```go
// smokeTestBinary runs "<path> --version" and returns nil only if it exits 0.
func smokeTestBinary(path string) error
```

This catches corrupt binaries, wrong-arch builds, and missing dynamic libs
before they replace a working service. On failure, `update()` returns an error
and `.old` is never created.

Edge: a glibc NVML agent binary run on a musl host would fail the smoke test —
correct behavior (don't install a binary that won't run). The archive-suffix
selection already targets the host's libc, so this is a backstop, not the
primary mechanism.

### G4 — Unique `(host, port)` monitors

New migration `internal/migrations/1781654404_monitors_unique_host_port.go`:
drop the old non-unique index, dedupe existing rows (keep the oldest per
host:port, delete the rest), add a unique index. (SQLite/PocketBase: drop via
`col.RemoveIndex(name)` then `col.AddIndex(name, true, ...)`, save.)

Frontend (`monitors.tsx` `addMonitor`): before create, check the current list
for a matching `host` + `port` and show a toast instead of creating. Also
disable on `readonly` (already handled). This is client-side UX; the unique
index is the real guarantee.

`loadMonitorConfig` already builds targets per record; with uniqueness enforced,
no Go-side dedup needed.

### G5 — Malformed `AGENT_REPO` warning

In `hub/update.go`'s `Update` and `agent/update.go`'s `Update`, before calling
`ghupdate.Update`, check: `AGENT_REPO` is non-empty AND does not contain `/` →
log a warning (slog) that it's being ignored and upstream will be used. The
existing resolvers keep their behavior (return "" → upstream default); the
warning makes it visible.

Helper (duplicated in both packages, or a tiny shared one — keeping it local to
each to avoid a new shared package): inline check.

## Testing

- **G2** (`internal/ghupdate/extract_test.go` or `ghupdate_test.go`): build an
  in-memory tar.gz with a `../escape` entry, call `extractTarGz`, assert error.
  Also a valid tar.gz extracts cleanly (regression).
- **G1** (`internal/ghupdate/ghupdate_test.go`): `TestParseChecksumLine` —
  found / not-found / multiple lines.
- **G3** (`internal/ghupdate/ghupdate_test.go`): `TestSmokeTestBinary` — a
  non-existent path errors; the test's own binary (`os.Executable`) with
  `--version`-equivalent... simpler: assert `smokeTestBinary` returns an error
  for a path to an empty/non-exec file, nil for a real executable run with a
  harmless arg. (Use `os.Executable()` as a known-good binary; if it doesn't
  support `--version`, run it with `--help` or a no-op. Hub/agent binaries
  support `--version`; the test binary itself may not — so test the negative
  case robustly and the positive case with `os.Executable()` guarded.)
- **G4**: migration is exercised by the existing `monitors_test.go` harness
  indirectly; add a focused test creating two same-host:port records and
  asserting the second is rejected (or, post-migration, the unique index
  rejects it). Frontend: Biome + build only.
- **G5** (`internal/hub/api_test.go` or `update_test.go`): the warning is a
  log side-effect; test the predicate (is `AGENT_REPO` malformed) rather than
  the log. Add a small `agentRepoMalformed() bool` and test it.

## Files touched

| File | Change |
|---|---|
| `internal/ghupdate/extract.go` | `sanitizeExtractPath` + tar guard |
| `internal/ghupdate/ghupdate.go` | `parseChecksumLine`, checksum verify step, `smokeTestBinary`, smoke step in `update()` |
| `internal/ghupdate/ghupdate_test.go` | tar-slip, parseChecksumLine, smoke tests |
| `internal/migrations/1781654404_monitors_unique_host_port.go` (new) | unique index + dedupe |
| `internal/site/src/components/routes/settings/monitors.tsx` | duplicate-prevention toast in `addMonitor` |
| `internal/hub/update.go` | malformed `AGENT_REPO` warning + `agentRepoMalformed` predicate |
| `agent/update.go` | malformed `AGENT_REPO` warning |
| `internal/hub/update_test.go` | `agentRepoMalformed` test |

No entity/CBOR/protocol changes. One new migration (additive + index change,
no schema field changes). `internal/ghupdate` diverges further from upstream
PocketBase — accepted (already diverged via P0 G3); documented in the fork
notes.
