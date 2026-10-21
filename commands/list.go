package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
)

// onlineWindow is how recently a device must have been seen to count as online.
const onlineWindow = 5 * time.Minute

// deviceFilter holds the optional `list` filters; all set filters combine with AND.
type deviceFilter struct {
	Online   bool
	Offline  bool
	OS       string
	Tags     []string // all must match; "tag:" prefix optional
	Accounts []string // any of
	Name     string   // substring of Name or Hostname
	Sort     string   // name (default), lastseen, os, account
}

// active reports whether any filter (not sorting) is set.
func (f deviceFilter) active() bool {
	return f.Online || f.Offline || f.OS != "" || len(f.Tags) > 0 || len(f.Accounts) > 0 || f.Name != ""
}

// deviceOnline reports whether the device is connected to control or was
// seen within the last five minutes.
func deviceOnline(d client.Device, now time.Time) bool {
	if d.ConnectedToControl {
		return true
	}
	if d.LastSeen.IsZero() {
		return false
	}
	return now.Sub(d.LastSeen) <= onlineWindow
}

func normalizeTag(t string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(t)), "tag:")
}

func hasTag(d client.Device, want string) bool {
	want = normalizeTag(want)
	for _, t := range d.Tags {
		if normalizeTag(t) == want {
			return true
		}
	}
	return false
}

func (f deviceFilter) matches(d client.Device, now time.Time) bool {
	if f.Online && !deviceOnline(d, now) {
		return false
	}
	if f.Offline && deviceOnline(d, now) {
		return false
	}
	if f.OS != "" && !strings.EqualFold(d.OS, f.OS) {
		return false
	}
	for _, t := range f.Tags {
		if !hasTag(d, t) {
			return false
		}
	}
	if len(f.Accounts) > 0 {
		ok := false
		for _, a := range f.Accounts {
			if strings.EqualFold(d.AccountName, a) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if f.Name != "" {
		n := strings.ToLower(f.Name)
		if !strings.Contains(strings.ToLower(d.Name), n) && !strings.Contains(strings.ToLower(d.Hostname), n) {
			return false
		}
	}
	return true
}

func sortKeyName(d client.Device) string {
	if d.Name != "" {
		return strings.ToLower(d.Name)
	}
	return strings.ToLower(d.Hostname)
}

// filterDevices returns the devices matching f, sorted per f.Sort. The input
// slice is not modified. The result is never nil.
func filterDevices(devices []client.Device, f deviceFilter, now time.Time) []client.Device {
	out := make([]client.Device, 0, len(devices))
	for _, d := range devices {
		if f.matches(d, now) {
			out = append(out, d)
		}
	}
	byName := func(a, b client.Device) int { return strings.Compare(sortKeyName(a), sortKeyName(b)) }
	var cmpFn func(a, b client.Device) int
	switch strings.ToLower(f.Sort) {
	case "lastseen":
		// most recently seen first; connected devices count as seen now
		seen := func(d client.Device) time.Time {
			if d.ConnectedToControl {
				return now
			}
			return d.LastSeen
		}
		cmpFn = func(a, b client.Device) int {
			if c := seen(b).Compare(seen(a)); c != 0 {
				return c
			}
			return byName(a, b)
		}
	case "os":
		cmpFn = func(a, b client.Device) int {
			if c := strings.Compare(strings.ToLower(a.OS), strings.ToLower(b.OS)); c != 0 {
				return c
			}
			return byName(a, b)
		}
	case "account":
		cmpFn = func(a, b client.Device) int {
			if c := strings.Compare(strings.ToLower(a.AccountName), strings.ToLower(b.AccountName)); c != 0 {
				return c
			}
			return byName(a, b)
		}
	default:
		cmpFn = byName
	}
	slices.SortStableFunc(out, cmpFn)
	return out
}

// listDevicesFn fetches the devices to list; tests replace it.
var listDevicesFn = fetchDevicesForList

func fetchDevicesForList(apiKey, tailnet string, quiet bool) ([]client.Device, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}
	if len(cfg.Accounts) == 0 {
		return nil, fmt.Errorf("no accounts configured.\nRun 'ts-cli login --tailnet=<name>' first to add an account")
	}
	if !quiet {
		// Warning only, doesn't block.
		WarnIfTailscaleNotRunning()
	}
	if apiKey != "" && tailnet != "" {
		devices, err := client.NewClient(apiKey).ListDevices(tailnet)
		if err != nil {
			return nil, fmt.Errorf("failed to list devices: %w", err)
		}
		for i := range devices {
			devices[i].AccountName = tailnet
			devices[i].AccountTailnet = tailnet
		}
		return devices, nil
	}
	accounts := make([]client.AccountInfo, len(cfg.Accounts))
	for i, acc := range cfg.Accounts {
		accounts[i] = client.AccountInfo{Name: acc.Name, APIKey: acc.APIKey, Tailnet: acc.Tailnet}
	}
	return client.ListDevicesFromAccounts(accounts), nil
}

// NewListCommand creates the list command
func NewListCommand() *cobra.Command {
	var apiKey, tailnet, format string
	var quiet bool
	var f deviceFilter

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all devices in your Tailscale tailnet",
		Long: `List all devices (machines) in your Tailscale tailnet.
Displays device information including name, addresses, OS, and status.
Filters combine with AND; repeatable flags are noted below.`,
		Example: `  # List devices in table format (default)
  ts-cli list

  # Online Linux machines
  ts-cli list --online --os linux

  # Machine names with a tag, one per line (for scripts)
  ts-cli list --tag tag:server --format names

  # JSON for one account
  ts-cli list --account work --format json

  # Override tailnet
  ts-cli list --tailnet=example.com`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.Online && f.Offline {
				return fmt.Errorf("--online and --offline are mutually exclusive")
			}
			switch strings.ToLower(f.Sort) {
			case "name", "lastseen", "os", "account":
			default:
				return fmt.Errorf("unknown sort '%s'. Use 'name', 'lastseen', 'os' or 'account'", f.Sort)
			}
			format = strings.ToLower(format)
			switch format {
			case "table", "json", "names":
			default:
				return fmt.Errorf("unknown format '%s'. Use 'table', 'json' or 'names'", format)
			}

			machineReadable := format != "table" || quiet
			devices, err := listDevicesFn(apiKey, tailnet, machineReadable)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			shown := filterDevices(devices, f, time.Now())

			if len(shown) == 0 {
				if format == "json" {
					return writeDevicesJSON(out, shown)
				}
				if f.active() {
					fmt.Fprintln(cmd.ErrOrStderr(), "No devices match the filters.")
				} else {
					fmt.Fprintln(out, "No devices found in any of your configured accounts.")
				}
				return nil
			}

			switch format {
			case "json":
				return writeDevicesJSON(out, shown)
			case "names":
				writeDeviceNames(out, shown)
			default:
				writeDevicesTable(out, shown, quiet)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "Tailscale API key (overrides TAILSCALE_API_KEY env var)")
	cmd.Flags().StringVar(&tailnet, "tailnet", "", "Tailnet name (overrides stored configuration)")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table, json or names")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Table output without header and footer")
	cmd.Flags().BoolVar(&f.Online, "online", false, "Only online devices")
	cmd.Flags().BoolVar(&f.Offline, "offline", false, "Only offline devices")
	cmd.Flags().StringVar(&f.OS, "os", "", "Only devices with this OS (case-insensitive)")
	cmd.Flags().StringArrayVar(&f.Tags, "tag", nil, "Only devices with this tag, with or without 'tag:' prefix (repeatable, all must match)")
	cmd.Flags().StringArrayVar(&f.Accounts, "account", nil, "Only devices of this account (repeatable, any of)")
	cmd.Flags().StringVar(&f.Name, "name", "", "Only devices whose name or hostname contains this text")
	cmd.Flags().StringVar(&f.Sort, "sort", "name", "Sort by: name, lastseen, os or account")

	return cmd
}

// shortName is the machine name used by `--format names`.
func shortName(d client.Device) string {
	if d.Hostname != "" {
		return d.Hostname
	}
	return d.Name
}

func writeDeviceNames(w io.Writer, devices []client.Device) {
	for _, d := range devices {
		fmt.Fprintln(w, shortName(d))
	}
}

// writeDevicesTable prints devices as a table; quiet drops header and footer.
func writeDevicesTable(out io.Writer, devices []client.Device, quiet bool) {
	showTags := false
	for _, d := range devices {
		if len(d.Tags) > 0 {
			showTags = true
			break
		}
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if !quiet {
		head := []string{"ACCOUNT", "NAME", "HOSTNAME", "ADDRESS", "OS"}
		if showTags {
			head = append(head, "TAGS")
		}
		head = append(head, "LAST SEEN", "AUTHORIZED")
		under := make([]string, len(head))
		for i, h := range head {
			under[i] = strings.Repeat("-", len(h))
		}
		fmt.Fprintln(w, strings.Join(head, "\t"))
		fmt.Fprintln(w, strings.Join(under, "\t"))
	}

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
		switch {
		case device.ConnectedToControl:
			lastSeen = "online now" // the API omits lastSeen while connected
		case device.LastSeen.IsZero():
			lastSeen = "never"
		}
		authorized := "Yes"
		if !device.Authorized {
			authorized = "No"
		}
		row := []string{account, name, device.Hostname, address, device.OS}
		if showTags {
			tags := "-"
			if len(device.Tags) > 0 {
				tags = strings.Join(device.Tags, ",")
			}
			row = append(row, tags)
		}
		row = append(row, lastSeen, authorized)
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	_ = w.Flush()
	if !quiet {
		fmt.Fprintf(out, "\nTotal devices: %d\n", len(devices))
	}
}

// deviceJSON is the stable output shape of `list --format=json`.
type deviceJSON struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Hostname   string   `json:"hostname"`
	OS         string   `json:"os"`
	Addresses  []string `json:"addresses"`
	Authorized bool     `json:"authorized"`
	LastSeen   string   `json:"lastSeen,omitempty"`
	Connected  bool     `json:"connectedToControl"`
	Account    string   `json:"account,omitempty"`
	Tags       []string `json:"tags"`
	Online     bool     `json:"online"`
}

// writeDevicesJSON encodes devices as a JSON array. Using encoding/json
// guarantees valid output regardless of characters in device names.
func writeDevicesJSON(w io.Writer, devices []client.Device) error {
	out := make([]deviceJSON, 0, len(devices))
	for _, d := range devices {
		addrs := d.Addresses
		if addrs == nil {
			addrs = []string{}
		}
		tags := d.Tags
		if tags == nil {
			tags = []string{}
		}
		lastSeen := ""
		if !d.LastSeen.IsZero() {
			lastSeen = d.LastSeen.Format(time.RFC3339)
		}
		out = append(out, deviceJSON{
			ID:         d.ID,
			Name:       d.Name,
			Hostname:   d.Hostname,
			OS:         d.OS,
			Addresses:  addrs,
			Authorized: d.Authorized,
			LastSeen:   lastSeen,
			Connected:  d.ConnectedToControl,
			Account:    d.AccountName,
			Tags:       tags,
			Online:     deviceOnline(d, time.Now()),
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
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
