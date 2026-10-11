//go:build integration

package git_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmironenko/composer/internal/app/composer/adapters/git"
	"github.com/dmironenko/composer/test/gitserver"
)

func TestCheckoutSSH(t *testing.T) {
	root, commit := checkoutFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	repository, auth := gitserver.New(t, root)
	t.Setenv("PATH", t.TempDir())

	directory := t.TempDir()
	if err := git.NewRepository(git.Auth(auth)).Checkout(ctx, repository, commit.String(), directory); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(directory, "version.txt"), "selected")
	assertCheckoutFiles(t, directory)
}

func TestCheckoutRequiresSSHCredentials(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/nonexistent/composer-test-agent")

	err := git.NewRepository(git.Auth{Type: "none"}).Checkout(
		t.Context(), "ssh://git@127.0.0.1:1/source.git", strings.Repeat("1", 40), t.TempDir(),
	)
	if err == nil || err.Error() != "unsupported git authentication" {
		t.Fatalf("SSH without explicit credentials: actual=%v expected=unsupported git authentication", err)
	}
}

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
