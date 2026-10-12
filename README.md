![views](https://komarev.com/ghpvc/?username=dmironenko-composer&label=views&color=blue&abbreviated=true&style=flat)

# Composer

Composer deploys Docker Compose apps from Git. It polls your repository and applies
changes, including config updates. Each app has its own Compose project, so updating
one app leaves unrelated containers running. Revert a commit to roll back.

Map Git branches to prod and staging environments on the same Docker host.
Composer handles fetching and deployment; GitHub Actions is optional.

## Install

On macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/dmironenko/composer/master/install.sh | sh -s -- v1.0.0
```

The [installer](install.sh) detects your architecture, checks SHA256, and installs
to `~/.local/bin` without sudo. Go is not required. Change the version to update.

If needed, add `~/.local/bin` to your PATH. For bash or zsh:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Add that line to your shell profile to keep it. For fish, use
`fish_add_path ~/.local/bin`. Check the installation with `composer --version`.

## Try it locally

You need Git and a running Docker Engine or Docker Desktop. The commands below
use bash or zsh and port `18081`; choose another port if it is busy.

### 1. Create a repository and app

```sh
mkdir composer-demo
cd composer-demo
composer init example --branch master --installation example
cd example
git init -b master

mkdir -p apps/demo/nginx/vars apps/demo/nginx/configs
cat > apps/demo/nginx/vars/base.yaml <<'YAML'
compose:
  services:
    nginx:
      image: nginx:1.28-alpine
      ports:
        - 127.0.0.1:18081:80
      volumes:
        - ./configs/index.html:/usr/share/nginx/html/index.html:ro
YAML
printf '%s\n' 'Hello from Composer' > apps/demo/nginx/configs/index.html

git add .
git commit -m 'feat: add nginx'
```

Apps live at `apps/<namespace>/<app>`. Put Docker Compose settings under `compose`
in `vars/base.yaml`. The host config (`../composer.yaml`) and deployment state
(`../example-state/`) stay outside Git.

### 2. Deploy

From the `example` directory:

```sh
composer apply --config ../composer.yaml --wait
curl http://127.0.0.1:18081/
```

You should see `Hello from Composer`. Start watching for commits:

```sh
composer run --config ../composer.yaml
```

The default poll interval is one minute. For faster local feedback, set
`poll_interval` to `2s` under `source` in `../composer.yaml`.

### 3. Change the app

In a second terminal, open `composer-demo/example` and run:

```sh
printf '%s\n' 'Updated by a Git commit' > apps/demo/nginx/configs/index.html
git add .
git commit -m 'feat: update welcome page'
```

Reload [localhost:18081](http://127.0.0.1:18081/) after the next poll to see the change.
Local mode reads commits from disk; remote repositories also need `git push`.
Run `git revert --no-edit HEAD` to roll back. Ctrl-C stops Composer and leaves the
app running; [delete the app through Git](docs/apps.md#remove-or-disable-an-app)
to remove its containers.

## Projects using Composer

- [composer-example](https://github.com/dmironenko/composer-example): source builds,
  encrypted secrets, HTTPS, and prod/stage routing.
- [dwspurpleteam/deployer](https://github.com/dwspurpleteam/deployer): Vareonix calls
  in production, with OpenVidu, nginx, Certbot, and shared storage.

## Guides

- [Run on Ubuntu with systemd](docs/systemd.md)
- [Add apps and configs](docs/apps.md)
- [Configure branches, environments, and Git authentication](docs/configuration.md)
- [Build from source](docs/builds.md)
- [Use encrypted secrets](docs/secrets.md)

Run `composer --help` for commands. To contribute, see
[Contributing](CONTRIBUTING.md) and [architecture](docs/architecture.md).
