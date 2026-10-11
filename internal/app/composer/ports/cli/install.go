package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func installCommand(configPath *string, bootstrap Bootstrap) *cobra.Command {
	return &cobra.Command{
		Use: "install", Short: "Install and start a systemd service (Linux, root)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			unit, err := bootstrap.Install(cmd.Context(), *configPath, cmd.ErrOrStderr())
			if err != nil {
				return fmt.Errorf("install service: %w", err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Started %s; logs: journalctl -u %s -f\n", unit, unit)
			if err != nil {
				return fmt.Errorf("write installation result: %w", err)
			}

			return nil
		},
	}
}
