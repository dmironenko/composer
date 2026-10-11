//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/compose/v5/pkg/api"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.yaml.in/yaml/v3"
)

// Docker SDK process globals and startup timing require serial lifecycle tests.
//
//nolint:gocognit,gocyclo,cyclop,funlen,maintidx,paralleltest // Preserve the ordered lifecycle assertions.
func TestGitOpsLifecycle(t *testing.T) {
	const productionMessage = "api: production"

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	repoPath, host := filepath.Join(root, "repo"), filepath.Join(root, "composer.yaml")
	installation := fmt.Sprintf("composer-simple-%d", time.Now().UnixNano())

	run := func(args ...string) error {
		t.Helper()

		var output bytes.Buffer

		cmd := newCommand("test", &output, &output) //nolint:contextcheck // ExecuteContext sets the callback context.
		cmd.SetArgs(append(args, "--config", host))

		err := cmd.ExecuteContext(ctx)
		if err != nil {
			t.Log(output.String())

			return fmt.Errorf("execute composer: %w", err)
		}

		return nil
	}
	if checkErr := run("init", repoPath, "--branch", "master", "--installation", installation); checkErr != nil {
		t.Fatal(checkErr)
	}

	seed(t, repoPath)

	cfg, setupErr := loadConfig(host)
	if setupErr != nil {
		t.Fatal(setupErr)
	}

	cfg.Source.PollInterval = time.Second
	cfg.Reconcile.Parallelism = 1

	data, setupErr := yaml.Marshal(cfg)
	if setupErr != nil {
		t.Fatal(setupErr)
	}

	writeFile(t, host, string(data))

	basePath := filepath.Join(repoPath, "apps/demo/api/vars/base.yaml")

	base, setupErr := os.ReadFile(basePath)
	if setupErr != nil {
		t.Fatal(setupErr)
	}
	// Startup config must be readable by an arbitrary container UID.
	content := strings.Replace(string(base), "    api-http:\n", "    api-http:\n      user: '65534:65534'\n", 1)
	content = strings.Replace(content, "      volumes:\n", "      volumes:\n        - data:/var/data\n", 1)
	content += "  volumes:\n    data: {}\n"
	writeFile(t, basePath, content)
	writeFile(t, filepath.Join(repoPath, "apps/demo/api/vars/stage.yaml"),
		"message: 'api: staging'\ncompose:\n  services:\n    api-http:\n      deploy: {replicas: 1}\n")

	repo, setupErr := gogit.PlainInitWithOptions(repoPath, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("master")},
	})
	if setupErr != nil {
		t.Fatal(setupErr)
	}

	commitAll(t, repo)

	engine := dockerEngine(t, installation)

	items := func(env, app string, expected int) []container.Summary {
		t.Helper()

		return dockerContainers(t, ctx, engine, installation+"-"+env+"-demo-"+app+"-", expected)
	}

	apply := func() {
		t.Helper()

		if checkErr := run("apply", "--wait", "--timeout", "30s"); checkErr != nil {
			t.Fatal(checkErr)
		}
	}
	apply()

	prod, stage, web := items("prod", "api", 1)[0], items("stage", "api", 1)[0], items("prod", "web", 1)[0]
	items("stage", "web", 0)

	if actual := request(t, ctx, engine, prod.ID); actual != productionMessage {
		t.Fatal(actual)
	}

	if actual := request(t, ctx, engine, stage.ID); actual != "api: staging" {
		t.Fatal(actual)
	}

	prodInspect, inspectErr := engine.ContainerInspect(ctx, prod.ID, client.ContainerInspectOptions{})
	if inspectErr != nil {
		t.Fatal(inspectErr)
	}

	stageInspect, inspectErr := engine.ContainerInspect(ctx, stage.ID, client.ContainerInspectOptions{})
	if inspectErr != nil {
		t.Fatal(inspectErr)
	}

	webInspect, inspectErr := engine.ContainerInspect(ctx, web.ID, client.ContainerInspectOptions{})
	if inspectErr != nil {
		t.Fatal(inspectErr)
	}

	for network := range prodInspect.Container.NetworkSettings.Networks {
		if stageInspect.Container.NetworkSettings.Networks[network] != nil {
			t.Fatal("stage shares prod network")
		}
	}

	apply()

	if items("prod", "api", 1)[0].ID != prod.ID {
		t.Fatal("noop recreated container")
	}

	updatedContent := strings.Replace(content, `message: "api: production"`, "message: updated", 1)
	writeFile(t, basePath, updatedContent)
	// Dirty worktree is ignored.
	apply()

	if items("prod", "api", 1)[0].ID != prod.ID {
		t.Fatal("dirty worktree deployed")
	}

	commitAll(t, repo)
	apply()

	updated := items("prod", "api", 1)[0]
	if updated.ID == prod.ID || updated.ImageID != prod.ImageID {
		t.Fatal("config update did not recreate with same image")
	}

	if actual := request(t, ctx, engine, updated.ID); actual != "updated" {
		t.Fatal(actual)
	}

	if items("stage", "api", 1)[0].ID != stage.ID || items("prod", "web", 1)[0].ID != web.ID {
		t.Fatal("neighbour changed")
	}

	for _, previous := range []client.ContainerInspectResult{webInspect, stageInspect} {
		current, checkErr := engine.ContainerInspect(ctx, previous.Container.ID, client.ContainerInspectOptions{})
		if checkErr != nil {
			t.Fatal(checkErr)
		}

		if current.Container.State.StartedAt != previous.Container.State.StartedAt {
			t.Fatal("config-only update restarted neighbour", previous.Container.ID)
		}
	}

	for _, replicas := range []int{3, 1, 0, 1} {
		writeFile(t, basePath, strings.Replace(updatedContent, "replicas: 1", fmt.Sprintf("replicas: %d", replicas), 1))
		commitAll(t, repo)
		apply()

		replicasNow := items("prod", "api", replicas)
		if replicas == 3 {
			original := false
			for _, item := range replicasNow {
				original = original || item.ID == updated.ID
				if actual := request(t, ctx, engine, item.ID); actual != "updated" {
					t.Fatal(actual)
				}
			}

			if !original {
				t.Fatal("scale recreated existing replica")
			}
		}
	}

	writeFile(t, basePath, content)

	commitAll(t, repo)
	apply()

	if actual := request(t, ctx, engine, items("prod", "api", 1)[0].ID); actual != productionMessage {
		t.Fatal(actual)
	}
	// A failed partial update must invalidate success, so reverting to A really applies A.
	broken := strings.Replace(content, `message: "api: production"`, "message: broken rollout", 1)
	broken = strings.Replace(
		broken, "test: [CMD, wget, -q, -O, /dev/null, http://127.0.0.1:8080]", "test: [CMD, 'false']", 1,
	)
	writeFile(t, basePath, broken)
	commitAll(t, repo)

	if checkErr := run("apply"); checkErr == nil {
		t.Fatal("unhealthy deployment succeeded")
	}

	writeFile(t, basePath, content)

	commitAll(t, repo)
	apply()

	if actual := request(t, ctx, engine, items("prod", "api", 1)[0].ID); actual != productionMessage {
		t.Fatal("failed update revert:", actual)
	}
	// Invalid app remains in place, while a valid neighbour update proceeds.
	writeFile(t, basePath, "message: [\n")

	webConfig := filepath.Join(repoPath, "apps/demo/web/configs/message.txt.tmpl")
	writeFile(t, webConfig, "independent update\n")
	commitAll(t, repo)

	if checkErr := run("apply"); checkErr == nil {
		t.Fatal("invalid yaml succeeded")
	}

	items("prod", "api", 1)

	if actual := request(t, ctx, engine, items("prod", "web", 1)[0].ID); actual != "independent update" {
		t.Fatal(actual)
	}

	writeFile(t, basePath, content)

	if checkErr := os.Remove(filepath.Join(repoPath, "apps/demo/api/vars/stage.yaml")); checkErr != nil {
		t.Fatal(checkErr)
	}

	commitAll(t, repo)
	apply()
	items("stage", "api", 0)

	if checkErr := os.RemoveAll(filepath.Join(repoPath, "apps/demo/api")); checkErr != nil {
		t.Fatal(checkErr)
	}

	commitAll(t, repo)
	apply()
	items("prod", "api", 0)

	if _, checkErr := engine.VolumeInspect(
		ctx, prod.Labels[api.ProjectLabel]+"_data", client.VolumeInspectOptions{},
	); checkErr != nil {
		t.Fatal("volume removed:", checkErr)
	}

	for name := range stageInspect.Container.NetworkSettings.Networks {
		if _, checkErr := engine.NetworkInspect(ctx, name, client.NetworkInspectOptions{}); checkErr != nil {
			t.Fatal("namespace network removed:", checkErr)
		}
	}

	// Foreground polling sees later commits; cancellation leaves services running.
	runCtx, stopRun := context.WithCancel(ctx)
	done := make(chan error, 1)
	cmd := newCommand("test", io.Discard, io.Discard)
	cmd.SetArgs([]string{"run", "--config", host})

	go func() { done <- cmd.ExecuteContext(runCtx) }()

	writeFile(t, webConfig, "poll update\n")
	commitAll(t, repo)

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		response, err := engine.ContainerList(ctx, client.ContainerListOptions{All: true})
		if err != nil {
			t.Fatal(err)
		}

		var candidates []container.Summary

		for _, item := range response.Items {
			if strings.HasPrefix(item.Labels[api.ProjectLabel], installation+"-prod-demo-web-") {
				candidates = append(candidates, item)
			}
		}

		if len(candidates) == 1 {
			actual, pollErr := engine.ContainerInspect(ctx, candidates[0].ID, client.ContainerInspectOptions{})
			if pollErr == nil && actual.Container.State.Health != nil &&
				actual.Container.State.Health.Status == "healthy" &&
				request(t, ctx, engine, candidates[0].ID) == "poll update" {
				break
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	locked := newCommand("test", io.Discard, io.Discard)
	locked.SetArgs([]string{"validate", "--config", host})

	if checkErr := locked.ExecuteContext(ctx); checkErr == nil {
		t.Fatal("second composer process acquired the occupied state directory")
	}

	stopRun()

	if checkErr := <-done; checkErr != nil {
		t.Fatal(checkErr)
	}

	if actual := request(t, ctx, engine, items("prod", "web", 1)[0].ID); actual != "poll update" {
		t.Fatal(actual)
	}
}

func request(t *testing.T, ctx context.Context, engine client.APIClient, id string) string {
	t.Helper()

	created, err := engine.ExecCreate(ctx, id, client.ExecCreateOptions{
		Cmd: []string{"wget", "-qO-", "http://127.0.0.1:8080"}, AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	attached, err := engine.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer attached.Close()

	var output bytes.Buffer
	if _, checkErr := stdcopy.StdCopy(&output, io.Discard, attached.Reader); checkErr != nil {
		t.Fatal(checkErr)
	}

	return strings.TrimSpace(output.String())
}
