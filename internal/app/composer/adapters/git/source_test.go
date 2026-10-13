package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/dmironenko/composer/v2/internal/app/composer/adapters/git"
)

//nolint:gocognit,gocyclo,cyclop,funlen,maintidx,paralleltest // One lifecycle uses a process-wide PATH fixture.
func TestLocalSnapshot(t *testing.T) {
	// Local reads and test setup must work without a git executable.
	t.Setenv("PATH", t.TempDir())

	cases := []struct{ name string }{{"master"}, {"main"}, {"release/production"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			repo := newRepository(t, root, tc.name)
			first := commitFile(t, repo, root, "config.txt", "committed")
			// Read configured branch even when HEAD points somewhere else.
			otherRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName("other"), first)
			if err := repo.Storer.SetReference(otherRef); err != nil {
				t.Fatal(err)
			}

			worktree, err := repo.Worktree()
			if err != nil {
				t.Fatal(err)
			}

			if err = worktree.Checkout(&gogit.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("other")}); err != nil {
				t.Fatal(err)
			}

			if err = os.WriteFile(filepath.Join(root, "config.txt"), []byte("dirty"), 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err = worktree.Add("config.txt"); err != nil {
				t.Fatal(err)
			}

			before, err := worktree.Status()
			if err != nil {
				t.Fatal(err)
			}

			state := t.TempDir()
			source := git.SourceConfig{Mode: "local", Path: root, Branch: tc.name}

			snapshot, err := git.New(source, state).Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			if snapshot.Commit != first.String() {
				t.Fatalf("commit: %s", snapshot.Commit)
			}

			assertFile(t, filepath.Join(snapshot.Root, "config.txt"), "committed")
			assertFile(t, filepath.Join(root, "config.txt"), "dirty")

			after, err := worktree.Status()
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(before, after) {
				t.Fatal("index or worktree changed")
			}

			head, err := repo.Head()
			if err != nil {
				t.Fatal(err)
			}

			if head.Name() != plumbing.NewBranchReferenceName("other") {
				t.Fatal("HEAD changed")
			}
			// A later commit on the selected branch creates a new snapshot;
			// the previously returned tree remains available and unchanged.
			second := commitFile(t, repo, root, "config.txt", "next")

			selectedRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName(tc.name), second)
			if err = repo.Storer.SetReference(selectedRef); err != nil {
				t.Fatal(err)
			}

			next, err := git.New(source, state).Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			if next.Commit != second.String() || next.Root == snapshot.Root {
				t.Fatal("new commit was not read")
			}

			assertFile(t, filepath.Join(next.Root, "config.txt"), "next")
			assertFile(t, filepath.Join(snapshot.Root, "config.txt"), "committed")
		})
	}
}

func TestReadErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		commit, cancel bool
		branch         string
		expectedErr    error
	}{
		{name: "empty", branch: "master", expectedErr: git.ErrEmpty},
		{name: "missing branch", commit: true, branch: "main", expectedErr: git.ErrBranch},
		{name: "invalid branch", branch: "../bad", expectedErr: git.ErrBranch},
		{name: "cancel", commit: true, branch: "master", cancel: true, expectedErr: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := git.SourceConfig{Mode: "local", Path: t.TempDir(), Branch: tc.branch}

			repo := newRepository(t, source.Path, "master")
			if tc.commit {
				commitFile(t, repo, source.Path, "file", "value")
			}

			ctx := readContext(t, tc.cancel)

			_, err := git.New(source, t.TempDir()).Read(ctx)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("actual=%v expected=%v", err, tc.expectedErr)
			}
		})
	}
}

func readContext(t *testing.T, canceled bool) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	if canceled {
		cancel()
	}

	return ctx
}

func newRepository(t *testing.T, root, branch string) *gogit.Repository {
	t.Helper()

	repo, err := gogit.PlainInitWithOptions(root, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName(branch)},
	})
	if err != nil {
		t.Fatal(err)
	}

	return repo
}

func commitFile(t *testing.T, repo *gogit.Repository, root, name, value string) plumbing.Hash {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	if _, err = worktree.Add(name); err != nil {
		t.Fatal(err)
	}

	hash, err := worktree.Commit("test snapshot", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.invalid", When: time.Unix(1700000000, 0)},
	})
	if err != nil {
		t.Fatal(err)
	}

	return hash
}

func assertFile(t *testing.T, name, expected string) {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != expected {
		t.Fatalf("file: actual=%q expected=%q", data, expected)
	}
}
