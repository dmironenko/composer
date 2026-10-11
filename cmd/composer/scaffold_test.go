package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

//nolint:gocognit,gocyclo,cyclop,maintidx // Keep each table case and its related assertions together.
func TestCreate(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, branch string }{{name: "main", branch: "main"}, {name: "master", branch: "master"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			repo, host := filepath.Join(dir, "repo"), filepath.Join(dir, "composer.yaml")
			if err := createRepository(repo, host, tc.branch, "test"); err != nil {
				t.Fatal(err)
			}

			actual, err := loadConfig(host)
			if err != nil {
				t.Fatal(err)
			}

			if actual.Source.Branches["prod"] != tc.branch {
				t.Fatalf("branch: actual=%s expected=%s", actual.Source.Branches["prod"], tc.branch)
			}

			for _, path := range []string{"apps/.gitkeep"} {
				if _, err = os.Stat(filepath.Join(repo, path)); err != nil {
					t.Fatal(err)
				}
			}

			for _, path := range []string{"apps/demo", "project.yaml", "stage", "prod"} {
				if _, err = os.Stat(filepath.Join(repo, path)); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("unexpected generated path %s: %v", path, err)
				}
			}

			err = createRepository(repo, host, tc.branch, "test")
			if !errors.Is(err, fs.ErrExist) {
				t.Fatalf("second init: actual=%v expected=%v", err, fs.ErrExist)
			}
		})
	}
}
