package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
)

// Report exposes deployment outcomes, never manifests, credentials or build inputs.
type Report struct {
	Commits  map[string]string `json:"commits,omitempty"`
	Commit   string            `json:"commit"`
	Projects []ProjectReport   `json:"projects"`
}

// ProjectReport is the public status of one independent deployment.
type ProjectReport struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
	Enabled     bool   `json:"enabled"`
	Hash        string `json:"hash,omitempty"`
	Error       string `json:"error,omitempty"`
	Action      string `json:"action"`
}

func reportOutput(cmd *cobra.Command, wait bool) func(domain.Report, error) error {
	return func(result domain.Report, stepErr error) error {
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(publicReport(result)); err != nil {
			return fmt.Errorf("write report: %w", err)
		}

		if stepErr != nil && (cmd.Name() == actionRun || cmd.Name() == actionApply && wait) {
			cmd.PrintErrln(stepErr)
		}

		return nil
	}
}

func publicReport(result domain.Report) Report {
	output := Report{Commit: result.Commit, Commits: result.Commits}
	for _, change := range result.Projects {
		output.Projects = append(output.Projects, ProjectReport{
			Name: change.Name, Environment: change.Environment, Enabled: change.Enabled,
			Hash: change.Hash, Error: change.Error, Action: change.Action,
		})
	}

	return output
}
