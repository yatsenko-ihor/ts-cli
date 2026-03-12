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

// attemptToStartTailscale tries to start Tailscale based on the OS
func attemptToStartTailscale() bool {
	fmt.Println("🔄 Attempting to start Tailscale...")

	var cmd *exec.Cmd
	var description string

	switch runtime.GOOS {
	case "darwin":
		// On macOS, try to open the Tailscale app
		cmd = exec.Command("open", "-a", "Tailscale")
		description = "Opening Tailscale app on macOS"
	case "linux":
		// On Linux, provide instructions instead of executing sudo
		fmt.Println("   To start Tailscale on Linux, run:")
		fmt.Println("   sudo systemctl start tailscaled")
		fmt.Println("\n   Or if using a different init system:")
		fmt.Println("   sudo service tailscaled start")
		return false
	default:
		fmt.Println("⚠️  Automatic Tailscale start not supported on this OS")
		return false
	}

	fmt.Printf("   %s...\n", description)

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("❌ Failed to start Tailscale: %v\n", err)
		if len(output) > 0 {
			fmt.Printf("   Output: %s\n", string(output))
		}
		return false
	}

	fmt.Println("✅ Tailscale start command executed successfully")

	// Wait a moment for Tailscale to start
	fmt.Println("   Waiting for Tailscale to initialize...")
	// Give it a couple seconds to start
	cmd = exec.Command("sleep", "2")
	cmd.Run()

	// Verify it's running
	isRunning, _ := CheckTailscaleStatus()
	if isRunning {
		fmt.Println("✅ Tailscale is now running!")
		return true
	}

	fmt.Println("⚠️  Tailscale start command succeeded, but service not yet active")
	fmt.Println("   Please wait a moment and try again, or start Tailscale manually")
	return false
}

// WarnIfTailscaleNotRunning checks Tailscale status and prints a warning if not running
func WarnIfTailscaleNotRunning() {
	isRunning, message := CheckTailscaleStatus()
	if !isRunning {
		fmt.Printf("\n⚠️  Warning: %s\n\n", message)

		// Attempt to start Tailscale automatically
		fmt.Println("Note: ts-cli uses the Tailscale API to query devices, but you'll need")
		fmt.Println("Tailscale running locally to actually connect to them via SSH.")
		fmt.Println()

		started := attemptToStartTailscale()
		if !started {
			fmt.Println("\n💡 Please start Tailscale manually and try again.")
		}
		fmt.Println()
	}
}
