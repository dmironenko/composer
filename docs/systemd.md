# Run on Ubuntu

Run Composer on the server that hosts your Docker containers. This guide uses
`installation: my-stack` and a GitOps repository whose production branch is
`master`. Use `main` instead if that is your branch name.

## 1. Push your deployment files

Create an empty repository on your Git provider. From the machine where you
created `my-gitops` in the [local walkthrough](../README.md#try-it-locally), add
its URL and push. If you completed the cleanup step, first restore the demo app
with `git -C my-gitops revert --no-edit HEAD`, or add your own apps.
Replace `OWNER` and the repository name:

```sh
git -C my-gitops remote add origin git@github.com:OWNER/gitops.git
git -C my-gitops push -u origin master
```

The walkthrough's nginx port binds to localhost; public access needs an ingress
with published ports and TLS. See
[composer-example](https://github.com/dmironenko/composer-example#deploy-on-a-server)
for nginx and Certbot.

## 2. Configure the server

On the Ubuntu server, install Docker Engine and [Composer](../README.md#install).
Docker installation, server users, firewall rules, and kernel settings belong to
host provisioning. Composer manages the applications once Docker is running.

Create a private directory and open the host config:

```sh
sudo install -d -m 700 /etc/composer
sudoedit /etc/composer/composer.yaml
```

Save this configuration with your repository URL:

```yaml
version: 1
installation: my-stack
source:
  mode: remote
  url: https://github.com/OWNER/gitops.git
  branches:
    prod: master
  poll_interval: 1m
state_dir: /var/lib/composer/my-stack
docker:
  host: unix:///var/run/docker.sock
```

This runs production only. Add more branch mappings when you want
[stage or personal environments](configuration.md#environments).

For a private HTTPS repository, save a read-only Git token in
`/etc/composer/github.token` and add `auth` inside `source`:

```yaml
  auth:
    type: https
    token_file: github.token
```

On GitHub, a fine-grained token needs access to this repository with read-only
`Contents` permission. See [Git authentication](configuration.md#remote-git-and-authentication)
for SSH. If your apps use encrypted secrets, also copy your age key to
`/etc/composer/age.txt` and set `age_key_file: age.txt` at the top level.

Keep the host config, token, and key outside Git with mode `0600`. Relative paths
resolve from `composer.yaml`. The systemd service runs as root; it uses root's
Docker configuration, and shell environment variables are not copied into it.

## 3. Start the agent

Check the host config and committed app definitions:

```sh
sudo "$(command -v composer)" config check --config /etc/composer/composer.yaml
sudo "$(command -v composer)" validate --config /etc/composer/composer.yaml
```

Install and start the service:

```sh
sudo "$(command -v composer)" install --config /etc/composer/composer.yaml
```

This copies the binary, creates `composer-my-stack.service`, and enables it at
boot. The service name comes from `installation`. It keeps reading the host config
at `/etc/composer/composer.yaml` and deploys the repository's current commit.

Follow the first deployment:

```sh
sudo systemctl status composer-my-stack
sudo journalctl -u composer-my-stack -f
```

Each report shows the Git `commits` used and an `action` for each app. `noop` means
the configuration matches the last successful deployment. Use `docker ps` to
check container health and `docker logs CONTAINER` for application logs.

## Deploy the next change

Edit app files on your development machine, commit, and push to the mapped
branch. The server picks up the change on its next poll. Revert and push a commit
to roll back. A default poll waits one minute after each pass.

Composer reloads most host settings each pass, including branch mappings and the
poll interval. Restart it after changing `installation`, `state_dir`, or `docker`.
See [host configuration](configuration.md) for details.

## Update or stop Composer

Install the desired release, then reinstall the agent:

```sh
curl -fsSL https://raw.githubusercontent.com/dmironenko/composer/master/install.sh | sudo sh -s -- v1.0.0 /usr/local/bin
sudo /usr/local/bin/composer install --config /etc/composer/composer.yaml
```

This replaces the binary at `/usr/local/lib/composer/my-stack/composer` and its
unit, then restarts the agent. It refuses to overwrite an unrelated unit.
Use `sudo systemctl edit composer-my-stack` for service overrides.

To pause and resume deployments:

```sh
sudo systemctl stop composer-my-stack
sudo systemctl start composer-my-stack
```

Stopping Composer leaves applications running. Stop the agent before using
`plan`, `validate`, or `apply` manually with the same state directory. A restart
uses that state to skip unchanged, successfully applied projects. Systemd restarts
a failed agent after five seconds and starts it after a host reboot.

To disable automatic deployments:

```sh
sudo systemctl disable --now composer-my-stack
```

To remove the service entirely, also delete its unit and installed binary
directory, then run `sudo systemctl daemon-reload`. State and Docker resources
are retained. Back up `state_dir` and protect it: it contains decrypted configs.
