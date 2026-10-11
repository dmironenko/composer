package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const defaultBranches = "branches: {prod: main, stage: main}"

const valid = `version: 1
installation: test
source:
  mode: local
  path: repo
  branches: {prod: main, stage: main}
state_dir: state
age_key_file: keys/age.txt
`

//nolint:maintidx // Keep each table case and its related assertions together.
func TestLoad(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, document string
		expectedErr    error
	}{
		{name: "defaults", document: valid},
		{name: "master", document: strings.ReplaceAll(valid, defaultBranches, "branches: {prod: master, stage: master}")},
		{name: "slash branch", document: strings.ReplaceAll(valid, defaultBranches, "branches: {prod: release/production}")},
		{
			name:        "missing branch",
			document:    strings.ReplaceAll(valid, defaultBranches, "branches: {prod: ''}"),
			expectedErr: errInvalidConfig,
		},
		{
			name:        "bad branch",
			document:    strings.ReplaceAll(valid, defaultBranches, "branches: {prod: feature..x}"),
			expectedErr: errInvalidConfig,
		},
		{name: "unknown field", document: valid + "typo: true\n", expectedErr: errInvalidConfig},
		{name: "duplicate field", document: valid + "version: 1\n", expectedErr: errInvalidConfig},
		{name: "two documents", document: valid + "---\nversion: 1\n", expectedErr: errInvalidConfig},
		{name: "zero timeout", document: valid + "reconcile:\n  git_timeout: 0s\n", expectedErr: errInvalidConfig},
		{
			name:        "bad duration",
			document:    strings.ReplaceAll(valid, "  "+defaultBranches, "  "+defaultBranches+"\n  poll_interval: invalid"),
			expectedErr: errInvalidConfig,
		},
		{
			name:        "docker conflict",
			document:    valid + "docker:\n  host: unix:///tmp/docker.sock\n  context: desktop-linux\n",
			expectedErr: errInvalidConfig,
		},
		{
			name:        "duplicate environment",
			document:    strings.ReplaceAll(valid, defaultBranches, "branches: {prod: main, prod: dev}"),
			expectedErr: errInvalidConfig,
		},
		{
			name:        "state inside repo",
			document:    strings.ReplaceAll(valid, "state_dir: state", "state_dir: repo/state"),
			expectedErr: errInvalidConfig,
		},
		{
			name:        "invalid concurrency",
			document:    valid + "reconcile:\n  parallelism: -1\n",
			expectedErr: errInvalidConfig,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			path := filepath.Join(dir, "composer.yaml")
			if err := os.WriteFile(path, []byte(tc.document), 0o600); err != nil {
				t.Fatal(err)
			}

			actual, err := loadConfig(path)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("load: actual=%v expected=%v", err, tc.expectedErr)
			}

			if tc.expectedErr != nil {
				return
			}

			checkLoadedConfig(t, dir, actual)
		})
	}
}

func checkLoadedConfig(t *testing.T, dir string, actual hostConfig) {
	t.Helper()

	if actual.Source.Path != filepath.Join(dir, "repo") || actual.StateDir != filepath.Join(dir, "state") {
		t.Fatal("paths were not resolved against config directory")
	}

	if actual.AgeKeyFile != filepath.Join(dir, "keys/age.txt") {
		t.Fatal("age key path was not resolved against config directory")
	}

	if actual.Source.PollInterval != 10*time.Second || actual.Reconcile.Parallelism != 4 {
		t.Fatalf("unexpected defaults: %+v", actual.Reconcile)
	}
}

func TestLoadBranches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, branches string
		expectedErr    error
	}{
		{name: "environment branches", branches: "{prod: master, stage: dev}"},
		{name: "production only", branches: "{prod: master}"},
		{name: "personal stages", branches: "{prod: master, dan: users/dan, alex: feature/alex}"},
		{name: "stage only", branches: "{dan: users/dan}"},
		{name: "no environments", branches: "{}", expectedErr: errInvalidConfig},
		{name: "empty branch", branches: "{prod: ''}", expectedErr: errInvalidConfig},
		{name: "path traversal", branches: "{../dan: dev}", expectedErr: errInvalidConfig},
		{name: "uppercase name", branches: "{Dan: dev}", expectedErr: errInvalidConfig},
		{name: "invalid DNS name", branches: "{dan_alex: dev}", expectedErr: errInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "composer.yaml")
			writeFile(t, path, strings.ReplaceAll(valid, "{prod: main, stage: main}", tc.branches))

			_, err := loadConfig(path)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("load branches: actual=%v expected=%v", err, tc.expectedErr)
			}
		})
	}
}
