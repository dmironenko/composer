//go:build integration

package git_test

import (
	"path/filepath"
	"testing"

	"github.com/dmironenko/composer/v2/internal/app/composer/adapters/git"
	"github.com/dmironenko/composer/v2/test/gitserver"
)

func TestSourceSSH(t *testing.T) {
	root := t.TempDir()
	repo := newRepository(t, root, "main")
	commit := commitFile(t, repo, root, "config.txt", "first")
	repository, auth := gitserver.New(t, root)
	t.Setenv("PATH", t.TempDir())

	source := git.New(git.SourceConfig{
		Mode: "remote", URL: repository, Branch: "main", Auth: git.Auth(auth),
	}, t.TempDir())

	initial, err := source.Read(t.Context())
	if err != nil || initial.Commit != commit.String() {
		t.Fatalf("SSH snapshot: actual=%v err=%v", initial, err)
	}

	assertFile(t, filepath.Join(initial.Root, "config.txt"), "first")
	commitFile(t, repo, root, "config.txt", "second")

	updated, err := source.Read(t.Context())
	if err != nil || updated.Commit == initial.Commit {
		t.Fatalf("SSH update: actual=%v err=%v", updated, err)
	}

	assertFile(t, filepath.Join(updated.Root, "config.txt"), "second")
}
