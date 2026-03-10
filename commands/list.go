package commands

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ihor/ts-cli/client"
)

// ListCommand implements the list command
type ListCommand struct{}

// Help returns the help text for the list command
func (c *ListCommand) Help() string {
	helpText := `
Usage: ts-cli list [options]

  List all devices (machines) in your Tailscale tailnet.
  Displays device information including name, addresses, OS, and status.

Options:

  --api-key=<key>     Tailscale API key (overrides TAILSCALE_API_KEY env var)
  --tailnet=<name>    Tailnet name (overrides stored configuration)
  --format=<format>   Output format: table (default) or json

Example:

  ts-cli list
  ts-cli list --format=table
  ts-cli list --tailnet=example.com
`
	return strings.TrimSpace(helpText)
}

// Synopsis returns a short synopsis of the list command
func (c *ListCommand) Synopsis() string {
	return "List all devices in your Tailscale tailnet"
}

// Run executes the list command
func (c *ListCommand) Run(args []string) int {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	var apiKey string
	var tailnet string
	var format string

	flags.StringVar(&apiKey, "api-key", "", "Tailscale API key")
	flags.StringVar(&tailnet, "tailnet", "", "Tailnet name")
	flags.StringVar(&format, "format", "table", "Output format (table or json)")

	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %s\n", err)
		return 1
	}

	// Load configuration from stored config if not provided
	if apiKey == "" || tailnet == "" {
		storedAPIKey, storedTailnet, err := c.loadConfig()
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
		fmt.Fprintf(os.Stderr, "Error: API key not provided.\n")
		fmt.Fprintf(os.Stderr, "Run 'ts-cli login' first or set TAILSCALE_API_KEY environment variable.\n")
		return 1
	}

	if tailnet == "" {
		fmt.Fprintf(os.Stderr, "Error: Tailnet name not configured.\n")
		fmt.Fprintf(os.Stderr, "Run 'ts-cli login --tailnet=<name>' first or use --tailnet flag.\n")
		return 1
	}

	// Fetch devices
	apiClient := client.NewClient(apiKey)
	devices, err := apiClient.ListDevices(tailnet)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to list devices: %s\n", err)
		return 1
	}

	if len(devices) == 0 {
		fmt.Println("No devices found in your tailnet.")
		return 0
	}

	// Display devices based on format
	switch format {
	case "table":
		c.displayTable(devices)
	case "json":
		c.displayJSON(devices)
	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown format '%s'. Use 'table' or 'json'.\n", format)
		return 1
	}

	return 0
}

// loadConfig loads the stored configuration
func (c *ListCommand) loadConfig() (apiKey, tailnet string, err error) {
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
func (c *ListCommand) displayTable(devices []client.Device) {
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

		lastSeen := c.formatDuration(time.Since(device.LastSeen))
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
func (c *ListCommand) displayJSON(devices []client.Device) {
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
func (c *ListCommand) formatDuration(d time.Duration) string {
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
