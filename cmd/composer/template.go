package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops"
)

const (
	templateMode          = 0o644
	templateDirectoryMode = 0o755
)

var errOutputRequired = errors.New("--output is required")

func (bootstrap) Export(ctx context.Context, output string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("export template: %w", err)
	}

	return writeTemplate(output)
}

func writeTemplate(output string) (err error) {
	if output == "" {
		return errOutputRequired
	}

	if err = os.MkdirAll(filepath.Dir(output), templateDirectoryMode); err != nil {
		return fmt.Errorf("create template directory: %w", err)
	}

	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, templateMode)
	if err != nil {
		return fmt.Errorf("create template: %w", err)
	}

	defer func() { err = errors.Join(err, file.Close()) }()

	if _, err = file.WriteString(gitops.DefaultTemplate()); err != nil {
		return fmt.Errorf("write template: %w", err)
	}

	return nil
}
