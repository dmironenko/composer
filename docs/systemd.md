# Run on Ubuntu

Install Docker Engine and [Composer](../README.md#install). Composer deploys apps;
server users, Docker installation, firewall rules, and kernel settings remain
part of your host provisioning.

Keep the host config at `/etc/composer/composer.yaml`, outside the GitOps
repository. Configure its [Git source and environment branches](configuration.md),
and place the age key and Git credentials alongside it with mode `0600`.
Relative paths resolve from the host YAML. For a local rootful Docker daemon:

```yaml
docker:
  host: unix:///var/run/docker.sock
```

Check the host config and render the apps before starting the service:

```sh
sudo "$(command -v composer)" config check --config /etc/composer/composer.yaml
sudo "$(command -v composer)" validate --config /etc/composer/composer.yaml
```

Install and start the service in one command:

```sh
sudo "$(command -v composer)" install --config /etc/composer/composer.yaml
```

For `installation: demo`, this validates the configuration, copies the current
binary to `/usr/local/lib/composer/demo/composer`, and creates and enables
`composer-demo.service`. The unit uses the original host YAML by its absolute
path. The command returns when the agent starts; follow deployment progress with:

```sh
sudo systemctl status composer-demo
sudo journalctl -u composer-demo -f
```

The agent runs as root and uses root's Docker configuration and context. For a
local rootful Docker daemon, use the socket shown above.
Shell environment variables are not copied into the unit. Keys and credentials
must be accessible to the service.

A report with `action: noop` means the app already matches its Git configuration.
Check `commits` to confirm the branch revisions being deployed. Composer does
not repair containers changed or removed manually; inspect their health with
`docker ps` and their logs with `docker logs CONTAINER`.

## Update or stop

Install the new CLI version, then run `install` again:

```sh
go install github.com/dmironenko/composer/cmd/composer@latest
sudo "$(command -v composer)" install --config /etc/composer/composer.yaml
```

This replaces the binary and unit and restarts that installation's agent. It
refuses to overwrite an unrelated unit. Use `sudo systemctl edit composer-demo`
for service overrides.

```sh
sudo systemctl stop composer-demo
sudo systemctl start composer-demo
```

Stopping the agent leaves applications running. With the same `state_dir`, a
restart skips unchanged, successfully applied projects. Systemd restarts a
failed agent after five seconds and starts it automatically after a host reboot.

To disable the agent while keeping applications running:

```sh
sudo systemctl disable --now composer-demo
```

To remove the service entirely, also delete its unit and installed binary
directory, then run `sudo systemctl daemon-reload`. State and Docker resources
are retained.

## Test systemd changes

Run [test/systemd/check.sh](../test/systemd/check.sh) as root in a disposable Ubuntu
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
