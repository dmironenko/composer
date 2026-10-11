//go:build integration

package git_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/dmironenko/composer/internal/app/composer/adapters/git"
)

func TestCheckoutCommit(t *testing.T) {
	t.Parallel()

	for _, bare := range []bool{false, true} {
		t.Run(map[bool]string{false: "worktree", true: "bare"}[bare], func(t *testing.T) {
			t.Parallel()

			root, commit := checkoutFixture(t)
			if bare {
				root = filepath.Join(root, ".git")
			}

			directory := t.TempDir()
			if err := git.NewRepository(git.Auth{}).Checkout(t.Context(), root, commit.String(), directory); err != nil {
				t.Fatal(err)
			}

			assertFile(t, filepath.Join(directory, "version.txt"), "selected")
			assertCheckoutFiles(t, directory)
		})
	}
}

func TestCheckoutMissingCommit(t *testing.T) {
	t.Parallel()

	root, _ := checkoutFixture(t)

	err := git.NewRepository(git.Auth{}).Checkout(t.Context(), root, plumbing.ZeroHash.String(), t.TempDir())
	if err == nil {
		t.Fatal("checkout missing commit: expected an error")
	}
}

//nolint:gocyclo,cyclop,maintidx // Build one committed source fixture with native file modes.
func checkoutFixture(t *testing.T) (string, plumbing.Hash) {
	t.Helper()

	root := t.TempDir()
	repo := newRepository(t, root, "release/source")
	allowCommitFetch(t, repo)
	commitFile(t, repo, root, "version.txt", "selected")

	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(filepath.Join(root, "run.sh"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("version.txt", filepath.Join(root, "version-link")); err != nil {
		t.Fatal(err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"run.sh", "version-link"} {
		if _, err = worktree.Add(name); err != nil {
			t.Fatal(err)
		}
	}

	commit, err := worktree.Commit("source files", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.invalid"},
	})
	if err != nil {
		t.Fatal(err)
	}

	commitFile(t, repo, root, "version.txt", "branch moved")

	return root, commit
}

func assertCheckoutFiles(t *testing.T, directory string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(directory, ".git")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("build metadata: actual=%v expected=%v", err, fs.ErrNotExist)
	}

	info, err := os.Stat(filepath.Join(directory, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm()&0o100 == 0 {
		t.Fatal("checkout lost executable permission")
	}

	link, err := os.Readlink(filepath.Join(directory, "version-link"))
	if err != nil {
		t.Fatal(err)
	}

	if link != "version.txt" {
		t.Fatalf("symlink target: actual=%q expected=%q", link, "version.txt")
	}
}

//nolint:gocyclo,cyclop,maintidx // A real gitlink requires encoding a tree and commit.
func TestCheckoutSubmodule(t *testing.T) {
	t.Parallel()

	root, commit := checkoutFixture(t)

	repo, err := gogit.PlainOpen(root)
	if err != nil {
		t.Fatal(err)
	}

	tree := &object.Tree{Entries: []object.TreeEntry{{Name: "dependency", Mode: filemode.Submodule, Hash: commit}}}

	encoded := repo.Storer.NewEncodedObject()
	if err = tree.Encode(encoded); err != nil {
		t.Fatal(err)
	}

	treeHash, err := repo.Storer.SetEncodedObject(encoded)
	if err != nil {
		t.Fatal(err)
	}

	submoduleCommit := &object.Commit{TreeHash: treeHash, ParentHashes: []plumbing.Hash{commit}, Message: "submodule"}

	encoded = repo.Storer.NewEncodedObject()
	if err = submoduleCommit.Encode(encoded); err != nil {
		t.Fatal(err)
	}

	hash, err := repo.Storer.SetEncodedObject(encoded)
	if err != nil {
		t.Fatal(err)
	}

	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName("submodule"), hash)
	if err = repo.Storer.SetReference(ref); err != nil {
		t.Fatal(err)
	}

	err = git.NewRepository(git.Auth{}).Checkout(t.Context(), root, hash.String(), t.TempDir())
	if !errors.Is(err, git.ErrSubmodules) {
		t.Fatalf("checkout submodule: actual=%v expected=%v", err, git.ErrSubmodules)
	}
}

func allowCommitFetch(t *testing.T, repo *gogit.Repository) {
	t.Helper()

	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}

	cfg.Raw.SetOption("uploadpack", "", "allowReachableSHA1InWant", "true")

	if err = repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
}
