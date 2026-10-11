package state_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/dmironenko/composer/internal/app/composer/adapters/state"
	"github.com/dmironenko/composer/internal/app/composer/domain"
	"github.com/dmironenko/composer/internal/app/composer/service/deployment"
)

//nolint:gocognit,gocyclo,cyclop,funlen,maintidx // Verify each plan and its unchanged on-disk state together.
func TestPlan(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, environment, applied, hash, renderError, expected string
		present, enabled, attempted                             bool
	}{
		{name: "successful hash", present: true, enabled: true, attempted: true, applied: "A", hash: "A", expected: "noop"},
		{name: "changed hash", present: true, enabled: true, attempted: true, applied: "A", hash: "B", expected: "apply"},
		{name: "first deployment", present: true, enabled: true, hash: "A", expected: "apply"},
		{name: "failed attempt retries", present: true, enabled: true, attempted: true, hash: "B", expected: "apply"},
		{
			name: "revert after invalidated success", present: true, enabled: true,
			attempted: true, hash: "A", expected: "apply",
		},
		{
			name: "invalid project is retained", present: true, attempted: true, applied: "A",
			renderError: "invalid vars yaml", expected: "failed",
		},
		{name: "disabled attempted project", present: true, attempted: true, applied: "A", expected: "remove"},
		{name: "deleted attempted project", attempted: true, applied: "A", expected: "remove"},
		{name: "deleted partial deployment", attempted: true, expected: "remove"},
		{name: "excluded environment", environment: "stage", attempted: true, applied: "A"},
		{name: "validation artifacts only", expected: "noop"},
		{name: "disabled before deployment", present: true, expected: "noop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := fixture{StateDir: t.TempDir()}
			cfg.store = state.New(cfg.StateDir, []string{"prod"})

			environment := tc.environment
			if environment == "" {
				environment = "prod"
			}

			directory := filepath.Join(cfg.StateDir, "deployments", environment, "project")
			if err := os.MkdirAll(directory, 0o700); err != nil {
				t.Fatal(err)
			}

			markers := map[string]string{}
			if tc.attempted {
				markers["attempted"] = ""
			}

			if tc.applied != "" {
				markers["applied"] = tc.applied
			}

			for name, value := range markers {
				if err := os.WriteFile(filepath.Join(directory, name), []byte(value), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			var projects []domain.Project
			if tc.present {
				projects = append(projects, domain.Project{
					Name: "project", Environment: environment,
					Directory: directory, Enabled: tc.enabled, Hash: tc.hash, Error: tc.renderError,
				})
			}

			actual, err := deployment.Plan(cfg.store, projects)
			if err != nil {
				t.Fatal(err)
			}

			if tc.expected == "" {
				if len(actual) != 0 {
					t.Fatalf("changes=%v expected none", actual)
				}
			} else if len(actual) != 1 || actual[0].Action != tc.expected || actual[0].Name != "project" ||
				actual[0].Environment != environment || actual[0].Error != tc.renderError {
				t.Fatalf("changes=%v expected action=%q environment=%q error=%q", actual, tc.expected, environment, tc.renderError)
			}

			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != len(markers) {
				t.Fatalf("plan changed marker directory: entries=%v error=%v", entries, err)
			}

			for name, expected := range markers {
				actual, err := os.ReadFile(filepath.Join(directory, name))
				if err != nil || string(actual) != expected {
					t.Fatalf("marker %s: actual=%q expected=%q error=%v", name, actual, expected, err)
				}
			}
		})
	}
}

//nolint:gocognit,gocyclo,cyclop,maintidx // Keep network expectations beside the per-project failure assertions.
func TestExecutorNetworks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		expectedErr error
	}{
		{name: "all networks precede deployments"},
		{name: "failed network blocks only its consumers", expectedErr: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, changes := deployments(t, "one", "two", "three")
			changes[2].Network = changes[0].Network
			backend := NewMockBackend(gomock.NewController(t))
			first := backend.EXPECT().EnsureNetwork(gomock.Any(), "one", cfg.Installation).Return(tc.expectedErr)
			last := backend.EXPECT().EnsureNetwork(gomock.Any(), "two", cfg.Installation).Return(nil).After(first)
			backend.EXPECT().Up(gomock.Any(), changes[1].Project, time.Second).Return(nil).After(last)

			if tc.expectedErr == nil {
				backend.EXPECT().Up(gomock.Any(), changes[0].Project, time.Second).Return(nil).After(last)
				backend.EXPECT().Up(gomock.Any(), changes[2].Project, time.Second).Return(nil).After(last)
			}

			err := deployment.New(backend, nil, cfg.store).Apply(t.Context(), cfg.Options, changes)
			if (err != nil) != (tc.expectedErr != nil) {
				t.Fatalf("Apply error=%v expected failure=%v", err, tc.expectedErr)
			}

			for _, change := range changes {
				expected := "apply"
				if tc.expectedErr != nil && change.Network == "one" {
					expected = "failed"

					assertMarker(t, change.Directory, "attempted", "", os.ErrNotExist)
				} else {
					assertMarker(t, change.Directory, "applied", change.Hash, nil)
				}

				if change.Action != expected {
					t.Errorf("project %s action=%s expected=%s", change.Name, change.Action, expected)
				}
			}
		})
	}
}

//nolint:maintidx // Synchronization and marker assertions describe one concurrent rollout.
func TestExecutorIndependentWorkers(t *testing.T) {
	t.Parallel()

	cases := []struct{ name string }{{name: "failed rollout does not cancel another worker"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				cfg, changes := deployments(t, "broken", "healthy")
				backend := NewMockBackend(gomock.NewController(t))
				backend.EXPECT().EnsureNetwork(gomock.Any(), "broken", cfg.Installation).Return(nil)
				backend.EXPECT().EnsureNetwork(gomock.Any(), "healthy", cfg.Installation).Return(nil)

				release := make(chan struct{})
				started := make(chan struct{})

				backend.EXPECT().Up(gomock.Any(), changes[0].Project, time.Second).
					DoAndReturn(func(context.Context, domain.Project, time.Duration) error {
						<-started

						return context.Canceled
					})
				backend.EXPECT().Up(gomock.Any(), changes[1].Project, time.Second).
					DoAndReturn(func(ctx context.Context, _ domain.Project, _ time.Duration) error {
						close(started)
						<-release

						if err := ctx.Err(); err != nil {
							t.Errorf("neighbor failure canceled healthy project: %v", err)
						}

						return nil
					})

				completed := make(chan error, 1)

				go func() { completed <- deployment.New(backend, nil, cfg.store).Apply(t.Context(), cfg.Options, changes) }()

				synctest.Wait()
				close(release)

				if err := <-completed; err == nil {
					t.Fatal("Apply succeeded despite a failed project")
				}

				assertMarker(t, changes[0].Directory, "applied", "", os.ErrNotExist)
				assertMarker(t, changes[0].Directory, "attempted", "", nil)
				assertMarker(t, changes[1].Directory, "applied", changes[1].Hash, nil)
			})
		})
	}
}

//nolint:gocognit,gocyclo,cyclop,maintidx // Verify failure, replanning and recovery as one lifecycle.
func TestExecutorFailedRolloutRevert(t *testing.T) {
	t.Parallel()

	cases := []struct{ name string }{{name: "failure invalidates old success before docker and permits revert"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, changes := deployments(t, "app")

			change := &changes[0]
			if err := os.WriteFile(filepath.Join(change.Directory, "applied"), []byte("previous"), 0o600); err != nil {
				t.Fatal(err)
			}

			backend := NewMockBackend(gomock.NewController(t))
			backend.EXPECT().EnsureNetwork(gomock.Any(), change.Network, cfg.Installation).Return(nil).Times(2)
			failed := backend.EXPECT().Up(gomock.Any(), change.Project, time.Second).
				DoAndReturn(func(context.Context, domain.Project, time.Duration) error {
					assertMarker(t, change.Directory, "applied", "", os.ErrNotExist)
					assertMarker(t, change.Directory, "attempted", "", nil)

					return fmt.Errorf("SDK failure containing a secret: %w", context.Canceled)
				})
			expectedRevert := change.Project
			expectedRevert.Hash = "previous"
			backend.EXPECT().Up(gomock.Any(), expectedRevert, time.Second).Return(nil).After(failed)

			executor := deployment.New(backend, nil, cfg.store)
			if err := executor.Apply(t.Context(), cfg.Options, changes); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("expected sanitized deployment failure, actual=%v", err)
			}

			if strings.Contains(change.Error, "secret") {
				t.Fatalf("report exposes SDK error: %s", change.Error)
			}

			change.Hash, change.Error = "previous", ""

			reverted, err := deployment.Plan(cfg.store, []domain.Project{change.Project})
			if err != nil || len(reverted) != 1 || reverted[0].Action != "apply" {
				t.Fatalf("revert plan=%v error=%v", reverted, err)
			}

			if err := executor.Apply(t.Context(), cfg.Options, reverted); err != nil {
				t.Fatal(err)
			}

			assertMarker(t, change.Directory, "applied", "previous", nil)
			assertMarker(t, change.Directory, "attempted", "", nil)
		})
	}
}

//nolint:gocognit,maintidx // Compare successful and failed removal with the resulting on-disk state.
func TestExecutorRemoval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		expectedErr error
	}{
		{name: "successful removal deletes markers"},
		{name: "failed removal retains attempted marker", expectedErr: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, changes := deployments(t, "app")
			change := &changes[0]

			change.Enabled, change.Action = false, "remove"
			for _, marker := range []string{"applied", "attempted"} {
				if err := os.WriteFile(filepath.Join(change.Directory, marker), []byte("previous"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			backend := NewMockBackend(gomock.NewController(t))
			backend.EXPECT().Down(gomock.Any(), change.Name).
				DoAndReturn(func(context.Context, string) error {
					assertMarker(t, change.Directory, "applied", "", os.ErrNotExist)

					return tc.expectedErr
				})

			err := deployment.New(backend, nil, cfg.store).Apply(t.Context(), cfg.Options, changes)
			if (err != nil) != (tc.expectedErr != nil) {
				t.Fatalf("Apply error=%v expected failure=%v", err, tc.expectedErr)
			}

			if tc.expectedErr == nil {
				if _, err := os.Stat(change.Directory); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("removed deployment directory remains: %v", err)
				}
			} else {
				assertMarker(t, change.Directory, "attempted", "previous", nil)
			}
		})
	}
}

func deployments(t *testing.T, names ...string) (fixture, []domain.Change) {
	t.Helper()
	cfg := fixture{
		StateDir: t.TempDir(),
		Options:  deployment.Options{Installation: "test", Parallelism: len(names), DeployTimeout: time.Minute},
	}
	cfg.store = state.New(cfg.StateDir, []string{"prod"})

	changes := make([]domain.Change, 0, len(names))
	for _, name := range names {
		directory := filepath.Join(cfg.StateDir, "deployments", "prod", name)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}

		changes = append(changes, domain.Change{
			Project: domain.Project{
				Name: name, Environment: "prod", Enabled: true, Directory: directory,
				Network: name, Hash: "current", Manifest: domain.Manifest{Content: []byte(name)},
			},
			Action: "apply",
		})
	}

	return cfg, changes
}

func assertMarker(t *testing.T, directory, name, expected string, expectedErr error) {
	t.Helper()

	actual, err := os.ReadFile(filepath.Join(directory, name))
	if !errors.Is(err, expectedErr) || string(actual) != expected {
		t.Errorf("marker %s: actual=%q expected=%q error=%v expected error=%v", name, actual, expected, err, expectedErr)
	}
}

type fixture struct {
	deployment.Options

	StateDir string
	store    *state.Store
}

func TestInventoryScope(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	for _, env := range []string{"prod", "dan"} {
		if err := os.MkdirAll(filepath.Join(root, "deployments", env, "app"), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name  string
		scope []string
		count int
	}{
		{name: "all historical environments", count: 2},
		{name: "explicit environment", scope: []string{"prod"}, count: 1},
		{name: "empty scope", scope: []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projects, err := state.New(root, tc.scope).List()
			if err != nil || len(projects) != tc.count {
				t.Fatalf("inventory: count=%d, error=%v", len(projects), err)
			}
		})
	}
}
