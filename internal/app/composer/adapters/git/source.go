// Package git reads committed GitOps trees without changing the user's worktree.
package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

var (
	ErrEmpty  = errors.New("repository has no commits")
	ErrBranch = errors.New("configured branch does not exist")
	ErrFetch  = errors.New("cannot fetch configured git source")
	errMode   = errors.New("unsupported source mode")
)

const privateDirectoryMode = 0o700

// SourceConfig identifies a GitOps repository and its credentials.
type SourceConfig struct {
	Mode   string
	Path   string
	URL    string
	Branch string
	Auth   Auth
}

// Source binds repository configuration and local snapshot storage.
type Source struct {
	config   SourceConfig
	stateDir string
}

// New constructs a Git source without contacting the repository.
func New(source SourceConfig, stateDir string) *Source {
	return &Source{config: source, stateDir: stateDir}
}

// Read returns a fully materialized immutable tree. Failed fetches never become
// empty inventories or silently return an older remote ref. The caller serializes
// access to stateDir with its process lock.
func (s *Source) Read(ctx context.Context) (domain.Snapshot, error) {
	source, stateDir := s.config, s.stateDir

	if err := plumbing.NewBranchReferenceName(source.Branch).Validate(); err != nil {
		return domain.Snapshot{}, ErrBranch
	}

	if err := ctx.Err(); err != nil {
		return domain.Snapshot{}, fmt.Errorf("read source: %w", err)
	}

	if err := os.MkdirAll(stateDir, privateDirectoryMode); err != nil {
		return domain.Snapshot{}, fmt.Errorf("create source directory: %w", err)
	}

	repo, refName, err := openSource(ctx, source, stateDir)
	if err != nil {
		return domain.Snapshot{}, err
	}

	return readSnapshot(ctx, repo, refName, stateDir)
}

func openSource(
	ctx context.Context, source SourceConfig, stateDir string,
) (*gogit.Repository, plumbing.ReferenceName, error) {
	var (
		repo *gogit.Repository
		err  error
	)

	refName := plumbing.NewBranchReferenceName(source.Branch)
	switch source.Mode {
	case "local":
		repo, err = gogit.PlainOpen(source.Path)
	case "remote":
		repo, err = fetch(ctx, source, stateDir)
		refName = plumbing.ReferenceName("refs/remotes/origin/" + source.Branch)
	default:
		return nil, "", fmt.Errorf("%w %q", errMode, source.Mode)
	}

	if err != nil {
		return nil, "", fmt.Errorf("open source: %w", err)
	}

	return repo, refName, nil
}

func readSnapshot(
	ctx context.Context, repo *gogit.Repository, refName plumbing.ReferenceName, stateDir string,
) (domain.Snapshot, error) {
	commit, err := resolveCommit(repo, refName)
	if err != nil {
		return domain.Snapshot{}, err
	}

	tree, err := commit.Tree()
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("read tree: %w", err)
	}

	root := filepath.Join(stateDir, "snapshots", commit.Hash.String())

	exists, err := snapshotExists(root)
	if err != nil {
		return domain.Snapshot{}, err
	}

	if exists {
		return domain.Snapshot{Commit: commit.Hash.String(), Root: root}, nil
	}

	return materialize(ctx, tree, commit.Hash.String(), root)
}

func resolveCommit(repo *gogit.Repository, refName plumbing.ReferenceName) (*object.Commit, error) {
	ref, err := repo.Reference(refName, true)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil, missingReference(repo)
	}

	if err != nil {
		return nil, fmt.Errorf("read reference: %w", err)
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("read commit: %w", err)
	}

	return commit, nil
}

func snapshotExists(root string) (bool, error) {
	info, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("stat snapshot: %w", err)
	}

	return info.IsDir(), nil
}

//nolint:nonamedreturns // Deferred staging cleanup errors must be joined to the operation error.
func materialize(ctx context.Context, tree *object.Tree, commit, root string) (snapshot domain.Snapshot, err error) {
	if err = os.MkdirAll(filepath.Dir(root), privateDirectoryMode); err != nil {
		return domain.Snapshot{}, fmt.Errorf("create snapshot directory: %w", err)
	}

	staging, err := os.MkdirTemp(filepath.Dir(root), ".snapshot-")
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("create snapshot staging directory: %w", err)
	}

	defer func() { err = errors.Join(err, os.RemoveAll(staging)) }()

	if err = exportTree(ctx, tree, staging); err != nil {
		return domain.Snapshot{}, err
	}

	if err = os.Rename(staging, root); err != nil {
		return domain.Snapshot{}, fmt.Errorf("publish snapshot: %w", err)
	}

	return domain.Snapshot{Commit: commit, Root: root}, nil
}

func missingReference(repo *gogit.Repository) error {
	refs, refErr := repo.References()
	if refErr != nil {
		return fmt.Errorf("read references: %w", refErr)
	}

	hasCommit := false

	refErr = refs.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() == plumbing.HashReference && !ref.Hash().IsZero() {
			hasCommit = true
		}

		return nil
	})
	if refErr != nil {
		return fmt.Errorf("read references: %w", refErr)
	}

	if !hasCommit {
		return ErrEmpty
	}

	return ErrBranch
}

func fetch(ctx context.Context, source SourceConfig, stateDir string) (*gogit.Repository, error) {
	key := sha256.Sum256([]byte(source.URL))
	path := filepath.Join(stateDir, "repos", hex.EncodeToString(key[:]))

	repo, err := gogit.PlainOpen(path)
	if errors.Is(err, gogit.ErrRepositoryNotExists) {
		repo, err = gogit.PlainInit(path, true)
	}

	if err != nil {
		return nil, fmt.Errorf("open git object store: %w", err)
	}

	auth, err := authentication(source.Auth, source.URL)
	if err != nil {
		return nil, err
	}
	// An anonymous remote avoids persisting credentials that might be in the URL.
	remote := gogit.NewRemote(repo.Storer, &gitconfig.RemoteConfig{Name: "origin", URLs: []string{source.URL}})

	err = remote.FetchContext(ctx, &gogit.FetchOptions{
		RefSpecs: []gitconfig.RefSpec{
			gitconfig.RefSpec("+refs/heads/" + source.Branch + ":refs/remotes/origin/" + source.Branch),
		},
		Depth: 1, Tags: gogit.NoTags, Force: true, Auth: auth,
	})
	if err = fetchError(ctx, err); err != nil {
		return nil, err
	}

	return repo, nil
}

func fetchError(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil
	}

	if ctx.Err() != nil {
		return fmt.Errorf("fetch source: %w", ctx.Err())
	}

	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return ErrEmpty
	}
	// Transport errors may contain URLs, credentials and server responses.
	return ErrFetch
}
