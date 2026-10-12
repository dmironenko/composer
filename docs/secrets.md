# Secrets

Keep secret names in YAML and encrypt each value with age. Composer decrypts
values before merging `base.yaml` and `stage.yaml`, then exposes them as
`.Secrets` in templates.

## Create a key once

Install the [age tools](https://github.com/FiloSottile/age#installation), then
create a key outside the GitOps repository:

```sh
mkdir -p "$HOME/.config/composer"
chmod 700 "$HOME/.config/composer"
age-keygen -o "$HOME/.config/composer/age.txt"
chmod 600 "$HOME/.config/composer/age.txt"
```

Copy the key to the Composer host with mode `0600`, readable by the user running
Composer. Set its path once in the host YAML:

```yaml
age_key_file: /etc/composer/age.txt
```

One native X25519 age identity can encrypt secrets for all applications. Relative
paths resolve from the host YAML. Applications need no recipients or metadata,
and unencrypted vars need no key.

## Add a secret

Pass the value on stdin. For a demonstration token:

```sh
printf '%s' 'some-token' | composer secrets encrypt --key-file "$HOME/.config/composer/age.txt"
```

Copy the returned `ENC[...]` string into the application's `vars/base.yaml`.
Encrypt each password or token separately and keep `secrets` at the end:

```yaml
app:
  database:
    host: db

compose:
  services:
    api:
      image: your-app:version

secrets:
  stage_token: ENC[...]
  database:
    password: ENC[...]
```

For real secrets, redirect a protected file with `< /path/to/secret` to avoid
putting the value in shell history. Encryption preserves the exact input,
including spaces and newlines; `printf '%s'` adds no trailing newline.

Templates can read `.Secrets.stage_token` and `.Secrets.database.password`.
To combine application settings and secrets in a YAML config:

```gotemplate
{{ merge .Vars.app .Secrets | toYAML }}
```

Commit and push to deploy. A change to a secret used by the project recreates its
containers with the existing image unless it also changes the rendered
Dockerfile. Encrypting the same value again does not trigger a deployment.
Names and nesting stay visible; decrypted values are always strings, including
`123`, `true`, and `null`. Dollar signs stay literal.

## Read or replace a secret

Run the decrypt command, paste only the `ENC[...]` value, then press Enter and
Ctrl-D to finish stdin:

```sh
composer secrets decrypt --key-file "$HOME/.config/composer/age.txt"
```

To replace a secret, encrypt the new value and replace its YAML entry. An entry
in `stage.yaml` overrides the matching base secret; other secrets are inherited.
A wrong key or damaged ciphertext fails without exposing secret values or
removing the current project.

## Rendered files

Rendered configs contain plaintext secrets in `state_dir`, including after
`validate` and `plan`. Directories use mode `0700`; files use `0444`, or `0555`
when executable. Mount configs read-only with `:ro` and protect state backups.
Old config versions remain until project removal. Docker users can also inspect
secrets passed through container environment variables.

Legacy `age:...` values, including encrypted whole YAML blocks, remain readable
for old commits. SOPS files are not supported.
