import { t } from "@lingui/core/macro"
import { useEffect, useMemo, useState } from "react"
import LineChartDefault from "@/components/charts/line-chart"
import { pb } from "@/lib/api"
import { decimalString, toFixedFloat } from "@/lib/utils"
import type { ChartData, MonitorRecord, SystemStatsRecord } from "@/types"
import { ChartCard } from "../chart-card"

// Latency line chart: one line per configured ping target, driven by the
// persisted stats.p map (per-target latest latency). Loss% is shown in the
// tooltip. Hidden when there are no ping targets or no latency data.
export function LatencyChart({
	chartData,
	grid,
	dataEmpty,
}: {
	chartData: ChartData
	grid: boolean
	dataEmpty: boolean
}) {
	// fetch monitor records once for human-readable line labels (id -> name)
	const [nameById, setNameById] = useState<Record<string, string>>({})
	useEffect(() => {
		let active = true
		pb.collection<MonitorRecord>("monitors")
			.getFullList({ fields: "id,name" })
			.then((items) => {
				if (!active) return
				const map: Record<string, string> = {}
				for (const m of items) map[m.id] = m.name
				setNameById(map)
			})
			.catch(() => {})
		return () => {
			active = false
		}
	}, [])

	// Derive target ids present in the loaded history (newest-first scan so the
	// active target set is used even if a target was removed mid-range).
	const targetIdsKey = useMemo(() => {
		const ids = new Set<string>()
		for (let i = chartData.systemStats.length - 1; i >= 0; i--) {
			const p = chartData.systemStats[i].stats?.p
			if (p) {
				for (const id of Object.keys(p)) ids.add(id)
			}
		}
		return Array.from(ids).sort().join("\0")
	}, [chartData.systemStats])

	const sortedIds = targetIdsKey ? targetIdsKey.split("\0") : []

	const dataPoints = useMemo(() => {
		return sortedIds.map((id, i) => ({
			label: nameById[id] ?? id,
			color: `hsl(${((i * 360) / Math.max(sortedIds.length, 1)) % 360}, 70%, 50%)`,
			dataKey: ({ stats }: SystemStatsRecord) => stats?.p?.[id]?.l,
		}))
	}, [sortedIds, nameById])

	// hide when no targets configured and no latency data
	const hasData = sortedIds.length > 0
	if (!hasData) return null

	return (
		<ChartCard
			empty={dataEmpty}
			grid={grid}
			title={t`Latency`}
			description={t`Ping latency to configured targets`}
			legend={true}
		>
			<LineChartDefault
				chartData={chartData}
				legend={true}
				tickFormatter={(value) => String(toFixedFloat(value, 1))}
				contentFormatter={(item, key) => {
					// find the target id from the label to read loss
					const id = sortedIds.find((tid) => (nameById[tid] ?? tid) === key) ?? key
					const loss = chartData.systemStats
						.map((d) => d.stats?.p?.[id]?.lo)
						.find((v) => typeof v === "number" && v > 0)
					const latency = decimalString(item.value, 1)
					if (typeof loss === "number" && loss > 0) {
						return `${latency} ms (${Math.round(loss)}% ${t`loss`})`
					}
					return `${latency} ms`
				}}
				dataPoints={dataPoints}
			/>
		</ChartCard>
	)
}
