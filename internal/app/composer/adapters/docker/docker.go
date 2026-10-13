// Package docker adapts Docker Engine and Compose SDK operations for deployment.
package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/errdefs"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/moby/moby/client"

	"github.com/dmironenko/composer/v2/internal/app/composer/domain"
)

// Options selects a Docker daemon by endpoint or named context.
type Options struct {
	Host    string
	Context string
}

// Client owns one Docker connection and creates independent Compose operations.
type Client struct {
	cli    *command.DockerCli
	engine client.APIClient
}

// New configures Docker access; the caller owns Close.
func New(cfg Options) (*Client, error) {
	cli, err := command.NewDockerCli(command.WithOutputStream(io.Discard), command.WithErrorStream(io.Discard))
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	options := &flags.ClientOptions{Context: cfg.Context}
	if cfg.Host != "" {
		options.Hosts = []string{cfg.Host}
	}

	if err = cli.Initialize(options); err != nil {
		return nil, fmt.Errorf("initialize docker client: %w", err)
	}

	engine, err := command.NewAPIClientFromFlags(options, cli.ConfigFile())
	if err != nil {
		return nil, fmt.Errorf("create docker engine: %w", err)
	}

	if err = command.WithAPIClient(engine)(cli); err != nil {
		return nil, errors.Join(fmt.Errorf("attach docker engine: %w", err), engine.Close())
	}

	return &Client{cli: cli, engine: engine}, nil
}

// Close releases the shared Docker connection after all operations finish.
func (c *Client) Close() error {
	if err := c.engine.Close(); err != nil {
		return fmt.Errorf("close docker client: %w", err)
	}

	return nil
}

// EnsureNetwork creates a missing managed network.
func (c *Client) EnsureNetwork(ctx context.Context, name, installation string) error {
	_, err := c.engine.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	if err == nil {
		return nil
	}

	if !errdefs.IsNotFound(err) {
		return fmt.Errorf("inspect network: %w", err)
	}

	_, err = c.engine.NetworkCreate(ctx, name, client.NetworkCreateOptions{
		Labels: map[string]string{"io.composer.installation": installation},
	})
	if err != nil {
		return fmt.Errorf("create network: %w", err)
	}

	return nil
}

// Up applies a project and waits for Compose readiness.
func (c *Client) Up(ctx context.Context, deployment domain.Project, waitTimeout time.Duration) error {
	project, err := loadProject(ctx, deployment)
	if err != nil {
		return err
	}

	// compose mutates shared network settings when creating replicas.
	// serialize sdk operations within this app; other apps still deploy concurrently.
	sdk, err := compose.NewComposeService(c.cli,
		compose.WithStreams(io.Discard, io.Discard, nil), compose.WithMaxConcurrency(1))
	if err != nil {
		return fmt.Errorf("create compose client: %w", err)
	}

	err = sdk.Up(ctx, project, api.UpOptions{
		Create: api.CreateOptions{RemoveOrphans: true, QuietPull: true},
		Start:  api.StartOptions{Project: project, Wait: true, WaitTimeout: waitTimeout},
	})
	if err != nil {
		return fmt.Errorf("start compose project: %w", err)
	}

	return nil
}

// Down removes project containers while retaining images, volumes and external networks.
func (c *Client) Down(ctx context.Context, name string) error {
	sdk, err := compose.NewComposeService(c.cli, compose.WithStreams(io.Discard, io.Discard, nil))
	if err != nil {
		return fmt.Errorf("create compose client: %w", err)
	}

	if err = sdk.Down(ctx, name, api.DownOptions{RemoveOrphans: true}); err != nil {
		return fmt.Errorf("remove compose project: %w", err)
	}

	return nil
}

// loadProject restores Compose SDK metadata that is absent from the portable manifest.
func loadProject(ctx context.Context, deployment domain.Project) (*types.Project, error) {
	project, err := loader.LoadWithContext(ctx, types.ConfigDetails{
		WorkingDir: deployment.Manifest.WorkingDir, Environment: map[string]string{},
		ConfigFiles: []types.ConfigFile{{Filename: "compose.yaml", Content: deployment.Manifest.Content}},
	}, func(options *loader.Options) {
		options.SetProjectName(deployment.Name, true)
		options.SkipInterpolation = true
	})
	if err != nil {
		return nil, fmt.Errorf("%w: compose validation failed", domain.ErrInvalid)
	}

	for name, service := range project.Services {
		service.Labels = service.Labels.Add("io.composer.config", deployment.Manifest.ConfigHash)
		service.CustomLabels = types.Labels{
			api.ProjectLabel: project.Name, api.ServiceLabel: name,
			api.VersionLabel: api.ComposeVersion, api.WorkingDirLabel: deployment.Manifest.WorkingDir, api.OneoffLabel: "False",
		}
		project.Services[name] = service
	}

	return project, nil
}
