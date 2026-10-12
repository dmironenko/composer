# Host configuration

Keep `composer.yaml` outside the GitOps repository. It tells Composer where to
read Git, which branches to deploy, and where to store state. All relative file
paths resolve from this YAML file.

```yaml
version: 1
installation: my-stack
source:
  mode: local
  path: my-gitops
  branches:
    prod: master
    stage: dev
  poll_interval: 1m
state_dir: my-stack-state
```

Create both branches before starting. Local mode reads committed snapshots
without changing your working tree; no push is needed.

The default `poll_interval` is `1m`: Composer waits one minute after each pass.
For local development, set `2s` for faster feedback. Changing this value does
not require a restart. Unchanged apps are not restarted.

Check host settings with `composer config check --config composer.yaml`.
Use `composer validate` to also read Git and render the apps.

## Environments

`source.branches` is the complete list of environments:

| Environment | Branch in this example | App settings |
| --- | --- | --- |
| `prod` | `master` | `vars/base.yaml` |
| `stage` | `dev` | `vars/base.yaml` + `vars/stage.yaml` |
| `alice` | `users/alice` | `vars/base.yaml` + `vars/stage.yaml` |

Only the name `prod` is special. Branch names have no built-in meaning:
`prod: main` works the same way as `prod: master`. Each environment reads its whole
branch, including Dockerfiles and templates. Missing `stage.yaml` disables that
app in all non-prod environments; an empty file or `{}` enables it.

To add stage to the README example:

```sh
git -C my-gitops switch -c dev
printf '{}\n' > my-gitops/apps/demo/nginx/vars/stage.yaml
git -C my-gitops add .
git -C my-gitops commit -m 'feat: enable nginx on stage'
```

Set `stage: dev` in the host YAML. Make changes to `base.yaml` on `dev`, check
stage, then promote them:

```sh
git -C my-gitops switch master
git -C my-gitops merge dev
```

Keep shared settings in `base.yaml`; reserve `stage.yaml` for differences such as
test credentials. Its overrides remain active after a merge into production.
There is no `prod.yaml` in the current format.

For a personal environment, create its branch and add `alice: users/alice` under
`source.branches`. Names must start with a lowercase letter, contain only
lowercase letters, digits, or hyphens, and be at most 63 characters long.
Each environment gets separate projects and namespace networks. Routing is
configured in GitOps; see the [example](https://github.com/dmironenko/composer-example#personal-environments).

`run` reloads host YAML each pass. Removing an environment removes its containers,
including after an agent restart, but keeps its volumes and images. Missing
branches and fetch/render errors preserve the affected environment. An invalid
host config keeps the last accepted settings. Restart the agent after changing
`installation`, `state_dir`, or `docker`.

## Remote Git and authentication

Replace the local `source` block with:

```yaml
source:
  mode: remote
  url: https://github.com/OWNER/gitops.git
  branches:
    prod: master
    stage: dev
  poll_interval: 1m
  auth:
    type: https
    token_file: github.token
```

`github.token` contains the raw token. Keep it outside Git and readable only by
the agent user. Omit `auth` for an anonymous public repository. Remote mode uses
shallow Git fetches; commits must be pushed before Composer can see them.
A GitHub fine-grained token needs access to this repository and read-only
**Contents** permission; Composer does not need write or workflow access.

For SSH, use an SSH URL and replace `auth`:

```yaml
auth:
  type: ssh
  key_file: deploy-key
  known_hosts_file: known_hosts
```

Use an unencrypted private key and a `known_hosts` file with verified server keys.
Authentication settings apply to the GitOps repository only; they are not passed
to app builds.

## Optional settings

These are the defaults; omit them unless you need to change them:

```yaml
docker: {}
reconcile:
  parallelism: 4
  git_timeout: 30s
  deploy_timeout: 5m
```

`docker: {}` uses the Docker CLI's connection settings. Set either `docker.context`
or `docker.host` to select a daemon explicitly. `parallelism` limits concurrent
app operations. Increase `deploy_timeout` for slow builds, and use
`composer apply --wait --timeout 30m` to increase the overall apply deadline.
`apply --wait` retries a fixed snapshot; `run` fetches new snapshots on each pass.

Set `age_key_file: composer.agekey` to use [encrypted secrets](secrets.md).
Use a unique `installation` and `state_dir` for each stack, and separate any
published host ports. Keep state outside Git and back it up: it tracks deletions
and contains decrypted configs, even after `validate` or `plan`.
Only one agent can use a state directory at a time.

GitOps contents have Docker-level privileges. Use repositories you trust.
