package commands

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// NewSecretsCommand creates the `secrets` command group
func NewSecretsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Choose where the encryption key for stored credentials lives",
		Long: `ts-cli encrypts SSH passwords and Tailscale API keys with one master key.
That key can be kept in a file (~/.ts-cli/key, default) or in the operating
system's credential store (macOS Keychain, Linux Secret Service, Windows
Credential Manager).

With the keychain the key is unlocked together with your login session and
is not part of ~/.ts-cli, so a copied or backed-up config directory alone
cannot be decrypted. When the keychain cannot be reached (headless or SSH
session, no D-Bus), ts-cli falls back to the key file if one exists.`,
	}

	cmd.AddCommand(newSecretsStatusCommand())
	cmd.AddCommand(newSecretsUseCommand())
	return cmd
}

func newSecretsStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show where the master key is stored and whether the keychain is available",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Secret store: %s\n", util.CurrentSecretStore())
			if err := util.KeychainAvailable(); err != nil {
				fmt.Fprintf(out, "Keychain:     unavailable (%v)\n", err)
			} else {
				fmt.Fprintln(out, "Keychain:     available")
			}

			// Loading the config exercises the key; report how it went.
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(out, "Config:       cannot be decrypted: %v\n", err)
				return nil
			}
			fmt.Fprintf(out, "Config:       %d account(s), %d saved machine credential(s)\n",
				len(cfg.Accounts), len(cfg.MachineCredentials))
			if notice := util.KeyStoreNotice(); notice != "" {
				fmt.Fprintf(out, "Notice:       %s\n", notice)
			}
			return nil
		},
	}
}

func newSecretsUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use <keychain|file>",
		Short: "Move the master key into the OS keychain or back into ~/.ts-cli/key",
		Example: `  # Keep the key in the OS credential store
  ts-cli secrets use keychain

  # Go back to the key file
  ts-cli secrets use file`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"keychain", "file"},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			switch util.SecretStore(args[0]) {
			case util.SecretStoreKeychain:
				if err := util.UseKeychain(); err != nil {
					return err
				}
				if util.KeyWasReset() {
					reportLostKey(out)
				}
				fmt.Fprintln(out, "✓ Master key is now stored in the OS keychain; ~/.ts-cli/key removed")
			case util.SecretStoreFile:
				if err := util.UseFile(); err != nil {
					return err
				}
				fmt.Fprintln(out, "✓ Master key is now stored in ~/.ts-cli/key; keychain item removed")
			default:
				return fmt.Errorf("unknown store %q (use 'keychain' or 'file')", args[0])
			}

			// Prove the switch worked before telling the user it did.
			if _, err := config.Load(); err != nil {
				return fmt.Errorf("switched, but the config can no longer be decrypted: %w", err)
			}
			fmt.Fprintln(out, "✓ Stored credentials verified readable")
			return nil
		},
	}
}

// reportLostKey explains that the old master key was gone, removes the
// secrets that were encrypted with it and says what was lost.
func reportLostKey(out io.Writer) {
	fmt.Fprintln(out, "⚠ The master key was missing from the keychain and there was no key file,")
	fmt.Fprintln(out, "  so a NEW key was created. Secrets encrypted with the old key cannot be recovered.")
	dropped, err := config.DropUnreadableSecrets()
	if err != nil {
		fmt.Fprintf(out, "  Could not remove the unreadable secrets: %v\n", err)
		return
	}
	if len(dropped.Accounts) == 0 && len(dropped.Passwords) == 0 {
		fmt.Fprintln(out, "  No stored secret depended on the old key.")
		return
	}
	if len(dropped.Accounts) > 0 {
		fmt.Fprintf(out, "  Removed accounts (add them again with `ts-cli account add`): %s\n", strings.Join(dropped.Accounts, ", "))
	}
	if len(dropped.Passwords) > 0 {
		fmt.Fprintf(out, "  Removed saved SSH passwords for: %s (you will be asked again on the next connection)\n", strings.Join(dropped.Passwords, ", "))
	}
}
