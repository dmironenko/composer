package state_test

import (
	"errors"
	"os"
	"testing"

	"github.com/dmironenko/composer/internal/app/composer/adapters/state"
)

func TestBuildWorkspace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name string }{{name: "private directory removed after build"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testBuildWorkspace(t)
		})
	}
}

func testBuildWorkspace(t *testing.T) {
	t.Helper()
	store := state.New(t.TempDir(), nil)

	directory, err := store.CreateBuildContext()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if cleanupErr := os.RemoveAll(directory); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})

	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode() != os.ModeDir|0o700 {
		t.Fatalf("build context mode: %v", info.Mode())
	}

	if err := store.RemoveBuildContext(directory); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed context: %v", err)
	}
}
