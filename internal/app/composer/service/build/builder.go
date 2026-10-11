// Package build ensures source images exist in Docker before deployment.
package build

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/singleflight"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

// Builder prepares absent images and coalesces concurrent requests for the same image.
// Completed results are never cached: every later request consults Docker again.
type Builder struct {
	workspace Workspace
	source    Source
	backend   Backend
	flights   singleflight.Group
}

// New injects the Git and Docker operations needed to build application images.
func New(source Source, backend Backend, workspace Workspace) *Builder {
	return &Builder{source: source, backend: backend, workspace: workspace}
}

// Ensure returns only after the image operation and temporary directory cleanup finish.
// The first caller owns a shared build's context. Other callers await that build,
// including its cleanup, before reporting their own cancellation or its result.
func (b *Builder) Ensure(ctx context.Context, spec domain.BuildSpec) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("ensure image: %w", err)
	}

	if !spec.ValidCommit() {
		return fmt.Errorf("build commit must be a full lowercase Git SHA-1: %w", domain.ErrInvalid)
	}

	_, err, _ := b.flights.Do(spec.Image(), func() (any, error) {
		return nil, b.ensure(ctx, spec)
	})

	if ctx.Err() != nil {
		return fmt.Errorf("ensure image: %w", ctx.Err())
	}

	if err != nil {
		return fmt.Errorf("ensure image: %w", err)
	}

	return nil
}

func (b *Builder) ensure(ctx context.Context, spec domain.BuildSpec) (err error) {
	exists, err := b.backend.HasImage(ctx, spec.Image())
	if err != nil {
		return fmt.Errorf("inspect image: %w", err)
	}

	if exists {
		return nil
	}

	directory, err := b.workspace.CreateBuildContext()
	if err != nil {
		return fmt.Errorf("create build context: %w", err)
	}

	defer func() { err = errors.Join(err, b.workspace.RemoveBuildContext(directory)) }()

	if err = b.source.Checkout(ctx, spec.Repository, spec.Commit, directory); err != nil {
		return fmt.Errorf("checkout image source: %w", err)
	}

	if err = b.backend.Build(ctx, spec, directory); err != nil {
		return fmt.Errorf("build image: %w", err)
	}

	return nil
}
