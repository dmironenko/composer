//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/moby/moby/api/types/container"
	"go.yaml.in/yaml/v3"
)

// Docker SDK process globals and startup timing require serial lifecycle tests.
//
//nolint:gocognit,gocyclo,cyclop,funlen,maintidx,paralleltest // Preserve the ordered lifecycle assertions.
func TestSecretRollout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	root := t.TempDir()
	repoPath, host := filepath.Join(root, "repo"), filepath.Join(root, "composer.yaml")
	installation := fmt.Sprintf("composer-age-%d", time.Now().UnixNano())

	run := func(args ...string) error {
		t.Helper()

		var output bytes.Buffer

		cmd := newCommand("test", &output, &output) //nolint:contextcheck // ExecuteContext sets the callback context.
		cmd.SetArgs(append(args, "--config", host))
		err := cmd.ExecuteContext(ctx)

		for _, secret := range []string{
			"base-test-secret", "base-test-secret", "stage-test-secret", "updated-test-secret", "AGE-SECRET-KEY-",
		} {
			if strings.Contains(output.String(), secret) {
				t.Fatal("CLI output contains secret material")
			}
		}

		if err != nil {
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

	cfg.Source.PollInterval, cfg.AgeKeyFile = time.Second, filepath.Join(root, "age-key.txt")

	key, setupErr := os.ReadFile("../../internal/app/composer/adapters/gitops/internal/vars/testdata/age-key.txt")
	if setupErr != nil {
		t.Fatal(setupErr)
	}

	writeFile(t, cfg.AgeKeyFile, string(key))

	encoded, setupErr := yaml.Marshal(cfg)
	if setupErr != nil {
		t.Fatal(setupErr)
	}

	writeFile(t, host, string(encoded))

	folder := filepath.Join(repoPath, "apps/demo/api")
	if checkErr := os.RemoveAll(filepath.Join(folder, "configs")); checkErr != nil {
		t.Fatal(checkErr)
	}

	if checkErr := os.MkdirAll(filepath.Join(folder, "configs"), 0o700); checkErr != nil {
		t.Fatal(checkErr)
	}

	writeFile(t, filepath.Join(folder, "configs/config.yaml.tmpl"), "{{ merge .Vars.app .Secrets | toYAML }}")

	copyEncrypted := func(source, target string) {
		t.Helper()

		path := filepath.Join("../../internal/app/composer/adapters/gitops/internal/vars/testdata", source)

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}

		writeFile(t, filepath.Join(folder, "vars", target), string(data))
	}
	for _, name := range []string{"base.yaml", "stage.yaml"} {
		copyEncrypted(name, name)
	}

	repo, setupErr := gogit.PlainInitWithOptions(repoPath, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("master")},
	})
	if setupErr != nil {
		t.Fatal(setupErr)
	}

	commitAll(t, repo)

	engine := dockerEngine(t, installation)

	item := func(env, app string) container.Summary {
		t.Helper()

		return dockerContainers(t, ctx, engine, installation+"-"+env+"-demo-"+app+"-", 1)[0]
	}

	apply := func() {
		t.Helper()

		if checkErr := run("apply", "--wait", "--timeout", "30s"); checkErr != nil {
			t.Fatal(checkErr)
		}
	}
	assertSecret := func(id, expected string) {
		t.Helper()

		var config map[string]any
		if checkErr := yaml.Unmarshal([]byte(request(t, ctx, engine, id)), &config); checkErr != nil {
			t.Fatal(checkErr)
		}

		database, ok := config["database"].(map[string]any)
		if !ok || database["password"] != expected || config["retries"] != 3 {
			t.Fatal("process did not read merged decrypted app config")
		}
	}

	if checkErr := run("validate"); checkErr != nil {
		t.Fatal(checkErr)
	}

	apply()

	prod, stage, web := item("prod", "api"), item("stage", "api"), item("prod", "web")
	assertSecret(prod.ID, "base-test-secret")
	assertSecret(stage.ID, "stage-test-secret")
	copyEncrypted("base-reencrypted.yaml", "base.yaml")
	commitAll(t, repo)
	apply()

	if item("prod", "api").ID != prod.ID {
		t.Fatal("reencryption-only commit restarted service")
	}

	copyEncrypted("base-updated.yaml", "base.yaml")
	commitAll(t, repo)
	apply()

	updated := item("prod", "api")
	assertSecret(updated.ID, "updated-test-secret")

	if updated.ID == prod.ID || updated.ImageID != prod.ImageID {
		t.Fatal("secret update must recreate using the same image")
	}

	if item("stage", "api").ID != stage.ID || item("prod", "web").ID != web.ID {
		t.Fatal("secret update changed neighbour")
	}
	// An unavailable private key must not delete or change the running project.
	writeFile(t, cfg.AgeKeyFile, "invalid-test-identity")

	if checkErr := run("apply"); checkErr == nil {
		t.Fatal("invalid identity succeeded")
	}

	if item("prod", "api").ID != updated.ID {
		t.Fatal("decryption failure changed running project")
	}

	writeFile(t, cfg.AgeKeyFile, string(key))
	copyEncrypted("base.yaml", "base.yaml")
	commitAll(t, repo)
	apply()
	assertSecret(item("prod", "api").ID, "base-test-secret")
	// The source cache stores ciphertext, not decrypted vars or private identities.
	snapshots := filepath.Join(cfg.StateDir, "snapshots")

	walkErr := filepath.WalkDir(snapshots, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}

		data, err := os.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read snapshot: %w", err)
		}

		if bytes.Contains(data, []byte("test-secret")) || bytes.Contains(data, []byte("AGE-SECRET-KEY-")) {
			t.Fatal("source snapshot contains plaintext secret/key")
		}

		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
}
