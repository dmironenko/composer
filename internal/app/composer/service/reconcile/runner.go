// Package reconcile runs a GitOps pass independently of the CLI and external clients.
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
	"github.com/dmironenko/composer/v2/internal/app/composer/service/deployment"
)

const (
	actionRun      = "run"
	actionValidate = "validate"
)

// Options contains timeouts and deployment policy for a reconciliation pass.
type Options struct {
	GitTimeout time.Duration
	Deployment deployment.Options
}

// Settings is the validated configuration of a reconciliation pass.
type Settings struct {
	Options Options
	Inputs  []Input
}

// Input binds one environment's source, renderer and scoped deployment inventory.
// Its snapshot is retained across apply retries, but refreshed on every run pass.
type Input struct {
	Environment string
	Source      Source
	Renderer    Renderer
	State       deployment.Inventory
	snapshot    domain.Snapshot
}

// Runner prepares all inputs before applying projects under one network barrier.
// A runner is used sequentially under the installation's process lock.
type Runner struct {
	cfg           Options
	inputs        []Input
	deployer      Deployer
	configuration Configuration
	inventory     deployment.Inventory
}

var errDeployer = errors.New("deployer is required for apply and run")

// New injects initial settings, deployment operations and optional live configuration/inventory.
// A nil deployer permits read-only commands; inventory spans all historical environments.
func New(settings Settings, deployer Deployer, configuration Configuration, inventory deployment.Inventory) *Runner {
	return &Runner{
		cfg: settings.Options, inputs: slices.Clone(settings.Inputs), deployer: deployer,
		configuration: configuration, inventory: inventory,
	}
}

// Step reads and plans environments independently, then applies their combined changes.
// A failed source or renderer never contributes deletions for that environment.
func (r *Runner) Step(ctx context.Context, action string) (domain.Report, error) {
	configErr := r.reload(action)

	report, failures := r.prepare(ctx, action)
	failures = errors.Join(configErr, failures)

	switch action {
	case actionValidate, "plan":
		return report, errors.Join(failures, reportError(report))
	}

	if failures != nil && len(report.Projects) == 0 {
		return report, failures
	}

	if r.deployer == nil {
		return report, errors.Join(failures, errDeployer)
	}

	if err := r.deployer.Apply(ctx, r.cfg.Deployment, report.Projects); err != nil {
		return report, errors.Join(failures, fmt.Errorf("apply snapshots: %w", err))
	}

	return report, failures
}

// Run emits each pass and retries readiness failures or polls Git until cancellation.
// The caller owns the overall apply deadline and the output destination.
func (r *Runner) Run(ctx context.Context, action string, wait bool, emit func(domain.Report, error) error) error {
	for {
		report, err := r.Step(ctx, action)
		if err != nil {
			err = fmt.Errorf("run GitOps pass: %w", err)
		}

		if emitErr := emit(report, err); emitErr != nil {
			return emitErr
		}

		if !shouldRetry(action, wait, err) {
			return err
		}

		if pause(ctx, r.cfg.Deployment.PollInterval) != nil {
			break
		}
	}

	if action == actionRun {
		return nil
	}

	return fmt.Errorf("apply deadline: %w", ctx.Err())
}

func (r *Runner) prepare(ctx context.Context, action string) (domain.Report, error) {
	report := domain.Report{Commits: make(map[string]string)}

	var failures error

	for i := range r.inputs {
		input := &r.inputs[i]

		changes, err := input.prepare(ctx, r.cfg.GitTimeout, action)
		if err != nil {
			failures = errors.Join(failures, fmt.Errorf("environment %q: %w", input.Environment, err))
		}

		report.Commits[input.Environment] = input.snapshot.Commit
		report.Projects = append(report.Projects, changes...)
	}

	report.Commit = sharedCommit(r.inputs)

	if action == actionValidate {
		return report, failures
	}

	removed, err := r.removed()
	report.Projects = append(report.Projects, removed...)

	return report, errors.Join(failures, err)
}

func (r *Runner) reload(action string) error {
	if action != actionRun || r.configuration == nil {
		return nil
	}

	settings, err := r.configuration.Load()
	if err != nil {
		return fmt.Errorf("reload configuration: %w", err)
	}

	r.cfg, r.inputs = settings.Options, settings.Inputs

	return nil
}

func (r *Runner) removed() ([]domain.Change, error) {
	if r.inventory == nil {
		return nil, nil
	}

	previous, err := r.inventory.List()
	if err != nil {
		return nil, fmt.Errorf("list removed environments: %w", err)
	}

	var changes []domain.Change

	for _, project := range previous {
		if slices.ContainsFunc(r.inputs, func(input Input) bool { return input.Environment == project.Environment }) {
			continue
		}

		attempted, err := r.inventory.Attempted(project)
		if err != nil {
			return nil, fmt.Errorf("read removed environment: %w", err)
		}

		if attempted {
			changes = append(changes, domain.Change{Project: project, Action: "remove"})
		}
	}

	return changes, nil
}

func (in *Input) prepare(ctx context.Context, timeout time.Duration, action string) ([]domain.Change, error) {
	if err := in.read(ctx, timeout, action == actionRun); err != nil {
		return nil, err
	}

	projects, err := in.Renderer.Load(ctx, in.snapshot.Root)
	if err != nil {
		return nil, fmt.Errorf("render snapshot: %w", err)
	}

	return plan(in.State, projects, action)
}

func (in *Input) read(ctx context.Context, timeout time.Duration, refresh bool) error {
	if !refresh && in.snapshot.Root != "" {
		return nil
	}

	gitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	snapshot, err := in.Source.Read(gitCtx)
	in.snapshot = snapshot

	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}

	return nil
}

func plan(state deployment.Inventory, projects []domain.Project, action string) ([]domain.Change, error) {
	if action == actionValidate {
		if len(projects) == 0 {
			return nil, nil
		}

		changes := make([]domain.Change, len(projects))
		for i, project := range projects {
			changes[i] = domain.Change{Project: project, Action: "validate"}
		}

		return changes, nil
	}

	changes, err := deployment.Plan(state, projects)
	if err != nil {
		return changes, fmt.Errorf("plan snapshot: %w", err)
	}

	return changes, nil
}

func reportError(report domain.Report) error {
	for _, project := range report.Projects {
		if project.Error != "" {
			return domain.ErrInvalid
		}
	}

	return nil
}

func pause(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for next attempt: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func shouldRetry(action string, wait bool, err error) bool {
	return action == actionRun || action == "apply" && wait && err != nil
}

// sharedCommit preserves the single-commit report when all inputs agree.
func sharedCommit(inputs []Input) string {
	if len(inputs) == 0 {
		return ""
	}

	commit := inputs[0].snapshot.Commit
	for _, input := range inputs[1:] {
		if input.snapshot.Commit != commit {
			return ""
		}
	}

	return commit
}
