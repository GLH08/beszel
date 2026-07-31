# Server Expiry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-system expiry column to the home systems table with manual date setup (or "permanent"), and a one-click renew button that advances the end date by a preset monthly/yearly cycle.

**Architecture:** Hub-only feature (no agent/CBOR changes). New `systems` collection fields store expiry metadata; a pure Go function computes renewal dates (TDD-tested); a `POST /systems/renew` endpoint mutates the record (PocketBase realtime pushes the update to the frontend). The home table reads expiry fields directly off the `SystemRecord` (no extra store/poll, unlike the traffic column).

**Tech Stack:** Go + PocketBase v0.36.8 (`core.DateField`, `core.SelectField`), React 19 + TanStack Table + Lingui (English source strings, translations in `.po`), Biome for lint.

## Global Constraints

- All Go tests require `-tags=testing` (test files carry `//go:build testing`).
- All custom work goes on the `custom` branch; never commit to `main`.
- CBOR wire-protocol field numbers are not touched by this feature (hub + frontend only; no `internal/entities`, no `MinVersion*` bumps).
- Frontend i18n source strings are **English** in `t`...`` / `<Trans>` macros (e.g. `t`System``); Chinese goes in `src/locales/zh/zh.po` and `zh-CN/zh-CN.po`.
- Frontend uses **Biome** (`bun run check`), not ESLint/Prettier. Prefer `bun`, fall back to `npm`.
- Build hygiene: after `go build`/`go test`/`bun run build`, remove `./build/` binaries and any `internal/site/dist` so the worktree stays clean.
- `internal/hub` package tests cannot run on Windows (they import `agent` → Windows LHM `.NET` embed needs `dotnet`). Use `GOOS=linux go build ./...` + `go vet ./...` for the hub package. The `internal/hub/systems` package does NOT import `agent`, so its tests run natively on Windows.

---

## File Structure

| File | Responsibility | Action |
|------|----------------|--------|
| `internal/migrations/1785542400_add_expire.go` | Adds `expire_type`, `expire_start`, `expire_end`, `renew_cycle` to `systems` | Create |
| `internal/hub/systems/expiry.go` | `computeRenewal` pure fn + `(*System).Renew()` method | Create |
| `internal/hub/systems/expiry_test.go` | Tests for `computeRenewal` | Create |
| `internal/hub/api.go` | Register `/systems/renew` route + `renewSystem` handler | Modify |
| `internal/site/src/types.d.ts` | `SystemRecord` expiry fields | Modify |
| `internal/site/src/lib/systemsManager.ts` | Add expiry fields to `FIELDS_DEFAULT` | Modify |
| `internal/site/src/components/systems-table/systems-table-columns.tsx` | New `expiry` column + `RenewButton` | Modify |
| `internal/site/src/components/add-system.tsx` | Expiry fields in `SystemDialog` | Modify |
| `internal/site/src/locales/zh/zh.po`, `zh-CN/zh-CN.po` | Chinese translations | Modify |
| other `src/locales/*/*.po` | Extracted empty entries (lingui sync) | Modify |

---

## Task 1: Database migration — add expiry fields

**Files:**
- Create: `internal/migrations/1785542400_add_expire.go`

**Interfaces:**
- Produces: `systems` collection fields `expire_type` (select: `permanent`,`fixed`), `expire_start` (date), `expire_end` (date), `renew_cycle` (select: `month`,`year`). All optional. Consumed by Task 2 (Go) and Tasks 3-5 (frontend).

- [ ] **Step 1: Write the migration**

Create `internal/migrations/1785542400_add_expire.go`:

```go
package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds per-system expiry tracking fields to the systems collection:
// expire_type ("" | permanent | fixed), expire_start, expire_end (dates),
// renew_cycle (month | year). All optional / additive. See
// docs/superpowers/specs/2026-07-31-server-expiry-design.md
func init() {
	m.Register(func(app core.App) error {
		sysCol, err := app.FindCollectionByNameOrId("systems")
		if err != nil {
			return err
		}
		// expire_type: "" = not configured, "permanent" = no expiry, "fixed" = has end date
		sysCol.Fields.Add(&core.SelectField{
			Name:   "expire_type",
			Values: []string{"permanent", "fixed"},
		})
		sysCol.Fields.Add(&core.DateField{
			Name: "expire_start",
		})
		sysCol.Fields.Add(&core.DateField{
			Name: "expire_end",
		})
		// renew_cycle: only meaningful when expire_type == "fixed"; UI defaults to "month"
		sysCol.Fields.Add(&core.SelectField{
			Name:   "renew_cycle",
			Values: []string{"month", "year"},
		})
		return app.Save(sysCol)
	}, func(app core.App) error {
		return nil
	})
}
```

- [ ] **Step 2: Verify it compiles (linux target, since hub pkg has build-tag splits)**

Run: `GOOS=linux go build ./internal/migrations/...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/migrations/1785542400_add_expire.go
git commit -m "feat(db): add systems expiry fields migration"
```

---

## Task 2: Renewal logic + API (Go, TDD)

**Files:**
- Create: `internal/hub/systems/expiry.go`
- Create: `internal/hub/systems/expiry_test.go`
- Modify: `internal/hub/api.go` (register route + add `renewSystem` handler)

**Interfaces:**
- Consumes: `systems` fields from Task 1; `(*System).getRecord(app)` (package-internal), `sys.manager.hub` (`core.App`), `(*System).HasUser`.
- Produces: `computeRenewal(end time.Time, cycle string, now time.Time) time.Time` (pure); `(*System).Renew() (time.Time, error)`; HTTP handler `(*Hub).renewSystem` at `POST /api/beszel/systems/renew?system=<id>`.

- [ ] **Step 1: Write the failing test**

Create `internal/hub/systems/expiry_test.go`:

```go
//go:build testing

package systems

import (
	"testing"
	"time"
)

func TestComputeRenewal(t *testing.T) {
	now := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		end   time.Time
		cycle string
		want  time.Time
	}{
		{
			name:  "month not yet expired renews from end",
			end:   time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
			cycle: "month",
			want:  time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "year not yet expired renews from end",
			end:   time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
			cycle: "year",
			want:  time.Date(2027, 8, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "month expired renews from today",
			end:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			cycle: "month",
			want:  time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "year expired renews from today",
			end:   time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
			cycle: "year",
			want:  time.Date(2027, 7, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "empty cycle defaults to month",
			end:   time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
			cycle: "",
			want:  time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeRenewal(tt.end, tt.cycle, now)
			if !got.Equal(tt.want) {
				t.Errorf("computeRenewal(%v, %q, %v) = %v, want %v", tt.end, tt.cycle, now, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/hub/systems/... -run TestComputeRenewal -v`
Expected: FAIL / compile error (`computeRenewal` undefined).

- [ ] **Step 3: Write minimal implementation**

Create `internal/hub/systems/expiry.go`:

```go
package systems

import (
	"errors"
	"time"

	"github.com/pocketbase/pocketbase/tools/types"
)

// computeRenewal returns the next expiry date after advancing one cycle.
// If end is already in the past, renewal starts from now (so the result is a
// future date). cycle "year" adds a year; anything else (incl. "") adds a month.
func computeRenewal(end time.Time, cycle string, now time.Time) time.Time {
	base := end
	if base.Before(now) {
		base = now
	}
	if cycle == "year" {
		return base.AddDate(1, 0, 0)
	}
	return base.AddDate(0, 1, 0)
}

// Renew advances the system's expire_end by one renewal cycle and persists it.
// Returns the new expiry time. Returns an error if the system has no fixed
// expiry (expire_type != "fixed" or no expire_end).
func (sys *System) Renew() (time.Time, error) {
	record, err := sys.getRecord(sys.manager.hub)
	if err != nil {
		return time.Time{}, err
	}
	if record.GetString("expire_type") != "fixed" {
		return time.Time{}, errors.New("system has no fixed expiry date")
	}
	end := record.GetDateTime("expire_end").Time()
	if end.IsZero() {
		return time.Time{}, errors.New("system has no expiry date")
	}
	cycle := record.GetString("renew_cycle")
	newEnd := computeRenewal(end, cycle, time.Now())
	record.Set("expire_end", types.DateTime(newEnd))
	if err := sys.manager.hub.SaveNoValidate(record); err != nil {
		return time.Time{}, err
	}
	return newEnd, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags=testing ./internal/hub/systems/... -run TestComputeRenewal -v`
Expected: PASS.

- [ ] **Step 5: Add the HTTP handler + route registration**

In `internal/hub/api.go`:

1. Inside `registerApiRoutes`, after the `/traffic/all` line, add:
```go
	// renew a system's expiry date by one cycle
	apiAuth.POST("/systems/renew", h.renewSystem).Bind(excludeReadOnlyRole)
```

2. Add the handler (e.g. near `refreshSmartData` at the end of the file):
```go
// renewSystem handles POST /api/beszel/systems/renew?system=<id>
// Advances the system's expire_end by one renewal cycle (month or year).
func (h *Hub) renewSystem(e *core.RequestEvent) error {
	systemID := e.Request.URL.Query().Get("system")
	if systemID == "" {
		return e.BadRequestError("Invalid system parameter", nil)
	}
	sys, err := h.sm.GetSystem(systemID)
	if err != nil || !sys.HasUser(e.App, e.Auth) {
		return e.NotFoundError("", nil)
	}
	newEnd, err := sys.Renew()
	if err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	return e.JSON(http.StatusOK, map[string]string{"expire_end": newEnd.Format("2006-01-02")})
}
```

- [ ] **Step 6: Verify build + vet (linux target for hub pkg)**

Run:
```
go test -tags=testing ./internal/hub/systems/... -v
GOOS=linux go build ./...
GOOS=linux go vet ./...
```
Expected: tests pass; build + vet clean.

- [ ] **Step 7: Commit**

```bash
git add internal/hub/systems/expiry.go internal/hub/systems/expiry_test.go internal/hub/api.go
git commit -m "feat(hub): add system expiry renewal endpoint and logic"
```

---

## Task 3: Frontend types + systems manager fields

**Files:**
- Modify: `internal/site/src/types.d.ts` (`SystemRecord`)
- Modify: `internal/site/src/lib/systemsManager.ts` (`FIELDS_DEFAULT`)

**Interfaces:**
- Produces: `SystemRecord.expire_type`, `expire_start`, `expire_end`, `renew_cycle` on the type; these fields are now returned by the systems list fetch + realtime subscription. Consumed by Tasks 4 & 5.

- [ ] **Step 1: Add fields to SystemRecord**

In `internal/site/src/types.d.ts`, in the `SystemRecord` interface, after `traffic_reset_day?: number`:

```ts
	/** expiry type: "" = not set, "permanent" = no expiry, "fixed" = has end date */
	expire_type?: "" | "permanent" | "fixed"
	/** start date YYYY-MM-DD (fixed only) */
	expire_start?: string
	/** end date YYYY-MM-DD (fixed only) */
	expire_end?: string
	/** renewal cycle (fixed only); UI defaults to "month" */
	renew_cycle?: "month" | "year"
```

- [ ] **Step 2: Add fields to FIELDS_DEFAULT**

In `internal/site/src/lib/systemsManager.ts`, change the `FIELDS_DEFAULT` constant:

```ts
const FIELDS_DEFAULT =
	"id,name,host,port,info,status,traffic_quota,traffic_reset_day,expire_type,expire_start,expire_end,renew_cycle"
```

> This single change makes both the initial `getFullList` fetch and the realtime `subscribe` carry the expiry fields, so the column renders on load and updates automatically after a renew (PocketBase pushes the "update" event).

- [ ] **Step 3: Verify (Biome + type check)**

Run:
```
cd internal/site
bun run check
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/site/src/types.d.ts internal/site/src/lib/systemsManager.ts
git commit -m "feat(ui): expose system expiry fields to frontend"
```

---

## Task 4: Home page expiry column + renew button

**Files:**
- Modify: `internal/site/src/components/systems-table/systems-table-columns.tsx`

**Interfaces:**
- Consumes: `SystemRecord.expire_type/expire_end/renew_cycle` (Task 3); `pb.send` to `/api/beszel/systems/renew` (Task 2).
- Produces: a new `expiry` column in `SystemsTableColumns`; a `RenewButton` component. The realtime subscription (Task 3) updates the record after renew — no manual store mutation needed.

- [ ] **Step 1: Add the import for CalendarClock icon**

In the lucide-react import block at the top of `systems-table-columns.tsx`, add `CalendarClock` to the existing import list (it imports many icons from `"lucide-react"`):

```ts
	CalendarClock,
```

Add `pb` to the existing `@/lib/api` import if not already present (it already imports `isReadOnlyUser, pb`).

- [ ] **Step 2: Add the RenewButton component**

Add this component near the other helpers (e.g. after `IndicatorDot`):

```tsx
/** Small inline button shown in the expiry column when a system is within the
 * renew window (<= 7 days) or already expired. POSTs to /systems/renew; the
 * realtime subscription updates the record, so no manual store mutation. */
export const RenewButton = memo(({ systemId }: { systemId: string }) => {
	const [loading, setLoading] = useState(false)

	async function handleRenew(e: React.MouseEvent) {
		e.preventDefault()
		e.stopPropagation()
		setLoading(true)
		try {
			await pb.send("/api/beszel/systems/renew", {
				method: "POST",
				params: { system: systemId },
			})
		} catch (err) {
			console.error("Renew failed:", err)
		} finally {
			setLoading(false)
		}
	}

	return (
		<Button
			variant="outline"
			size="sm"
			className="h-6 px-2 py-0 text-xs relative z-10"
			onClick={handleRenew}
			disabled={loading}
		>
			<Trans>Renew</Trans>
		</Button>
	)
})
```

- [ ] **Step 3: Add the expiry column definition**

Inside the `SystemsTableColumns` return array, add this column (e.g. right before the `actions` column):

```tsx
		{
			id: "expiry",
			accessorFn: ({ expire_end }) => expire_end || undefined,
			name: () => t({ message: "Expiry", comment: "Server expiry column in systems table" }),
			size: 60,
			Icon: CalendarClock,
			header: sortableHeader,
			hideSort: true,
			sortUndefined: "last",
			cell(info) {
				const sys = info.row.original
				const type = sys.expire_type
				if (!type) return null
				if (type === "permanent") {
					return (
						<span className="text-muted-foreground text-xs whitespace-nowrap">
							<Trans>Permanent</Trans>
						</span>
					)
				}
				// fixed
				const end = sys.expire_end
				if (!end) return null
				const today = new Date()
				today.setHours(0, 0, 0, 0)
				const endDate = new Date(`${end}T00:00:00`)
				const days = Math.ceil((endDate.getTime() - today.getTime()) / 86_400_000)

				let color = ""
				let label = ""
				if (days < 0) {
					color = "text-red-500"
					label = t`${-days} days ago`
				} else if (days <= 7) {
					color = "text-red-500"
					label = t`${days} days left`
				} else if (days <= 30) {
					color = "text-yellow-500"
					label = t`${days} days left`
				} else {
					label = t`${days} days left`
				}

				const showRenew = days <= 7
				return (
					<div className="flex items-center gap-1 tabular-nums whitespace-nowrap">
						<span className="flex flex-col leading-tight">
							<span className={color}>{end}</span>
							<span className={cn("text-xs", color || "text-muted-foreground")}>{label}</span>
						</span>
						{showRenew && <RenewButton systemId={sys.id} />}
					</div>
				)
			},
		},
```

- [ ] **Step 4: Verify (Biome + build)**

Run:
```
cd internal/site
bun run check
bun run build
```
Expected: no errors; `dist/` builds. (Do NOT commit `dist/`.)

- [ ] **Step 5: Commit**

```bash
git add internal/site/src/components/systems-table/systems-table-columns.tsx
git commit -m "feat(ui): add expiry column and renew button to systems table"
```

---

## Task 5: Edit dialog — expiry fields

**Files:**
- Modify: `internal/site/src/components/add-system.tsx`

**Interfaces:**
- Consumes: `SystemRecord` expiry fields (Task 3).
- Produces: form fields `expire_type`, `expire_start`, `expire_end`, `renew_cycle` submitted via the existing `handleSubmit` `FormData` → `pb.collection("systems").create/update`.

- [ ] **Step 1: Add expire-type state to SystemDialog**

In `internal/site/src/components/add-system.tsx`, inside `SystemDialog`, next to the other `useState` calls (after `const [token, setToken] = ...`), add:

```tsx
	const [expireType, setExpireType] = useState(system?.expire_type ?? "")
```

- [ ] **Step 2: Add expiry fields to the form grid**

In the `<div className="grid xs:grid-cols-[auto_1fr] ...">` block, after the `traffic_reset_day` Label+Input pair, add:

```tsx
						<Label htmlFor="expire_type" className="xs:text-end">
							<Trans>Expiry</Trans>
						</Label>
						<select
							id="expire_type"
							name="expire_type"
							value={expireType}
							onChange={(e) => setExpireType(e.target.value)}
							className="bg-transparent border border-input rounded-md h-9 px-2 text-sm"
						>
							<option value="">
								<Trans>None</Trans>
							</option>
							<option value="permanent">
								<Trans>Permanent</Trans>
							</option>
							<option value="fixed">
								<Trans>Fixed expiry</Trans>
							</option>
						</select>
						{expireType === "fixed" && (
							<>
								<Label htmlFor="expire_start" className="xs:text-end">
									<Trans>Start date</Trans>
								</Label>
								<Input
									id="expire_start"
									name="expire_start"
									type="date"
									defaultValue={system?.expire_start}
								/>
								<Label htmlFor="expire_end" className="xs:text-end">
									<Trans>End date</Trans>
								</Label>
								<Input
									id="expire_end"
									name="expire_end"
									type="date"
									defaultValue={system?.expire_end}
								/>
								<Label htmlFor="renew_cycle" className="xs:text-end">
									<Trans>Renew cycle</Trans>
								</Label>
								<select
									id="renew_cycle"
									name="renew_cycle"
									defaultValue={system?.renew_cycle ?? "month"}
									className="bg-transparent border border-input rounded-md h-9 px-2 text-sm"
								>
									<option value="month">
										<Trans>Month</Trans>
									</option>
									<option value="year">
										<Trans>Year</Trans>
									</option>
								</select>
							</>
						)}
```

- [ ] **Step 3: Clear date fields when not fixed in handleSubmit**

In `handleSubmit`, after the existing `data.traffic_reset_day = ...` coercion line, add:

```tsx
		// expire: clear date/cycle fields unless fixed; default cycle to month
		if (data.expire_type !== "fixed") {
			data.expire_start = ""
			data.expire_end = ""
			data.renew_cycle = ""
		} else if (!data.renew_cycle) {
			data.renew_cycle = "month"
		}
```

- [ ] **Step 4: Verify (Biome + build)**

Run:
```
cd internal/site
bun run check
bun run build
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/site/src/components/add-system.tsx
git commit -m "feat(ui): add expiry fields to system add/edit dialog"
```

---

## Task 6: i18n — extract strings + Chinese translations

**Files:**
- Modify: all `internal/site/src/locales/*/*.po` (lingui sync)
- Modify: `internal/site/src/locales/zh/zh.po`, `internal/site/src/locales/zh-CN/zh-CN.po` (Chinese msgstr)

**Interfaces:**
- Consumes: the English source strings added in Tasks 4 & 5 (`Expiry`, `Permanent`, `Renew`, `{n} days left`, `{n} days ago`, `None`, `Fixed expiry`, `Start date`, `End date`, `Renew cycle`, `Month`, `Year`).

- [ ] **Step 1: Extract strings (regenerate catalogs)**

Run:
```
cd internal/site
bun run sync
```
Expected: new entries appear in all 30 locale `.po` files for the 11 new strings. The `en/en.po` msgstr is the English source (auto).

- [ ] **Step 2: Fill Chinese translations**

In `internal/site/src/locales/zh/zh.po` and `internal/site/src/locales/zh-CN/zh-CN.po`, set the `msgstr` for each new `msgid` (match by the English `msgid`):

| msgid (English) | msgstr (Chinese) |
|-----------------|------------------|
| `Expiry` | `到期` |
| `Permanent` | `长期` |
| `Renew` | `续期` |
| `{0} days left` | `还剩 {0} 天` |
| `{0} days ago` | `{0} 天前` |
| `None` | `无` |
| `Fixed expiry` | `到期日` |
| `Start date` | `开始日期` |
| `End date` | `结束日期` |
| `Renew cycle` | `续期周期` |
| `Month` | `月` |
| `Year` | `年` |

> The exact `{0}` placeholder index depends on how lingui extracted the template string; match whatever placeholder syntax appears in the generated `.po` (`{0}` or `{days}`). Keep the placeholder token identical.

- [ ] **Step 3: Verify (Biome + build)**

Run:
```
cd internal/site
bun run check
bun run build
```
Expected: no errors; build compiles the new catalogs.

- [ ] **Step 4: Commit**

```bash
git add internal/site/src/locales
git commit -m "i18n: extract and translate server expiry strings"
```

---

## Task 7: Final integration verify + build hygiene

**Files:** none (verification only)

- [ ] **Step 1: Regenerate dist for Go embed + run Go tests**

Run from repo root:
```
cd internal/site
bun run build
cd ../..
go test -tags=testing ./internal/hub/systems/... -v
GOOS=linux go build ./...
GOOS=linux go vet ./...
```
Expected: UI builds; `TestComputeRenewal` passes; linux build + vet clean.

- [ ] **Step 2: Clean artifacts + confirm clean tree**

Run:
```
Remove-Item -Recurse -Force build -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force internal\site\dist -ErrorAction SilentlyContinue
git status
```
Expected: working tree clean (only source changes committed); no `build/` or `dist/` left.

- [ ] **Step 3: Final commit (only if cleanup surfaced changes)**

If `git status` shows nothing to commit, skip. Otherwise:
```bash
git add -A
git commit -m "chore: clean build artifacts"
```

---

## Notes for the implementer

- **Realtime-driven renew refresh:** after `Renew()` saves the record, PocketBase fires an "update" realtime event. The frontend `systemsManager` subscription (now including expiry fields via `FIELDS_DEFAULT`) refreshes the store, so the column re-renders with the new date. The `RenewButton` does NOT manually mutate the store.
- **Month-end overflow:** Go `AddDate(0,1,0)` overflows (Jan 31 + 1 month = Mar 3). Accepted for a personal tracker; documented in the spec. Do not add clamping unless asked.
- **Renew window:** `days <= 7` shows the button (covers both "within 7 days before" and "expired"). The `7` is hardcoded in the column cell — to change it, edit the `showRenew` condition.
- **No agent changes:** this feature is entirely hub + frontend. Do not touch `internal/entities`, `internal/common`, or `agent/`.
- **Windows test caveat:** only `./internal/hub/systems/...` tests run natively on Windows. The full `./internal/hub/...` suite needs Linux (agent LHM `.NET` embed). Run `GOOS=linux go build/vet` for the hub package.
