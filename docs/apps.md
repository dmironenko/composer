# Apps

An app is a directory at `apps/<namespace>/<app>`. Its `vars/base.yaml` supplies
Compose settings and template variables. One app can contain several services;
Composer deploys that Compose project independently of other apps.

## Add an app

Create `apps/demo/web/vars/base.yaml`:

```yaml
compose:
  services:
    web:
      image: nginx:1.28-alpine
```

Commit the file to a watched branch. For a remote source, push it too.
`composer run` picks it up; use `composer apply --wait` for a one-time deployment.
To run the app outside prod, also commit `vars/stage.yaml` containing `{}` or the
settings to override.

The default template adds `restart: always`, connects services to the
namespace's environment-specific network, and rotates JSON logs at 10 MB with
three files. Put Compose settings under `compose`; they are validated by the
Compose SDK. Explicit service `networks`, `network_mode`, and `logging` replace
the corresponding defaults. Vars maps merge recursively, lists and scalars
replace previous values, and `null` removes a key.

For a one-off initialization service, set `restart: 'no'` to override the default.
Use Compose `depends_on` with `condition: service_completed_successfully` for
services that must wait for that job within the same app. Separate apps start
concurrently, so clients must retry unavailable dependencies.

Scale through `deploy.replicas`. Avoid `container_name` and fixed host ports on
services that need multiple replicas. Put published ports on your ingress.
Changing replica count preserves existing replicas.

An app directory can contain:

```text
apps/demo/web/
  vars/base.yaml            # required: shared settings, including production
  vars/stage.yaml           # optional: enable non-prod and override settings
  configs/config.yaml.tmpl  # optional: application config template
  Dockerfile.tmpl           # optional: build an image from source
```

## Add a config

Store application settings under `app` in `base.yaml`, above `compose`:

```yaml
app:
  listen: :8080
  log_level: info

compose:
  services:
    api:
      image: ghcr.io/your-org/api:v1.0.0
      command: /app/api --config /etc/app/config.yaml
      volumes:
        - ./configs/config.yaml:/etc/app/config.yaml:ro
```

Replace the image and command with your app's. Create `configs/config.yaml.tmpl`:

```gotemplate
{{ .Vars.app | toYAML }}
```

Composer renders it to `configs/config.yaml`. The command uses the path inside
the container, `/etc/app/config.yaml`. Files without `.tmpl` are copied unchanged;
templates can produce JSON, nginx configuration, or any other text format.
Rendered files are read-only and readable by non-root container users; executable
source files retain execute permission. Parent state directories stay private.
Mount configs read-only and put writable application data in named volumes.

For a stage-only setting, put this in `vars/stage.yaml`:

```yaml
app:
  log_level: debug
```

Config changes recreate the affected app's containers even if its image stays
the same. See [secrets](secrets.md) for `.Secrets` and [source builds](builds.md)
for `Dockerfile.tmpl`.

## Connect networks

The built-in template accepts these optional settings under `app`:

| Setting | Effect |
| --- | --- |
| `networks` | Additional namespace/environment networks for the app's services |
| `network_alias` | Alias suffixed with the environment, such as `api-prod` |
| `route_namespace` | Connections to one namespace in every configured environment |

For example, connect an app to production storage:

```yaml
app:
  networks:
    storage:
      namespace: storage
      environment: prod
```

These networks are created before deployments begin. `network_alias: api` adds
`api-<environment>` on the app's default network. A proxy with
`route_namespace: product` joins that namespace through `env_prod`, `env_stage`,
and any other configured environments. Routing rules stay in the proxy's config.
Explicit service `networks` or `network_mode` override these automatic attachments.

## Customize the shared template

Export the built-in template into your GitOps repository:

```sh
composer template export compose --output example/templates/compose.yaml.tmpl
```

Edit it to change defaults for all apps. This replaces the built-in template;
you maintain the copied version. App-level `compose` values override defaults
when merged by the template.

Templates receive `.Vars`, `.Secrets`, `.Installation`, `.Env`, `.Environments`,
`.Namespace`, `.App`, and `.Runtime` (`ProjectName`, `NetworkName`, `Image`).
Helpers: `toYAML`, `toJSON`, `quote`, `indent`, `get`, `default`, `dict`, `list`,
`merge`, and `network`. Template expressions inside vars are not evaluated again.
Compose's `${VAR}` interpolation is disabled; use Go templates for substitutions.

Each namespace/environment has a separate Docker network. The template helper
`network "prod" "payments"` returns a managed network name and schedules its
creation before app deployments. Use it in a custom template for network wiring
that differs from the built-in options above.
Other external networks must already exist. Composer does not order startup
across apps or automatically fall back to prod when a stage service is missing.

## Rename an app or namespace

The directory path determines the Compose project and network names. Renaming
an app or namespace removes the old projects and creates new ones; expect an
interruption. Update references such as `app.route_namespace` and `app.networks`.
Old named volumes remain, but new project names do not automatically reuse them.
Plan data migration before renaming an app with persistent storage. Fixed host
ports must be released by the old containers before replacements can start.

## Remove or disable an app

Delete its directory and commit, or set `enabled: false` at the top of `base.yaml`.
To disable it only outside prod, remove `stage.yaml` or set `enabled: false` there.
Composer removes the affected containers and retains volumes, images, and
namespace networks. To remove an app from every environment, delete it from
every mapped branch. Revert the commit to restore the app.
