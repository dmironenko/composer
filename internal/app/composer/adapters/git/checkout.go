package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

const sshProtocol = "ssh"

// ErrSubmodules reports unsupported gitlinks in an application source tree.
var ErrSubmodules = errors.New("build source submodules are not supported")

// Repository checks out application source using explicitly configured credentials.
type Repository struct {
	auth Auth
}

// NewRepository constructs a build source reader without contacting Git.
func NewRepository(auth Auth) *Repository {
	return &Repository{auth: auth}
}

// Checkout materializes the requested full commit hash in an empty directory.
// The caller owns the directory and removes it after the image build.
func (r *Repository) Checkout(ctx context.Context, repository, commit, directory string) error {
	auth, err := r.buildAuthentication(repository)
	if err != nil {
		return err
	}

	repo, err := gogit.PlainInit(directory, false)
	if err != nil {
		return fmt.Errorf("initialize build source: %w", err)
	}

	remote := gogit.NewRemote(repo.Storer, &gitconfig.RemoteConfig{Name: "origin", URLs: []string{repository}})
	if err = remote.FetchContext(ctx, &gogit.FetchOptions{
		RefSpecs: []gitconfig.RefSpec{gitconfig.RefSpec("+" + commit + ":refs/composer/build")},
		Depth:    1, Tags: gogit.NoTags, Auth: auth,
	}); err != nil {
		return fmt.Errorf("fetch build commit: %w", err)
	}

	if err = checkoutCommit(repo, plumbing.NewHash(commit)); err != nil {
		return err
	}

	if err = os.RemoveAll(filepath.Join(directory, ".git")); err != nil {
		return fmt.Errorf("remove build source metadata: %w", err)
	}

	return nil
}

func checkoutCommit(repo *gogit.Repository, commit plumbing.Hash) error {
	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("open build worktree: %w", err)
	}

	if err = worktree.Checkout(&gogit.CheckoutOptions{Hash: commit}); err != nil {
		return fmt.Errorf("checkout build commit: %w", err)
	}

	index, err := repo.Storer.Index()
	if err != nil {
		return fmt.Errorf("read build source index: %w", err)
	}

	for _, entry := range index.Entries {
		if entry.Mode == filemode.Submodule {
			return fmt.Errorf("%w: %s", ErrSubmodules, entry.Name)
		}
	}

	return nil
}

func (r *Repository) buildAuthentication(repository string) (transport.AuthMethod, error) {
	endpoint, err := transport.NewEndpoint(repository)
	if err != nil {
		return nil, fmt.Errorf("parse build repository: %w", err)
	}

	if endpoint.Protocol == sshProtocol && r.auth.Type != sshProtocol {
		return nil, errAuth
	}

	return authentication(r.auth, repository)
}
