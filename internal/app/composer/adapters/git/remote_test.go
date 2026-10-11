//go:build integration

package git_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/dmironenko/composer/internal/app/composer/adapters/git"
)

// go-git's file transport starts git-upload-pack. Keep this separate from the
// hermetic unit suite; it exercises real fetch negotiation without a server.
//
//nolint:gocognit,gocyclo,cyclop,funlen,maintidx // One lifecycle verifies fetch, updates, revert and failure.
func TestRemoteFetch(t *testing.T) {
	t.Parallel()

	for _, branch := range []string{"master", "main", "release/production"} {
		t.Run(branch, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			repo := newRepository(t, root, branch)

			first := commitFile(t, repo, root, "config.txt", "first")
			for _, ref := range []string{"refs/heads/unselected", "refs/tags/unselected"} {
				if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(ref), first)); err != nil {
					t.Fatal(err)
				}
			}

			source := git.SourceConfig{Mode: "remote", URL: root, Branch: branch}
			state := t.TempDir()

			initial, err := git.New(source, state).Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			unchanged, err := git.New(source, state).Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			if initial != unchanged {
				t.Fatal("unchanged ref produced a different snapshot")
			}

			store, err := gogit.PlainOpen(filepath.Join(state, "repos", fmt.Sprintf("%x", sha256.Sum256([]byte(root)))))
			if err != nil {
				t.Fatal(err)
			}

			refs, err := store.References()
			if err != nil {
				t.Fatal(err)
			}

			if err = refs.ForEach(func(ref *plumbing.Reference) error {
				if ref.Type() == plumbing.HashReference && ref.Name() != plumbing.ReferenceName("refs/remotes/origin/"+branch) {
					t.Errorf("fetched unrelated ref: %s", ref.Name())
				}

				return nil
			}); err != nil {
				t.Fatal(err)
			}

			second := commitFile(t, repo, root, "config.txt", "second")

			updated, err := git.New(source, state).Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			if updated.Commit != second.String() {
				t.Fatal("fetch missed new commit")
			}

			assertFile(t, filepath.Join(updated.Root, "config.txt"), "second")

			branchRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), first)
			if err = repo.Storer.SetReference(branchRef); err != nil {
				t.Fatal(err)
			}

			reverted, err := git.New(source, state).Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			if reverted != initial {
				t.Fatal("force-updated ref did not restore cached snapshot")
			}

			if err := os.Rename(filepath.Join(root, ".git"), filepath.Join(root, "unavailable")); err != nil {
				t.Fatal(err)
			}

			if _, err := git.New(source, state).Read(t.Context()); !errors.Is(err, git.ErrFetch) {
				t.Fatalf("failed fetch must not return stale state: %v", err)
			}
		})
	}
}
