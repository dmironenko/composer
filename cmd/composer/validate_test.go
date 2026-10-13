package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmironenko/composer/v2/internal/app/composer/ports/cli"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/dmironenko/composer/v2/internal/app/composer/adapters/gitops"
)

//nolint:gocognit,gocyclo,cyclop,maintidx // Keep each table case setup, action and assertions together.
func TestValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, branch, change string
		commit               bool
		expectedErr          error
	}{
		{name: "master", branch: "master"},
		{name: "main", branch: "main"},
		{name: "dirty config ignored", branch: "master", change: "config"},
		{name: "committed config", branch: "master", change: "config", commit: true},
		{
			name: "broken app retains identity", branch: "master", change: "invalid",
			commit: true, expectedErr: domain.ErrInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parent := t.TempDir()
			root, host := filepath.Join(parent, "repo"), filepath.Join(parent, "composer.yaml")
			runCLI(t, nil, "init", root, "--branch", tc.branch, "--config", host)
			seed(t, root)

			repo, err := gogit.PlainInitWithOptions(root, &gogit.PlainInitOptions{
				InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName(tc.branch)},
			})
			if err != nil {
				t.Fatal(err)
			}

			commitAll(t, repo)

			before := validationReport(t, host, nil)
			if len(before.Projects) != 4 {
				t.Fatalf("expected four identities, got %d", len(before.Projects))
			}

			for _, deployment := range before.Projects {
				expected := !strings.Contains(deployment.Name, "-web-") || deployment.Environment != "stage"
				if deployment.Enabled != expected {
					t.Fatalf("unexpected enabled: %+v", deployment)
				}
			}

			switch tc.change {
			case "config":
				writeFile(t, filepath.Join(root, "apps/demo/api/configs/message.txt.tmpl"), "changed config\n")
			case "invalid":
				writeFile(t, filepath.Join(root, "apps/demo/api/vars/base.yaml"), "message: [\n")
			}

			if tc.commit {
				commitAll(t, repo)
			}

			after := validationReport(t, host, tc.expectedErr)
			if len(after.Projects) != len(before.Projects) {
				t.Fatal("invalid app disappeared")
			}

			for index, deployment := range after.Projects {
				previous := before.Projects[index]
				if deployment.Name != previous.Name {
					t.Fatal("identity changed")
				}

				changed := deployment.Hash != previous.Hash

				expected := tc.commit && strings.Contains(deployment.Name, "-api-")
				if changed != expected {
					t.Fatalf("%+v fingerprint changed=%v expected=%v", deployment.Name, changed, expected)
				}

				if strings.Contains(deployment.Name, "-web-") && deployment.Error != "" {
					t.Fatalf("healthy app failed: %s", deployment.Error)
				}
			}
		})
	}
}

//nolint:gocognit // Keep each table case setup, action and assertions together.
func TestTemplateExport(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		exists      bool
		expectedErr error
	}{
		{name: "exact template"}, {name: "refuse overwrite", exists: true, expectedErr: fs.ErrExist},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "compose.yaml.tmpl")
			if tc.exists {
				writeFile(t, path, "user template")
			}

			runCLI(t, tc.expectedErr, "template", "export", "compose", "--output", path)

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			expected := gitops.DefaultTemplate()
			if tc.exists {
				expected = "user template"
			}

			if string(data) != expected {
				t.Fatal("unexpected exported contents")
			}
		})
	}
}

func validationReport(t *testing.T, host string, expectedErr error) cli.Report {
	t.Helper()
	output := runCLI(t, expectedErr, "validate", "--config", host, "--output", "json")

	var report cli.Report
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatal(err)
	}

	if report.Commit == "" {
		t.Fatal("missing snapshot SHA")
	}

	if strings.Contains(string(output), "api: production") {
		t.Fatal("report exposed config contents")
	}

	return report
}

func runCLI(t *testing.T, expectedErr error, args ...string) []byte {
	t.Helper()

	var stdout, stderr bytes.Buffer

	cmd := newCommand("test", &stdout, &stderr)
	cmd.SetArgs(args)

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("%v: actual=%v expected=%v", args, err, expectedErr)
	}

	return stdout.Bytes()
}

func commitAll(t *testing.T, repo *gogit.Repository) {
	t.Helper()

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	if err = worktree.AddWithOptions(&gogit.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}

	if _, err = worktree.Commit("test", &gogit.CommitOptions{Author: &object.Signature{
		Name: "test", Email: "test@example.invalid", When: time.Unix(1700000000, 0),
	}}); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

//go:embed testdata/gitops
var fixtures embed.FS

func seed(t *testing.T, root string) {
	t.Helper()

	err := fs.WalkDir(fixtures, "testdata/gitops", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("copy fixture: %w", err)
		}

		if entry.IsDir() {
			return nil
		}

		return copyFixture(root, name)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func copyFixture(root, name string) error {
	data, err := fixtures.ReadFile(name)
	if err != nil {
		return fmt.Errorf("copy fixture: %w", err)
	}

	target := filepath.Join(root, strings.TrimPrefix(name, "testdata/gitops/"))
	if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("copy fixture: %w", err)
	}

	if err = os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("write fixture: %w", err)
	}

	return nil
}

func TestEnvironmentBranches(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{localMode, remoteMode} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			checkEnvironmentBranches(t, mode)
		})
	}
}

//nolint:gocognit,gocyclo,cyclop,funlen,maintidx // Keep the branch isolation and fast-forward lifecycle together.
func checkEnvironmentBranches(t *testing.T, mode string) {
	t.Helper()
	parent := t.TempDir()
	root, host := filepath.Join(parent, "repo"), filepath.Join(parent, "composer.yaml")
	runCLI(t, nil, "init", root, "--branch", "master", "--config", host)
	seed(t, root)

	repo, err := gogit.PlainInitWithOptions(root, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("master")},
	})
	if err != nil {
		t.Fatal(err)
	}

	commitAll(t, repo)
	before := validationReport(t, host, nil)

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	checkout := &gogit.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("dev"), Create: true}
	if err = worktree.Checkout(checkout); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(root, "apps/demo/api/configs/message.txt.tmpl"), "changed on dev\n")
	commitAll(t, repo)

	cfg, err := loadConfig(host)
	if err != nil {
		t.Fatal(err)
	}

	cfg.Source.Branches = map[string]string{"prod": "master", "stage": "dev"}
	if mode == remoteMode {
		cfg.Source.Mode, cfg.Source.URL, cfg.Source.Path = remoteMode, root, ""
	}
	// writeConfig intentionally refuses existing files.
	if err = os.Remove(host); err != nil {
		t.Fatal(err)
	}

	if err = writeConfig(host, cfg); err != nil {
		t.Fatal(err)
	}

	var after cli.Report
	if err = json.Unmarshal(runCLI(t, nil, "validate", "--config", host), &after); err != nil {
		t.Fatal(err)
	}

	if after.Commit != "" || after.Commits["prod"] == after.Commits["stage"] {
		t.Fatalf("distinct branch commits lost: %+v", after)
	}

	for i, project := range after.Projects {
		expected := project.Environment == "stage" && strings.Contains(project.Name, "-api-")
		if (project.Hash != before.Projects[i].Hash) != expected {
			t.Fatalf("dev changed unexpected project: %+v", project)
		}
	}
	// A fast-forward promotion transfers the very same tree to production.
	ref, err := repo.Reference(plumbing.NewBranchReferenceName("dev"), true)
	if err != nil {
		t.Fatal(err)
	}

	promotedRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName("master"), ref.Hash())
	if err = repo.Storer.SetReference(promotedRef); err != nil {
		t.Fatal(err)
	}

	promoted := validationReport(t, host, nil)
	for i, project := range promoted.Projects {
		expected := project.Environment == "prod" && strings.Contains(project.Name, "-api-")
		if (project.Hash != after.Projects[i].Hash) != expected {
			t.Fatalf("promotion changed unexpected project: %+v", project)
		}
	}
}
