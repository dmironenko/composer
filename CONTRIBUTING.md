# Contributing

Keep pull requests focused. Describe the behavior change and how you checked it.
Preserve independent app deployments and the boundaries in
[the architecture guide](docs/architecture.md#package-boundaries).

## Local checks

Clone the repository and use the Go version in `go.mod` or newer:

```sh
git clone https://github.com/dmironenko/composer.git
cd composer
```

Run the checks:

```sh
go generate ./...
go test ./...
go test -race ./...
go vet ./...
go build -o bin/composer ./cmd/composer
```

Test behavior changes and commit regenerated mocks with interface changes.
Write documentation and comments in English. For documentation-only changes,
check the commands and links; new tests are not needed.

For installer changes, run `shellcheck install.sh` and
`python3 -m unittest discover -s test/install -v`. The installer tests use local
archives and temporary directories; they do not download or replace your CLI.

Maintainers also run the shared Vixar platform linter through the Git submodule:

```sh
git submodule update --init platform
make go/vendor
GOTOOLCHAIN=go1.26.9 make go/lint
GOTOOLCHAIN=go1.26.9 platform/bin/golangci-lint run --build-tags=integration ./... \
  --config platform/configs/go/golangci.yml
```

Use the platform configuration unchanged. Contributors do not need platform
access; maintainers run these checks during review.

## Docker checks

Integration tests create containers, networks, and images. Use a disposable Docker
host or an isolated development installation with free ports:

```sh
go test -tags=integration ./internal/app/composer/adapters/git -count=1
go test -tags=integration ./internal/app/composer/adapters/docker -count=1
go test -race -tags=integration ./cmd/composer -count=1
```

Run systemd tests only in a disposable Ubuntu VM, as described below.

Keep credentials, rendered state, `vendor/`, and binaries out of commits.
Age keys in the test fixtures are public and must only be used in tests.
Run `gitleaks git --redact` to check for secrets before submitting.

## Release

Commit and push the release changes, including the release workflow, then tag
that commit:

```sh
git tag -a v1.0.0 -m "release v1.0.0"
git push origin v1.0.0
```

The release workflow runs on `v*` tag pushes. It tests the code, checks for known
vulnerabilities, and publishes a GitHub Release with Linux and macOS archives
for amd64 and arm64, plus `checksums.txt`. Each archive contains `composer`,
`LICENSE`, and `README.md`; the binary reports the tag with `composer --version`.
Users do not need Go to run it. No additional GitHub secrets are required.

## Test systemd changes

Run [test/systemd/check.sh](test/systemd/check.sh) as root in a disposable Ubuntu
VM with Docker, Git, and Python 3. Copy the script to
`/var/tmp/systemd-check.sh` and a Linux Composer binary to `/tmp/composer`:

```sh
sudo bash /var/tmp/systemd-check.sh /tmp/composer
# note the printed /var/tmp/composer-systemd.<suffix> directory.
sudo reboot
# after reconnecting:
sudo bash /var/tmp/systemd-check.sh reboot-check /var/tmp/composer-systemd.<suffix>
```

The check covers installation, updates, stop/start, crash recovery, config-only
deployment, preservation of an unrelated unit, and boot startup with the same
application container. Delete the VM when finished.
