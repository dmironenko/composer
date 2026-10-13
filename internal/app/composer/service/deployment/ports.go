package deployment

//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -source=ports.go -destination=ports_mock_test.go -package=deployment_test

import (
	"context"
	"time"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
)

// Inventory reads recorded deployment state for planning.
type Inventory interface {
	List() ([]domain.Project, error)
	Applied(project domain.Project) (string, error)
	Attempted(project domain.Project) (bool, error)
}

// Journal records the four transitions in a deployment lifecycle.
//
//nolint:interfacebloat // Invalidation, attempt, success and removal are distinct persistent transitions.
type Journal interface {
	Invalidate(project domain.Project) error
	Attempt(project domain.Project) error
	Save(project domain.Project) error
	Remove(project domain.Project) error
}

// Backend is the Docker boundary required by the deployment policy.
// Implementations must allow concurrent operations on independent projects.
type Backend interface {
	EnsureNetwork(ctx context.Context, name, installation string) error
	Up(ctx context.Context, project domain.Project, waitTimeout time.Duration) error
	Down(ctx context.Context, name string) error
}

// Builder ensures a source image exists before any running project is changed.
type Builder interface {
	Ensure(ctx context.Context, spec domain.BuildSpec) error
}
