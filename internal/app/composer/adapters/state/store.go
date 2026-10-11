// Package state persists deployment markers and provides private build workspaces.
package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

const markerMode = 0o600

// Store keeps the existing deployment inventory and marker layout on disk.
type Store struct {
	directory    string
	environments []string
}

// New binds state operations to the installation's directory and environments.
func New(directory string, environments []string) *Store {
	return &Store{directory: directory, environments: environments}
}

// List inventories scoped deployments, or all environments when the scope is nil.
func (s *Store) List() ([]domain.Project, error) {
	var projects []domain.Project

	environments, err := s.scope()
	if err != nil {
		return nil, err
	}

	for _, env := range environments {
		root := filepath.Join(s.directory, "deployments", env)

		entries, err := os.ReadDir(root)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("list deployments: %w", err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			projects = append(projects, domain.Project{
				Name: entry.Name(), Environment: env, Directory: filepath.Join(root, entry.Name()),
			})
		}
	}

	return projects, nil
}

// Applied reads the last successfully applied hash, or an empty hash when absent.
func (s *Store) Applied(project domain.Project) (string, error) {
	data, err := os.ReadFile(filepath.Join(project.Directory, "applied"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read applied hash: %w", err)
	}

	return string(data), nil
}

// Attempted reports whether Docker might have created resources for the project.
func (s *Store) Attempted(project domain.Project) (bool, error) {
	_, err := os.Stat(filepath.Join(project.Directory, "attempted"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("read attempted marker: %w", err)
	}

	return true, nil
}

// Invalidate forgets success before an operation that can partially change containers.
func (s *Store) Invalidate(project domain.Project) error {
	err := os.Remove(filepath.Join(project.Directory, "applied"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("invalidate applied hash: %w", err)
	}

	return nil
}

// Attempt records possible resource creation before the Docker operation.
func (s *Store) Attempt(project domain.Project) error {
	if err := os.WriteFile(filepath.Join(project.Directory, "attempted"), nil, markerMode); err != nil {
		return fmt.Errorf("record deployment attempt: %w", err)
	}

	return nil
}

// Save atomically publishes a successful revision.
func (s *Store) Save(project domain.Project) error {
	marker := filepath.Join(project.Directory, "applied")
	if err := os.WriteFile(marker+".tmp", []byte(project.Hash), markerMode); err != nil {
		return fmt.Errorf("write applied hash: %w", err)
	}

	if err := os.Rename(marker+".tmp", marker); err != nil {
		return fmt.Errorf("publish applied hash: %w", err)
	}

	return nil
}

// Remove removes deployment files after Docker confirms project removal.
func (s *Store) Remove(project domain.Project) error {
	if err := os.RemoveAll(project.Directory); err != nil {
		return fmt.Errorf("remove deployment files: %w", err)
	}

	return nil
}

// CreateBuildContext creates a private temporary directory for source checkout.
func (s *Store) CreateBuildContext() (string, error) {
	directory, err := os.MkdirTemp("", "composer-build-")
	if err != nil {
		return "", fmt.Errorf("create build context: %w", err)
	}

	return directory, nil
}

// RemoveBuildContext removes a completed or failed source checkout.
func (s *Store) RemoveBuildContext(directory string) error {
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("remove build context: %w", err)
	}

	return nil
}

func (s *Store) scope() ([]string, error) {
	if s.environments != nil {
		return s.environments, nil
	}

	entries, err := os.ReadDir(filepath.Join(s.directory, "deployments"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("list environments: %w", err)
	}

	var environments []string

	for _, entry := range entries {
		if entry.IsDir() {
			environments = append(environments, entry.Name())
		}
	}

	return environments, nil
}
