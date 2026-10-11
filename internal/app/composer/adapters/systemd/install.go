// Package systemd installs the Composer agent as a Linux system service.
package systemd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	managedHeader  = "# Managed by composer install\n"
	unitMode       = 0o644
	executableMode = 0o755
)

var errPlatform = errors.New("install requires Linux and root; use composer run for foreground operation")

// Install copies the current executable and enables/restarts only this installation's unit.
// The caller validates installation and supplies an absolute, persistent config path.
func Install(ctx context.Context, installation, configPath string, output io.Writer) (string, error) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return "", errPlatform
	}

	control, err := exec.LookPath("systemctl")
	if err != nil {
		return "", fmt.Errorf("find systemctl: %w", err)
	}

	unit := "composer-" + installation + ".service"

	unitPath := filepath.Join("/etc/systemd/system", unit)
	if err = checkOwnership(unitPath); err != nil {
		return "", err
	}

	binary := filepath.Join("/usr/local/lib/composer", installation, "composer")
	if err = installBinary(binary); err != nil {
		return "", err
	}

	if err = writeFile(unitPath, strings.NewReader(renderUnit(binary, configPath)), unitMode); err != nil {
		return "", err
	}

	return unit, activate(ctx, control, unit, output)
}

func activate(ctx context.Context, control, unit string, output io.Writer) error {
	for _, args := range [][]string{{"daemon-reload"}, {"enable", unit}, {"restart", unit}} {
		command := exec.CommandContext(ctx, control, args...)

		command.Stdout, command.Stderr = output, output
		if err := command.Run(); err != nil {
			return fmt.Errorf("systemctl %s: %w", args[0], err)
		}
	}

	return nil
}

func checkOwnership(path string) error {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read existing unit: %w", err)
	}

	if !strings.HasPrefix(string(contents), managedHeader) {
		return fmt.Errorf("refusing to replace unmanaged unit %s: %w", path, os.ErrExist)
	}

	return nil
}

func installBinary(destination string) (err error) {
	path, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open executable: %w", err)
	}

	defer func() { err = errors.Join(err, file.Close()) }()

	if err = os.MkdirAll(filepath.Dir(destination), executableMode); err != nil {
		return fmt.Errorf("create installation directory: %w", err)
	}

	return writeFile(destination, file, executableMode)
}

func writeFile(path string, source io.Reader, mode fs.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".composer-*")
	if err != nil {
		return fmt.Errorf("create installation file: %w", err)
	}
	defer os.Remove(file.Name())

	_, copyErr := io.Copy(file, source)

	err = errors.Join(copyErr, file.Chmod(mode), file.Close())
	if err != nil {
		return fmt.Errorf("write installation file: %w", err)
	}

	// Both paths are inside installation directories derived from the validated installation name.
	if err = os.Rename(file.Name(), path); err != nil { //nolint:gosec // Atomic replacement of the owned file.
		return fmt.Errorf("publish installation file: %w", err)
	}

	return nil
}

func renderUnit(binary, configPath string) string {
	// systemd has its own specifier/variable expansion in addition to C-style quoting.
	quote := func(value string) string {
		return strconv.Quote(strings.NewReplacer("%", "%%", "$", "$$").Replace(value))
	}

	return managedHeader + fmt.Sprintf(`[Unit]
Description=Composer GitOps agent
Wants=network-online.target
After=network-online.target docker.service

[Service]
Type=exec
ExecStart=%s run --config %s
Restart=on-failure
RestartSec=5s
TimeoutStopSec=30s
UMask=0077

[Install]
WantedBy=multi-user.target
`, quote(binary), quote(configPath))
}
