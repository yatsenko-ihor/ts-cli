package commands

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// Seams for tests.
var (
	lookPathFn         = exec.LookPath
	tailscaleStatusFn  = CheckTailscaleStatus
	checkAccountFn     = client.CheckAccount
	decryptSecretFn    = util.DecryptPassword
	currentSecretStore = util.CurrentSecretStore
)

type checkLevel int

const (
	checkOK checkLevel = iota
	checkWarn
	checkFail
)

var levelMark = map[checkLevel]string{checkOK: "✓", checkWarn: "⚠", checkFail: "✗"}

// check is one line of the report.
type check struct {
	level  checkLevel
	name   string
	detail string
}

// keyExpiryWarn is how close to expiry an API key gets a warning.
const keyExpiryWarn = 14 * 24 * time.Hour

// toolChecks reports the programs ts-cli drives.
func toolChecks() []check {
	var out []check
	tool := func(name string, missing checkLevel, why string) {
		if path, err := lookPathFn(name); err == nil {
			out = append(out, check{checkOK, name, path})
			return
		}
		out = append(out, check{missing, name, "not found: " + why})
	}
	tool("ssh", checkFail, "ts-cli cannot open sessions or run commands without it")
	tool("scp", checkFail, "file copy needs it")
	tool("sshpass", checkWarn, "saved-password logins need it (key logins still work)")
	tool("tmux", checkWarn, "ssh sessions cannot open in a split pane")
	tool("tailscale", checkWarn, "ping, path details and Tailscale SSH need the CLI")
	return out
}

// tailscaleCheck reports whether the local daemon runs and is logged in.
func tailscaleCheck() check {
	if _, err := lookPathFn("tailscale"); err != nil {
		return check{checkWarn, "tailscaled", "not checked (no tailscale CLI)"}
	}
	if ok, msg := tailscaleStatusFn(); !ok {
		first, _, _ := strings.Cut(msg, "\n")
		return check{checkFail, "tailscaled", first}
	}
	return check{checkOK, "tailscaled", "running and logged in"}
}

// configChecks reports the configuration and the stored secrets, and returns
// the loaded configuration (nil when it cannot be read).
func configChecks() ([]check, *config.Config) {
	cfg, err := config.Load()
	if err != nil {
		return []check{{checkFail, "config", "cannot be read: " + err.Error()}}, nil
	}
	out := []check{{checkOK, "config", fmt.Sprintf("%d account(s), %d machine login(s)", len(cfg.Accounts), len(cfg.MachineCredentials))}}
	out = append(out, check{checkOK, "secrets", "master key in " + fmt.Sprint(currentSecretStore())})
	unreadable := 0
	for _, cred := range cfg.MachineCredentials {
		if cred.PasswordEncrypted != "" {
			if _, err := decryptSecretFn(cred.PasswordEncrypted); err != nil {
				unreadable++
			}
		}
	}
	if unreadable > 0 {
		out = append(out, check{checkWarn, "passwords", fmt.Sprintf("%d saved password(s) cannot be decrypted (the key changed); they will be asked for again", unreadable)})
	}
	if len(cfg.Accounts) == 0 {
		out = append(out, check{checkWarn, "accounts", "none configured: run `ts-cli login --tailnet=<name>`"})
	}
	return out, cfg
}

// accountChecks asks the API about each account's key.
func accountChecks(cfg *config.Config, now time.Time) []check {
	var out []check
	for _, acc := range cfg.Accounts {
		h := checkAccountFn(client.AccountInfo{Name: acc.Name, APIKey: acc.APIKey, Tailnet: acc.Tailnet})
		switch {
		case h.Unauth:
			out = append(out, check{checkFail, "account " + acc.Name, "the API key is rejected, revoked or expired: " + h.Err.Error()})
		case !h.Valid:
			out = append(out, check{checkWarn, "account " + acc.Name, "could not be checked: " + h.Err.Error()})
		case h.HasExpiry && h.Expires.Before(now):
			out = append(out, check{checkFail, "account " + acc.Name, "the API key expired on " + h.Expires.Format("2006-01-02")})
		case h.HasExpiry && h.Expires.Sub(now) < keyExpiryWarn:
			out = append(out, check{checkWarn, "account " + acc.Name, fmt.Sprintf("the API key expires in %d day(s), on %s", int(h.Expires.Sub(now).Hours()/24), h.Expires.Format("2006-01-02"))})
		case h.HasExpiry:
			out = append(out, check{checkOK, "account " + acc.Name, fmt.Sprintf("key valid, expires in %d days", int(h.Expires.Sub(now).Hours()/24))})
		default:
			out = append(out, check{checkOK, "account " + acc.Name, "key valid (" + noteOrUnknown(h.ExpiryNote) + ")"})
		}
	}
	return out
}

func noteOrUnknown(note string) string {
	if note == "" {
		return "expiry unknown"
	}
	return note
}

// printChecks writes a titled group and returns how many warnings and failures it held.
func printChecks(out io.Writer, title string, checks []check) (warn, fail int) {
	fmt.Fprintln(out, title)
	for _, c := range checks {
		fmt.Fprintf(out, "  %s %-14s %s\n", levelMark[c.level], c.name, c.detail)
		switch c.level {
		case checkWarn:
			warn++
		case checkFail:
			fail++
		}
	}
	return
}

// NewDoctorCommand creates the doctor command.
func NewDoctorCommand() *cobra.Command {
	var offline bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that everything ts-cli needs is in place",
		Long: `Report what works and what does not: the tools ts-cli drives (ssh, scp, sshpass,
tmux, tailscale), the local Tailscale daemon, the configuration and the
encryption key, saved passwords that can no longer be decrypted, and each
account's API key (valid, close to expiry, rejected).

Exit status is 1 when something is broken (a ✗ line); warnings (⚠) are
optional features that are unavailable.

  --offline  skip the API key checks (no network requests)`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "ts-cli doctor (v%s)\n\n", Version)
			var warns, fails int
			add := func(w, f int) { warns, fails = warns+w, fails+f }

			add(printChecks(out, "Tools", append(toolChecks(), tailscaleCheck())))
			fmt.Fprintln(out)
			cfgChecks, cfg := configChecks()
			add(printChecks(out, "Configuration", cfgChecks))
			if cfg != nil && len(cfg.Accounts) > 0 {
				fmt.Fprintln(out)
				if offline {
					add(printChecks(out, "Accounts", []check{{checkWarn, "api keys", "not checked (--offline)"}}))
				} else {
					add(printChecks(out, "Accounts", accountChecks(cfg, time.Now())))
				}
			}
			fmt.Fprintf(out, "\n%d problem(s), %d warning(s)\n", fails, warns)
			if fails > 0 {
				cmd.SilenceUsage = true // a finding, not a usage mistake
				return fmt.Errorf("%d problem(s) found", fails)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "Skip the API key checks")
	return cmd
}
