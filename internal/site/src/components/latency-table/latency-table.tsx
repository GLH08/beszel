import { Trans } from "@lingui/react/macro"
import { ActivityIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { Card, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { pb } from "@/lib/api"
import { $systems } from "@/lib/stores"
import { useStore } from "@nanostores/react"
import type { MonitorRecord, PingResult } from "@/types"

interface PingSummary {
	system: string
	status: string
	results?: Record<string, PingResult>
}

/** Renders a Nezha-style latency grid: one row per system, one column per ping target. */
export function LatencyTable() {
	const systems = useStore($systems)
	const [monitors, setMonitors] = useState<MonitorRecord[]>([])
	const [summaries, setSummaries] = useState<PingSummary[]>([])

	// load ping targets
	useEffect(() => {
		let active = true
		pb.collection<MonitorRecord>("monitors")
			.getFullList({ sort: "created" })
			.then((items) => {
				if (active) setMonitors(items.filter((m) => m.enabled))
			})
			.catch(() => {})
		return () => {
			active = false
		}
	}, [])

	// poll latest ping results
	useEffect(() => {
		if (monitors.length === 0) return
		let active = true
		const fetchPing = async () => {
			try {
				const data = await pb.send<PingSummary[]>("/api/beszel/ping", {})
				if (active) setSummaries(data)
			} catch {
				// ignore
			}
		}
		fetchPing()
		const timer = setInterval(fetchPing, 10_000)
		return () => {
			active = false
			clearInterval(timer)
		}
	}, [monitors.length])

	if (monitors.length === 0) return null

	// map system id -> name, preserve systems-table order
	const systemName = (id: string) => systems.find((s) => s.id === id)?.name ?? id
	const summaryBySystem = new Map(summaries.map((s) => [s.system, s]))

	return (
		<Card className="w-full px-3 py-5 sm:py-6 sm:px-6">
			<CardHeader className="p-0 mb-3 sm:mb-4">
				<div className="px-2 sm:px-1">
					<CardTitle className="flex items-center gap-2">
						<ActivityIcon className="size-4" />
						<Trans>Latency</Trans>
					</CardTitle>
				</div>
			</CardHeader>
			<div className="rounded-md border">
				<Table className="text-sm">
					<TableHeader className="sticky top-0 z-50">
						<TableRow>
							<TableHead className="px-3">
								<Trans>System</Trans>
							</TableHead>
							{monitors.map((m) => (
								<TableHead key={m.id} className="px-3 text-right" title={`${m.host}:${m.port}`}>
									{m.name}
								</TableHead>
							))}
						</TableRow>
					</TableHeader>
					<TableBody>
						{systems.map((sys) => {
							const sum = summaryBySystem.get(sys.id)
							return (
								<TableRow key={sys.id}>
									<TableCell className="px-3 py-1.5 truncate max-w-40">{sys.name}</TableCell>
									{monitors.map((m) => {
										const r = sum?.results?.[m.id]
										return (
											<TableCell key={m.id} className="px-3 py-1.5 text-right tabular-nums">
												<LatencyCell result={r} />
											</TableCell>
										)
									})}
								</TableRow>
							)
						})}
					</TableBody>
				</Table>
			</div>
		</Card>
	)
}

function LatencyCell({ result }: { result?: PingResult }) {
	if (!result) {
		return <span className="text-muted-foreground">—</span>
	}
	if (result.l <= 0 && result.lo >= 100) {
		return <span className="text-destructive">timeout</span>
	}
	const color = result.lo >= 50 ? "text-destructive" : result.l >= 200 ? "text-orange-500" : "text-emerald-500"
	const loss = result.lo > 0 ? ` (${Math.round(result.lo)}%)` : ""
	return (
		<span className={color}>
			{result.l.toFixed(1)} ms{loss}
		</span>
	)
}
