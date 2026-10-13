package reconcile

//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -source=ports.go -destination=ports_mock_test.go -package=reconcile_test

import (
	"context"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
	"github.com/dmironenko/composer/v2/internal/app/composer/service/deployment"
)

// Source supplies committed snapshots. Its caller serializes the state directory.
type Source interface {
	Read(ctx context.Context) (domain.Snapshot, error)
}

// Renderer loads desired projects, retaining invalid entries so errors never imply deletion.
type Renderer interface {
	Load(ctx context.Context, root string) ([]domain.Project, error)
}

// Deployer applies independent project changes after preparing their networks.
// Apply updates each change in place with its outcome for the report.
type Deployer interface {
	Apply(ctx context.Context, cfg deployment.Options, changes []domain.Change) error
}

// Configuration reloads validated inputs and policy; errors keep the last accepted settings.
type Configuration interface {
	Load() (Settings, error)
}
