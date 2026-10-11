// Package cli owns command parsing and presentation, not deployment policy.
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func NewCommand(version string, stdout, stderr io.Writer, bootstrap Bootstrap) *cobra.Command {
	root := &cobra.Command{
		Use: "composer", Short: "GitOps for Docker Compose", Version: version,
		SilenceUsage: true, SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	var configPath string
	root.PersistentFlags().StringVar(&configPath, "config", "composer.yaml", "Host configuration file")
	root.AddCommand(initCommand(&configPath, bootstrap), configCommand(&configPath, bootstrap), templateCommand(bootstrap))
	root.AddCommand(installCommand(&configPath, bootstrap), secretsCommand(bootstrap))
	root.AddCommand(
		runtimeCommand(&configPath, bootstrap, "validate", "Render and validate the committed snapshot"),
		runtimeCommand(&configPath, bootstrap, "plan", "Compare rendered projects with successful hashes"),
		runtimeCommand(&configPath, bootstrap, "apply", "Apply one committed snapshot"),
		runtimeCommand(&configPath, bootstrap, actionRun, "Poll Git and apply changed projects"),
	)

	return root
}

func initCommand(configPath *string, bootstrap Bootstrap) *cobra.Command {
	var branch, installation string

	cmd := &cobra.Command{
		Use: "init PATH", Short: "Generate an empty GitOps repo structure and host config",
		Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := bootstrap.Init(args[0], *configPath, branch, installation); err != nil {
				return fmt.Errorf("initialize repository: %w", err)
			}

			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"Created %s and %s. Commit the repo on branch %s before deployment.\n", args[0], *configPath, branch)
			if err != nil {
				return fmt.Errorf("write initialization result: %w", err)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "GitOps branch to follow (required)")
	cmd.Flags().StringVar(&installation, "installation", "demo", "Installation name")

	return cmd
}

func configCommand(configPath *string, bootstrap Bootstrap) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect host configuration"}
	cmd.AddCommand(&cobra.Command{
		Use: "check", Short: "Check host YAML without contacting Git or Docker", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := bootstrap.Check(*configPath)
			if err != nil {
				return fmt.Errorf("load configuration: %w", err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(),
				"Configuration valid: installation=%s branches=%v\n", cfg.Installation, cfg.Branches)
			if err != nil {
				return fmt.Errorf("write configuration result: %w", err)
			}

			return nil
		},
	})

	return cmd
}
