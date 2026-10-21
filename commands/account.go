package commands

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// NewAccountCommand creates the account management command
func NewAccountCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Manage Tailscale accounts",
		Long: `Add, edit, remove and inspect the Tailscale accounts (tailnets) ts-cli talks to.

Each account is a tailnet plus an API key (stored encrypted). An account can be
hidden: its machines then disappear from the list without deleting anything.`,
	}

	cmd.AddCommand(newAccountAddCommand())
	cmd.AddCommand(newAccountListCommand())
	cmd.AddCommand(newAccountEditCommand())
	cmd.AddCommand(newAccountUseCommand())
	cmd.AddCommand(newAccountVisibilityCommand(true))
	cmd.AddCommand(newAccountVisibilityCommand(false))
	cmd.AddCommand(newAccountRemoveCommand())

	return cmd
}

// ─── API key input ───────────────────────────────────────────────────────────

// keyInput collects where an API key may come from.
type keyInput struct {
	flag  string // --api-key (visible in the process list: discouraged)
	stdin bool   // --api-key-stdin
}

func (k *keyInput) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&k.flag, "api-key", "", "API key (visible in shell history and the process list; prefer --api-key-stdin or TAILSCALE_API_KEY)")
	cmd.Flags().BoolVar(&k.stdin, "api-key-stdin", false, "read the API key from standard input")
}

// resolve returns the key from stdin, the flag or TAILSCALE_API_KEY, in that
// order. It returns "" when none was given.
func (k keyInput) resolve(in io.Reader) (string, error) {
	switch {
	case k.stdin:
		line, err := bufio.NewReader(io.LimitReader(in, 4096)).ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("failed to read API key from stdin: %w", err)
		}
		return util.SanitizeInput(strings.TrimSpace(line)), nil
	case k.flag != "":
		return util.SanitizeInput(k.flag), nil
	}
	return util.SanitizeInput(os.Getenv("TAILSCALE_API_KEY")), nil
}

// duplicateNameError is returned when the display name belongs to another account.
type duplicateNameError struct{ name string }

func (e duplicateNameError) Error() string {
	return fmt.Sprintf("an account named %q already exists; choose another --name", e.name)
}

// addAccount validates the key against the API and stores the account.
func addAccount(out io.Writer, tailnet, name, apiKey string, makeActive bool) error {
	tailnet = util.SanitizeInput(tailnet)
	name = util.SanitizeInput(name)
	if apiKey == "" {
		return fmt.Errorf("API key not provided.\nUse --api-key-stdin, the TAILSCALE_API_KEY environment variable or --api-key")
	}
	if tailnet == "" {
		return fmt.Errorf("tailnet name is required.\nUse --tailnet to specify your tailnet name")
	}
	explicitName := name != ""
	if explicitName {
		if err := util.ValidateAccountName(name); err != nil {
			return fmt.Errorf("invalid name: %w", err)
		}
	} else {
		name = tailnet
	}
	if err := util.ValidateAPIKey(apiKey); err != nil {
		return fmt.Errorf("invalid API key: %w", err)
	}
	if err := util.ValidateTailnet(tailnet); err != nil {
		return fmt.Errorf("invalid tailnet: %w", err)
	}

	fmt.Fprintln(out, "Validating API key...")
	if err := client.NewClient(apiKey).ValidateAPIKey(tailnet); err != nil {
		return fmt.Errorf("failed to validate API key: %w", err)
	}
	fmt.Fprintln(out, "✓ API key is valid")

	// Read-modify-write under the config lock. A corrupt or unreadable
	// config is an error, never silently replaced.
	err := config.Update(func(cfg *config.Config) error {
		// The tailnet identifies the account. The name must not belong to a
		// different account (as its name or as its tailnet).
		existing := cfg.GetAccountByTailnet(tailnet)
		for i := range cfg.Accounts {
			other := &cfg.Accounts[i]
			if other != existing && (other.Name == name || other.Tailnet == name) {
				return duplicateNameError{name: name}
			}
		}
		if existing != nil {
			existing.APIKey = apiKey
			if explicitName {
				existing.Name = name
			}
			fmt.Fprintf(out, "✓ Updated account: %s\n", existing.Name)
		} else {
			cfg.AddAccount(name, apiKey, tailnet, false)
			fmt.Fprintf(out, "✓ Added new account: %s\n", name)
		}
		if makeActive {
			cfg.SetActiveAccount(tailnet)
		}
		return nil
	})
	var dup duplicateNameError
	if errors.As(err, &dup) {
		return dup
	}
	if err != nil {
		return fmt.Errorf("failed to store config: %w", err)
	}
	fmt.Fprintf(out, "✓ Authenticated with tailnet: %s\n", tailnet)
	return nil
}

func newAccountAddCommand() *cobra.Command {
	var tailnet, name string
	var keys keyInput
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add an account (validates the API key first)",
		Example: `  # Key from the environment (recommended)
  export TAILSCALE_API_KEY=tskey-api-xxxxx
  ts-cli account add --tailnet=example.com

  # Key from a password manager, never touching shell history
  pass show tailscale/work | ts-cli account add --tailnet=work.example.com --name=work --api-key-stdin`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := keys.resolve(cmd.InOrStdin())
			if err != nil {
				return err
			}
			return addAccount(cmd.OutOrStdout(), tailnet, name, key, false)
		},
	}
	cmd.Flags().StringVar(&tailnet, "tailnet", "", "tailnet name (e.g. example.com or user@example.com)")
	cmd.Flags().StringVar(&name, "name", "", "display name (default: the tailnet)")
	keys.bind(cmd)
	_ = cmd.MarkFlagRequired("tailnet")
	return cmd
}

// ─── list ────────────────────────────────────────────────────────────────────

// accountJSON is the stable output shape of `account list --format=json`.
type accountJSON struct {
	Name    string `json:"name"`
	Tailnet string `json:"tailnet"`
	Active  bool   `json:"active"`
	Hidden  bool   `json:"hidden"`
	Key     string `json:"key,omitempty"`     // with --check: ok | invalid | unreachable
	Expires string `json:"expires,omitempty"` // with --check: RFC3339 when known
}

// keyStatus summarises a health check as a short word and a human note.
func keyStatus(h client.Health, now time.Time) (word, note string) {
	switch {
	case h.Unauth:
		return "invalid", "key rejected"
	case !h.Valid:
		return "unreachable", "cannot check"
	case h.HasExpiry:
		if !h.Expires.After(now) {
			return "invalid", "expired"
		}
		days := int(h.Expires.Sub(now).Hours() / 24)
		return "ok", fmt.Sprintf("expires in %d days (%s)", days, h.Expires.Format("2006-01-02"))
	}
	if h.ExpiryNote != "" {
		return "ok", h.ExpiryNote
	}
	return "ok", "valid"
}

func newAccountListCommand() *cobra.Command {
	var format string
	var check bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List configured accounts",
		Example: `  ts-cli account list
  ts-cli account list --check          # also verify each API key and show its expiry
  ts-cli account list --format=json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}
			if format != "table" && format != "json" {
				return fmt.Errorf("unknown format '%s'. Use 'table' or 'json'", format)
			}

			health := make([]client.Health, len(cfg.Accounts))
			if check {
				var wg sync.WaitGroup
				for i, acc := range cfg.Accounts {
					wg.Add(1)
					go func() {
						defer wg.Done()
						health[i] = client.CheckAccount(client.AccountInfo{Name: acc.Name, APIKey: acc.APIKey, Tailnet: acc.Tailnet})
					}()
				}
				wg.Wait()
			}

			out := cmd.OutOrStdout()
			if format == "json" {
				rows := make([]accountJSON, 0, len(cfg.Accounts))
				for i, acc := range cfg.Accounts {
					r := accountJSON{Name: acc.Name, Tailnet: acc.Tailnet, Active: acc.Active, Hidden: acc.Hidden}
					if check {
						r.Key, _ = keyStatus(health[i], time.Now())
						if health[i].HasExpiry {
							r.Expires = health[i].Expires.Format(time.RFC3339)
						}
					}
					rows = append(rows, r)
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}

			if len(cfg.Accounts) == 0 {
				fmt.Fprintln(out, "No accounts configured.")
				fmt.Fprintln(out, "\nTo add an account, run:")
				fmt.Fprintln(out, "  ts-cli account add --tailnet=<your-tailnet>")
				return nil
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			header, rule := "NAME\tTAILNET\tACTIVE\tSHOWN", "----\t-------\t------\t-----"
			if check {
				header += "\tAPI KEY"
				rule += "\t-------"
			}
			fmt.Fprintln(w, header)
			fmt.Fprintln(w, rule)
			for i, acc := range cfg.Accounts {
				active, shown := "", "✓"
				if acc.Active {
					active = "✓"
				}
				if acc.Hidden {
					shown = "hidden"
				}
				line := fmt.Sprintf("%s\t%s\t%s\t%s", acc.Name, acc.Tailnet, active, shown)
				if check {
					word, note := keyStatus(health[i], time.Now())
					line += fmt.Sprintf("\t%s: %s", word, note)
				}
				fmt.Fprintln(w, line)
			}
			_ = w.Flush()
			fmt.Fprintf(out, "\nTotal accounts: %d\n", len(cfg.Accounts))
			if cfg.SSHUsername != "" {
				fmt.Fprintf(out, "Default SSH username: %s\n", cfg.SSHUsername)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "output format: table or json")
	cmd.Flags().BoolVar(&check, "check", false, "verify each API key against the API and show its expiry")
	return cmd
}

// ─── edit ────────────────────────────────────────────────────────────────────

func newAccountEditCommand() *cobra.Command {
	var newName string
	var keys keyInput
	cmd := &cobra.Command{
		Use:   "edit <account>",
		Short: "Rename an account and/or replace its API key",
		Example: `  ts-cli account edit work.example.com --name=work
  pass show tailscale/work | ts-cli account edit work --api-key-stdin`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var key string
			if keys.stdin || keys.flag != "" {
				var err error
				if key, err = keys.resolve(cmd.InOrStdin()); err != nil {
					return err
				}
				if err := util.ValidateAPIKey(key); err != nil {
					return fmt.Errorf("invalid API key: %w", err)
				}
			}
			newName = util.SanitizeInput(newName)
			if newName == "" && key == "" {
				return fmt.Errorf("nothing to change: pass --name and/or --api-key-stdin")
			}
			if newName != "" {
				if err := util.ValidateAccountName(newName); err != nil {
					return fmt.Errorf("invalid name: %w", err)
				}
			}
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}
			acc := cfg.FindAccount(args[0])
			if acc == nil {
				return fmt.Errorf("account '%s' not found", args[0])
			}
			if key != "" {
				fmt.Fprintln(cmd.OutOrStdout(), "Validating API key...")
				if err := client.NewClient(key).ValidateAPIKey(acc.Tailnet); err != nil {
					return fmt.Errorf("failed to validate API key: %w", err)
				}
			}
			err = config.Update(func(cfg *config.Config) error { return cfg.UpdateAccount(args[0], newName, key) })
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓ Account updated")
			return nil
		},
	}
	cmd.Flags().StringVar(&newName, "name", "", "new display name")
	keys.bind(cmd)
	return cmd
}

// ─── use / hide / show / remove ──────────────────────────────────────────────

func newAccountUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "use <account>",
		Aliases: []string{"set-active"},
		Short:   "Make an account active (and switch this computer to it)",
		Long: `Mark the account as the active one in ts-cli and, when the tailscale CLI is
installed, run 'tailscale switch' so this computer is logged in to it too.
The tailscale step is best effort: a failure is only a warning.`,
		Example: `  ts-cli account use work.example.com`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var tailnet string
			err := config.Update(func(cfg *config.Config) error {
				if len(cfg.Accounts) == 0 {
					return fmt.Errorf("no accounts configured")
				}
				if !cfg.SetActiveAccount(args[0]) {
					return fmt.Errorf("account '%s' not found", args[0])
				}
				acc := cfg.GetActiveAccount()
				tailnet = acc.Tailnet
				fmt.Fprintf(cmd.OutOrStdout(), "✓ Set '%s' as active account\n", acc.Name)
				return nil
			})
			if err != nil {
				return err
			}
			if _, lookErr := exec.LookPath("tailscale"); lookErr == nil {
				if out, err := exec.Command("tailscale", "switch", tailnet).CombinedOutput(); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: tailscale switch failed: %s\n", strings.TrimSpace(string(out)))
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "✓ This computer now uses %s\n", tailnet)
				}
			}
			return nil
		},
	}
}

func newAccountVisibilityCommand(hide bool) *cobra.Command {
	use, short, verb := "show <account>", "Show an account's machines in the list again", "shown"
	if hide {
		use, short, verb = "hide <account>", "Hide an account's machines from the list (nothing is deleted)", "hidden"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.Update(func(cfg *config.Config) error {
				if !cfg.SetAccountHidden(args[0], hide) {
					return fmt.Errorf("account '%s' not found", args[0])
				}
				fmt.Fprintf(cmd.OutOrStdout(), "✓ Account '%s' is now %s\n", args[0], verb)
				return nil
			})
		},
	}
}

func newAccountRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <account>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove an account from ts-cli",
		Long:    `Remove a Tailscale account from the ts-cli configuration. The API key is deleted from ~/.ts-cli; nothing changes in Tailscale itself.`,
		Example: `  ts-cli account remove personal.com`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.Update(func(cfg *config.Config) error {
				if len(cfg.Accounts) == 0 {
					return fmt.Errorf("no accounts configured")
				}
				acc := cfg.FindAccount(args[0])
				if acc == nil {
					return fmt.Errorf("account '%s' not found", args[0])
				}
				removedName, wasActive := acc.Name, acc.Active
				cfg.RemoveAccount(args[0])
				fmt.Fprintf(cmd.OutOrStdout(), "✓ Removed account: %s\n", removedName)
				if wasActive {
					if active := cfg.GetActiveAccount(); active != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "Set '%s' as the new active account\n", active.Name)
					}
				}
				return nil
			})
		},
	}
}
