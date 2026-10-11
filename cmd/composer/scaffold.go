package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Create refuses nonempty targets except for an existing .git directory.
func createRepository(target, configPath, branch, installation string) error {
	root, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve repository path: %w", err)
	}

	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("resolve host config path: %w", err)
	}

	if err = checkConfig(configPath); err != nil {
		return err
	}

	if err = checkDirectory(root); err != nil {
		return err
	}

	cfg := defaultConfig()
	cfg.Version, cfg.Installation = 1, installation
	cfg.Source.Mode, cfg.Source.Path = "local", root
	cfg.Source.Branches = map[string]string{"prod": branch, "stage": branch}

	cfg.StateDir = root + "-state"
	if err = cfg.Validate(); err != nil {
		return fmt.Errorf("validate host config: %w", err)
	}

	if err := writeNew(filepath.Join(root, "apps", ".gitkeep"), nil); err != nil {
		return err
	}

	return writeConfig(configPath, cfg)
}

func checkConfig(configPath string) error {
	_, err := os.Lstat(configPath)
	if err == nil {
		return fmt.Errorf("config already exists: %w", fs.ErrExist)
	}

	if !os.IsNotExist(err) {
		return fmt.Errorf("inspect host config: %w", err)
	}

	return nil
}

func checkDirectory(root string) error {
	entries, err := os.ReadDir(root)
	if !errors.Is(err, fs.ErrNotExist) && err != nil {
		return fmt.Errorf("read target: %w", err)
	}

	for _, entry := range entries {
		if entry.Name() != ".git" || !entry.IsDir() {
			return fmt.Errorf("target is not empty: %w", fs.ErrExist)
		}
	}

	return nil
}

func writeNew(path string, data []byte) (err error) {
	const (
		fileMode      = 0o600
		directoryMode = 0o755
	)

	if err = os.MkdirAll(filepath.Dir(path), directoryMode); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}

	defer func() { err = errors.Join(err, file.Close()) }()

	_, err = file.Write(data)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

func writeConfig(path string, cfg hostConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode host config: %w", err)
	}

	return writeNew(path, data)
}
