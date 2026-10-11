package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"go.yaml.in/yaml/v3"
)

const (
	localMode  = "local"
	remoteMode = "remote"
)

var errInvalidConfig = errors.New("invalid configuration")

var environmentPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

var installationPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

type hostConfig struct {
	Version      int             `yaml:"version"`
	Installation string          `yaml:"installation"`
	Source       sourceConfig    `yaml:"source"`
	StateDir     string          `yaml:"state_dir"`
	AgeKeyFile   string          `yaml:"age_key_file,omitempty"`
	Docker       dockerConfig    `yaml:"docker"`
	Reconcile    reconcileConfig `yaml:"reconcile"`
}

type sourceConfig struct {
	Mode         string            `yaml:"mode"`
	Path         string            `yaml:"path,omitempty"`
	URL          string            `yaml:"url,omitempty"`
	Branches     map[string]string `yaml:"branches,omitempty"`
	PollInterval time.Duration     `yaml:"poll_interval"`
	Auth         authConfig        `yaml:"auth,omitempty"`
}

type authConfig struct {
	Type           string `yaml:"type,omitempty"`
	KeyFile        string `yaml:"key_file,omitempty"`
	KnownHostsFile string `yaml:"known_hosts_file,omitempty"`
	TokenFile      string `yaml:"token_file,omitempty"`
}

type dockerConfig struct {
	Host    string `yaml:"host,omitempty"`
	Context string `yaml:"context,omitempty"`
}

type reconcileConfig struct {
	Parallelism   int           `yaml:"parallelism"`
	GitTimeout    time.Duration `yaml:"git_timeout"`
	DeployTimeout time.Duration `yaml:"deploy_timeout"`
}

// defaultConfig returns the shared host defaults used by loading and scaffolding.
func defaultConfig() hostConfig {
	const (
		pollInterval  = 10 * time.Second
		parallelism   = 4
		gitTimeout    = 30 * time.Second
		deployTimeout = 5 * time.Minute
	)

	return hostConfig{
		Source: sourceConfig{PollInterval: pollInterval},
		Reconcile: reconcileConfig{
			Parallelism: parallelism,
			GitTimeout:  gitTimeout, DeployTimeout: deployTimeout,
		},
	}
}

// loadConfig resolves paths relative to the config file, never the caller's cwd.
func loadConfig(path string) (hostConfig, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return hostConfig{}, fmt.Errorf("resolve config path: %w", err)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return hostConfig{}, fmt.Errorf("read config: %w", err)
	}

	cfg := defaultConfig()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	if err = decoder.Decode(&cfg); err != nil {
		return hostConfig{}, fmt.Errorf("%w: decode host yaml", errInvalidConfig)
	}

	var extra yaml.Node
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return hostConfig{}, fmt.Errorf("%w: expected one yaml document", errInvalidConfig)
	}

	if err = cfg.Validate(); err != nil {
		return hostConfig{}, err
	}

	if err = cfg.resolvePaths(filepath.Dir(abs)); err != nil {
		return hostConfig{}, err
	}

	return cfg, nil
}

func (c hostConfig) Validate() error {
	if c.Version != 1 {
		return invalid("version must be 1")
	}

	if !installationPattern.MatchString(c.Installation) {
		return invalid("installation must be a lowercase docker-compatible name")
	}

	if c.StateDir == "" {
		return invalid("state_dir is required")
	}

	if c.Docker.Host != "" && c.Docker.Context != "" {
		return invalid("docker.host and docker.context are mutually exclusive")
	}

	return errors.Join(
		c.Reconcile.validate(), c.Source.validate(),
	)
}

func (c *hostConfig) resolvePaths(base string) error {
	for _, field := range []*string{
		&c.StateDir, &c.AgeKeyFile, &c.Source.Path, &c.Source.Auth.KeyFile,
		&c.Source.Auth.KnownHostsFile, &c.Source.Auth.TokenFile,
	} {
		if *field != "" && !filepath.IsAbs(*field) {
			*field = filepath.Join(base, *field)
		}
	}

	if c.Source.Mode != localMode {
		return nil
	}

	return outsideSource(c.Source.Path, c.StateDir)
}

func outsideSource(source, state string) error {
	rel, err := filepath.Rel(source, state)
	if err != nil || filepath.IsLocal(rel) {
		return invalid("state_dir must be outside the source repo")
	}

	return nil
}

func (r reconcileConfig) validate() error {
	if r.Parallelism < 1 || r.GitTimeout <= 0 || r.DeployTimeout <= 0 {
		return invalid("concurrency limits and durations must be positive")
	}

	return nil
}

func (s sourceConfig) validate() error {
	if s.PollInterval <= 0 {
		return invalid("poll interval must be positive")
	}

	if s.Mode == localMode && !slices.Contains([]string{"", "none"}, s.Auth.Type) {
		return invalid("local source does not support authentication")
	}

	return errors.Join(s.validateLocation(), s.validateBranches(), s.Auth.validate())
}

func (s sourceConfig) validateLocation() error {
	var location, unused string

	switch s.Mode {
	case localMode:
		location, unused = s.Path, s.URL
	case remoteMode:
		location, unused = s.URL, s.Path
	default:
		return invalid("source.mode must be local or remote")
	}

	if location == "" || unused != "" {
		return invalid("source requires path for local mode or url for remote mode")
	}

	return nil
}

func (a authConfig) validate() error {
	provided := [3]bool{a.KeyFile != "", a.KnownHostsFile != "", a.TokenFile != ""}

	var expected [3]bool

	switch a.Type {
	case "", "none":
	case "ssh":
		expected = [3]bool{true, true, false}
	case "https":
		expected = [3]bool{false, false, true}
	default:
		return invalid("unsupported auth type")
	}

	if provided != expected {
		return invalid("credentials do not match auth type")
	}

	return nil
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", errInvalidConfig, message)
}

func (c hostConfig) environments() []string {
	return slices.Sorted(maps.Keys(c.Source.Branches))
}

func (s sourceConfig) validateBranches() error {
	if len(s.Branches) == 0 {
		return invalid("source.branches must contain at least one environment")
	}

	for env, branch := range s.Branches {
		if !environmentPattern.MatchString(env) || plumbing.NewBranchReferenceName(branch).Validate() != nil {
			return invalid("source.branches requires lowercase environment names and valid branch names")
		}
	}

	return nil
}
