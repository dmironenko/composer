package docker_test

import (
	"errors"
	"os"
	"testing"

	"github.com/dmironenko/composer/internal/app/composer/adapters/docker"
)

//nolint:paralleltest // Docker CLI initialization updates process-wide logging and environment.
func TestClientLifecycle(t *testing.T) {
	cases := []struct {
		name   string
		config docker.Options
	}{
		{name: "unreachable daemon", config: docker.Options{Host: "tcp://127.0.0.1:1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := docker.New(tc.config)
			if err != nil {
				t.Fatalf("construct client without a daemon: %v", err)
			}

			if err = actual.Close(); err != nil {
				t.Fatalf("close unused client: %v", err)
			}
		})
	}
}

//nolint:paralleltest // Docker CLI initialization updates process-wide logging and environment.
func TestInvalidContext(t *testing.T) {
	cases := []struct {
		name        string
		config      docker.Options
		expectedErr error
	}{
		{
			name: "missing context", config: docker.Options{Context: "composer-test-nonexistent-context"},
			expectedErr: os.ErrNotExist,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := docker.New(tc.config)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("construct client: error=%v, expected=%v", err, tc.expectedErr)
			}

			if actual != nil {
				t.Fatal("invalid context returned a client")
			}
		})
	}
}
