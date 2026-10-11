// Package deployment plans and applies changed Compose projects after preparing shared networks.
package deployment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

// Options contains the deployment policy for one installation.
type Options struct {
	Installation  string
	Parallelism   int
	DeployTimeout time.Duration
	PollInterval  time.Duration
}

var errOperation = errors.New("compose operation failed")

// Executor owns network ordering, parallelism and deployment markers.
// Client construction and cleanup belong to the caller.
type Executor struct {
	backend Backend
	builder Builder
	state   Journal
}

// New injects the platform operations used to apply projects.
func New(backend Backend, builder Builder, state Journal) *Executor {
	return &Executor{backend: backend, builder: builder, state: state}
}

// Apply uses bounded independent workers; one failure does not cancel neighbours.
func (e *Executor) Apply(ctx context.Context, cfg Options, changes []domain.Change) error {
	networks := e.prepareNetworks(ctx, cfg, changes)

	var group errgroup.Group
	group.SetLimit(cfg.Parallelism)

	for i := range changes {
		networkErr := networks[changes[i].Network]
		for _, name := range changes[i].Networks {
			networkErr = errors.Join(networkErr, networks[name])
		}

		group.Go(func() error { return e.applyChange(ctx, cfg, &changes[i], networkErr) })
	}

	if err := group.Wait(); err != nil {
		return fmt.Errorf("deploy projects: %w", err)
	}

	return nil
}

func (e *Executor) prepareNetworks(
	ctx context.Context, cfg Options, changes []domain.Change,
) map[string]error {
	networks := map[string]error{}

	for _, change := range changes {
		if !change.Enabled || change.Error != "" {
			continue
		}

		for _, name := range append([]string{change.Network}, change.Networks...) {
			if _, exists := networks[name]; exists {
				continue
			}

			networkCtx, cancel := context.WithTimeout(ctx, cfg.DeployTimeout)
			networks[name] = e.backend.EnsureNetwork(networkCtx, name, cfg.Installation)

			cancel()
		}
	}

	return networks
}

func (e *Executor) applyChange(
	ctx context.Context, cfg Options, change *domain.Change, networkErr error,
) error {
	if change.Error != "" {
		return fmt.Errorf("%s: %s: %w", change.Name, change.Error, errOperation)
	}

	err := networkErr
	if err == nil && change.Action != "noop" {
		jobCtx, cancel := context.WithTimeout(ctx, cfg.DeployTimeout)
		defer cancel()

		err = e.apply(jobCtx, *change, cfg)
	}

	if err == nil {
		return nil
	}
	// SDK errors can contain rendered values. Report only the project and operation.
	change.Error, change.Action = errOperation.Error(), "failed"
	if ctx.Err() != nil {
		change.Error = ctx.Err().Error()
	}

	return fmt.Errorf("%s: %s: %w", change.Name, change.Error, errOperation)
}

func (e *Executor) apply(ctx context.Context, change domain.Change, cfg Options) error {
	if err := e.prepareImage(ctx, change); err != nil {
		return err
	}

	// Compose may change containers before failing. Invalidate success before Up/Down,
	// so a Git revert cannot be mistaken for an unchanged project.
	if err := e.state.Invalidate(change.Project); err != nil {
		return fmt.Errorf("invalidate deployment: %w", err)
	}

	if change.Action == "remove" {
		if err := e.backend.Down(ctx, change.Name); err != nil {
			return fmt.Errorf("remove compose project: %w", err)
		}

		if err := e.state.Remove(change.Project); err != nil {
			return fmt.Errorf("remove deployment files: %w", err)
		}

		return nil
	}

	return e.up(ctx, change, cfg)
}

func (e *Executor) up(ctx context.Context, change domain.Change, cfg Options) error {
	// Record possible partial creation before Up, so later deletion also cleans it up.
	if err := e.state.Attempt(change.Project); err != nil {
		return fmt.Errorf("record deployment attempt: %w", err)
	}

	// Compose readiness polling needs at least one second per attempt.
	err := e.backend.Up(ctx, change.Project, max(time.Second, cfg.PollInterval))
	if err != nil {
		return fmt.Errorf("start compose project: %w", err)
	}

	if err = e.state.Save(change.Project); err != nil {
		return fmt.Errorf("save deployment: %w", err)
	}

	return nil
}

func (e *Executor) prepareImage(ctx context.Context, change domain.Change) error {
	if change.Action != "apply" || change.Build == nil {
		return nil
	}

	if e.builder == nil {
		return errOperation
	}

	if err := e.builder.Ensure(ctx, *change.Build); err != nil {
		return fmt.Errorf("prepare source image: %w", err)
	}

	return nil
}
