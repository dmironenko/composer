# Composer

Composer deploys Docker Compose applications from Git. Run it on your Docker host,
commit your deployment files, and push. It checks the repository and applies
changed apps, including changes to their config files. Revert a commit to roll back.

Each app has its own Compose project. Updating an API leaves unrelated apps,
such as a database, running. You can map different Git branches to production
and staging on the same host. This workflow is called GitOps.

## Projects using Composer

- [composer-example](https://github.com/dmironenko/composer-example): a runnable
  demo with source builds, encrypted secrets, HTTPS, and routing between prod and stage.
- [dwspurpleteam/deployer](https://github.com/dwspurpleteam/deployer): the production
  deployment repository for Vareonix calls. It runs OpenVidu and Meet with nginx,
  Certbot, MongoDB, Redis, and MinIO.

In `deployer`, look at [apps/calls](https://github.com/dwspurpleteam/deployer/tree/main/apps/calls)
for the calling services, [apps/ingress](https://github.com/dwspurpleteam/deployer/tree/main/apps/ingress)
for HTTPS and environment routing, and [apps/storage](https://github.com/dwspurpleteam/deployer/tree/main/apps/storage)
for shared databases and object storage. The app files include config templates
and encrypted secrets. `metal/` contains Ansible host provisioning; Composer
manages the apps.

## Try it locally

You need Git and a running Docker Engine or Docker Desktop.
Use bash or zsh for the commands below, with `curl` available
and Git configured to create commits. The walkthrough works on macOS and Linux
and uses `127.0.0.1:18081`; choose another port if it is busy.

### Install

Install a specific release:

```sh
curl -fsSL https://raw.githubusercontent.com/dmironenko/composer/master/install.sh | sh -s -- v1.0.0
```

The script detects Linux or macOS and amd64 or arm64, verifies the release's
SHA256 checksum, and installs to `~/.local/bin` without sudo. Go and the Docker
Compose CLI are not required. Repeat with another release tag to update.

If `~/.local/bin` is not on your PATH, add this to your bash or zsh profile:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

For fish, use `fish_add_path ~/.local/bin`. Open a new terminal and run
`composer --version`. If an older installation takes precedence, check
`command -v composer` and remove the old binary or adjust your PATH.

For a system-wide installation, pass the directory explicitly:

```sh
curl -fsSL https://raw.githubusercontent.com/dmironenko/composer/master/install.sh | sudo sh -s -- v1.0.0 /usr/local/bin
```

You can [read the script](install.sh) first or download an archive from
[Releases](https://github.com/dmironenko/composer/releases).

### 1. Create a deployment repository

Run this in a new directory:

```sh
mkdir composer-demo
cd composer-demo
composer init my-gitops --branch master --installation my-stack --config composer.yaml
```

This creates two things:

| Path | What you put there |
| --- | --- |
| `composer.yaml` | Host settings: where to read Git, which branches to deploy, and where to store state |
| `my-gitops/apps/` | Deployment files that you commit: images, Compose settings, and app configs |

The host config stays outside Git. `my-stack` is the installation name used in
Docker resource names. Composer stores generated files and deployment state in
`my-gitops-state/`, also outside Git.

Open `composer.yaml` and change `source.poll_interval` from `1m` to `2s` for this
local walkthrough. The other generated settings can stay as they are.

### 2. Add an app

An app lives at `apps/<namespace>/<app>`. A namespace groups apps on a Docker
network. Here, `demo` is the namespace and `nginx` is the app:

```sh
mkdir -p my-gitops/apps/demo/nginx/vars my-gitops/apps/demo/nginx/configs
cat > my-gitops/apps/demo/nginx/vars/base.yaml <<'YAML'
compose:
  services:
    nginx:
      image: nginx:1.28-alpine
      ports:
        - 127.0.0.1:18081:80
      volumes:
        - ./configs/index.html:/usr/share/nginx/html/index.html:ro
YAML
printf '%s\n' 'Hello from Composer' > my-gitops/apps/demo/nginx/configs/index.html
```

`base.yaml` holds app settings. Under `compose`, use the same keys as in a
Compose file. This example mounts a page from `configs/` into nginx.
An app can contain more than one service; Composer deploys them together.

### 3. Deploy it

Composer reads committed files, so create the first commit before deploying:

```sh
git -C my-gitops init -b master
git -C my-gitops add .
git -C my-gitops commit -m 'feat: add nginx'
composer apply --config composer.yaml --wait
curl http://127.0.0.1:18081/
```

The response should be `Hello from Composer`. You can also open
[localhost:18081](http://127.0.0.1:18081/) in your browser.

`apply` deploys once. This app runs in `prod`: apps need a `vars/stage.yaml` file
to run in other environments. You can add staging after this walkthrough.

### 4. Deploy a change automatically

Start the agent and leave it running:

```sh
composer run --config composer.yaml
```

In a second terminal, open the same `composer-demo` directory and run:

```sh
printf '%s\n' 'Updated by a Git commit' > my-gitops/apps/demo/nginx/configs/index.html
git -C my-gitops add apps/demo/nginx/configs/index.html
git -C my-gitops commit -m 'feat: update welcome page'
```

Wait for the next poll and reload the page. It should say `Updated by a Git commit`.
Composer redeploys nginx because its config changed; the image stays the same.
Local mode reads commits directly from your disk. A remote repository also needs
`git push`. Saving a file without committing it does not deploy anything.

To roll back this change:

```sh
git -C my-gitops revert --no-edit HEAD
```

After the next poll, the page should say `Hello from Composer` again.
Ctrl-C in the agent terminal stops polling and leaves nginx running.

### 5. Remove the demo app

After stopping the agent, remove the app through Git:

```sh
git -C my-gitops rm -r apps/demo/nginx
git -C my-gitops commit -m 'chore: remove demo nginx'
composer apply --config composer.yaml --wait
```

Composer removes the app's containers. It retains volumes, images, and namespace
networks. Keep the state directory while managing a deployment: Composer uses
it to track what must be updated or removed.

## Use it for your project

[Run on Ubuntu](docs/systemd.md) shows how to connect your GitOps repository and
start Composer as a systemd service. Install Composer on each Docker host you
want to manage. GitHub Actions is optional; the agent fetches and deploys itself.

Continue with the guide for your next task:

- [Add an app or mount its configuration](docs/apps.md)
- [Test on a stage branch, then promote to prod](docs/configuration.md#environments)
- [Connect a private Git repository](docs/configuration.md#remote-git-and-authentication)
- [Build your app from source](docs/builds.md)
- [Encrypt and use a secret](docs/secrets.md)

Deployments can briefly interrupt the changed app. Composer tracks successful
Git deployments; it does not repair manual Docker changes. Check container health
with `docker ps` and logs with `docker logs CONTAINER`.

For CLI options, run `composer --help` or `composer COMMAND --help`.
For development, see [Contributing](CONTRIBUTING.md) and [architecture](docs/architecture.md).
