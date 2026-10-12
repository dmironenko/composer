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

Maintainers also run the shared Vixar platform linter:

```sh
make go/vendor PLATFORM_DIR=/path/to/platform
GOTOOLCHAIN=go1.26.9 make go/lint PLATFORM_DIR=/path/to/platform
GOTOOLCHAIN=go1.26.9 /path/to/platform/bin/golangci-lint run --build-tags=integration ./... \
  --config /path/to/platform/configs/go/golangci.yml
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

Run systemd tests only in a disposable Ubuntu VM as described in
[docs/systemd.md](docs/systemd.md).

Keep credentials, rendered state, `vendor/`, and binaries out of commits.
Age keys in the test fixtures are public and must only be used in tests.
Run `gitleaks git --redact` to check for secrets before submitting.
