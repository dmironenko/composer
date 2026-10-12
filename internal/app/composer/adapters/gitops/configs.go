package gitops

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmironenko/composer/internal/app/composer/domain"
)

type artifact struct {
	Content []byte      `json:"Content"` //nolint:tagliatelle // Preserve existing config hashes across upgrades.
	Mode    fs.FileMode `json:"Mode"`    //nolint:tagliatelle // Preserve existing config hashes across upgrades.
}

func prepareConfigs(files fs.FS, folder string, data templateData, directory string) (string, string, error) {
	configs, err := renderConfigs(files, folder, data)
	if err != nil {
		return "", "", err
	}

	encoded, err := json.Marshal(configs)
	if err != nil {
		return "", "", fmt.Errorf("encode configs: %w", err)
	}

	configHash := fmt.Sprintf("%x", sha256.Sum256(encoded))

	working := filepath.Join(directory, "configs", configHash)
	if publishErr := publish(working, configs); publishErr != nil {
		return "", "", publishErr
	}

	return working, configHash, nil
}

func renderConfigs(files fs.FS, folder string, data templateData) (map[string]artifact, error) {
	result := map[string]artifact{}

	err := fs.WalkDir(files, folder+"/configs", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		relative, file, err := renderConfig(files, folder, name, entry, data)
		if err != nil {
			return err
		}

		if _, exists := result[relative]; exists {
			return fmt.Errorf("%w: duplicate output %s", domain.ErrInvalid, relative)
		}

		result[relative] = file

		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}

	return result, err
}

func renderConfig(files fs.FS, folder, name string, entry fs.DirEntry, data templateData) (string, artifact, error) {
	info, err := entry.Info()
	if err != nil {
		return "", artifact{}, fmt.Errorf("read config %s: %w", name, err)
	}

	content, err := fs.ReadFile(files, name)
	if err != nil {
		return "", artifact{}, fmt.Errorf("read config %s: %w", name, err)
	}

	relative := strings.TrimPrefix(name, folder+"/")
	if strings.HasSuffix(relative, ".tmpl") {
		content, err = execute(relative, string(content), data)
		if err != nil {
			return "", artifact{}, fmt.Errorf("read config %s: %w", name, err)
		}

		relative = strings.TrimSuffix(relative, ".tmpl")
	}

	return relative, artifact{Content: content, Mode: configFileMode | info.Mode()&executableMask}, nil
}

// Publish once: existing containers may still have this directory mounted.
func publish(root string, files map[string]artifact) (err error) {
	if _, statErr := os.Stat(root); statErr == nil {
		return nil
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(root), privateDirMode); mkdirErr != nil {
		return fmt.Errorf("create config parent: %w", mkdirErr)
	}

	temporary, err := os.MkdirTemp(filepath.Dir(root), ".render-")
	if err != nil {
		return fmt.Errorf("create config staging directory: %w", err)
	}

	defer func() { err = errors.Join(err, os.RemoveAll(temporary)) }()

	if writeErr := writeConfigs(temporary, files); writeErr != nil {
		return writeErr
	}

	if renameErr := os.Rename(temporary, root); renameErr != nil {
		return fmt.Errorf("publish configs: %w", renameErr)
	}

	return nil
}

func writeConfigs(root string, files map[string]artifact) error {
	for name, file := range files {
		if err := writeConfig(root, name, file); err != nil {
			return err
		}
	}

	return nil
}

func writeConfig(root, name string, file artifact) error {
	target := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), configDirMode); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	if err := os.WriteFile(target, file.Content, file.Mode); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// restore declared modes after creation: systemd may set umask 0077.
	if err := os.Chmod(target, file.Mode); err != nil {
		return fmt.Errorf("set config permissions: %w", err)
	}

	for directory := filepath.Dir(target); directory != root; directory = filepath.Dir(directory) {
		if err := os.Chmod(directory, configDirMode); err != nil {
			return fmt.Errorf("set config directory permissions: %w", err)
		}
	}

	return nil
}
