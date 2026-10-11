# Composer

GitOps for Docker Compose, written in Go. Composer polls Git, renders configuration,
and applies changed Compose projects independently. Revert a Git commit to roll back.

Applications remain ordinary Compose services: Composer has no built-in knowledge
of nginx, databases, or your application framework. It runs as a binary on macOS
with Docker Desktop, or as a systemd service on Ubuntu.

## Build

Requires Go 1.26.3 or newer. Docker Engine with its embedded BuildKit, or Docker
Desktop, is required for deployments. The Compose CLI is not required by Composer.

```sh
git clone --branch master https://github.com/dmironenko/composer.git
cd composer
go build -o bin/composer ./cmd/composer
bin/composer --help
```

Builds and Go tests do not require the maintainers' platform repository.
See [CONTRIBUTING.md](CONTRIBUTING.md) for development checks.

## Try it locally

The separate [composer-example](https://github.com/dmironenko/composer-example)
repository includes HTTP services, nginx routing, and Certbot. Its README walks
through starting the stack with one command and watching local commits.

To start an empty GitOps repository instead:

```sh
bin/composer init ../my-gitops --branch master --config ./composer.local.yaml
mkdir -p ../my-gitops/apps/demo/web/vars
```

Create `../my-gitops/apps/demo/web/vars/base.yaml`:

```yaml
compose:
  services:
    web:
      image: nginx:alpine
      deploy:
        replicas: 2
```

Commit it, then deploy:

```sh
git -C ../my-gitops init -b master
git -C ../my-gitops add .
git -C ../my-gitops commit -m 'Add web service'
bin/composer validate --config ./composer.local.yaml
bin/composer apply --config ./composer.local.yaml --wait --timeout 15m
bin/composer run --config ./composer.local.yaml
```

This service has no published host port. Access it through a proxy in its namespace
network, as the example repository demonstrates. Stop `run` with Ctrl+C; containers
keep running. `init` creates the directory layout and host configuration, not a Git commit.

## Repository layout

```text
apps/<namespace>/<app>/
  vars/base.yaml
  vars/stage.yaml            # optional; enables non-production environments
  configs/config.yaml.tmpl   # any config format; .tmpl selects Go templates
  Dockerfile.tmpl            # optional source build
templates/compose.yaml.tmpl  # optional replacement for the default template
```

Each application directory becomes an independent Compose project with one or more
services. Each namespace/environment pair gets a separate Docker network. Networks
are created before parallel deployments, including networks requested by templates.
Keep fixed host ports and `container_name` out of services that need multiple replicas.

- `prod` reads `base.yaml`; every other environment reads `base.yaml` plus `stage.yaml`.
- A missing `stage.yaml` disables the application outside production; `{}` enables it.
- `enabled: false` disables a project. `prod.yaml` is not used.
- Maps merge recursively, lists replace previous lists, and `null` removes a key.
- The default Compose template sets `restart: always` and the namespace network.

Export the default template to customize shared settings:

```sh
bin/composer template export compose --output ../my-gitops/templates/compose.yaml.tmpl
```

## Environments and Git

Host YAML is kept outside the GitOps repository. Paths are relative to that file:

```yaml
version: 1
installation: my-stack
source:
  mode: local
  path: my-gitops
  branches:
    prod: master
    stage: dev
  poll_interval: 10s
state_dir: my-stack-state
docker: {}
reconcile:
  parallelism: 4
  git_timeout: 30s
  deploy_timeout: 5m
```

Create each branch before selecting it. Branch names are arbitrary, including
`main`. Local sources read committed snapshots without changing your working tree.
Remote sources use shallow fetches without tags and support SSH keys with
`known_hosts`, HTTPS token files, or anonymous access.

Each environment reads its entire branch: vars, configs, Dockerfiles, and templates.
Make changes on `dev`, inspect stage, then merge into `master` to promote them.
Add `alice: users/alice` to `source.branches` for a personal environment. Names start
with a lowercase letter and contain lowercase letters, digits, or hyphens, up to
63 characters. Routing to personal environments is implemented by the example's templates.

`run` reloads host YAML each pass. Invalid configuration retains the last accepted
settings. Changes to `installation`, `state_dir`, `docker`, or `build_auth` require
an agent restart. Removing an environment removes its containers, including after
an agent restart. A missing branch or failed fetch preserves its existing deployment.

## Deployment behavior

`validate` renders and validates Compose documents. `plan` compares them with stored
successful hashes; neither contacts Docker, though both may write local rendered
configuration. `apply --wait` retries a fixed snapshot until success or timeout.
`run` fetches new snapshots each pass and retries failed projects independently.
Reports are JSON lines; errors go to stderr.

Config-only changes update content-addressed config mounts and a service label,
so processes that read configuration only at startup receive it. Scaling alone
preserves existing replicas. Successful unchanged projects are skipped.

Deleting an app, removing its `stage.yaml`, or setting `enabled: false` removes the
affected project's containers. Volumes, images, and namespace networks are retained.
Rendering and fetch errors do not count as deletions. Git revert triggers deployment
of the reverted configuration; there is no separate rollback state machine.

Composer does not continuously repair manual Docker changes. Docker handles restart
policies. To explicitly reapply a project, stop Composer and remove its
`state_dir/deployments/<env>/<project>/applied` marker. Losing the whole state directory
also loses inventory of projects already removed from Git. Use one agent per state directory.

Services must tolerate dependencies starting later. Compose handles readiness;
`apply --wait` and `run` retry unsuccessful operations. There is no cross-project
startup ordering or promise of zero-downtime deployment.

## Templates, secrets, and builds

Templates receive `.Vars`, `.Secrets`, `.Installation`, `.Env`, `.Environments`,
`.Namespace`, `.App`, and `.Runtime` (`ProjectName`, `NetworkName`, `Image`). Helpers:
`toYAML`, `toJSON`, `quote`, `indent`, `get`, `default`, `dict`, `list`, `merge`, `network`.
For a complete YAML config from an `app` block, use `{{ .Vars.app | toYAML }}`.
Mount it as `./configs/config.yaml:/etc/app/config.yaml:ro` and use the container path
in your application's command. Files without `.tmpl` are copied unchanged.

SOPS with age encrypts vars in Git. Set `age_key_file` in host YAML and keep the
private key outside Git. Rendered configs contain plaintext secrets: protect the
state directory and backups. Config files use mode `0444` (`0555` for executables),
inside a state directory with mode `0700`. Compose interpolation is disabled, so
`$` reaches applications literally; Go templates perform substitutions.

For source builds, set `build.repository` and a full 40-character `build.commit`,
and add `Dockerfile.tmpl`. Composer uses go-git and Docker's embedded BuildKit.
Docker owns image and build caching; retained images allow quick reverts. A failed
build leaves the previous project running. Source credentials use `build_auth`,
separately from GitOps `source.auth`.

GitOps repositories are trusted input with Docker-level privileges. Do not point
Composer at untrusted repositories or pull-request branches. The example routing
cookie is a traffic selector, not application authentication.

## Ubuntu service

```sh
sudo ./bin/composer install --config /etc/composer/host.yaml
```

The generated service runs as root. Docker, repository credentials, and the age key
must already be available. On macOS, use `composer run` in the foreground.

Detailed guides (currently in Russian): [architecture](docs/architecture.md),
[builds](docs/builds.md), [secrets](docs/secrets.md), [systemd](docs/systemd.md).
The nginx/Certbot example has been exercised on Docker Desktop, including local
ACME renewal. Public ACME with real DNS and Ubuntu reboot acceptance of the current
ingress setup remain separate deployment checks.
