import { atom } from "nanostores"
import { pb } from "@/lib/api"

export interface TrafficSummary {
	period: string
	bytes_up: number
	bytes_down: number
	quota_gib: number // 0 = unlimited
	reset_day: number
	has_history: boolean
}

// $trafficSummaries holds the monthly-traffic summary per system id, fetched
// in one batch via /api/beszel/traffic/all for the home page column. Polled
// every 30s while the systems table is mounted.
export const $trafficSummaries = atom<Record<string, TrafficSummary>>({})

let initialized = false

/** Starts polling /api/beszel/traffic/all every 30s. Idempotent. */
export function initTrafficSummaries() {
	if (initialized) return
	initialized = true
	const fetchAll = async () => {
		try {
			const data = await pb.send<Record<string, TrafficSummary>>("/api/beszel/traffic/all", {})
			$trafficSummaries.set(data ?? {})
		} catch {
			// ignore — column just shows nothing until next poll
		}
	}
	fetchAll()
	const timer = setInterval(fetchAll, 30_000)
	// best-effort: clear on page hide. The interval lives for the app lifetime;
	// polling is cheap (one request, ~12 rows) for a personal deployment.
	window.addEventListener("pagehide", () => clearInterval(timer), { once: true })
}
