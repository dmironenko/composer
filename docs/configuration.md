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
state_dir: my-gitops-state
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

To add stage to the [README walkthrough](../README.md#try-it-locally), enable the
app on a new branch and give it a different host port. If you already removed
the demo app, restore it first with `git -C my-gitops revert --no-edit HEAD`.

```sh
git -C my-gitops switch -c dev
cat > my-gitops/apps/demo/nginx/vars/stage.yaml <<'YAML'
compose:
  services:
    nginx:
      ports:
        - 127.0.0.1:18082:80
YAML
git -C my-gitops add .
git -C my-gitops commit -m 'feat: enable nginx on stage'
```

The generated host YAML initially maps both environments to `master`. Change
`stage: master` to `stage: dev` in `composer.yaml`, then start
`composer run --config composer.yaml` if it is not running. Prod remains at
[localhost:18081](http://127.0.0.1:18081/); stage is at
[localhost:18082](http://127.0.0.1:18082/). Both ports must be free before deployment.

In another terminal, edit an app file on `dev` and commit it. For example, change
`apps/demo/nginx/configs/index.html`. Check the result on stage, then promote it:

```sh
git -C my-gitops switch master
git -C my-gitops merge dev
```

Keep shared settings in `base.yaml`; reserve `stage.yaml` for differences such as
test credentials. After the merge, prod still reads only `base.yaml`; non-prod
environments keep applying their `stage.yaml` overrides. There is no `prod.yaml`.

For a personal environment, create its branch and add `alice: users/alice` under
`source.branches`. Names must start with a lowercase letter, contain only
lowercase letters, digits, or hyphens, and be at most 63 characters long.
Each environment gets separate projects and namespace networks. If you use fixed
host ports as in the walkthrough, give each environment a free port in its branch.
For ingress routing without per-app host ports, see the
[example](https://github.com/dmironenko/composer-example#personal-environments).

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
`Contents` permission; Composer does not need write or workflow access.

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

## Check a deployment

Commands read `composer.yaml` by default. Add `--config PATH` to use another file.

| Command | What it does |
| --- | --- |
| `composer config check` | Check host YAML without contacting Git or Docker |
| `composer validate` | Fetch Git and validate committed app definitions without Docker |
| `composer plan` | Compare them with the last successful deployment without Docker |
| `composer apply --wait` | Deploy once and retry until ready or timeout |
| `composer run` | Poll Git and deploy changes continuously |

Stop a running agent before using `validate`, `plan`, or `apply` with the same
state directory. Start it again when finished. Stopping Composer leaves the
application containers running.

Reports contain the Git `commits` used for each environment and an `action` for
each app. `apply` means a deployment was needed; `noop` means its configuration
matches the last successful deployment. A `failed` action includes an `error`.
Check the exit status of one-shot commands as well as the report.

If an edit did not deploy, check that it was committed to the mapped branch
(and pushed in remote mode). A non-prod app also needs `vars/stage.yaml`.
Use `docker ps` and `docker logs CONTAINER` to check the running application.
