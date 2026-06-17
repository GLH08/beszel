import { Trans } from "@lingui/react/macro"
import { ArrowDownIcon, ArrowUpIcon, GaugeIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { Card, CardHeader, CardTitle } from "@/components/ui/card"
import { pb } from "@/lib/api"
import { cn, decimalString, formatBytes } from "@/lib/utils"

interface TrafficSummary {
	period: string // "2006-01-02"
	bytes_up: number
	bytes_down: number
	quota_gib: number // 0 = unlimited
	reset_day: number
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

/** Renders the current billing-cycle traffic usage and quota progress for a system. */
export function TrafficCard({ systemId }: { systemId: string }) {
	const [summary, setSummary] = useState<TrafficSummary | null>(null)

	useEffect(() => {
		let active = true
		const fetchSummary = async () => {
			try {
				const data = await pb.send<TrafficSummary>("/api/beszel/traffic", { query: { system: systemId } })
				if (active) setSummary(data)
			} catch {
				// ignore — card simply stays hidden until data is available
			}
		}
		fetchSummary()
		const timer = setInterval(fetchSummary, 30_000)
		return () => {
			active = false
			clearInterval(timer)
		}
	}, [systemId])

	// hide until we have data, and hide unlimited quotas with no usage yet
	if (!summary) return null
	const unlimited = !summary.quota_gib || summary.quota_gib <= 0
	const used = summary.bytes_up + summary.bytes_down
	if (unlimited && used === 0) return null

	const quotaBytes = summary.quota_gib * 1024 * 1024 * 1024
	const pct = unlimited ? 0 : Math.min(100, (used / quotaBytes) * 100)
	const up = formatBytes(summary.bytes_up)
	const down = formatBytes(summary.bytes_down)

	return (
		<Card className="@container w-full px-3 py-5 sm:py-6 sm:px-6">
			<CardHeader className="p-0 mb-3 sm:mb-4">
				<div className="px-2 sm:px-1">
					<CardTitle className="mb-2 flex items-center gap-2">
						<GaugeIcon className="size-4" />
						<Trans>Monthly Traffic</Trans>
					</CardTitle>
					<div className="text-sm text-muted-foreground">
						<Trans>Cycle</Trans> {periodLabel(summary.period)}
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
								{summary.quota_gib} GiB ({decimalString(pct, 1)}%)
							</span>
						</div>
						<span className="grid bg-muted h-2.5 rounded-sm overflow-hidden">
							<span className={meterClass(pct)} style={{ width: `${pct}%` }} />
						</span>
					</div>
				)}
				<div className="flex gap-6 text-sm tabular-nums">
					<span className="flex items-center gap-1.5">
						<ArrowUpIcon className="size-3.5 text-muted-foreground" />
						{up.value.toFixed(1)} {up.unit}
					</span>
					<span className="flex items-center gap-1.5">
						<ArrowDownIcon className="size-3.5 text-muted-foreground" />
						{down.value.toFixed(1)} {down.unit}
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
