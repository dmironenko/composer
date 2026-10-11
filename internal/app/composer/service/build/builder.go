// Package build ensures source images exist in Docker before deployment.
package build

import (
	"context"
	"fmt"

	"golang.org/x/sync/singleflight"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

// Builder prepares absent images and coalesces concurrent requests for the same image.
// Completed results are never cached: every later request consults Docker again.
type Builder struct {
	backend Backend
	flights singleflight.Group
}

// New injects Docker operations needed to build application images.
func New(backend Backend) *Builder {
	return &Builder{backend: backend}
}

// Ensure returns only after the image operation finishes.
// The first caller owns a shared build's context. Other callers await that build
// before reporting their own cancellation or its result.
func (b *Builder) Ensure(ctx context.Context, spec domain.BuildSpec) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("ensure image: %w", err)
	}

	if !spec.ValidRef() {
		return fmt.Errorf("build ref must be nonempty: %w", domain.ErrInvalid)
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

func (b *Builder) ensure(ctx context.Context, spec domain.BuildSpec) error {
	exists, err := b.backend.HasImage(ctx, spec.Image())
	if err != nil {
		return fmt.Errorf("inspect image: %w", err)
	}

	if exists {
		return nil
	}

	if err = b.backend.Build(ctx, spec); err != nil {
		return fmt.Errorf("build image: %w", err)
	}

	return nil
}
