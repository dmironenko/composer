package build

//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -source=ports.go -destination=ports_mock_test.go -package=build_test

import (
	"context"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

// Source checks out the exact application revision into a private directory.
type Source interface {
	Checkout(ctx context.Context, repository, commit, directory string) error
}

// Backend owns Docker's image storage and build execution.
type Backend interface {
	HasImage(ctx context.Context, image string) (bool, error)
	Build(ctx context.Context, spec domain.BuildSpec, contextDir string) error
}

// Workspace provides private temporary directories for source builds.
type Workspace interface {
	CreateBuildContext() (string, error)
	RemoveBuildContext(directory string) error
}
