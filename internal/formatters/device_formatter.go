// Package formatters provides output formatting utilities
// Following Single Responsibility Principle
package formatters

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ihor/ts-cli/client"
)

// DeviceFormatter handles device output formatting
type DeviceFormatter struct {
	writer *tabwriter.Writer
}

// NewDeviceFormatter creates a new device formatter
func NewDeviceFormatter() *DeviceFormatter {
	return &DeviceFormatter{}
}

// FormatAsTable formats devices as a table
func (f *DeviceFormatter) FormatAsTable(devices []client.Device) string {
	var output strings.Builder
	w := tabwriter.NewWriter(&output, 0, 0, 3, ' ', 0)
	// Print header
	fmt.Fprintln(w, "NAME\tHOSTNAME\tADDRESS\tOS\tLAST SEEN\tACCOUNT\t")
	fmt.Fprintln(w, "----\t--------\t-------\t--\t---------\t-------\t")
	// Print devices
	for _, device := range devices {
		address := "-"
		if len(device.Addresses) > 0 {
			address = device.Addresses[0]
		}
		lastSeen := formatTimeSince(device.LastSeen)
		accountInfo := device.AccountName
		if accountInfo == "" {
			accountInfo = device.AccountTailnet
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t\n",
			device.Name,
			device.Hostname,
			address,
			device.OS,
			lastSeen,
			accountInfo,
		)
	}
	w.Flush()
	return output.String()
}

// FormatAsJSON formats devices as JSON
func (f *DeviceFormatter) FormatAsJSON(devices []client.Device) (string, error) {
	data, err := json.MarshalIndent(devices, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal devices to JSON: %w", err)
	}
	return string(data), nil
}

// FormatDeviceSummary creates a summary string for a single device
func (f *DeviceFormatter) FormatDeviceSummary(device *client.Device) string {
	var output strings.Builder
	output.WriteString(fmt.Sprintf("Device: %s\n", device.Name))
	output.WriteString(fmt.Sprintf("Hostname: %s\n", device.Hostname))
	output.WriteString(fmt.Sprintf("ID: %s\n", device.ID))
	output.WriteString(fmt.Sprintf("OS: %s\n", device.OS))
	output.WriteString(fmt.Sprintf("Client Version: %s\n", device.ClientVersion))
	if len(device.Addresses) > 0 {
		output.WriteString("Addresses:\n")
		for _, addr := range device.Addresses {
			output.WriteString(fmt.Sprintf("  - %s\n", addr))
		}
	}
	if len(device.Tags) > 0 {
		output.WriteString("Tags:\n")
		for _, tag := range device.Tags {
			output.WriteString(fmt.Sprintf("  - %s\n", tag))
		}
	}
	output.WriteString(fmt.Sprintf("Authorized: %t\n", device.Authorized))
	output.WriteString(fmt.Sprintf("Last Seen: %s\n", formatTimeSince(device.LastSeen)))
	output.WriteString(fmt.Sprintf("Account: %s\n", device.AccountName))
	return output.String()
}

// formatTimeSince formats a time as a human-readable "time since" string
func formatTimeSince(t time.Time) string {
	duration := time.Since(t)
	switch {
	case duration < time.Minute:
		return fmt.Sprintf("%ds ago", int(duration.Seconds()))
	case duration < time.Hour:
		return fmt.Sprintf("%dm ago", int(duration.Minutes()))
	case duration < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(duration.Hours()))
	case duration < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(duration.Hours()/24))
	case duration < 30*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(duration.Hours()/24/7))
	default:
		return fmt.Sprintf("%dmo ago", int(duration.Hours()/24/30))
	}
}

// FormatError formats an error message for display
func FormatError(err error) string {
	return fmt.Sprintf("Error: %v", err)
}

// FormatSuccess formats a success message
func FormatSuccess(message string) string {
	return fmt.Sprintf("✓ %s", message)
}

// FormatWarning formats a warning message
func FormatWarning(message string) string {
	return fmt.Sprintf("⚠ %s", message)
}

// FormatInfo formats an info message
func FormatInfo(message string) string {
	return fmt.Sprintf("ℹ %s", message)
}
