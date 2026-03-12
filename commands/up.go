package commands

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

// NewUpCommand creates the up command
func NewUpCommand() *cobra.Command {
	var acceptRoutes bool
	var acceptDNS bool
	var exitNode string
	var hostname string

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start Tailscale and connect to your network",
		Long: `Start the Tailscale connection (equivalent to 'tailscale up').
This command runs 'tailscale up' with optional flags for configuration.`,
		Example: `  # Connect to Tailscale (basic)
  ts-cli up

  # Connect with custom hostname
  ts-cli up --hostname=my-machine

  # Connect and accept routes
  ts-cli up --accept-routes

  # Connect and use an exit node
  ts-cli up --exit-node=exit-node-hostname`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check if tailscale command is available
			if !isTailscaleInstalled() {
				return fmt.Errorf("tailscale command not found.\n%s", getInstallInstructions())
			}

			// Build tailscale up command with flags
			tailscaleArgs := []string{"up"}

			if acceptRoutes {
				tailscaleArgs = append(tailscaleArgs, "--accept-routes")
			}

			if acceptDNS {
				tailscaleArgs = append(tailscaleArgs, "--accept-dns")
			}

			if exitNode != "" {
				tailscaleArgs = append(tailscaleArgs, fmt.Sprintf("--exit-node=%s", exitNode))
			}

			if hostname != "" {
				tailscaleArgs = append(tailscaleArgs, fmt.Sprintf("--hostname=%s", hostname))
			}

			fmt.Println("Starting Tailscale...")
			fmt.Printf("Running: tailscale %s\n\n", joinArgs(tailscaleArgs))

			// Execute tailscale up command - interactive mode
			tailscaleCmd := exec.Command("tailscale", tailscaleArgs...)
			tailscaleCmd.Stdin = os.Stdin
			tailscaleCmd.Stdout = os.Stdout
			tailscaleCmd.Stderr = os.Stderr

			if err := tailscaleCmd.Run(); err != nil {
				return fmt.Errorf("failed to start Tailscale: %w", err)
			}

			fmt.Println("\n✓ Tailscale is now connected!")
			fmt.Println("\nYou can now use ts-cli to:")
			fmt.Println("  • List devices: ts-cli list")
			fmt.Println("  • Interactive mode: ts-cli interactive")
			fmt.Println("  • SSH to devices: ts-cli ssh <device-name>")

			return nil
		},
	}

	cmd.Flags().BoolVar(&acceptRoutes, "accept-routes", false, "Accept subnet routes advertised by other nodes")
	cmd.Flags().BoolVar(&acceptDNS, "accept-dns", true, "Accept DNS configuration from Tailscale")
	cmd.Flags().StringVar(&exitNode, "exit-node", "", "Use specified node as an exit node")
	cmd.Flags().StringVar(&hostname, "hostname", "", "Set a custom hostname for this device")

	return cmd
}

// isTailscaleInstalled checks if the tailscale command is available
func isTailscaleInstalled() bool {
	_, err := exec.LookPath("tailscale")
	return err == nil
}

// getInstallInstructions returns OS-specific installation instructions
func getInstallInstructions() string {
	switch runtime.GOOS {
	case "darwin":
		return `To install Tailscale on macOS:
  brew install --cask tailscale

Or download from: https://tailscale.com/download/mac`
	case "linux":
		return `To install Tailscale on Linux:
  Visit: https://tailscale.com/download/linux

Or for Debian/Ubuntu:
  curl -fsSL https://tailscale.com/install.sh | sh`
	case "windows":
		return `To install Tailscale on Windows:
  Download from: https://tailscale.com/download/windows`
	default:
		return `To install Tailscale:
  Visit: https://tailscale.com/download`
	}
}

// joinArgs joins command arguments for display
func joinArgs(args []string) string {
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		result += arg
	}
	return result
}
