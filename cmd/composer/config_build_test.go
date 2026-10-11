package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

//nolint:gocognit,gocyclo,cyclop,maintidx // Keep credential validation and relative path assertions together.
func TestBuildAuth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, document string
		want           authConfig
		wantErr        error
	}{
		{name: "anonymous"},
		{
			name:     "ssh",
			document: "build_auth:\n  type: ssh\n  key_file: key\n  known_hosts_file: hosts\n",
			want:     authConfig{Type: "ssh", KeyFile: "key", KnownHostsFile: "hosts"},
		},
		{
			name:     "https",
			document: "build_auth:\n  type: https\n  token_file: token\n",
			want:     authConfig{Type: "https", TokenFile: "token"},
		},
		{name: "missing pinned host", document: "build_auth:\n  type: ssh\n  key_file: key\n", wantErr: errInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			file := filepath.Join(dir, "composer.yaml")
			if err := os.WriteFile(file, []byte(valid+tc.document), 0o600); err != nil {
				t.Fatal(err)
			}

			actual, err := loadConfig(file)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("loadConfig error=%v, expected=%v", err, tc.wantErr)
			}

			if tc.wantErr != nil {
				return
			}

			want := tc.want
			for _, path := range []*string{&want.KeyFile, &want.KnownHostsFile, &want.TokenFile} {
				if *path != "" {
					*path = filepath.Join(dir, *path)
				}
			}

			if actual.BuildAuth != want {
				t.Fatalf("BuildAuth=%+v, expected=%+v", actual.BuildAuth, want)
			}
		})
	}
}
