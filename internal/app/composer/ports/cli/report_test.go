//nolint:testpackage // Verify the private presentation mapper at the CLI boundary.
package cli

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

func TestReportEmptyInventory(t *testing.T) {
	t.Parallel()

	checkReport(t, domain.Report{Commit: "revision"}, "{\"commit\":\"revision\",\"projects\":null}\n")
}

func TestReportPrivateInputs(t *testing.T) {
	t.Parallel()

	result := domain.Report{Commit: "revision", Projects: []domain.Change{{
		Project: domain.Project{
			Name: "api", Environment: "prod", Enabled: true, Hash: "hash",
			Directory: "private-directory", Network: "private-network",
			Manifest: domain.Manifest{Content: []byte("secret-manifest"), ConfigHash: "private-config"},
			Build:    &domain.BuildSpec{Repository: "private-repo", Ref: "private-commit", Dockerfile: "private-build"},
		}, Action: "apply",
	}}}
	expected := "{\"commit\":\"revision\",\"projects\":[{\"name\":\"api\",\"environment\":\"prod\"," +
		"\"enabled\":true,\"hash\":\"hash\",\"action\":\"apply\"}]}\n"
	checkReport(t, result, expected)
}

func checkReport(t *testing.T, result domain.Report, expected string) {
	t.Helper()

	var output bytes.Buffer

	command := &cobra.Command{Use: "validate"}
	command.SetOut(&output)

	if err := reportOutput(command, false)(result, nil); err != nil {
		t.Fatal(err)
	}

	if output.String() != expected {
		t.Fatalf("report actual=%s expected=%s", output.String(), expected)
	}
}
