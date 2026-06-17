import { Trans } from "@lingui/react/macro"
import { PlusIcon, Trash2Icon } from "lucide-react"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { toast } from "@/components/ui/use-toast"
import { pb } from "@/lib/api"
import { isReadOnlyUser } from "@/lib/api"
import type { MonitorRecord } from "@/types"

/** Settings sub-page for managing global ping (latency-test) targets. */
export default function MonitorsSettings() {
	const [monitors, setMonitors] = useState<MonitorRecord[]>([])
	const [loading, setLoading] = useState(true)
	const readonly = isReadOnlyUser()

	useEffect(() => {
		let active = true
		pb.collection<MonitorRecord>("monitors")
			.getFullList({ sort: "created" })
			.then((items) => {
				if (active) setMonitors(items)
			})
			.catch(() => {})
			.finally(() => active && setLoading(false))
		// subscribe to changes so the table stays in sync
		let unsub: (() => void) | undefined
		pb.collection<MonitorRecord>("monitors")
			.subscribe("*", (e) => {
				setMonitors((prev) => {
					if (e.action === "delete") return prev.filter((m) => m.id !== e.record.id)
					const idx = prev.findIndex((m) => m.id === e.record.id)
					if (idx === -1) return [...prev, e.record]
					const next = [...prev]
					next[idx] = e.record
					return next
				})
			})
			.then((u) => {
				unsub = u
			})
		return () => {
			active = false
			unsub?.()
		}
	}, [])

	async function addMonitor(e: React.FormEvent<HTMLFormElement>) {
		e.preventDefault()
		const form = new FormData(e.target as HTMLFormElement)
		const data = Object.fromEntries(form)
		try {
			await pb.collection("monitors").create({
				name: data.name,
				host: data.host,
				port: Number(data.port) || 443,
				enabled: true,
			})
			;(e.target as HTMLFormElement).reset()
		} catch (err) {
			toast({ title: "Failed to add target", description: String(err), variant: "destructive" })
		}
	}

	async function updateMonitor(id: string, patch: Partial<MonitorRecord>) {
		try {
			await pb.collection("monitors").update(id, patch)
		} catch (err) {
			toast({ title: "Update failed", description: String(err), variant: "destructive" })
		}
	}

	async function deleteMonitor(id: string) {
		try {
			await pb.collection("monitors").delete(id)
		} catch (err) {
			toast({ title: "Delete failed", description: String(err), variant: "destructive" })
		}
	}

	return (
		<div>
			<div>
				<h3 className="text-xl font-medium mb-2">
					<Trans>Ping Targets</Trans>
				</h3>
				<p className="text-sm text-muted-foreground leading-relaxed">
					<Trans>
						Add hosts or IPs to measure latency from each monitored system. Targets are shared across all systems.
					</Trans>
				</p>
			</div>
			<Separator className="my-4" />

			{!readonly && (
				<form onSubmit={addMonitor} className="flex flex-wrap gap-2 items-end mb-4">
					<div className="grid gap-1.5">
						<Label htmlFor="name" className="text-xs">
							<Trans>Name</Trans>
						</Label>
						<Input id="name" name="name" required className="w-36" placeholder="Google" />
					</div>
					<div className="grid gap-1.5">
						<Label htmlFor="host" className="text-xs">
							<Trans>Host / IP</Trans>
						</Label>
						<Input id="host" name="host" required className="w-52" placeholder="8.8.8.8" />
					</div>
					<div className="grid gap-1.5">
						<Label htmlFor="port" className="text-xs">
							<Trans>Port</Trans>
						</Label>
						<Input id="port" name="port" type="number" min={1} max={65535} defaultValue={443} className="w-24" />
					</div>
					<Button type="submit">
						<PlusIcon className="size-4" />
						<Trans>Add</Trans>
					</Button>
				</form>
			)}

			{loading ? (
				<p className="text-sm text-muted-foreground">Loading…</p>
			) : monitors.length === 0 ? (
				<p className="text-sm text-muted-foreground">
					<Trans>No ping targets yet.</Trans>
				</p>
			) : (
				<div className="rounded-md border">
					<div className="grid grid-cols-[1fr_2fr_auto_auto_40px] gap-3 px-3 py-2 text-xs text-muted-foreground border-b">
						<span>
							<Trans>Name</Trans>
						</span>
						<span>
							<Trans>Host / IP</Trans>
						</span>
						<span>
							<Trans>Port</Trans>
						</span>
						<span>
							<Trans>Enabled</Trans>
						</span>
						<span />
					</div>
					{monitors.map((m) => (
						<div
							key={m.id}
							className="grid grid-cols-[1fr_2fr_auto_auto_40px] gap-3 px-3 py-2 items-center border-b last:border-0"
						>
							<span className="text-sm truncate">{m.name}</span>
							<span className="text-sm font-mono truncate">{m.host}</span>
							<span className="text-sm tabular-nums">{m.port}</span>
							<Switch
								checked={m.enabled}
								onCheckedChange={(checked) => updateMonitor(m.id, { enabled: checked })}
								disabled={readonly}
							/>
							{!readonly && (
								<Button variant="ghost" size="icon" className="size-8" onClick={() => deleteMonitor(m.id)}>
									<Trash2Icon className="size-4 text-destructive" />
								</Button>
							)}
						</div>
					))}
				</div>
			)}
		</div>
	)
}
