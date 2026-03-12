package commands

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/ihor/ts-cli/client"
	"github.com/spf13/cobra"
)

// NewListCommand creates the list command
func NewListCommand() *cobra.Command {
	var apiKey string
	var tailnet string
	var format string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all devices in your Tailscale tailnet",
		Long: `List all devices (machines) in your Tailscale tailnet.
Displays device information including name, addresses, OS, and status.`,
		Example: `  # List devices in table format (default)
  ts-cli list

  # List devices in JSON format
  ts-cli list --format=json

  # Override tailnet
  ts-cli list --tailnet=example.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			// Check if Tailscale is running (warning only, doesn't block)
			WarnIfTailscaleNotRunning()

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
				fmt.Println("No devices found in any of your configured accounts.")
				return nil
			}

			// Display devices based on format
			switch format {
			case "table":
				displayTable(devices)
			case "json":
				displayJSON(devices)
			default:
				return fmt.Errorf("unknown format '%s'. Use 'table' or 'json'", format)
			}

			return nil
		},
	}

	// Define flags
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Tailscale API key (overrides TAILSCALE_API_KEY env var)")
	cmd.Flags().StringVar(&tailnet, "tailnet", "", "Tailnet name (overrides stored configuration)")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table (default) or json")

	return cmd
}

// displayTable displays devices in a formatted table
func displayTable(devices []client.Device) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT\tNAME\tHOSTNAME\tADDRESS\tOS\tLAST SEEN\tAUTHORIZED")
	fmt.Fprintln(w, "-------\t----\t--------\t-------\t--\t---------\t----------")

	for _, device := range devices {
		account := device.AccountName
		if account == "" {
			account = "-"
		}

		name := device.Name
		if name == "" {
			name = device.Hostname
		}

		address := "N/A"
		if len(device.Addresses) > 0 {
			address = device.Addresses[0]
		}

		lastSeen := formatDuration(time.Since(device.LastSeen))
		authorized := "Yes"
		if !device.Authorized {
			authorized = "No"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			account,
			name,
			device.Hostname,
			address,
			device.OS,
			lastSeen,
			authorized,
		)
	}

	w.Flush()
	fmt.Printf("\nTotal devices: %d\n", len(devices))
}

// displayJSON displays devices in JSON format
func displayJSON(devices []client.Device) {
	// Simple JSON output (for production, use encoding/json)
	fmt.Println("[")
	for i, device := range devices {
		fmt.Printf("  {\n")
		fmt.Printf("    \"id\": \"%s\",\n", device.ID)
		fmt.Printf("    \"name\": \"%s\",\n", device.Name)
		fmt.Printf("    \"hostname\": \"%s\",\n", device.Hostname)
		fmt.Printf("    \"os\": \"%s\",\n", device.OS)
		fmt.Printf("    \"addresses\": %v,\n", device.Addresses)
		fmt.Printf("    \"authorized\": %t,\n", device.Authorized)
		fmt.Printf("    \"lastSeen\": \"%s\"\n", device.LastSeen.Format(time.RFC3339))
		if i < len(devices)-1 {
			fmt.Printf("  },\n")
		} else {
			fmt.Printf("  }\n")
		}
	}
	fmt.Println("]")
}

// formatDuration formats a duration into a human-readable string
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}
