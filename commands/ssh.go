package commands

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ihor/ts-cli/client"
	"github.com/spf13/cobra"
)

// NewSSHCommand creates the SSH command
func NewSSHCommand() *cobra.Command {
	var apiKey string
	var tailnet string
	var user string

	cmd := &cobra.Command{
		Use:   "ssh [device-name-or-hostname]",
		Short: "Open an SSH connection to a Tailscale device",
		Long: `Open an SSH connection to a specified Tailscale device by name or hostname.
The command will look up the device in your tailnet and establish an SSH connection.`,
		Example: `  # SSH to a device by name
  ts-cli ssh laptop.example.com

  # SSH with custom user
  ts-cli ssh laptop.example.com --user=admin

  # SSH using device hostname
  ts-cli ssh laptop`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deviceName := args[0]

			// Load configuration
			if apiKey == "" || tailnet == "" {
				storedAPIKey, storedTailnet, err := loadConfig()
				if err == nil {
					if apiKey == "" {
						apiKey = storedAPIKey
					}
					if tailnet == "" {
						tailnet = storedTailnet
					}
				}
			}

			if apiKey == "" {
				apiKey = os.Getenv("TAILSCALE_API_KEY")
			}

			if apiKey == "" {
				return fmt.Errorf("API key not provided.\nRun 'ts-cli login' first or set TAILSCALE_API_KEY environment variable")
			}

			if tailnet == "" {
				return fmt.Errorf("tailnet name not configured.\nRun 'ts-cli login --tailnet=<name>' first or use --tailnet flag")
			}

			// Fetch devices to find the target
			apiClient := client.NewClient(apiKey)
			devices, err := apiClient.ListDevices(tailnet)
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}

			// Find the device by name or hostname
			var targetDevice *client.Device
			for _, device := range devices {
				if device.Name == deviceName ||
					device.Hostname == deviceName ||
					device.ID == deviceName {
					targetDevice = &device
					break
				}
			}

			if targetDevice == nil {
				return fmt.Errorf("device '%s' not found in tailnet", deviceName)
			}

			// Get the primary IP address
			if len(targetDevice.Addresses) == 0 {
				return fmt.Errorf("device '%s' has no IP addresses", deviceName)
			}

			address := targetDevice.Addresses[0]

			// Build SSH command
			sshArgs := []string{"ssh"}
			if user != "" {
				sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", user, address))
			} else {
				sshArgs = append(sshArgs, address)
			}

			fmt.Printf("Connecting to %s (%s)...\n", targetDevice.Name, address)

			// Execute SSH
			sshCmd := exec.Command("ssh", sshArgs[1:]...)
			sshCmd.Stdin = os.Stdin
			sshCmd.Stdout = os.Stdout
			sshCmd.Stderr = os.Stderr

			if err := sshCmd.Run(); err != nil {
				return fmt.Errorf("SSH connection failed: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "Tailscale API key")
	cmd.Flags().StringVar(&tailnet, "tailnet", "", "Tailnet name")
	cmd.Flags().StringVar(&user, "user", "", "SSH user (default: current user)")

	return cmd
}
