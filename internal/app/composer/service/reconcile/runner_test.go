package reconcile_test

//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -destination=state_mock_test.go -package=reconcile_test github.com/dmironenko/composer/internal/app/composer/service/deployment Inventory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/dmironenko/composer/internal/app/composer/domain"
	"github.com/dmironenko/composer/internal/app/composer/service/deployment"
	"github.com/dmironenko/composer/internal/app/composer/service/reconcile"
)

func TestSnapshotLifecycle(t *testing.T) {
	t.Parallel()

	cases := []snapshotCase{
		{name: "apply retries same snapshot", action: "apply", expected: "first"},
		{name: "run refreshes snapshot", action: "run", expected: "second"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			testSnapshotLifecycle(t, tc)
		})
	}
}

func TestFetchFailureDoesNotApplyOldSnapshot(t *testing.T) {
	t.Parallel()

	cfg, snapshot := fixture(t, "first")
	controller := gomock.NewController(t)
	source := NewMockSource(controller)
	deployer := NewMockDeployer(controller)
	renderer := NewMockRenderer(controller)
	expectedErr := io.ErrUnexpectedEOF
	gomock.InOrder(
		source.EXPECT().Read(gomock.Any()).Return(snapshot, nil),
		renderer.EXPECT().Load(gomock.Any(), snapshot.Root).
			Return([]domain.Project{desiredProject("first")}, nil),
		deployer.EXPECT().Apply(gomock.Any(), cfg.Deployment, gomock.Any()).Return(nil),
		source.EXPECT().Read(gomock.Any()).Return(domain.Snapshot{}, expectedErr),
	)

	runner := newRunner(cfg, deployer,
		reconcile.Input{Source: source, Renderer: renderer, State: emptyState(controller)},
	)
	if _, err := runner.Step(t.Context(), "run"); err != nil {
		t.Fatal(err)
	}

	report, err := runner.Step(t.Context(), "run")
	if !errors.Is(err, expectedErr) || report.Commit != "" || len(report.Projects) != 0 {
		t.Fatalf("failed fetch retained stale result: report=%+v err=%v", report, err)
	}
}

func TestReadOnly(t *testing.T) {
	t.Parallel()

	cases := []readOnlyCase{
		{name: "validate without docker", action: "validate", expected: "validate"},
		{name: "plan without docker", action: "plan", expected: "apply"},
		{
			name: "validate invalid template", action: "validate", expected: "validate",
			invalid: true, expectedErr: domain.ErrInvalid,
		},
		{
			name: "plan invalid template", action: "plan", expected: "failed",
			invalid: true, expectedErr: domain.ErrInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			testReadOnly(t, tc)
		})
	}
}

func TestSourceContext(t *testing.T) {
	t.Parallel()

	cases := []sourceContextCase{
		{name: "git timeout", expectedErr: context.DeadlineExceeded},
		{name: "parent cancellation", cancel: true, expectedErr: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testSourceContext(t, tc)
		})
	}
}

func TestDeployerContext(t *testing.T) {
	t.Parallel()

	cfg, snapshot := fixture(t, "first")
	controller := gomock.NewController(t)
	source := NewMockSource(controller)
	deployer := NewMockDeployer(controller)
	renderer := NewMockRenderer(controller)

	source.EXPECT().Read(gomock.Any()).Return(snapshot, nil)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	renderer.EXPECT().Load(ctx, snapshot.Root).Return([]domain.Project{desiredProject("first")}, nil)
	deployer.EXPECT().Apply(ctx, cfg.Deployment, gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ deployment.Options, _ []domain.Change) error {
			cancel()

			return ctx.Err()
		},
	)

	runner := newRunner(cfg, deployer,
		reconcile.Input{Source: source, Renderer: renderer, State: emptyState(controller)},
	)
	if _, err := runner.Step(ctx, "apply"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func fixture(t *testing.T, commit string) (reconcile.Options, domain.Snapshot) {
	t.Helper()

	cfg := reconcile.Options{GitTimeout: time.Minute}

	return cfg, domain.Snapshot{Commit: commit, Root: "snapshots/" + commit}
}

type snapshotCase struct {
	name, action, expected string
}

func testSnapshotLifecycle(t *testing.T, tc snapshotCase) {
	t.Helper()

	cfg, first := fixture(t, "first")
	_, second := fixture(t, "second")
	controller := gomock.NewController(t)
	source := NewMockSource(controller)
	deployer := NewMockDeployer(controller)
	renderer := NewMockRenderer(controller)

	source.EXPECT().Read(gomock.Any()).Return(first, nil)

	if tc.action == "run" {
		source.EXPECT().Read(gomock.Any()).Return(second, nil)
	}

	renderer.EXPECT().Load(gomock.Any(), first.Root).Return([]domain.Project{desiredProject("first")}, nil)
	renderer.EXPECT().Load(gomock.Any(), "snapshots/"+tc.expected).
		Return([]domain.Project{desiredProject(tc.expected)}, nil)
	deployer.EXPECT().Apply(gomock.Any(), cfg.Deployment, gomock.Any()).Return(io.ErrUnexpectedEOF)
	deployer.EXPECT().Apply(gomock.Any(), cfg.Deployment, gomock.Any()).Return(nil)
	runner := newRunner(cfg, deployer,
		reconcile.Input{Source: source, Renderer: renderer, State: emptyState(controller)},
	)

	step(t, runner, tc.action, io.ErrUnexpectedEOF)
	report := step(t, runner, tc.action, nil)

	if singleProject(t, report, tc.expected).Hash != tc.expected {
		t.Fatalf("retry used wrong snapshot: %+v", report)
	}
}

type readOnlyCase struct {
	name, action, expected string
	invalid                bool
	expectedErr            error
}

func testReadOnly(t *testing.T, tc readOnlyCase) {
	t.Helper()

	cfg, snapshot := fixture(t, "first")

	project := desiredProject("first")
	if tc.invalid {
		project.Error = "invalid template"
	}

	controller := gomock.NewController(t)
	source := NewMockSource(controller)
	renderer := NewMockRenderer(controller)

	source.EXPECT().Read(gomock.Any()).Return(snapshot, nil)
	renderer.EXPECT().Load(gomock.Any(), snapshot.Root).Return([]domain.Project{project}, nil)

	report := step(t, newRunner(cfg, nil,
		reconcile.Input{Source: source, Renderer: renderer, State: emptyState(controller)},
	), tc.action, tc.expectedErr)

	if singleProject(t, report, snapshot.Commit).Action != tc.expected {
		t.Fatalf("project lost or wrong action: %+v", report)
	}

	if tc.invalid && report.Projects[0].Error == "" {
		t.Fatal("template error lost from report")
	}
}

type sourceContextCase struct {
	name        string
	cancel      bool
	expectedErr error
}

func testSourceContext(t *testing.T, tc sourceContextCase) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		cfg := reconcile.Options{GitTimeout: time.Second}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		if tc.cancel {
			cancel()
		}

		source := NewMockSource(gomock.NewController(t))
		source.EXPECT().Read(gomock.Any()).DoAndReturn(func(ctx context.Context) (domain.Snapshot, error) {
			<-ctx.Done()

			return domain.Snapshot{}, ctx.Err()
		})

		_, err := newRunner(cfg, nil, reconcile.Input{Source: source}).Step(ctx, "apply")
		if !errors.Is(err, tc.expectedErr) {
			t.Fatalf("expected %v, got %v", tc.expectedErr, err)
		}
	})
}

func singleProject(t *testing.T, report domain.Report, commit string) domain.Change {
	t.Helper()

	if report.Commit != commit || len(report.Projects) != 1 {
		t.Fatalf("expected commit %s and one project, got %+v", commit, report)
	}

	return report.Projects[0]
}

func desiredProject(hash string) domain.Project {
	return domain.Project{
		Name: "worker", Environment: "prod", Enabled: true, Hash: hash,
	}
}

func TestRendererFailureStopsDeployment(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"validate", "plan", "apply", "run"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			cfg, snapshot := fixture(t, "first")
			controller := gomock.NewController(t)
			source := NewMockSource(controller)
			renderer := NewMockRenderer(controller)
			deployer := NewMockDeployer(controller)

			source.EXPECT().Read(gomock.Any()).Return(snapshot, nil)
			renderer.EXPECT().Load(t.Context(), snapshot.Root).Return(nil, io.ErrUnexpectedEOF)

			report, err := newRunner(cfg, deployer,
				reconcile.Input{Source: source, Renderer: renderer, State: emptyState(controller)},
			).Step(t.Context(), action)
			if !errors.Is(err, io.ErrUnexpectedEOF) || report.Commit != snapshot.Commit {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		})
	}
}

func TestDeploymentOutcomeReported(t *testing.T) {
	t.Parallel()

	cfg, snapshot := fixture(t, "first")
	controller := gomock.NewController(t)
	source := NewMockSource(controller)
	renderer := NewMockRenderer(controller)
	deployer := NewMockDeployer(controller)

	source.EXPECT().Read(gomock.Any()).Return(snapshot, nil)
	renderer.EXPECT().Load(gomock.Any(), snapshot.Root).
		Return([]domain.Project{desiredProject("first")}, nil)
	deployer.EXPECT().Apply(gomock.Any(), cfg.Deployment, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ deployment.Options, changes []domain.Change) error {
			changes[0].Action = "failed"
			changes[0].Error = "start failed"

			return io.ErrUnexpectedEOF
		},
	)

	report, err := newRunner(cfg, deployer,
		reconcile.Input{Source: source, Renderer: renderer, State: emptyState(controller)},
	).Step(t.Context(), "apply")
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected deployment failure, got %v", err)
	}

	change := singleProject(t, report, snapshot.Commit)
	if change.Action != "failed" || change.Error != "start failed" {
		t.Fatalf("deployment outcome lost: %+v", change)
	}
}

func step(t *testing.T, runner *reconcile.Runner, action string, expectedErr error) domain.Report {
	t.Helper()

	report, err := runner.Step(t.Context(), action)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}

	return report
}

func emptyState(controller *gomock.Controller) *MockInventory {
	state := NewMockInventory(controller)
	state.EXPECT().List().Return(nil, nil).AnyTimes()
	state.EXPECT().Applied(gomock.Any()).Return("", nil).AnyTimes()

	return state
}

//nolint:maintidx // Keep independent source/render failure expectations with their assertion.
func TestEnvironmentFailureIsolation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		renderError bool
	}{
		{name: "missing dev branch"},
		{name: "invalid stage tree", renderError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			cfg, snapshot := fixture(t, "master-commit")
			prodSource, stageSource := NewMockSource(controller), NewMockSource(controller)
			prodRenderer, stageRenderer := NewMockRenderer(controller), NewMockRenderer(controller)
			stageState := NewMockInventory(controller) // No listing: failure must never imply removals.

			prodSource.EXPECT().Read(gomock.Any()).Return(snapshot, nil)
			prodRenderer.EXPECT().Load(gomock.Any(), snapshot.Root).Return([]domain.Project{desiredProject("prod")}, nil)

			if tc.renderError {
				stageSource.EXPECT().Read(gomock.Any()).Return(domain.Snapshot{Root: "dev", Commit: "dev-commit"}, nil)
				stageRenderer.EXPECT().Load(gomock.Any(), "dev").Return(nil, io.ErrUnexpectedEOF)
			} else {
				stageSource.EXPECT().Read(gomock.Any()).Return(domain.Snapshot{}, io.ErrUnexpectedEOF)
			}

			deployer := NewMockDeployer(controller)
			expected := []domain.Change{{Project: desiredProject("prod"), Action: "apply"}}
			deployer.EXPECT().Apply(gomock.Any(), cfg.Deployment, expected).Return(nil)
			runner := newRunner(cfg, deployer,
				reconcile.Input{Environment: "prod", Source: prodSource, Renderer: prodRenderer, State: emptyState(controller)},
				reconcile.Input{Environment: "stage", Source: stageSource, Renderer: stageRenderer, State: stageState},
			)

			report := step(t, runner, "run", io.ErrUnexpectedEOF)
			if len(report.Projects) != 1 || report.Commits["prod"] != "master-commit" {
				t.Fatalf("healthy environment was lost: %+v", report)
			}
		})
	}
}

func newRunner(cfg reconcile.Options, deployer reconcile.Deployer, inputs ...reconcile.Input) *reconcile.Runner {
	return reconcile.New(reconcile.Settings{Options: cfg, Inputs: inputs}, deployer, nil, nil)
}

//nolint:maintidx // Keep the accepted/rejected reload wiring and assertions together.
func TestReloadConfiguration(t *testing.T) {
	t.Parallel()

	for _, reloadErr := range []error{nil, io.ErrUnexpectedEOF} {
		t.Run(fmt.Sprint(reloadErr), func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			cfg, snapshot := fixture(t, "current")
			configuration := NewMockConfiguration(controller)
			oldSource, newSource := NewMockSource(controller), NewMockSource(controller)
			deployer, renderer := NewMockDeployer(controller), NewMockRenderer(controller)
			input := reconcile.Input{Environment: "prod", Source: oldSource, Renderer: renderer, State: emptyState(controller)}
			initial := reconcile.Settings{Options: cfg, Inputs: []reconcile.Input{input}}
			input.Source = newSource
			configuration.EXPECT().Load().Return(reconcile.Settings{Options: cfg, Inputs: []reconcile.Input{input}}, reloadErr)

			source := newSource
			if reloadErr != nil {
				source = oldSource
			}

			source.EXPECT().Read(gomock.Any()).Return(snapshot, nil)
			renderer.EXPECT().Load(gomock.Any(), snapshot.Root).Return([]domain.Project{desiredProject("prod")}, nil)
			deployer.EXPECT().Apply(gomock.Any(), cfg.Deployment, gomock.Any()).Return(nil)

			_, err := reconcile.New(initial, deployer, configuration, nil).Step(t.Context(), "run")
			if !errors.Is(err, reloadErr) {
				t.Fatalf("reload: got %v, want %v", err, reloadErr)
			}
		})
	}
}

//nolint:gocognit,maintidx // Compare configured failures and explicit removals in one table.
func TestRemovedEnvironments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                  string
		configured, attempted bool
		want                  int
	}{
		{name: "removed environment", attempted: true, want: 1},
		{name: "rendered only"},
		{name: "unavailable configured branch", configured: true, attempted: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			cfg, _ := fixture(t, "current")
			inventory := NewMockInventory(controller)
			project := domain.Project{Name: "dan-app", Environment: "dan"}
			inventory.EXPECT().List().Return([]domain.Project{project}, nil)

			settings := reconcile.Settings{Options: cfg}

			if tc.configured {
				source := NewMockSource(controller)
				source.EXPECT().Read(gomock.Any()).Return(domain.Snapshot{}, io.ErrUnexpectedEOF)
				settings.Inputs = []reconcile.Input{{Environment: "dan", Source: source}}
			} else {
				inventory.EXPECT().Attempted(project).Return(tc.attempted, nil)
			}

			report, err := reconcile.New(settings, nil, nil, inventory).Step(t.Context(), "plan")
			if (err != nil) != tc.configured || len(report.Projects) != tc.want {
				t.Fatalf("removed: report=%+v, error=%v", report, err)
			}

			if tc.want == 1 && report.Projects[0].Action != "remove" {
				t.Fatalf("unexpected removal: %+v", report.Projects)
			}
		})
	}
}
