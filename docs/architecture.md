# Architecture

This guide is for contributors. For host settings and everyday commands, see
[configuration](configuration.md) and the [README](../README.md).

Composer reads Git snapshots, renders templates, prepares networks, and applies
changed projects through the Compose SDK. Application types and routing belong
to the GitOps repository.

## Package boundaries

```text
cmd/composer/                 composition root, host YAML, bootstrap, lifecycle
internal/app/composer/
  domain/                     Project, Manifest, Change, Snapshot, Report, BuildSpec
  service/
    reconcile/                snapshot → render → plan → apply, polling/retry
    deployment/               revision comparison, removal, networks, workers
    build/                    image lookup and build, concurrent build deduplication
  ports/cli/                  Cobra commands, flags, JSON reports
  adapters/
    git/                      GitOps snapshots, SSH/HTTPS authentication
    gitops/                   vars, secrets, templates, Compose validation
      internal/vars/          YAML merge and age encryption
    docker/                   Compose lifecycle, Engine, embedded BuildKit
    state/                    deployment inventory and markers
    systemd/                  binary/unit installation, systemctl
```

| Layer | Allowed dependencies | Excluded dependencies |
|---|---|---|
| `domain` | Pure standard-library code | Services, ports, adapters, SDKs, YAML, I/O |
| `service` | Domain, other use cases, context/time, errgroup/singleflight | Ports, adapters, SDKs, Cobra, host config, filesystem |
| `ports/cli` | Domain and service contracts, Cobra, presentation DTOs | Concrete adapters, host YAML loader, Docker/Git SDKs |
| `adapters/<dependency>` | Domain and service contracts, its SDK and own subpackages | Sibling adapters, inbound ports |
| `cmd/composer` | All layers | Deployment and revision-comparison policy |

Services declare outgoing interfaces in `service/*/ports.go`. Adapters implement
those interfaces; `cmd/composer` constructs and connects them. The composition
root owns host YAML, the process lock, and Docker client cleanup. Services own
polling, retries, and operation order; `adapters/state` performs state-file I/O.

Compose SDK models stay inside the GitOps and Docker adapters. They exchange
`domain.Manifest`, which carries the rendered document, working directory, and
config hash. Domain and service code do not parse Compose fields. The CLI maps
`domain.Report` to JSON without manifests or secrets.

`make go/architecture` checks these boundaries; `make go/lint` includes that
check. See [CONTRIBUTING.md](../CONTRIBUTING.md) for checks and mock generation.

## Reconciliation

Each `apps/<namespace>/<app>` directory becomes an independent Compose project.
A reconciliation pass:

1. Reads a committed snapshot of each configured branch through go-git.
2. Decrypts vars, merges overrides, and renders configs, Dockerfiles, and Compose.
3. Compares the project hash with its last successful deployment.
4. Creates namespace/environment networks and networks requested by templates.
5. Builds missing images and calls Compose Up or Down within `parallelism`.
6. Records each successful deployment's hash.

All managed networks are prepared before parallel project operations. A network
failure blocks only projects that use it. Other external networks must already
exist. There is no startup order between projects; Compose manages service
readiness and scaling within each project.

A source or render failure preserves that environment and allows others to
proceed. `run` fetches fresh snapshots each pass. `apply --wait` retains the first
successfully read snapshot of each environment and retries until success or its
overall timeout. A process lock prevents concurrent agents sharing `state_dir`.

Each project operation, including its build, is bounded by `deploy_timeout`.
A Compose readiness attempt lasts at most `max(1s, poll_interval)`; retries
release workers so other projects can proceed even with `parallelism: 1`.

## Deployment state

Builds finish before changing containers or invalidating the previous success
marker. Before Up or Down, Composer removes the `applied` marker so a partially
failed operation cannot hide a later Git revert. An `attempted` marker records
possible resource creation even if Up fails. Inventory survives agent restarts,
including pending removals. Removal retains volumes, images, and namespace
networks.

Unchanged, successfully applied projects are skipped. Composer does not repair
manual Docker changes. To force another deployment, stop the agent and remove
`state_dir/deployments/<env>/<project>/applied`. Losing the state directory also
loses the inventory of projects already removed from Git. Reports use JSON
lines; errors go to stderr.

## Rendered configs

Template inputs and helpers are documented in the [app guide](apps.md).

Rendered configs live in directories named by content and executable-bit hash.
Changing a config changes its bind path and `io.composer.config` label, causing
container recreation even with the same image. Changing only `deploy.replicas`
keeps those values and preserves existing replicas.

[Secrets](secrets.md) are decrypted before merging, so encrypting unchanged
values again does not change the model. [Builds](builds.md) reuse Docker's cache;
config variables affect the image only when they change the rendered Dockerfile.
