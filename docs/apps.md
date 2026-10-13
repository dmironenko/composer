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
installation network, and rotates JSON logs at 10 MB with
three files. Put Compose settings under `compose`; they are validated by the
Compose SDK. Explicit service `networks`, `network_mode`, and `logging` replace
the corresponding defaults. Vars maps merge recursively, lists and scalars
replace previous values, and `null` removes a key.

Use only `build`, `app`, and `compose` at the top level, in that order. Omit
unused blocks. `build` describes source builds; `app` holds service settings
and encrypted secrets; `compose` holds Docker settings and Composer options
under `x-composer`.

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

Store application settings under `app` in `base.yaml`, above `compose`.
Match the service's config format so templates can serialize it directly without
renaming keys or assembling nested objects. Avoid an extra `app.config` block
unless the app also needs another format, such as `app.env`.
These values go into application configs; the built-in Compose template does not use them.
Docker settings belong in `compose`:

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
the same. See [secrets](secrets.md) for encrypted values in `.Vars.app` and
[source builds](builds.md) for `Dockerfile.tmpl`.

## Environment variables

Keep environment variables under `app.env`, with the names expected by the
service. Composer passes them to a container through a rendered file:

```yaml
app:
  env:
    LOG_LEVEL: info
    API_TOKEN: ENC[...]

compose:
  services:
    api:
      image: ghcr.io/your-org/api:v1.0.0
      env_file: ./configs/app.env
```

Create `configs/app.env.tmpl`:

```gotemplate
{{- range $name, $value := .Vars.app.env }}
{{ $name }}={{ quoteEnv $value }}
{{- end }}
```

Use string values; quote YAML numbers and booleans when needed. `quoteEnv`
quotes each value, preserves URL characters such as `&`, and escapes `$` to
prevent Compose from expanding it. `app.env` is template data; the `env_file`
reference connects it to the container. For a service with both a config file
and environment variables, use `app.config`
and `app.env`, and serialize `.Vars.app.config` in the config template.

## Connect services

All apps in an installation share one Docker network. The ingress can reach
services in every namespace and environment without network settings in vars.
Keep service names unique across namespaces. Use standard Docker Compose
`services.<service>.networks` only when a service needs network-specific options;
an explicit `networks` or `network_mode` replaces the default attachment.

Service names are automatic: `meet` in prod, `meet-stage` in stage, and
`meet-dan` in an environment named `dan`. The suffix comes from the environment
key in `source.branches`, regardless of the Git branch name. No app setting is
needed; an empty `stage.yaml` enables the same service outside prod.

Keep `compose.services.meet` in vars for every environment. Composer names the
service after merging vars and updates Compose references such as `depends_on`
and `network_mode: service:meet`. This also changes Docker's automatic DNS name,
so stage does not register `meet` on networks shared with prod.

Addresses inside application configs and explicit network aliases remain under
your control. In config templates, use `{{ .Env }}` to address the current stage,
for example `meet{{ if ne .Env "prod" }}-{{ .Env }}{{ end }}:6080`.

`compose.x-composer` holds only the `enabled` flag. Composer omits it from the
generated Compose file. Keys under `app` are only template data.

## Customize the shared template

Export the built-in template into your GitOps repository:

```sh
composer template export compose --output example/templates/compose.yaml.tmpl
```

Edit it to change defaults for all apps. This replaces the built-in template;
you maintain the copied version. App-level `compose` values override defaults
when merged by the template.

Templates receive `.Vars`, `.Installation`, `.Env`, `.Environments`,
`.Namespace`, `.App`, and `.Runtime` (`ProjectName`, `NetworkName`, `Image`).
Helpers: `toYAML`, `toJSON`, `quote`, `quoteEnv`, `indent`, `get`, `default`, `dict`, `list`,
`merge`, and `network`. Template expressions inside vars are not evaluated again.
Compose's `${VAR}` interpolation is disabled; use Go templates for substitutions.

The template helper `network "prod" "payments"` returns a managed network name
and schedules its creation before app deployments. Use it only when a custom
template needs an additional namespace/environment network.
Other external networks must already exist. Composer does not order startup
across apps or automatically fall back to prod when a stage service is missing.

## Rename an app or namespace

The directory path determines the Compose project name. Renaming an app or
namespace removes the old projects and creates new ones; expect an interruption.
Old named volumes remain, but new project names do not automatically reuse them.
Plan data migration before renaming an app with persistent storage. Fixed host
ports must be released by the old containers before replacements can start.

## Remove or disable an app

Delete its directory and commit, or set this in `base.yaml`:

```yaml
compose:
  x-composer:
    enabled: false
```

To disable it only outside prod, remove `stage.yaml` or put the same override there.
Composer removes the affected containers and retains volumes, images, and
the installation network. To remove an app from every environment, delete it from
every mapped branch. Revert the commit to restore the app.

The old top-level `enabled` key remains readable for rollbacks.
`compose.x-composer.enabled` takes precedence when both are present; use it
in new configs.
