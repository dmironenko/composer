package cli

import (
	"context"
	"io"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

// Runner is the application entrypoint used by the CLI.
type Runner interface {
	Run(ctx context.Context, action string, wait bool, emit func(domain.Report, error) error) error
}

// Session owns the application and its process resources for one command.
type Session struct {
	Runner Runner
	Close  func() error
}

// HostInfo is the public result of checking a host configuration.
type HostInfo struct {
	Installation string
	Branches     map[string]string
}

// Bootstrap supplies process setup from cmd/composer. The port never constructs adapters.
//
//nolint:interfacebloat // Process bootstrap supplies the five independent CLI commands, not a business dependency.
type Bootstrap interface {
	Open(configPath, action string) (Session, error)
	Init(target, configPath, branch, installation string) error
	Check(configPath string) (HostInfo, error)
	Install(ctx context.Context, configPath string, output io.Writer) (string, error)
	Export(ctx context.Context, output string) error
}
