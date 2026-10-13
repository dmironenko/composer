package build

//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -source=ports.go -destination=ports_mock_test.go -package=build_test

import (
	"context"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
)

// Backend owns Docker's image storage and build execution.
type Backend interface {
	HasImage(ctx context.Context, image string) (bool, error)
	Build(ctx context.Context, spec domain.BuildSpec) error
}
