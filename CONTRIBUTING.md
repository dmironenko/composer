# Contributing

Keep changes small and preserve independent application deployments. Discuss a
substantial behavior change in an issue before adding a new abstraction or dependency.

## Local checks

Use the Go version declared in `go.mod` or newer. A normal checkout requires no
private repositories:

```sh
go generate ./...
go test ./...
go test -race ./...
go vet ./...
go build -o bin/composer ./cmd/composer
```

Mocks use pinned `go:generate` directives. Commit generated test mocks alongside
interface changes. Add tests for behavior changes; documentation-only changes do
not need new tests.

Maintainers use the shared Vixar platform linter directly. If you have that checkout:

```sh
make go/vendor PLATFORM_DIR=/path/to/platform
GOTOOLCHAIN=go1.26.9 make go/lint PLATFORM_DIR=/path/to/platform
GOTOOLCHAIN=go1.26.9 /path/to/platform/bin/golangci-lint run --build-tags=integration ./... \
  --config /path/to/platform/configs/go/golangci.yml
```

Do not copy or weaken the platform configuration. `vendor/` and binaries are local
build artifacts and are not committed. Platform access is not required to submit a contribution;
maintainers run the shared lint checks during review.

## Docker checks

Integration tests create containers, networks, and images. Use a disposable Docker
host or an isolated development installation with free ports:

```sh
go test -tags=integration ./internal/app/composer/adapters/git -count=1
go test -tags=integration ./internal/app/composer/adapters/docker -count=1
go test -race -tags=integration ./cmd/composer -count=1
```

The [example acceptance tests](https://github.com/dmironenko/composer-example/tree/master/test)
cover ingress and environment behavior. Run systemd tests only in a disposable
Ubuntu VM as described in [docs/systemd.md](docs/systemd.md).

Never include live credentials, rendered state, or private configuration in issues,
logs, or pull requests. Test age keys under `internal/app/composer/adapters/gitops/internal/vars/testdata`
are intentionally public fixtures; never use them outside tests.

Secret scanning uses the default Gitleaks rules with exceptions only for those two
fixture identity files. Run `gitleaks git --redact` for history; `gitleaks dir --redact`
also includes local untracked files, so run it in a clean source checkout.
