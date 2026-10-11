package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
	"github.com/spf13/cobra"

	"github.com/dmironenko/composer/internal/app/composer/adapters/docker"
	"github.com/dmironenko/composer/internal/app/composer/adapters/git"
	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops"
	"github.com/dmironenko/composer/internal/app/composer/adapters/state"
	"github.com/dmironenko/composer/internal/app/composer/adapters/systemd"
	"github.com/dmironenko/composer/internal/app/composer/ports/cli"
	"github.com/dmironenko/composer/internal/app/composer/service/build"
	"github.com/dmironenko/composer/internal/app/composer/service/deployment"
	"github.com/dmironenko/composer/internal/app/composer/service/reconcile"
)

const stateDirectoryMode = 0o700

var errLocked = errors.New("another composer process is using state_dir")

type bootstrap struct{}

func newCommand(version string, stdout, stderr io.Writer) *cobra.Command {
	return cli.NewCommand(version, stdout, stderr, bootstrap{})
}

func (bootstrap) Open(configPath, action string) (cli.Session, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return cli.Session{}, fmt.Errorf("load configuration: %w", err)
	}

	if err = os.MkdirAll(cfg.StateDir, stateDirectoryMode); err != nil {
		return cli.Session{}, fmt.Errorf("create state directory: %w", err)
	}

	lock := flock.New(filepath.Join(cfg.StateDir, "lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return cli.Session{}, fmt.Errorf("lock state directory: %w", err)
	}

	if !locked {
		return cli.Session{}, errLocked
	}

	runner, closeRuntime, err := openRuntime(cfg, configPath, action)
	if err != nil {
		return cli.Session{}, errors.Join(err, lock.Close())
	}

	return cli.Session{Runner: runner, Close: func() error {
		return errors.Join(closeRuntime(), lock.Close())
	}}, nil
}

func openRuntime(cfg hostConfig, configPath, action string) (*reconcile.Runner, func() error, error) {
	store := state.New(cfg.StateDir, nil)
	closeRuntime := func() error { return nil }

	var deployer reconcile.Deployer

	if action == "apply" || action == "run" {
		client, err := docker.New(docker.Options{Host: cfg.Docker.Host, Context: cfg.Docker.Context})
		if err != nil {
			return nil, nil, fmt.Errorf("initialize docker: %w", err)
		}

		closeRuntime = client.Close
		builder := build.New(client)
		deployer = deployment.New(client, builder, store)
	}

	return reconcile.New(cfg.settings(), deployer, runtimeConfig{path: configPath, initial: cfg}, store), closeRuntime, nil
}

func (cfg hostConfig) inputs() []reconcile.Input {
	names := cfg.environments()
	inputs := make([]reconcile.Input, 0, len(names))

	for _, env := range names {
		environments := []string{env}
		inputs = append(inputs, reconcile.Input{
			Environment: env,
			Source: git.New(git.SourceConfig{
				Mode: cfg.Source.Mode, Path: cfg.Source.Path, URL: cfg.Source.URL,
				Branch: cfg.Source.Branches[env], Auth: gitAuth(cfg.Source.Auth),
			}, cfg.StateDir),
			Renderer: gitops.New(gitops.Options{
				Installation: cfg.Installation, StateDir: cfg.StateDir,
				AgeKeyFile: cfg.AgeKeyFile, Environments: environments, AllEnvironments: names,
			}),
			State: state.New(cfg.StateDir, environments),
		})
	}

	return inputs
}

func gitAuth(auth authConfig) git.Auth {
	return git.Auth{Type: auth.Type, KeyFile: auth.KeyFile, KnownHostsFile: auth.KnownHostsFile, TokenFile: auth.TokenFile}
}

func (bootstrap) Init(target, configPath, branch, installation string) error {
	return createRepository(target, configPath, branch, installation)
}

func (bootstrap) Check(configPath string) (cli.HostInfo, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return cli.HostInfo{}, err
	}

	return cli.HostInfo{Installation: cfg.Installation, Branches: cfg.Source.Branches}, nil
}

func (bootstrap) Install(ctx context.Context, configPath string, output io.Writer) (string, error) {
	path, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("resolve config path: %w", err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		return "", fmt.Errorf("load configuration: %w", err)
	}

	unit, err := systemd.Install(ctx, cfg.Installation, path, output)
	if err != nil {
		return "", fmt.Errorf("activate systemd service: %w", err)
	}

	return unit, nil
}

func (c hostConfig) policy() reconcile.Options {
	return reconcile.Options{GitTimeout: c.Reconcile.GitTimeout, Deployment: deployment.Options{
		Installation: c.Installation, Parallelism: c.Reconcile.Parallelism,
		DeployTimeout: c.Reconcile.DeployTimeout, PollInterval: c.Source.PollInterval,
	}}
}

// runtimeConfig wires new inputs without replacing the locked installation or Docker client.
type runtimeConfig struct {
	path    string
	initial hostConfig
}

func (c runtimeConfig) Load() (reconcile.Settings, error) {
	cfg, err := loadConfig(c.path)
	if err != nil {
		return reconcile.Settings{}, fmt.Errorf("reload configuration: %w", err)
	}

	sameRuntime := cfg.Installation == c.initial.Installation && cfg.StateDir == c.initial.StateDir &&
		cfg.Docker == c.initial.Docker
	if !sameRuntime {
		return reconcile.Settings{}, invalid("installation, state_dir and docker changes require restart")
	}

	return cfg.settings(), nil
}

func (c hostConfig) settings() reconcile.Settings {
	return reconcile.Settings{Options: c.policy(), Inputs: c.inputs()}
}
