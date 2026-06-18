package hub

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/henrygd/beszel/internal/ghupdate"
	"github.com/spf13/cobra"
)

// Update updates beszel to the latest version
func Update(cmd *cobra.Command, _ []string) {
	dataDir := os.TempDir()

	// set dataDir to ./beszel_data if it exists
	if _, err := os.Stat("./beszel_data"); err == nil {
		dataDir = "./beszel_data"
	}

	// Check if china-mirrors flag is set
	useMirror, _ := cmd.Flags().GetBool("china-mirrors")

	// Get the executable path before update
	exePath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}

	if agentRepoMalformed() {
		fmt.Fprintf(os.Stderr, "Warning: AGENT_REPO=%q is not in 'owner/repo' form; ignoring and self-updating from upstream henrygd/beszel.\n", os.Getenv("AGENT_REPO"))
	}

	updated, err := ghupdate.Update(ghupdate.Config{
		ArchiveExecutable: "beszel",
		DataDir:           dataDir,
		UseMirror:         useMirror,
		// AGENT_REPO (owner/repo) lets a fork hub self-update from the fork's
		// GitHub releases instead of the upstream henrygd/beszel. The same env
		// var is used by the agent's update command for consistency.
		Owner: ghRepoField(0),
		Repo:  ghRepoField(1),
	})
	if err != nil {
		log.Fatal(err)
	}
	if !updated {
		return
	}

	// make sure the file is executable
	if err := os.Chmod(exePath, 0755); err != nil {
		fmt.Printf("Warning: failed to set executable permissions: %v\n", err)
	}

	// Fix SELinux context if necessary
	if err := ghupdate.HandleSELinuxContext(exePath); err != nil {
		ghupdate.ColorPrintf(ghupdate.ColorYellow, "Warning: SELinux context handling: %v", err)
	}

	// Try to restart the service if it's running
	restartService()
}

// restartService attempts to restart the beszel service
func restartService() {
	// Check if we're running as a service by looking for systemd
	if _, err := exec.LookPath("systemctl"); err == nil {
		// Check if beszel service exists and is active
		cmd := exec.Command("systemctl", "is-active", "beszel.service")
		if err := cmd.Run(); err == nil {
			ghupdate.ColorPrint(ghupdate.ColorYellow, "Restarting beszel service...")
			restartCmd := exec.Command("systemctl", "restart", "beszel.service")
			if err := restartCmd.Run(); err != nil {
				ghupdate.ColorPrintf(ghupdate.ColorYellow, "Warning: Failed to restart service: %v\n", err)
				ghupdate.ColorPrint(ghupdate.ColorYellow, "Please restart the service manually: sudo systemctl restart beszel")
			} else {
				ghupdate.ColorPrint(ghupdate.ColorGreen, "Service restarted successfully")
			}
			return
		}
	}

	// Check for OpenRC (Alpine Linux)
	if _, err := exec.LookPath("rc-service"); err == nil {
		cmd := exec.Command("rc-service", "beszel", "status")
		if err := cmd.Run(); err == nil {
			ghupdate.ColorPrint(ghupdate.ColorYellow, "Restarting beszel service...")
			restartCmd := exec.Command("rc-service", "beszel", "restart")
			if err := restartCmd.Run(); err != nil {
				ghupdate.ColorPrintf(ghupdate.ColorYellow, "Warning: Failed to restart service: %v\n", err)
				ghupdate.ColorPrint(ghupdate.ColorYellow, "Please restart the service manually: sudo rc-service beszel restart")
			} else {
				ghupdate.ColorPrint(ghupdate.ColorGreen, "Service restarted successfully")
			}
			return
		}
	}

	ghupdate.ColorPrint(ghupdate.ColorYellow, "Service restart not attempted. If running as a service, restart manually.")
}

// ghRepoField parses AGENT_REPO (owner/repo) and returns the field at the given
// index (0=owner, 1=repo). Empty => ghupdate default (henrygd/beszel).
func ghRepoField(index int) string {
	parts := strings.SplitN(os.Getenv("AGENT_REPO"), "/", 2)
	if len(parts) == 2 && parts[index] != "" {
		return parts[index]
	}
	return ""
}

// agentRepoMalformed reports whether AGENT_REPO is set but lacks the
// "owner/repo" slash, in which case the resolvers silently fall back to the
// upstream henrygd/beszel — almost certainly not what the operator intended.
func agentRepoMalformed() bool {
	v := os.Getenv("AGENT_REPO")
	return v != "" && !strings.Contains(v, "/")
}

// updateApiURL returns the GitHub releases API URL to check for hub updates.
// When AGENT_REPO is set to a valid "owner/repo", the badge checks the fork's
// releases; otherwise it returns "" so ghupdate.FetchLatestRelease uses its
// built-in upstream default (henrygd/beszel), leaving upstream behavior intact.
func updateApiURL() string {
	owner, repo := ghRepoField(0), ghRepoField(1)
	if owner == "" || repo == "" {
		return ""
	}
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
}
