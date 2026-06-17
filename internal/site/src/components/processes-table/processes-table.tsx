import { Trans } from "@lingui/react/macro"
import { ListTreeIcon } from "lucide-react"
import { Card, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { decimalString } from "@/lib/utils"
import type { Process } from "@/types"

/** Renders the top processes by CPU usage, fed from the realtime stream. */
export function ProcessesTable({ processes }: { processes: Process[] }) {
	if (!processes?.length) {
		return null
	}

	return (
		<Card className="@container w-full px-3 py-5 sm:py-6 sm:px-6">
			<CardHeader className="p-0 mb-3 sm:mb-4">
				<div className="px-2 sm:px-1">
					<CardTitle className="mb-2 flex items-center gap-2">
						<ListTreeIcon className="size-4" />
						<Trans>Top Processes</Trans>
					</CardTitle>
					<div className="text-sm text-muted-foreground">
						<Trans>Most CPU-intensive processes, updated live.</Trans>
					</div>
				</div>
			</CardHeader>
			<div className="rounded-md border">
				<Table className="text-sm">
					<TableHeader className="sticky top-0 z-50">
						<TableRow>
							<TableHead className="px-3 w-20">
								<Trans>PID</Trans>
							</TableHead>
							<TableHead className="px-3 w-32">
								<Trans>User</Trans>
							</TableHead>
							<TableHead className="px-3">
								<Trans>Command</Trans>
							</TableHead>
							<TableHead className="px-3 w-24 text-right">
								<Trans>CPU</Trans>
							</TableHead>
							<TableHead className="px-3 w-24 text-right">
								<Trans>Memory</Trans>
							</TableHead>
						</TableRow>
					</TableHeader>
					<TableBody>
						{processes.map((p) => (
							<TableRow key={p.pid}>
								<TableCell className="px-3 py-1.5 font-mono">{p.pid}</TableCell>
								<TableCell className="px-3 py-1.5 truncate max-w-40" title={p.u}>
									{p.u || "—"}
								</TableCell>
								<TableCell className="px-3 py-1.5 font-mono truncate max-w-96" title={p.c}>
									{p.c || "—"}
								</TableCell>
								<TableCell className="px-3 py-1.5 text-right tabular-nums">{decimalString(p.cpu, 1)}%</TableCell>
								<TableCell className="px-3 py-1.5 text-right tabular-nums">{decimalString(p.m, 1)}%</TableCell>
							</TableRow>
						))}
					</TableBody>
				</Table>
			</div>
		</Card>
	)
}
