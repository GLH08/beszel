import { i18n } from "@lingui/core"
import { memo } from "react"
import { copyToClipboard, getAgentImage, getAgentRepo, getHubURL } from "@/lib/utils"
import { DropdownMenuContent, DropdownMenuItem } from "./ui/dropdown-menu"

// const isbeta = beszel.hub_version.includes("beta")
// const imagetag = isbeta ? ":edge" : ""

// Fork note: get.beszel.dev serves the *upstream* install script, which lacks
// the fork's --repo / -Repo flags (causing "Invalid option: --repo"). When the
// hub overrides the repo (AGENT_REPO set), download the fork's own script from
// GitHub raw instead. The --repo-capable script lives on the fork's `custom`
// branch (main tracks upstream only), so the ref is hardcoded to `custom`.
const FORK_BRANCH = "custom"

/**
 * URL of the agent install script.
 * @param file - filename under supplemental/scripts/ for the fork (e.g. "install-agent.sh")
 * @param upstreamPath - path under get.beszel.dev for upstream (e.g. "" or "/brew")
 */
const getInstallScriptUrl = (file: string, upstreamPath: string = "") => {
	const repo = getAgentRepo()
	if (repo) {
		return `https://raw.githubusercontent.com/${repo}/${FORK_BRANCH}/supplemental/scripts/${file}`
	}
	return `https://get.beszel.dev${upstreamPath}`
}

export function copyDockerCompose(port = "45876", publicKey: string, token: string) {
	copyToClipboard(`services:
  beszel-agent:
    image: ${getAgentImage()}
    container_name: beszel-agent
    restart: unless-stopped
    network_mode: host
    # host PID namespace so Top Processes can see all host processes
    # (otherwise the agent only sees its own container's processes)
    pid: host
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./beszel_agent_data:/var/lib/beszel-agent
      # monitor other disks / partitions by mounting a folder in /extra-filesystems
      # - /mnt/disk/.beszel:/extra-filesystems/sda1:ro
    environment:
      LISTEN: ${port}
      KEY: '${publicKey}'
      TOKEN: ${token}
      HUB_URL: ${getHubURL()}`)
}

export function copyDockerRun(port = "45876", publicKey: string, token: string) {
	copyToClipboard(
		`docker run -d --name beszel-agent --network host --pid host --restart unless-stopped -v /var/run/docker.sock:/var/run/docker.sock:ro -v beszel_agent_data:/var/lib/beszel-agent -e KEY="${publicKey}" -e LISTEN=${port} -e TOKEN="${token}" -e HUB_URL="${getHubURL()}" ${getAgentImage()}`
	)
}

export function copyLinuxCommand(port = "45876", publicKey: string, token: string, brew = false) {
	// brew: the fork publishes no homebrew tap, so always use the upstream brew
	// script. For non-brew, a fork downloads its own install-agent.sh (with --repo).
	const scriptUrl = brew ? getInstallScriptUrl("install-agent-brew.sh", "/brew") : getInstallScriptUrl("install-agent.sh")
	let cmd = `curl -sL ${scriptUrl} -o /tmp/install-agent.sh && chmod +x /tmp/install-agent.sh && /tmp/install-agent.sh -p ${port} -k "${publicKey}" -t "${token}" -url "${getHubURL()}"`
	// brew script does not support --china-mirrors
	if (!brew && (i18n.locale + navigator.language).includes("zh-CN")) {
		cmd += ` --china-mirrors`
	}
	// install from a fork's GitHub releases when the hub overrides the repo
	const repo = getAgentRepo()
	if (!brew && repo) {
		cmd += ` --repo ${repo}`
	}
	copyToClipboard(cmd)
}

export function copyWindowsCommand(port = "45876", publicKey: string, token: string) {
	const repo = getAgentRepo()
	const repoArg = repo ? ` -Repo ${repo}` : ""
	copyToClipboard(
		`& iwr -useb ${getInstallScriptUrl("install-agent.ps1")} -OutFile "$env:TEMP\\install-agent.ps1"; & Powershell -ExecutionPolicy Bypass -File "$env:TEMP\\install-agent.ps1" -Key "${publicKey}" -Port ${port} -Token "${token}" -Url "${getHubURL()}"${repoArg}`
	)
}

export interface DropdownItem {
	text: string
	onClick?: () => void
	url?: string
	icons?: React.ComponentType<React.SVGProps<SVGSVGElement>>[]
}

export const InstallDropdown = memo(({ items }: { items: DropdownItem[] }) => {
	return (
		<DropdownMenuContent align="end">
			{items.map((item, index) => {
				const className = "cursor-pointer flex items-center gap-1.5"
				return item.url ? (
					<DropdownMenuItem key={index} asChild>
						<a href={item.url} className={className} target="_blank" rel="noopener noreferrer">
							{item.text}{" "}
							{item.icons?.map((Icon, iconIndex) => (
								<Icon key={iconIndex} className="size-4" />
							))}
						</a>
					</DropdownMenuItem>
				) : (
					<DropdownMenuItem key={index} onClick={item.onClick} className={className}>
						{item.text}{" "}
						{item.icons?.map((Icon, iconIndex) => (
							<Icon key={iconIndex} className="size-4" />
						))}
					</DropdownMenuItem>
				)
			})}
		</DropdownMenuContent>
	)
})
