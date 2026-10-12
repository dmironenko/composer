//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.yaml.in/yaml/v3"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

const sourceDockerfile = `FROM busybox:1.37.0
LABEL io.composer.tag={{ quote .Vars.build.tag }}
RUN case {{ quote .Vars.build.tag }} in \
    v1.0.0) printf 'source-one\n' > /source ;; \
    v2.0.0) printf 'source-two\n' > /source ;; \
    *) exit 1 ;; esac
RUN mkdir /www
CMD ["sh", "-c", "cat /source /etc/message > /www/index.html; exec httpd -f -p 8080 -h /www"]
`

//nolint:funlen,gocognit,gocyclo,cyclop,maintidx,paralleltest // Ordered Docker lifecycle with process environment.
func TestSourceBuildRollout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	first, second := "v1.0.0", "v2.0.0"
	installation := fmt.Sprintf("composer-source-%d", time.Now().UnixNano())
	repository := "https://example.test/" + installation + ".git"
	engine := dockerEngine(t, installation)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()

		removeContainers(t, cleanup, engine, installation)

		for _, tag := range []string{first, second} {
			rendered := strings.ReplaceAll(sourceDockerfile, "{{ quote .Vars.build.tag }}", fmt.Sprintf("%q", tag))
			spec := domain.BuildSpec{Repository: repository, Tag: tag, Dockerfile: rendered}

			_, err := engine.ImageRemove(cleanup, spec.Image(), client.ImageRemoveOptions{})
			if err != nil && !errdefs.IsNotFound(err) {
				t.Error(err)
			}
		}
	})
	// Source acquisition runs inside Docker; Composer needs no external client tools.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	repoPath, host := filepath.Join(root, "gitops"), filepath.Join(root, "host.yaml")
	repo := newBuildRepository(t, repoPath)
	cfg := defaultConfig()
	cfg.Version, cfg.Installation, cfg.StateDir = 1, installation, filepath.Join(root, "state")
	cfg.Source.Mode, cfg.Source.Path = "local", repoPath
	cfg.Source.Branches = map[string]string{"prod": "master", "stage": "master"}
	cfg.Source.PollInterval = time.Second
	cfg.Docker.Host = engine.DaemonHost()

	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, host, string(encoded))

	folder := filepath.Join(repoPath, "apps/demo/api")
	for _, subdir := range []string{"vars", "configs"} {
		if err = os.MkdirAll(filepath.Join(folder, subdir), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	writeFile(t, filepath.Join(folder, "Dockerfile.tmpl"), sourceDockerfile)
	writeFile(t, filepath.Join(folder, "configs/message.tmpl"), "{{ .Vars.app.message }}\n")
	writeFile(t, filepath.Join(folder, "vars/stage.yaml"), "build:\n  tag: "+second+"\n")

	apply := func(tag, message string) {
		t.Helper()
		writeFile(t, filepath.Join(folder, "vars/base.yaml"), sourceBuildVars(repository, tag, message))
		commitAll(t, repo)
		applySourceBuild(t, ctx, host)
	}
	item := func(env, expected string) container.Summary {
		t.Helper()

		result := dockerContainers(t, ctx, engine, installation+"-"+env+"-demo-api-", 1)[0]
		if actual := request(t, ctx, engine, result.ID); actual != expected {
			t.Fatalf("%s response: %q expected %q", env, actual, expected)
		}

		return result
	}

	apply(first, "config-one")

	prod := item("prod", "source-one\nconfig-one")
	stage := item("stage", "source-two\nconfig-one")

	apply(first, "config-two")

	changed := item("prod", "source-one\nconfig-two")

	changedStage := item("stage", "source-two\nconfig-two")
	if changed.ImageID != prod.ImageID || changed.ID == prod.ID || changedStage.ImageID != stage.ImageID {
		t.Fatal("config update rebuilt or failed to recreate")
	}

	apply(second, "config-two")

	promoted := item("prod", "source-two\nconfig-two")
	if promoted.ImageID != stage.ImageID {
		t.Fatal("promotion did not reuse stage image")
	}

	apply(first, "config-two")

	reverted := item("prod", "source-one\nconfig-two")

	unchangedStage := item("stage", "source-two\nconfig-two")
	if reverted.ImageID != prod.ImageID || unchangedStage.ID != changedStage.ID {
		t.Fatal("revert rebuilt image or changed stage")
	}

	for _, failure := range []struct{ name, file, content, restored string }{
		{
			name: "failed Dockerfile", file: "Dockerfile.tmpl",
			content: sourceDockerfile + "\nRUN exit 42\n", restored: sourceDockerfile,
		},
		{
			name: "rejected source tag", file: "vars/base.yaml",
			content:  sourceBuildVars(repository, "ffffffffffffffffffffffffffffffffffffffff", "config-two"),
			restored: sourceBuildVars(repository, first, "config-two"),
		},
	} {
		t.Run(failure.name, func(t *testing.T) {
			writeFile(t, filepath.Join(folder, failure.file), failure.content)
			commitAll(t, repo)

			var output bytes.Buffer

			cmd := newCommand("test", &output, &output) //nolint:contextcheck // ExecuteContext sets the callback context.
			cmd.SetArgs([]string{"apply", "--config", host})

			if applyErr := cmd.ExecuteContext(ctx); applyErr == nil {
				t.Fatal("failed source build reported success")
			}

			if item("prod", "source-one\nconfig-two").ID != reverted.ID ||
				item("stage", "source-two\nconfig-two").ID != unchangedStage.ID {
				t.Fatal("failed source build changed a running container")
			}

			writeFile(t, filepath.Join(folder, failure.file), failure.restored)
			commitAll(t, repo)
			applySourceBuild(t, ctx, host)

			if item("prod", "source-one\nconfig-two").ID != reverted.ID ||
				item("stage", "source-two\nconfig-two").ID != unchangedStage.ID {
				t.Fatal("revert after failed build recreated an unchanged container")
			}
		})
	}
}

func newBuildRepository(t *testing.T, path string) *gogit.Repository {
	t.Helper()

	repo, err := gogit.PlainInitWithOptions(path, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("master")},
	})
	if err != nil {
		t.Fatal(err)
	}

	return repo
}

func applySourceBuild(t *testing.T, ctx context.Context, host string) {
	t.Helper()

	var output bytes.Buffer

	cmd := newCommand("test", &output, &output) //nolint:contextcheck // ExecuteContext installs the command context.
	cmd.SetArgs([]string{"apply", "--config", host, "--wait", "--timeout", "45s"})

	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("apply source image: %v\n%s", err, output.String())
	}
}

func sourceBuildVars(repository, tag, message string) string {
	return fmt.Sprintf(`build:
  repository: %s
  tag: %s

app:
  message: %s

compose:
  services:
    api:
      volumes: ['./configs/message:/etc/message:ro']
      healthcheck:
        test: [CMD, wget, -qO-, 'http://127.0.0.1:8080']
        interval: 100ms
        timeout: 1s
        retries: 10
`, repository, tag, message)
}

//nolint:funlen,gocognit,gocyclo,cyclop,maintidx,paralleltest // Ordered Docker lifecycle.
func TestBranchDockerfileRollout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	tag := "v1.0.0"
	installation := fmt.Sprintf("composer-br-%d", time.Now().UnixNano())
	repository := "https://example.test/" + installation + ".git"
	engine := dockerEngine(t, installation)
	changedDockerfile := sourceDockerfile + "\nRUN printf 'dockerfile-dev\\n' > /source\n"

	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()

		removeContainers(t, cleanup, engine, installation)

		for _, dockerfile := range []string{sourceDockerfile, changedDockerfile} {
			rendered := strings.ReplaceAll(dockerfile, "{{ quote .Vars.build.tag }}", fmt.Sprintf("%q", tag))
			spec := domain.BuildSpec{Repository: repository, Tag: tag, Dockerfile: rendered}

			_, err := engine.ImageRemove(cleanup, spec.Image(), client.ImageRemoveOptions{})
			if err != nil && !errdefs.IsNotFound(err) {
				t.Error(err)
			}
		}
	})

	repoPath, host := filepath.Join(root, "gitops"), filepath.Join(root, "host.yaml")
	repo := newBuildRepository(t, repoPath)

	folder := filepath.Join(repoPath, "apps/demo/api")
	for _, subdir := range []string{"vars", "configs"} {
		if err := os.MkdirAll(filepath.Join(folder, subdir), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	writeFile(t, filepath.Join(folder, "Dockerfile.tmpl"), sourceDockerfile)
	writeFile(t, filepath.Join(folder, "vars/base.yaml"), sourceBuildVars(repository, tag, "config-one"))
	writeFile(t, filepath.Join(folder, "vars/stage.yaml"), "{}\n")
	writeFile(t, filepath.Join(folder, "configs/message.tmpl"), "{{ .Vars.app.message }}\n")
	commitAll(t, repo)

	master, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	checkout := &gogit.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("dev"), Create: true}
	if err = worktree.Checkout(checkout); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	cfg.Version, cfg.Installation, cfg.StateDir = 1, installation, filepath.Join(root, "state")
	cfg.Source.Mode, cfg.Source.Path = localMode, repoPath
	cfg.Source.Branches = map[string]string{"prod": "master", "stage": "dev"}
	cfg.Source.PollInterval = time.Second

	cfg.Docker.Host = engine.DaemonHost()
	if err = writeConfig(host, cfg); err != nil {
		t.Fatal(err)
	}

	applySourceBuild(t, ctx, host)
	prod := dockerContainers(t, ctx, engine, installation+"-prod-demo-api-", 1)[0]
	stage := dockerContainers(t, ctx, engine, installation+"-stage-demo-api-", 1)[0]

	writeFile(t, filepath.Join(folder, "Dockerfile.tmpl"), changedDockerfile)
	commitAll(t, repo)
	applySourceBuild(t, ctx, host)

	updated := dockerContainers(t, ctx, engine, installation+"-stage-demo-api-", 1)[0]
	if updated.ImageID == stage.ImageID || request(t, ctx, engine, updated.ID) != "dockerfile-dev\nconfig-one" {
		t.Fatal("dev Dockerfile did not replace the stage image")
	}

	if actual := dockerContainers(t, ctx, engine, installation+"-prod-demo-api-", 1)[0]; actual.ID != prod.ID {
		t.Fatal("dev Dockerfile restarted production")
	}
	// Promotion and revert reuse Docker images.
	dev, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}

	promoted := plumbing.NewHashReference(plumbing.NewBranchReferenceName("master"), dev.Hash())
	if err = repo.Storer.SetReference(promoted); err != nil {
		t.Fatal(err)
	}

	applySourceBuild(t, ctx, host)

	actual := dockerContainers(t, ctx, engine, installation+"-prod-demo-api-", 1)[0]
	if actual.ImageID != updated.ImageID || request(t, ctx, engine, actual.ID) != "dockerfile-dev\nconfig-one" {
		t.Fatal("promotion did not reuse verified stage image")
	}

	if err = repo.Storer.SetReference(master); err != nil {
		t.Fatal(err)
	}

	applySourceBuild(t, ctx, host)

	actual = dockerContainers(t, ctx, engine, installation+"-prod-demo-api-", 1)[0]
	if actual.ImageID != prod.ImageID || request(t, ctx, engine, actual.ID) != "source-one\nconfig-one" {
		t.Fatal("production revert did not reuse the original image")
	}

	if remaining := dockerContainers(t, ctx, engine, installation+"-stage-demo-api-", 1)[0]; remaining.ID != updated.ID {
		t.Fatal("production promotion/revert restarted stage")
	}
	// Deleting stage's opt-in file must not touch production's inventory.
	if err = os.Remove(filepath.Join(folder, "vars/stage.yaml")); err != nil {
		t.Fatal(err)
	}

	commitAll(t, repo)
	applySourceBuild(t, ctx, host)
	dockerContainers(t, ctx, engine, installation+"-stage-demo-api-", 0)

	if remaining := dockerContainers(t, ctx, engine, installation+"-prod-demo-api-", 1)[0]; remaining.ID != actual.ID {
		t.Fatal("stage removal changed production")
	}
}
