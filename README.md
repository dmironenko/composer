# Composer

GitOps for Docker Compose, written in Go. Composer polls Git and deploys changed
applications independently, including config-only changes. Revert a commit to roll back.
It runs as a binary on macOS or a systemd service on Ubuntu.

## Build and run

Requires Go 1.26.3 or newer and Docker Engine with embedded BuildKit or Docker
Desktop for deployments. The Compose CLI is not required.

```sh
git clone --branch master https://github.com/dmironenko/composer.git
cd composer
go build -o bin/composer ./cmd/composer
bin/composer --help
```

Follow [composer-example](https://github.com/dmironenko/composer-example) to run a
local stack and deploy changes from commits. To create an empty GitOps repository:

```sh
bin/composer init ../my-gitops --branch master --config ./composer.local.yaml
```

`init` creates files only. Initialize Git in `../my-gitops` and commit your apps,
then deploy:

```sh
bin/composer validate --config ./composer.local.yaml
bin/composer apply --config ./composer.local.yaml --wait --timeout 15m
bin/composer run --config ./composer.local.yaml
```

`run` polls for commits until stopped. Containers keep running after it exits.

## Repository layout

```text
apps/<namespace>/<app>/
  vars/base.yaml
  vars/stage.yaml            # optional; enables non-production environments
  configs/config.yaml.tmpl   # any config format; .tmpl selects Go templates
  Dockerfile.tmpl            # optional source build
templates/compose.yaml.tmpl  # optional replacement for the default template
```

Each app is an independent Compose project with one or more services. Each
namespace/environment pair has its own Docker network, created before deployments.
Keep fixed host ports and `container_name` out of services that need multiple replicas.

- `prod` reads `base.yaml`; other environments add overrides from `stage.yaml`.
- Missing `stage.yaml` disables the app outside prod; `{}` enables it.
- `enabled: false` disables the project.
- Maps merge recursively, lists replace lists, and `null` removes a key.

The default Compose template sets `restart: always` and the namespace network.
Export it to customize shared settings:

```sh
bin/composer template export compose --output ../my-gitops/templates/compose.yaml.tmpl
```

Config templates use Go templates. For example, `{{ .Vars.app | toYAML }}` renders
an entire `app` block. Mount the result as
`./configs/config.yaml:/etc/app/config.yaml:ro` and use the container path in your
app's command. Files without `.tmpl` are copied unchanged.

## Environments

Keep host YAML outside the GitOps repository. Paths are relative to that file:

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

Create the selected branches before starting Composer. Each environment reads its
whole branch, including configs, Dockerfiles, and templates. Local sources read
commits without changing your working tree. Remote sources use shallow fetches
and support SSH keys, HTTPS token files, or anonymous access.

Test on `dev`, then merge into `master` to deploy to prod. Branch names are
configurable. Add `alice: users/alice` for another environment; `run` reloads
host YAML each pass. Routing between environments belongs in your GitOps templates.

## Deployment behavior

`validate` checks rendered Compose documents; `plan` compares them with the last
successful deployment. Neither contacts Docker. `apply --wait` retries a fixed
snapshot; `run` fetches new commits and retries failed projects on each pass.
Unchanged projects are skipped. Scaling preserves existing replicas.

Deleting an app or environment, removing `stage.yaml`, or setting `enabled: false`
removes the affected containers. Volumes, images, and namespace networks remain.
Fetch and rendering errors preserve existing deployments.

Composer relies on stored state to track deployments and does not repair manual
Docker changes. Use one agent per state directory. Services must tolerate
dependencies starting later; there is no cross-project startup ordering or
zero-downtime guarantee.

Use trusted GitOps repositories: they have Docker-level privileges. Composer
encrypts secrets with age for storage in Git. Keep the private key outside Git
and protect the state directory, which contains decrypted configs.

## Guides

Detailed guides are currently in Russian:

- [Architecture and deployment rules](docs/architecture.md)
- [Source builds and Docker image caching](docs/builds.md)
- [Encrypted secrets](docs/secrets.md)
- [Ubuntu and systemd](docs/systemd.md)

See [CONTRIBUTING.md](CONTRIBUTING.md) for development checks.
