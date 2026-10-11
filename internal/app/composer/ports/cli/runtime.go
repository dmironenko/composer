package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

const (
	actionRun           = "run"
	actionApply         = "apply"
	defaultApplyTimeout = 15 * time.Minute
)

var (
	errOutput  = errors.New("output must be json")
	errTimeout = errors.New("timeout must be positive")
)

func runtimeCommand(configPath *string, bootstrap Bootstrap, action, short string) *cobra.Command {
	var (
		wait    bool
		timeout time.Duration
		output  string
	)

	cmd := &cobra.Command{
		Use: action, Short: short, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if output != "json" {
				return errOutput
			}

			if timeout <= 0 {
				return errTimeout
			}

			ctx := cmd.Context()

			if action == actionApply {
				var cancel context.CancelFunc

				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}

			return runConfigured(ctx, cmd, *configPath, wait, bootstrap)
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", false, "Retry until Compose reports readiness")
	cmd.Flags().DurationVar(&timeout, "timeout", defaultApplyTimeout, "Overall apply timeout")
	cmd.Flags().StringVar(&output, "output", "json", "Output format: json")

	return cmd
}

func runConfigured(
	ctx context.Context, cmd *cobra.Command, configPath string, wait bool, bootstrap Bootstrap,
) (err error) {
	session, err := bootstrap.Open(configPath, cmd.Name())
	if err != nil {
		return fmt.Errorf("open runtime: %w", err)
	}

	defer func() { err = errors.Join(err, session.Close()) }()

	err = session.Runner.Run(ctx, cmd.Name(), wait, reportOutput(cmd, wait))
	if err != nil {
		return fmt.Errorf("run composer: %w", err)
	}

	return nil
}
