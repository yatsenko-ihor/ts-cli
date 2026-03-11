package commands

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// CheckTailscaleStatus checks if the Tailscale daemon is running
func CheckTailscaleStatus() (isRunning bool, message string) {
	// Try to run 'tailscale status' command
	cmd := exec.Command("tailscale", "status")
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Tailscale command failed - daemon might not be running or not installed
		return false, buildTailscaleNotRunningMessage()
	}

	// Check if output indicates we're not connected
	outputStr := string(output)
	if strings.Contains(strings.ToLower(outputStr), "logged out") {
		return false, "Tailscale daemon is running but you're logged out.\nRun: tailscale up"
	}

	// Tailscale is running and connected
	return true, ""
}

// buildTailscaleNotRunningMessage creates an OS-specific message for starting Tailscale
func buildTailscaleNotRunningMessage() string {
	switch runtime.GOOS {
	case "darwin":
		return `Tailscale doesn't appear to be running.

To start Tailscale on macOS:
  1. Open the Tailscale app from Applications
  2. Or run: open -a Tailscale
  3. Or install via: brew install --cask tailscale

Then run: tailscale up`
	case "linux":
		return `Tailscale doesn't appear to be running.

To start Tailscale on Linux:
  sudo systemctl start tailscaled
  sudo systemctl enable tailscaled
  tailscale up

Or if not installed, visit: https://tailscale.com/download`
	case "windows":
		return `Tailscale doesn't appear to be running.

To start Tailscale on Windows:
  1. Open Tailscale from the Start menu
  2. Or install from: https://tailscale.com/download/windows`
	default:
		return `Tailscale doesn't appear to be running.

Please start the Tailscale daemon and run: tailscale up
Visit https://tailscale.com/download for installation instructions.`
	}
}

// WarnIfTailscaleNotRunning checks Tailscale status and prints a warning if not running
func WarnIfTailscaleNotRunning() {
	isRunning, message := CheckTailscaleStatus()
	if !isRunning {
		fmt.Printf("\n⚠️  Warning: %s\n\n", message)
		fmt.Println("Note: ts-cli uses the Tailscale API to query devices, but you'll need")
		fmt.Println("Tailscale running locally to actually connect to them via SSH.")
	}
}
