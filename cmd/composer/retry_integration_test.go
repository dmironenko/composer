//go:build integration

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"go.yaml.in/yaml/v3"
)

// Docker SDK process globals and startup timing require serial lifecycle tests.
//
//nolint:gocyclo,cyclop,funlen,maintidx,paralleltest // Preserve the ordered lifecycle assertions.
func TestSerialDependencies(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	root := t.TempDir()
	repoPath, host := filepath.Join(root, "repo"), filepath.Join(root, "composer.yaml")
	installation := fmt.Sprintf("composer-retry-%d", time.Now().UnixNano())
	cfg := hostConfig{
		Version: 1, Installation: installation,
		Source: sourceConfig{
			Mode: "local", Path: repoPath,
			Branches: map[string]string{"prod": "main"}, PollInterval: 100 * time.Millisecond,
		},
		StateDir:  filepath.Join(root, "state"),
		Reconcile: reconcileConfig{Parallelism: 1, GitTimeout: time.Second, DeployTimeout: 10 * time.Second},
	}

	data, setupErr := yaml.Marshal(cfg)
	if setupErr != nil {
		t.Fatal(setupErr)
	}

	writeFile(t, host, string(data))

	files := map[string]string{
		"apps/demo/api/vars/base.yaml": `compose:
  services:
    api:
      image: busybox:1.37.0
      restart: 'no'
      command: [sh, -c, 'wget -qO /tmp/ready http://db:8080 || exit 1; exec sleep 3600']
      healthcheck:
        test: [CMD, test, -s, /tmp/ready]
        interval: 100ms
        timeout: 1s
        retries: 2
`,
		"apps/demo/db/vars/base.yaml": `compose:
  services:
    db:
      image: busybox:1.37.0
      command:
        - sh
        - -c
        - sleep 2; mkdir -p /tmp/www; echo ready > /tmp/www/index.html; exec httpd -f -p 8080 -h /tmp/www
      healthcheck:
        test: [CMD, wget, -qO-, 'http://127.0.0.1:8080']
        interval: 100ms
        timeout: 1s
        retries: 30
`,
	}
	for name, value := range files {
		path := filepath.Join(repoPath, name)
		if checkErr := os.MkdirAll(filepath.Dir(path), 0o755); checkErr != nil {
			t.Fatal(checkErr)
		}

		writeFile(t, path, value)
	}

	repo, setupErr := gogit.PlainInitWithOptions(repoPath, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	if setupErr != nil {
		t.Fatal(setupErr)
	}

	commitAll(t, repo)

	dockerEngine(t, installation)

	cmd := newCommand("test", io.Discard, io.Discard)
	cmd.SetArgs([]string{"apply", "--config", host, "--wait", "--timeout", "20s"})

	if checkErr := cmd.ExecuteContext(ctx); checkErr != nil {
		t.Fatal(checkErr)
	}

	markers, err := filepath.Glob(filepath.Join(cfg.StateDir, "deployments", "prod", "*", "applied"))
	if err != nil || len(markers) != 2 {
		t.Fatalf("successful deployments: %v %v", markers, err)
	}
	// A permanently failed healthcheck times out, without recording a false success.
	writeFile(t, filepath.Join(repoPath, "apps/demo/api/vars/base.yaml"), `compose:
  services:
    api:
      image: busybox:1.37.0
      command: [sleep, '3600']
      healthcheck:
        test: [CMD, 'false']
        interval: 100ms
        timeout: 1s
        retries: 1
`)
	commitAll(t, repo)

	cmd = newCommand("test", io.Discard, io.Discard)
	cmd.SetArgs([]string{"apply", "--config", host, "--wait", "--timeout", "3s"})

	if checkErr := cmd.ExecuteContext(ctx); !errors.Is(checkErr, context.DeadlineExceeded) {
		t.Fatalf("readiness timeout: %v", checkErr)
	}
}
