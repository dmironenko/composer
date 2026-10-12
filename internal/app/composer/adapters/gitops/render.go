// Package gitops owns the GitOps layout, vars, templates and rendered config files.
package gitops

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops/internal/vars"
	"github.com/dmironenko/composer/internal/app/composer/domain"
)

const (
	configFileMode      = 0o444
	executableMask      = 0o111
	configDirMode       = 0o755
	privateDirMode      = 0o700
	identityPrefixLimit = 50
)

// Renderer translates committed GitOps files into deployable Compose projects.
type Renderer struct {
	options Options
}

// Options identifies the installation, output directory and environments to render.
type Options struct {
	Installation    string
	StateDir        string
	AgeKeyFile      string
	Environments    []string
	AllEnvironments []string
}

// New configures a GitOps renderer without reading repository files.
func New(options Options) *Renderer {
	return &Renderer{options: options}
}

// Load retains invalid directories in the result, so errors never imply deletion.
func (r *Renderer) Load(ctx context.Context, root string) ([]domain.Project, error) {
	cfg := r.options
	files := os.DirFS(root)

	folders, err := fs.Glob(files, "apps/*/*")
	if err != nil {
		return nil, fmt.Errorf("discover apps: %w", err)
	}

	var projects []domain.Project

	for _, folder := range folders {
		info, err := fs.Stat(files, folder)
		if err != nil {
			return nil, fmt.Errorf("stat app %s: %w", folder, err)
		}

		if !info.IsDir() {
			continue
		}

		for _, env := range cfg.Environments {
			project := loadProject(ctx, cfg, files, folder, env)
			projects = append(projects, project)
		}
	}

	if err := ctx.Err(); err != nil {
		return projects, fmt.Errorf("load projects: %w", err)
	}

	return projects, nil
}

func loadProject(ctx context.Context, cfg Options, files fs.FS, folder, env string) domain.Project {
	parts := strings.Split(folder, "/")
	name := identity(cfg.Installation, env, parts[1], parts[2])
	project := domain.Project{
		Name: name, Environment: env,
		Network:   identity(cfg.Installation, env, parts[1]),
		Directory: filepath.Join(cfg.StateDir, "deployments", env, name),
	}

	data := templateData{
		networks:     make(map[string]bool),
		Installation: cfg.Installation,
		Environments: cfg.AllEnvironments,
		Env:          env, Namespace: parts[1], App: parts[2],
		Runtime: runtimeData{ProjectName: name, NetworkName: project.Network},
	}
	if err := prepare(ctx, files, folder, cfg.AgeKeyFile, data, &project); err != nil {
		project.Error = err.Error()
	}

	project.Networks = slices.Sorted(maps.Keys(data.networks))

	return project
}

func prepare(
	ctx context.Context, files fs.FS, folder, ageKeyFile string, data templateData, project *domain.Project,
) error {
	if err := loadValues(files, folder, ageKeyFile, &data, project); err != nil {
		return err
	}

	if !project.Enabled {
		return nil
	}

	if err := prepareBuild(files, folder, &data, project); err != nil {
		return err
	}

	working, configHash, err := prepareConfigs(files, folder, data, project.Directory)
	if err != nil {
		return err
	}

	output, err := renderCompose(files, data)
	if err != nil {
		return err
	}

	return loadModel(ctx, working, configHash, output, project)
}

func renderCompose(files fs.FS, data templateData) ([]byte, error) {
	source, err := fs.ReadFile(files, "templates/compose.yaml.tmpl")
	if errors.Is(err, fs.ErrNotExist) {
		source = []byte(composeTemplate)
	} else if err != nil {
		return nil, fmt.Errorf("read compose template: %w", err)
	}

	return execute("compose.yaml.tmpl", string(source), data)
}

func loadModel(ctx context.Context, working, configHash string, output []byte, project *domain.Project) error {
	model, err := loader.LoadWithContext(ctx, types.ConfigDetails{
		WorkingDir: working, Environment: map[string]string{},
		ConfigFiles: []types.ConfigFile{{Filename: "compose.yaml", Content: output}},
	}, func(options *loader.Options) {
		options.SetProjectName(project.Name, true)
		// Values have already been rendered. A dollar sign in a secret is data.
		options.SkipInterpolation = true
	})
	if err != nil {
		return fmt.Errorf("%w: compose validation failed", domain.ErrInvalid)
	}

	output, err = renderEnvironmentServices(model, output, project.Environment)
	if err != nil {
		return err
	}

	for name, service := range model.Services {
		service.Labels = service.Labels.Add("io.composer.config", configHash)
		model.Services[name] = service
	}

	encoded, err := model.MarshalJSON()
	if err != nil {
		return fmt.Errorf("encode compose: %w", err)
	}

	project.Manifest = domain.Manifest{Content: output, WorkingDir: working, ConfigHash: configHash}
	project.Hash = fmt.Sprintf("%x", sha256.Sum256(encoded))

	return nil
}

func loadValues(files fs.FS, folder, ageKeyFile string, data *templateData, project *domain.Project) error {
	base, exists, err := vars.Read(files, folder+"/vars/base.yaml", ageKeyFile)
	if err != nil {
		return fmt.Errorf("read vars: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: missing %s/vars/base.yaml", domain.ErrInvalid, folder)
	}

	data.Vars = base
	if data.Env != "prod" {
		override, present, readErr := vars.Read(files, folder+"/vars/stage.yaml", ageKeyFile)
		if readErr != nil {
			return fmt.Errorf("read vars: %w", readErr)
		}

		if !present {
			return nil
		}

		data.Vars = vars.Merge(base, override)
	}

	return validateValues(data, project)
}

func validateValues(data *templateData, project *domain.Project) error {
	enabled, exists := data.Vars["enabled"]
	compose, _ := data.Vars["compose"].(map[string]any)

	options, _ := compose["x-composer"].(map[string]any)
	if value, ok := options["enabled"]; ok {
		enabled, exists = value, true
	}

	if exists {
		switch enabled {
		case true:
		case false:
			return nil
		default:
			return fmt.Errorf("%w: enabled must be boolean", domain.ErrInvalid)
		}
	}

	project.Enabled = true

	data.Secrets, _ = data.Vars["secrets"].(map[string]any)

	return nil
}

func identity(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	prefix := strings.Join(parts, "-")
	if len(prefix) > identityPrefixLimit {
		prefix = prefix[:identityPrefixLimit]
	}

	return fmt.Sprintf("%s-%x", prefix, hash[:6])
}
