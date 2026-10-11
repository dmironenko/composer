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
