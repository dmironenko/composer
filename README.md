# Composer

GitOps for Docker Compose. Composer watches Git branches and deploys changed apps
independently, including config-only changes. Revert a commit to roll back.
Runs on macOS and Linux, with systemd installation on Ubuntu.

## Install

Requires [Go 1.26.8 or newer](https://go.dev/dl/):

```sh
go install github.com/dmironenko/composer/cmd/composer@latest
```

Make sure Go's binary directory is on your `PATH` (by default, `$HOME/go/bin`).
If `composer` is not found, add `export PATH="$HOME/go/bin:$PATH"` to your shell
profile and open a new terminal. Repeat the install command to update.

Deployments need Docker Engine with embedded BuildKit or Docker Desktop running.
The Docker Compose CLI and the Vixar platform repository are not required.

## First deployment

This creates a GitOps repository with one nginx app. Run it in a new directory;
Git must be installed and configured to create commits.

```sh
composer init my-gitops --branch master --installation my-stack --config composer.yaml
mkdir -p my-gitops/apps/demo/nginx/vars
cat > my-gitops/apps/demo/nginx/vars/base.yaml <<'YAML'
compose:
  services:
    nginx:
      image: nginx:1.28-alpine
YAML
git -C my-gitops init -b master
git -C my-gitops add .
git -C my-gitops commit -m 'feat: add nginx'
composer apply --config composer.yaml --wait
composer run --config composer.yaml
```

`apply` deploys the committed snapshot. `run` keeps polling for new commits;
Ctrl-C stops the agent and leaves containers running. This app publishes no host
ports. Use `docker ps --filter name=my-stack-prod-demo-nginx`
to see its container.

`init` creates an empty `apps/` directory and a host config outside the repository.
It points both environments at the chosen branch; this app runs only in prod
because it has no `stage.yaml`. To test changes on `dev`, follow
[environments](docs/configuration.md#environments).

For a browser-accessible stack with TLS, secrets, and stage routing, use
[composer-example](https://github.com/dmironenko/composer-example#quick-start).

## Everyday use

Edit app files, commit, and let `composer run` deploy them. With a remote source,
push the commit too. Uncommitted edits are ignored.

| Command | Purpose |
| --- | --- |
| `composer config check` | Check host YAML without contacting Git or Docker |
| `composer validate` | Fetch and validate committed app definitions without Docker |
| `composer plan` | Compare them with the last successful deployment without Docker |
| `composer apply --wait` | Deploy a snapshot and retry until ready or timeout |
| `composer run` | Poll Git and deploy changes continuously |

Commands read `composer.yaml` by default; use `--config PATH` for another file.
See `composer --help` or `composer COMMAND --help` for flags.
On Ubuntu, [install the systemd service](docs/systemd.md) to keep the agent running.

## How apps are organized

```text
apps/<namespace>/<app>/
  vars/base.yaml            # shared settings, including production
  vars/stage.yaml           # optional overrides; enables non-prod environments
  configs/config.yaml.tmpl  # optional config in any text format
  Dockerfile.tmpl           # optional source build
templates/compose.yaml.tmpl # optional shared Compose template
```

Each app is a separate Compose project with one or more services. Each
namespace/environment pair gets its own network. The environment named `prod`
uses `base.yaml`; every other environment adds `stage.yaml`. Branch names are
configured explicitly and can be `master`, `main`, or any other branch.

Removing an app removes its containers. Volumes, images, and namespace networks
remain. Composer tracks deployments in its state directory; it does not repair
manual Docker changes. Services must tolerate dependencies starting later.
Deployments can briefly interrupt the changed app.

## Guides

- [Add, configure, scale, or remove an app](docs/apps.md)
- [Git authentication, branches, and host settings](docs/configuration.md)
- [Build an app from source](docs/builds.md)
- [Use encrypted secrets](docs/secrets.md)
- [Run on Ubuntu with systemd](docs/systemd.md)

For contributors: [development checks](CONTRIBUTING.md) and
[architecture](docs/architecture.md).
