package gitops_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops"
)

//nolint:gocognit,gocyclo,cyclop,maintidx // Keep rendered build identity assertions beside each input change.
func TestSourceBuildInputs(t *testing.T) {
	t.Parallel()

	folder := "apps/demo/api/"
	base := "build:\n  repository: https://example.com/app.git\n  commit: " + strings.Repeat("a", 40) +
		"\nmessage: first\ncompose:\n  services:\n    api: {}\n    sidecar:\n      image: busybox:1.37.0\n"

	cases := []struct {
		name, base, dockerfile string
		sameImage              bool
	}{
		{name: "runtime config", base: strings.Replace(base, "message: first", "message: second", 1), sameImage: true},
		{name: "source commit", base: strings.ReplaceAll(base, strings.Repeat("a", 40), strings.Repeat("b", 40))},
		{name: "dockerfile", base: base, dockerfile: "FROM scratch\nLABEL version=changed\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			cfg := gitops.Options{}
			cfg.Installation = "build-test"
			cfg.StateDir = t.TempDir()
			cfg.Environments = []string{"prod"}

			write(t, root, folder+"vars/base.yaml", base)
			write(t, root, folder+"Dockerfile.tmpl", "FROM scratch\nLABEL version=private-build-value\n")
			write(t, root, folder+"configs/message.tmpl", "{{ .Vars.message }}")

			before := loadOne(t, cfg, root)
			if before.Build == nil || model(t, before).Services["api"].Image != before.Build.Image() {
				t.Fatal("source image was not assigned")
			}

			sidecar := model(t, before).Services["sidecar"]
			if sidecar.Image != "busybox:1.37.0" || sidecar.PullPolicy != "" {
				t.Fatalf("build changed sidecar: %+v", sidecar)
			}

			write(t, root, folder+"vars/base.yaml", tc.base)

			if tc.dockerfile != "" {
				write(t, root, folder+"Dockerfile.tmpl", tc.dockerfile)
			}

			actual := loadOne(t, cfg, root)
			if (actual.Build.Image() == before.Build.Image()) != tc.sameImage || actual.Hash == before.Hash {
				t.Fatal("incorrect build/deployment identity")
			}
		})
	}
}

//nolint:gocognit,gocyclo,cyclop,maintidx // Keep each input case and its rendered project assertions together.
func TestSourceBuildValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, repo, commit, env, override string
		invalid, disabled                 bool
	}{
		{name: "https", repo: "https://example.com/app.git", commit: strings.Repeat("a", 40), env: "prod"},
		{name: "ssh", repo: "git@example.com:app.git", commit: strings.Repeat("a", 40), env: "prod"},
		{
			name: "stage override", repo: "https://example.com/app.git", commit: strings.Repeat("a", 40),
			env: "stage", override: strings.Repeat("b", 40),
		},
		{
			name: "absent stage", repo: "https://example.com/app.git", commit: strings.Repeat("a", 40),
			env: "stage", disabled: true,
		},
		{name: "mutable revision", repo: "https://example.com/app.git", commit: "master", env: "prod", invalid: true},
		{
			name: "local transport unsupported", repo: "/local/repo", commit: strings.Repeat("a", 40),
			env: "prod", invalid: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			cfg := gitops.Options{}
			cfg.Installation = "build-test"
			cfg.StateDir = t.TempDir()
			cfg.Environments = []string{tc.env}
			folder := "apps/demo/api/"
			write(t, root, folder+"vars/base.yaml", "build:\n  repository: "+tc.repo+"\n  commit: "+tc.commit+
				"\ncompose:\n  services:\n    api: {}\n")
			write(t, root, folder+"Dockerfile.tmpl", "FROM scratch\n")

			if tc.override != "" {
				write(t, root, folder+"vars/stage.yaml", "build:\n  commit: "+tc.override+"\n")
			}

			actual, err := gitops.New(cfg).Load(t.Context(), root)
			if err != nil || len(actual) != 1 {
				t.Fatalf("load: %v %v", actual, err)
			}

			project := actual[0]
			if (project.Error != "") != tc.invalid || project.Enabled == tc.disabled {
				t.Fatalf("unexpected project: %+v", project)
			}

			if tc.override != "" && project.Build.Commit != tc.override {
				t.Fatal("stage did not override commit")
			}

			if tc.disabled && project.Build != nil {
				t.Fatal("disabled project requires a build")
			}

			if project.Directory != filepath.Join(cfg.StateDir, "deployments", tc.env, project.Name) {
				t.Fatal("build changed deployment storage")
			}
		})
	}
}
