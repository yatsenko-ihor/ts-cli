package commands

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
			// Load configuration from stored config if not provided
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

			// Try environment variable as fallback
			if apiKey == "" {
				apiKey = os.Getenv("TAILSCALE_API_KEY")
			}

			if apiKey == "" {
				return fmt.Errorf("API key not provided.\nRun 'ts-cli login' first or set TAILSCALE_API_KEY environment variable")
			}

			if tailnet == "" {
				return fmt.Errorf("tailnet name not configured.\nRun 'ts-cli login --tailnet=<name>' first or use --tailnet flag")
			}

			// Fetch devices
			apiClient := client.NewClient(apiKey)
			devices, err := apiClient.ListDevices(tailnet)
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}

			if len(devices) == 0 {
				fmt.Println("No devices found in your tailnet.")
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

// loadConfig loads the stored configuration
func loadConfig() (apiKey, tailnet string, err error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}

	configFile := filepath.Join(homeDir, ".ts-cli", "config")
	file, err := os.Open(configFile)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "TAILSCALE_API_KEY":
			apiKey = value
		case "TAILNET":
			tailnet = value
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", err
	}

	return apiKey, tailnet, nil
}

// displayTable displays devices in a formatted table
func displayTable(devices []client.Device) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tHOSTNAME\tADDRESS\tOS\tLAST SEEN\tAUTHORIZED")
	fmt.Fprintln(w, "----\t--------\t-------\t--\t---------\t----------")

	for _, device := range devices {
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

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
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
