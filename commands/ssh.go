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
			config, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}

			// Check if we have any accounts configured
			if len(config.Accounts) == 0 {
				return fmt.Errorf("no accounts configured.\nRun 'ts-cli login --tailnet=<name>' first to add an account")
			}

			var devices []client.Device

			// If specific flags are provided, use them to override
			if apiKey != "" && tailnet != "" {
				// Use the provided credentials
				apiClient := client.NewClient(apiKey)
				devices, err = apiClient.ListDevices(tailnet)
				if err != nil {
					return fmt.Errorf("failed to list devices: %w", err)
				}

				// Tag devices with account info
				for i := range devices {
					devices[i].AccountName = tailnet
					devices[i].AccountTailnet = tailnet
				}
			} else {
				// Fetch devices from all configured accounts
				accounts := make([]client.AccountInfo, len(config.Accounts))
				for i, acc := range config.Accounts {
					accounts[i] = client.AccountInfo{
						Name:    acc.Name,
						APIKey:  acc.APIKey,
						Tailnet: acc.Tailnet,
					}
				}

				devices = client.ListDevicesFromAccounts(accounts)
			}

			if len(devices) == 0 {
				return fmt.Errorf("no devices found in any of your configured accounts")
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

			// Check if Tailscale is running before attempting SSH
			isRunning, message := CheckTailscaleStatus()
			if !isRunning {
				fmt.Printf("\n⚠️  Warning: %s\n\n", message)
				fmt.Println("SSH connection may fail if Tailscale is not running.")
				fmt.Println("Press Ctrl+C to cancel or wait 3 seconds to continue anyway...")
				// Give user time to read and cancel if needed
				exec.Command("sleep", "3").Run()
			}

			// Use SSH username from config if not provided
			if user == "" && config.SSHUsername != "" {
				user = config.SSHUsername
			}

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
