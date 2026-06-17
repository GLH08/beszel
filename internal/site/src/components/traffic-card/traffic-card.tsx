import { Trans } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { ArrowDownIcon, ArrowUpIcon, GaugeIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { Card, CardHeader, CardTitle } from "@/components/ui/card"
import { pb } from "@/lib/api"
import { $allSystemsById } from "@/lib/stores"
import { cn, decimalString, formatBytes } from "@/lib/utils"

interface TrafficSummary {
	period: string // "2006-01-02"
	bytes_up: number
	bytes_down: number
	quota_gib: number // 0 = unlimited
	reset_day: number
	has_history: boolean // any prior cycle accumulated bytes
}

/** Formats a billing-cycle period start date as "Jun 15 – Jul 14". */
function periodLabel(period: string): string {
	const start = new Date(`${period}T00:00:00`)
	if (Number.isNaN(start.getTime())) return period
	const end = new Date(start)
	end.setMonth(end.getMonth() + 1)
	end.setDate(end.getDate() - 1)
	const fmt = (d: Date) => d.toLocaleDateString(undefined, { month: "short", day: "numeric" })
	return `${fmt(start)} – ${fmt(end)}`
}

function meterClass(pct: number) {
	return cn("h-full", pct >= 100 ? "bg-destructive" : pct >= 80 ? "bg-orange-500" : "bg-primary")
}

/** Renders the current billing-cycle traffic usage, quota progress, and live rate. */
export function TrafficCard({ systemId }: { systemId: string }) {
	// quota comes from the live systems store so editing it reflects immediately
	const system = useStore($allSystemsById)?.[systemId]
	const quotaGiB = system?.traffic_quota ?? 0

	const [summary, setSummary] = useState<TrafficSummary | null>(null)
	const [rate, setRate] = useState<[number, number]>([0, 0]) // [sent, recv] bytes/s

	// poll cumulative usage; quota is taken from the store so a quota edit alone
	// doesn't require a refetch, but we re-query when the period/quota changes
	useEffect(() => {
		let active = true
		const fetchSummary = async () => {
			try {
				const data = await pb.send<TrafficSummary>("/api/beszel/traffic", { query: { system: systemId } })
				if (active) setSummary(data)
			} catch {
				// ignore — card stays hidden until data is available
			}
		}
		fetchSummary()
		const timer = setInterval(fetchSummary, 30_000)
		return () => {
			active = false
			clearInterval(timer)
		}
	}, [systemId])

	// live upload/download rate from the realtime stream
	useEffect(() => {
		let active = true
		let unsub: (() => void) | undefined
		pb.realtime
			.subscribe(
				`rt_metrics`,
				(data: { stats: { b?: [number, number] } }) => {
					if (active && data.stats?.b) setRate(data.stats.b)
				},
				{ query: { system: systemId } }
			)
			.then((u) => {
				unsub = u
			})
		return () => {
			active = false
			unsub?.()
		}
	}, [systemId])

	const bytesUp = summary?.bytes_up ?? 0
	const bytesDown = summary?.bytes_down ?? 0
	const period = summary?.period
	const unlimited = !quotaGiB || quotaGiB <= 0
	const used = bytesUp + bytesDown

	// hide until we have data, and hide unlimited quotas with no usage yet
	if (!summary) return null
	if (unlimited && used === 0 && !summary.has_history) return null

	const quotaBytes = quotaGiB * 1024 * 1024 * 1024
	const pct = unlimited ? 0 : Math.min(100, (used / quotaBytes) * 100)
	const upTotal = formatBytes(bytesUp)
	const downTotal = formatBytes(bytesDown)
	const upRate = formatBytes(rate[0], true)
	const downRate = formatBytes(rate[1], true)

	return (
		<Card className="@container w-full px-3 py-5 sm:py-6 sm:px-6">
			<CardHeader className="p-0 mb-3 sm:mb-4">
				<div className="px-2 sm:px-1">
					<CardTitle className="mb-2 flex items-center gap-2">
						<GaugeIcon className="size-4" />
						<Trans>Monthly Traffic</Trans>
					</CardTitle>
					<div className="text-sm text-muted-foreground">
						<Trans>Cycle</Trans> {period ? periodLabel(period) : ""}
					</div>
				</div>
			</CardHeader>
			<div className="px-2 sm:px-1 space-y-3">
				{!unlimited && (
					<div className="grid gap-1.5">
						<div className="flex justify-between text-sm tabular-nums">
							<span className="font-medium">
								{formatBytes(used).value.toFixed(1)} {formatBytes(used).unit}
							</span>
							<span className="text-muted-foreground">
								{quotaGiB} GiB ({decimalString(pct, 1)}%)
							</span>
						</div>
						<span className="grid bg-muted h-2.5 rounded-sm overflow-hidden">
							<span className={meterClass(pct)} style={{ width: `${pct}%` }} />
						</span>
					</div>
				)}
				<div className="flex flex-wrap gap-x-6 gap-y-1 text-sm tabular-nums">
					<span className="flex items-center gap-1.5">
						<ArrowUpIcon className="size-3.5 text-muted-foreground" />
						{upTotal.value.toFixed(1)} {upTotal.unit}
						<span className="text-muted-foreground">
							({upRate.value.toFixed(1)} {upRate.unit})
						</span>
					</span>
					<span className="flex items-center gap-1.5">
						<ArrowDownIcon className="size-3.5 text-muted-foreground" />
						{downTotal.value.toFixed(1)} {downTotal.unit}
						<span className="text-muted-foreground">
							({downRate.value.toFixed(1)} {downRate.unit})
						</span>
					</span>
					{unlimited && (
						<span className="ml-auto text-muted-foreground">
							<Trans>Unlimited</Trans>
						</span>
					)}
				</div>
			</div>
		</Card>
	)
}
