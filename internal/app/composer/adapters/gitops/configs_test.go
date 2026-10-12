package gitops_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPublishRestrictiveUmask(t *testing.T) {
	t.Parallel()

	if os.Getenv("COMPOSER_TEST_UMASK") != "1" {
		runWithRestrictiveUmask(t)

		return
	}

	repository, cfg := fixture(t, "demo", "worker")
	write(t, repository, "apps/demo/worker/configs/nested/app.yaml", "value: secret\n")
	write(t, repository, "apps/demo/worker/configs/start.sh", "#!/bin/sh\n")

	if err := os.Chmod(filepath.Join(repository, "apps/demo/worker/configs/start.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	project := loadOne(t, cfg, repository)
	check(t, project.Error == "", project.Error)

	root := project.Manifest.WorkingDir

	for name, want := range map[string]os.FileMode{
		".": 0o700, "configs": 0o755, "configs/nested": 0o755,
		"configs/nested/app.yaml": 0o444, "configs/start.sh": 0o555,
	} {
		assertMode(t, filepath.Join(root, name), want)
	}
}

func runWithRestrictiveUmask(t *testing.T) {
	t.Helper()

	//nolint:gosec // re-execute this test binary; the shell command is constant.
	command := exec.CommandContext(t.Context(), "sh", "-c",
		`umask 077; exec "$1" -test.run '^TestPublishRestrictiveUmask$'`, "sh", os.Args[0])

	command.Env = append(os.Environ(), "COMPOSER_TEST_UMASK=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("restrictive umask: %v\n%s", err, output)
	}
}
