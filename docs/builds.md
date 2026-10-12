# Build applications from Git

Set the application version in the GitOps repository. Its `Dockerfile.tmpl`
clones the source and builds the application. Composer runs the build through
Docker, which stores the images and build cache.

## Add a build

In `apps/<namespace>/<app>/vars/base.yaml`:

```yaml
build:
  repository: https://github.com/example/api.git
  tag: v1.0.0

app:
  listen: :8080

compose:
  services:
    api:
      command: /app/api --config /etc/app/config.yaml
      volumes: [./configs/config.yaml:/etc/app/config.yaml:ro]
      deploy:
        replicas: 2
```

Replace the repository and tag with your own. `build.tag` accepts a commit SHA,
tag, or branch. The Dockerfile resolves it through Git. Repository URLs can use
HTTPS or SSH, including `git@host:org/repo.git`; private-source authentication
has the limits described below.

Create `Dockerfile.tmpl` alongside `vars`. This example expects a Go main package
at the root of the source repository:

```dockerfile
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
RUN git clone {{ quote .Vars.build.repository }} . \
    && git checkout {{ quote .Vars.build.tag }} \
    && CGO_ENABLED=0 go build -o /out/api .

FROM scratch
LABEL org.opencontainers.image.revision={{ quote .Vars.build.tag }}
COPY --from=builder /out/api /app/api
```

Create `configs/config.yaml.tmpl` to write the application settings as YAML:

```gotemplate
{{ .Vars.app | toYAML }}
```

Composer renders `.Vars.build.repository` and `.Vars.build.tag` before building.
It passes no build arguments and uses an empty build context. Download source
inside the Dockerfile; `COPY . .` cannot read GitOps or host files.

## Update and roll back

Change `build.tag` or the Dockerfile and commit. With `prod: master` and
`stage: dev`, test the change on `dev`, then merge into `master`. Identical build
inputs use the same local image in both environments.

The image name is `composer/build:<sha256>`, derived from the repository, tag,
and rendered Dockerfile. Composer reuses an existing image and combines
concurrent requests for the same build. Docker retains images and build cache
until you remove them.

Composer watches the GitOps repository. Moving a source branch or tag without
changing the GitOps files does not trigger a build or invalidate an existing
image. Use commit SHAs or tags that stay fixed for reproducible deployments.
Changes behind a base-image tag also do not trigger a rebuild.

A failed checkout or build leaves the running project unchanged. A config-only
change recreates containers with the existing image unless it also changes the
rendered Dockerfile. Reverting a GitOps commit reuses the previous image if it
is still available; otherwise Composer builds it again.

The default Compose template assigns the built image and `pull_policy: never`
to services without an explicit `image`. Multiple services can share that image.
A custom Compose template must use `.Runtime.Image` and `pull_policy: never`;
keep copied templates up to date yourself.

To use a different version in every non-prod environment, override it in
`vars/stage.yaml`:

```yaml
build:
  tag: v1.1.0
```

This override survives a merge into the production branch. Remove it to use the
version from `base.yaml`, keeping `stage.yaml` at least as `{}` to enable staging.

## Requirements and limits

Docker Engine must expose its embedded BuildKit endpoints, `/grpc` and
`/session`. Registry credentials come from Docker's configuration; configured
credential helpers must be installed. The builder image needs its own `git`
executable, as shown above.

`source.auth` authenticates access to the GitOps repository only. Composer does
not forward keys, SSH agents, or tokens into builds, and does not expose BuildKit
secret or SSH mounts. Private source clones using those credentials are not
supported. Do not put credentials in Dockerfile variables: they become build
instructions.

`composer validate` and `composer plan` render and check the Dockerfile without
accessing the application repository or Docker. Builds run during `apply` and
`run`. For long builds, increase `reconcile.deploy_timeout` in the
[host configuration](configuration.md) and `composer apply --timeout`.
Build integration checks are listed in [CONTRIBUTING.md](../CONTRIBUTING.md).
